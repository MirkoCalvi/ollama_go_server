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
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"

	"github.com/MirkoCalvi/httpserver/internal/auth"
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
	defaultJobTimeout = 180 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func main() {
	log := logger.New()

	cfg := loadConfig(log)

	// Pick an auth strategy:
	//   DEV_MODE=1                  → DevMiddleware (loopback only)
	//   FIREBASE_CREDENTIALS_FILE=… → verify Firebase ID tokens
	// Exactly one must be configured; the server fails closed otherwise.
	authCtx, authCancel := context.WithTimeout(context.Background(), 30*time.Second)
	authMiddleware, err := buildAuth(authCtx, log, &cfg)
	authCancel()
	if err != nil {
		log.Error("auth setup failed", "error", err.Error())
		os.Exit(1)
	}

	store := models.NewJobStore()
	client := ollama.NewClient(cfg.ollamaURL, cfg.ollamaModel)

	pool := worker.NewPool(workerCount, queueSize, store, client, cfg.jobTimeout, log)
	pool.Start()

	h := handlers.New(store, pool, log)
	mux := http.NewServeMux()
	// Method enforcement happens inside each handler so we can return 405
	// with a useful Allow header instead of mux returning 404 for the wrong
	// verb. Auth wraps each protected route; /healthz is intentionally
	// unauthenticated so liveness probes don't need a key.
	mux.Handle("/generate", authMiddleware(http.HandlerFunc(h.Generate)))
	mux.Handle("/jobs/", authMiddleware(http.HandlerFunc(h.GetJob)))
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

// buildAuth selects the middleware to wrap protected routes with.
//
// Exactly one of DEV_MODE=1 or FIREBASE_CREDENTIALS_FILE=... must be set;
// the server fails closed if neither or both are set.
func buildAuth(ctx context.Context, log *logger.Logger, cfg *config) (func(http.Handler) http.Handler, error) {
	devMode := os.Getenv("DEV_MODE") == "1"
	credPath := os.Getenv("FIREBASE_CREDENTIALS_FILE")

	switch {
	case devMode && credPath != "":
		return nil, errors.New("set either DEV_MODE=1 or FIREBASE_CREDENTIALS_FILE, not both")
	case !devMode && credPath == "":
		return nil, errors.New("no auth configured: set FIREBASE_CREDENTIALS_FILE or DEV_MODE=1")
	}

	if devMode {
		safeAddr, err := loopbackOnly(cfg.serverAddr)
		if err != nil {
			return nil, err
		}
		if safeAddr != cfg.serverAddr {
			log.Warn("DEV_MODE rewriting SERVER_ADDR to loopback",
				"original", cfg.serverAddr, "rewritten", safeAddr)
			cfg.serverAddr = safeAddr
		}
		log.Warn("DEV_MODE enabled — auth bypassed; every request runs as user",
			"user_id", auth.DevUserID, "addr", cfg.serverAddr)
		return auth.DevMiddleware, nil
	}

	// firebase.NewApp picks up the project ID from the credentials file.
	// We don't need to set firebase.Config.ProjectID explicitly.
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(credPath))
	if err != nil {
		return nil, fmt.Errorf("init firebase app from %q: %w", credPath, err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("init firebase auth client: %w", err)
	}
	log.Info("auth configured", "source", "firebase", "credentials_file", credPath)
	return auth.NewFirebase(client).Middleware, nil
}

// loopbackOnly enforces that DEV_MODE only binds a loopback interface.
// An unset host (":8080") binds 0.0.0.0 by default — we rewrite that to
// 127.0.0.1 so the rule is enforced even when the user takes the default.
func loopbackOnly(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid SERVER_ADDR %q: %w", addr, err)
	}
	switch host {
	case "":
		return net.JoinHostPort("127.0.0.1", port), nil
	case "localhost", "127.0.0.1", "::1":
		return addr, nil
	default:
		return "", fmt.Errorf("DEV_MODE refuses non-loopback host %q (use 127.0.0.1, localhost, or :PORT)", host)
	}
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
