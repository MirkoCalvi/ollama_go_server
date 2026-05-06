// Command httpserver is the entrypoint. It wires together the in-memory
// job store, the Ollama client, the worker pool, and the HTTP handlers,
// then runs an HTTP server with graceful shutdown.
//
// Configuration is read from environment variables with sensible defaults so
// the binary works out of the box against a local Ollama instance.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/MirkoCalvi/httpserver/internal/handlers"
	"github.com/MirkoCalvi/httpserver/internal/logger"
	"github.com/MirkoCalvi/httpserver/internal/models"
	"github.com/MirkoCalvi/httpserver/internal/ollama"
	"github.com/MirkoCalvi/httpserver/internal/worker"
)

// Tunables. Workers and queue size are intentionally constants — the
// requirement is "max 10 workers", and the queue size scales the burst the
// server can absorb before rejecting with 503.
const (
	defaultServerAddr  = ":8080"
	defaultOllamaURL   = "http://localhost:11434"
	defaultOllamaModel = "phi3"

	workerCount       = 10
	queueSize         = 100
	defaultJobTimeout = 60 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func main() {
	log := logger.New()

	cfg := loadConfig(log)

	store := models.NewJobStore()
	client := ollama.NewClient(cfg.ollamaURL, cfg.ollamaModel)

	pool := worker.NewPool(workerCount, queueSize, store, client, cfg.jobTimeout, log)
	pool.Start()

	h := handlers.New(store, pool, log)
	mux := http.NewServeMux()
	// Method enforcement happens inside each handler so we can return 405
	// with a useful Allow header instead of mux returning 404 for the wrong
	// verb.
	mux.HandleFunc("/generate", h.Generate)
	mux.HandleFunc("/jobs/", h.GetJob)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              cfg.serverAddr,
		Handler:           loggingMiddleware(log, mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Listen for SIGINT/SIGTERM to drive graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		log.Info("http server starting",
			"addr", cfg.serverAddr,
			"workers", workerCount,
			"queue_size", queueSize,
			"ollama_url", cfg.ollamaURL,
			"ollama_model", cfg.ollamaModel,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			log.Error("http server failed", "error", err.Error())
		}
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// 1. Stop accepting new HTTP requests (graceful: lets in-flight requests finish).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", "error", err.Error())
	}

	// 2. Stop the worker pool. Workers drain whatever is still queued.
	pool.Stop()
	log.Info("server stopped")
}

type config struct {
	serverAddr  string
	ollamaURL   string
	ollamaModel string
	jobTimeout  time.Duration
}

func loadConfig(log *logger.Logger) config {
	cfg := config{
		serverAddr:  envOr("SERVER_ADDR", defaultServerAddr),
		ollamaURL:   envOr("OLLAMA_URL", defaultOllamaURL),
		ollamaModel: envOr("OLLAMA_MODEL", defaultOllamaModel),
		jobTimeout:  defaultJobTimeout,
	}
	if v := os.Getenv("JOB_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.jobTimeout = time.Duration(n) * time.Second
		} else {
			log.Warn("invalid JOB_TIMEOUT_SECONDS, using default",
				"value", v,
				"default_seconds", int(defaultJobTimeout/time.Second),
			)
		}
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// statusRecorder lets the logging middleware capture the response status
// without altering handler behaviour.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(log *logger.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
