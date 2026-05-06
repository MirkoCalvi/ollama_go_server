# httpserver

A small, production-shaped Go HTTP server that fronts a local [Ollama](https://ollama.com) instance with an asynchronous job queue.

Clients submit a prompt over HTTP, receive a `job_id` immediately, and poll a separate endpoint to fetch the result once a worker has finished calling Ollama.

---

## Features

- `POST /generate` — accepts `{user_id, prompt}`, enqueues a job, returns `{job_id, status}` immediately.
- `GET /jobs/{id}` — returns the current state of a job (`queued`, `processing`, `done`, or `failed`) and the response if available.
- `GET /healthz` — liveness probe.
- Bounded **worker pool of 10 goroutines** consuming from a buffered channel queue (size 100).
- Per-job `context.WithTimeout` for the Ollama call (default 60s, configurable).
- Thread-safe in-memory job store (`sync.RWMutex` + map).
- Structured JSON logging to stdout.
- Graceful shutdown: stops accepting new HTTP requests, then drains queued jobs.
- Validates input, caps request bodies at 1 MiB, returns 503 when the queue is saturated.
- Standard Go layout (`cmd/` + `internal/`).

---

## Project layout

```
.
├── cmd/
│   └── httpserver/      # main package — entrypoint, wiring, graceful shutdown
├── internal/
│   ├── handlers/        # HTTP handlers (POST /generate, GET /jobs/{id})
│   ├── worker/          # worker pool: buffered channel + N goroutines
│   ├── ollama/          # thin client for http://localhost:11434/api/generate
│   ├── models/          # Job, JobResult, JobStatus, thread-safe JobStore
│   └── logger/          # tiny JSON-line structured logger (Go 1.18-compatible)
├── go.mod               # module github.com/MirkoCalvi/httpserver, go 1.18
└── go.sum
```

Each package owns one concern. The dependency graph is one-way:

```
cmd/httpserver
   └─> internal/handlers ──> internal/worker ──> internal/ollama
                       └──> internal/models
                       └──> internal/logger
```

Nothing depends on `handlers`, so swapping HTTP for, say, gRPC later is a localized change.

---

## Requirements

- **Go 1.18** or newer.
- **Ollama** running locally on port 11434 with the `phi3` model (or any model you set via `OLLAMA_MODEL`).

Pull the model before running the server:

```bash
ollama pull phi3
```

---

## Run

```bash
# from the repo root
go run ./cmd/httpserver
```

Or build and run:

```bash
go build -o bin/httpserver ./cmd/httpserver
./bin/httpserver
```

You should see startup logs like:

```json
{"addr":":8080","level":"INFO","msg":"http server starting","ollama_model":"phi3","ollama_url":"http://localhost:11434","queue_size":100,"workers":10,"time":"..."}
```

---

## Configuration

All knobs are environment variables with sensible defaults. Worker count and queue size are intentionally **compile-time constants** (the spec is "max 10 workers"); change `cmd/httpserver/main.go` if you need different values.

| Variable              | Default                  | Meaning                             |
| --------------------- | ------------------------ | ----------------------------------- |
| `SERVER_ADDR`         | `:8080`                  | HTTP listen address                 |
| `OLLAMA_URL`          | `http://localhost:11434` | Base URL of the Ollama server       |
| `OLLAMA_MODEL`        | `phi3`                   | Model to invoke                     |
| `JOB_TIMEOUT_SECONDS` | `60`                     | Per-job timeout for the Ollama call |

Example:

```bash
SERVER_ADDR=:9000 OLLAMA_MODEL=llama3.2 JOB_TIMEOUT_SECONDS=120 go run ./cmd/httpserver
```

---

## API

### `POST /generate`

**Request**

```json
{
  "user_id": "alice",
  "prompt": "Explain channels in Go in two sentences."
}
```

**Response — 202 Accepted**

```json
{
  "job_id": "9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11",
  "status": "queued"
}
```

**Errors**

| Status | When                                                      |
| ------ | --------------------------------------------------------- |
| 400    | Body is not valid JSON, or `user_id` / `prompt` is empty. |
| 405    | Wrong method (`Allow: POST`).                             |
| 413    | Body exceeds 1 MiB.                                       |
| 503    | Worker queue is full — retry later.                       |

### `GET /jobs/{id}`

**Response — 200 OK** (job still running)

```json
{ "job_id": "9f0c...", "status": "processing" }
```

**Response — 200 OK** (job finished)

```json
{
  "job_id": "9f0c...",
  "status": "done",
  "response": "Channels are typed conduits..."
}
```

**Response — 200 OK** (job failed, e.g. Ollama unreachable)

```json
{
  "job_id": "9f0c...",
  "status": "failed",
  "error": "call ollama: dial tcp 127.0.0.1:11434: connect: connection refused"
}
```

**Errors**

| Status | When                         |
| ------ | ---------------------------- |
| 404    | No job with that ID.         |
| 405    | Wrong method (`Allow: GET`). |

### `GET /healthz`

Returns `200 OK` with body `ok`.

---

## Example session

```bash
# 1. Submit a prompt
$ curl -s -X POST http://localhost:8080/generate \
       -H 'Content-Type: application/json' \
       -d '{"user_id":"alice","prompt":"What is a goroutine?"}'
{"job_id":"9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11","status":"queued"}

# 2. Poll for the result
$ curl -s http://localhost:8080/jobs/9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11
{"job_id":"9f0c...","status":"processing"}

# (wait a bit)
$ curl -s http://localhost:8080/jobs/9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11
{"job_id":"9f0c...","status":"done","response":"A goroutine is a lightweight thread..."}

# Health check
$ curl -s http://localhost:8080/healthz
ok
```

To stress-test concurrency, fire 25 prompts at once — the first 10 hit workers immediately, the next 15 sit in the queue, and anything past `queueSize=100` outstanding gets a clean 503:

```bash
for i in $(seq 1 25); do
  curl -s -X POST http://localhost:8080/generate \
       -H 'Content-Type: application/json' \
       -d "{\"user_id\":\"u$i\",\"prompt\":\"say hi $i\"}" &
done
wait
```

---

## Concurrency model

```
HTTP handler              Buffered channel (cap 100)            Worker pool (10 goroutines)
─────────────             ─────────────────────────             ────────────────────────────
POST /generate ──> Submit ──> [ job, job, job, ... ] ──> recv ──> worker calls Ollama
                  (non-blocking; 503 if full)                     ──> store.SetResult / SetError
```

- **Submit is non-blocking.** A full queue surfaces as HTTP 503, so request-handling goroutines never block on pool capacity.
- **Each job has its own context.** Per-job `context.WithTimeout` rooted at `context.Background()` — graceful shutdown does **not** cancel in-flight calls, it just stops accepting new HTTP requests and waits for the queue to drain.
- **Store access is RWMutex-guarded.** Reads (`GET /jobs/{id}`) take an RLock; writes (`Create`, `UpdateStatus`, `SetResult`, `SetError`) take a Lock.

---

## Graceful shutdown

On `SIGINT` / `SIGTERM`:

1. `http.Server.Shutdown` stops accepting new connections and lets in-flight HTTP requests finish (capped by `shutdownTimeout = 15s`).
2. `pool.Stop()` closes the jobs channel and `wg.Wait`s for the 10 workers to drain remaining queued jobs.
3. Process exits.

In-flight Ollama calls are not cancelled by shutdown — they have their own per-job timeout — so a job that has just started will run to completion (or hit `JOB_TIMEOUT_SECONDS`).

---

## Notes & tradeoffs

- **In-memory store only.** Restarting the server loses all job state. Persistence is intentionally out of scope; swap `JobStore` for a Redis- or Postgres-backed implementation when needed — the surface area is small (`Create`, `Get`, `UpdateStatus`, `SetResult`, `SetError`).
- **No per-user authn/authz.** `user_id` is stored but not authenticated. Anyone can fetch any `job_id` they know.
- **Job retention is unbounded.** A long-running server's map grows without bound. Add TTL/eviction before going to production.
- **No retries on Ollama errors.** A failed Ollama call marks the job `failed`. If you want at-least-once behaviour, add retry-with-backoff inside `worker.process`.
