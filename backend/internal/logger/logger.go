// Package logger provides a tiny JSON-line structured logger.
//
// It exists because the project targets Go 1.18, which predates the standard
// library's log/slog (added in 1.21). The API is intentionally slog-shaped —
// Info/Warn/Error take a message and variadic key-value pairs — so swapping
// it out for slog later is a near-mechanical rewrite.
//
// Each log line is a single JSON object terminated by '\n', which is what
// most log aggregators (Loki, Cloudwatch, Datadog) expect.
package logger

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// Logger is concurrency-safe: the underlying log.Logger serializes writes to
// stdout, and we marshal each record independently before emitting it.
type Logger struct {
	mu  sync.Mutex
	out *log.Logger
}

// New returns a Logger writing JSON lines to stdout.
func New() *Logger {
	return &Logger{out: log.New(os.Stdout, "", 0)}
}

// Info, Warn, Error emit a record at the named level.
//
// Key-value pairs are passed flat: logger.Info("queued", "job_id", id, "user", uid).
// Odd or non-string keys are dropped silently — log calls should never panic.
func (l *Logger) Info(msg string, kv ...interface{})  { l.emit("INFO", msg, kv) }
func (l *Logger) Warn(msg string, kv ...interface{})  { l.emit("WARN", msg, kv) }
func (l *Logger) Error(msg string, kv ...interface{}) { l.emit("ERROR", msg, kv) }

func (l *Logger) emit(level, msg string, kv []interface{}) {
	rec := make(map[string]interface{}, 3+len(kv)/2)
	rec["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	rec["level"] = level
	rec["msg"] = msg
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		rec[k] = kv[i+1]
	}

	b, err := json.Marshal(rec)
	if err != nil {
		// Should be unreachable for the simple types we log, but never let
		// logging failures crash the server.
		b = []byte(`{"level":"ERROR","msg":"logger marshal failed"}`)
	}

	l.mu.Lock()
	l.out.Println(string(b))
	l.mu.Unlock()
}
