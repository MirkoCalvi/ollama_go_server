# httpserver

A small, production-shaped Go HTTP server that fronts a local [Ollama](https://ollama.com) instance with an asynchronous job queue.

Clients submit a prompt over HTTP, receive a `job_id` immediately, and poll a separate endpoint to fetch the result once a worker has finished calling Ollama.

---

## Features

- `POST /generate` — accepts `{prompt}`, enqueues a job, returns `{job_id, status}` immediately. Authenticated.
- `GET /jobs/{id}` — returns the current state of a job (`queued`, `processing`, `done`, or `failed`) and the response if available. Authenticated; only the owning user can read a job.
- `GET /healthz` — liveness probe (no auth).
- **Firebase ID-token auth** via `Authorization: Bearer <id-token>`; the verified Firebase UID is stored as the job owner. `DEV_MODE=1` bypasses auth for local backend hacking.
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
│   ├── auth/            # Firebase ID-token middleware + dev bypass; ctx plumbing
│   ├── models/          # Job, JobResult, JobStatus, thread-safe JobStore
│   └── logger/          # tiny JSON-line structured logger
├── go.mod               # module github.com/MirkoCalvi/httpserver
└── go.sum
```

Each package owns one concern. The dependency graph is one-way:

```
cmd/httpserver
   └─> internal/auth     (middleware wraps protected routes)
   └─> internal/handlers ──> internal/worker ──> internal/ollama
                       └──> internal/auth      (reads user from ctx)
                       └──> internal/models
                       └──> internal/logger
```

Nothing depends on `handlers`, so swapping HTTP for, say, gRPC later is a localized change.

---

## Requirements

- **Go 1.21 or newer** (the module declares its toolchain via `go 1.25.0` and Firebase Admin SDK transitively requires recent Go; with Go ≥1.21 the `GOTOOLCHAIN=auto` default will fetch the right version on first build).
- **Ollama** running locally on port 11434 with the `phi3` model (or any model you set via `OLLAMA_MODEL`).
- **Firebase service-account JSON** for production auth — download from Firebase Console → ⚙️ Project settings → Service accounts → "Generate new private key". Save it on disk and `chmod 600`. Not needed if you only use `DEV_MODE`.

Pull the model before running the server:

```bash
ollama pull phi3
```

---

## Managing Ollama

On Linux the Ollama installer registers a `systemd` service that starts the daemon on boot. You usually don't need to do anything — but if it isn't running, the server's jobs will end up `failed` with `connection refused` (daemon down) or `context deadline exceeded` (daemon up but the model is loading).

```bash
# Start the daemon
sudo systemctl start ollama

# Stop the daemon
sudo systemctl stop ollama

# Restart (e.g. after changing GPU drivers)
sudo systemctl restart ollama

# Check status / recent logs
systemctl status ollama
journalctl -u ollama -f

# Verify it's reachable and see installed models
curl -s http://localhost:11434/api/tags
ollama list
```

### Running Ollama in the foreground (no systemd)

If you didn't install via the official script, or you want to see Ollama's logs directly in a terminal:

```bash
# Start in the foreground — blocks the terminal, Ctrl-C to stop.
ollama serve
```

You can't run `ollama serve` while the systemd service is also running — port 11434 will be in use. Stop the service first (`sudo systemctl stop ollama`) or pick whichever style you prefer.

### macOS

The Ollama desktop app starts the daemon automatically. Quit it from the menu bar to stop. The CLI equivalent:

```bash
ollama serve     # start (foreground)
# Ctrl-C          # stop
```

---

## Run

The server **requires an auth strategy** at startup; it refuses to start otherwise. Pick exactly one:

```bash
# 1. Local development — bypasses auth, forces loopback bind, every request runs as user "dev".
DEV_MODE=1 go run ./cmd/httpserver

# 2. Firebase ID-token verification — the production path. Point at the
#    service-account JSON downloaded from the Firebase console.
chmod 600 ~/secrets/firebase-admin.json
FIREBASE_CREDENTIALS_FILE=~/secrets/firebase-admin.json go run ./cmd/httpserver
```

Build instead of `go run`:

```bash
go build -o bin/httpserver ./cmd/httpserver
DEV_MODE=1 ./bin/httpserver
# or
FIREBASE_CREDENTIALS_FILE=~/secrets/firebase-admin.json ./bin/httpserver
```

Setting both `DEV_MODE` and `FIREBASE_CREDENTIALS_FILE` is rejected at startup so the active config is unambiguous.

You should see startup logs like:

```json
{"addr":":8080","level":"INFO","msg":"http server starting","ollama_model":"phi3","ollama_url":"http://localhost:11434","queue_size":100,"workers":10,"time":"..."}
```

---

## Configuration

All knobs are environment variables with sensible defaults. Worker count and queue size are intentionally **compile-time constants** (the spec is "max 10 workers"); change `cmd/httpserver/main.go` if you need different values.

| Variable                    | Default                  | Meaning                                                                              |
| --------------------------- | ------------------------ | ------------------------------------------------------------------------------------ |
| `DEV_MODE`                  | unset                    | If `1`, bypass auth and force-bind loopback. Set this **or** `FIREBASE_CREDENTIALS_FILE`. |
| `FIREBASE_CREDENTIALS_FILE` | unset                    | Path to the Firebase service-account JSON. Required for prod.                        |
| `SERVER_ADDR`               | `:8080`                  | HTTP listen address (rewritten to `127.0.0.1:PORT` under `DEV_MODE`).                |
| `OLLAMA_URL`                | `http://localhost:11434` | Base URL of the Ollama server                                                        |
| `OLLAMA_MODEL`              | `phi3`                   | Model to invoke                                                                      |
| `JOB_TIMEOUT_SECONDS`       | `60`                     | Per-job timeout for the Ollama call                                                  |

Example:

```bash
FIREBASE_CREDENTIALS_FILE=~/secrets/firebase-admin.json \
SERVER_ADDR=:9000 OLLAMA_MODEL=llama3.2 JOB_TIMEOUT_SECONDS=120 \
  go run ./cmd/httpserver
```

---

## Authentication

Every request to `/generate` and `/jobs/{id}` must carry a **Firebase ID token**:

```
Authorization: Bearer <firebase-id-token>
```

The token is the JWT the Firebase JS SDK gives the browser after a user signs in (e.g. with Google). The backend verifies it with the Firebase Admin SDK — signature, expiry, audience (must be your Firebase project), and issuer — then stores the verified UID on the request context. Handlers see the resolved UID via `auth.UserFrom(ctx)`; the body cannot override it.

- Missing, malformed, expired, wrong-project, or signature-invalid token → **401 Unauthorized** with `WWW-Authenticate: Bearer realm="httpserver"`. The response is the same regardless of which check failed, so attackers can't learn anything about the failure mode.
- `GET /jobs/{id}` returns **404** if the job doesn't exist *or* belongs to a different user. The two cases are intentionally indistinguishable so non-owners can't probe which IDs exist.
- `/healthz` is exempt — liveness probes don't need a token.

The job's `UserID` is the Firebase UID, which is stable and never reassigned. (If you'd rather store email or another claim, change the line `verified.UID` in `internal/auth/auth.go`.)

### Dev mode

Set `DEV_MODE=1` to skip auth entirely — useful when you're hacking on the backend and don't want to spin up a Firebase client:

```bash
DEV_MODE=1 go run ./cmd/httpserver
curl -X POST http://localhost:8080/generate \
     -H 'Content-Type: application/json' \
     -d '{"prompt":"hi"}'
```

Every request runs as user `dev`. As a safety net, dev mode **refuses to bind a non-loopback interface**: `SERVER_ADDR=:PORT` (or unset) is rewritten to `127.0.0.1:PORT`, and an explicit non-loopback host like `0.0.0.0` makes the server exit at startup.

### Frontend integration

A frontend (e.g. Vite + React + the Firebase JS SDK) signs the user in with Google, then calls this backend like:

```js
import { getAuth } from "firebase/auth";

const idToken = await getAuth().currentUser.getIdToken();
await fetch("http://localhost:8080/generate", {
  method: "POST",
  headers: {
    "Authorization": `Bearer ${idToken}`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({ prompt: "hello" }),
});
```

Firebase ID tokens expire after 1 hour; the JS SDK refreshes them automatically — no backend work needed. CORS is **not currently configured**; add a middleware in `cmd/httpserver/main.go` once a browser frontend is calling in cross-origin (typical dev origin would be `http://localhost:5173` for Vite).

---

## API

### `POST /generate`

**Request**

```json
{
  "prompt": "Explain channels in Go in two sentences."
}
```

The body does not carry `user_id` — the server takes it from the verified Firebase token (or, in `DEV_MODE`, hardcodes `dev`). Sending a `user_id` field returns 400 (unknown field).

**Response — 202 Accepted**

```json
{
  "job_id": "9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11",
  "status": "queued"
}
```

**Errors**

| Status | When                                                                                                         |
| ------ | ------------------------------------------------------------------------------------------------------------ |
| 400    | Body is not valid JSON, contains an unknown field, or `prompt` is empty.                                     |
| 401    | Missing or invalid Firebase ID token.                                                                        |
| 405    | Wrong method (`Allow: POST`).                                                                                |
| 413    | Body exceeds 1 MiB.                                                                                          |
| 503    | Worker queue is full — retry later.                                                                          |

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

| Status | When                                                              |
| ------ | ----------------------------------------------------------------- |
| 401    | Missing or invalid Firebase ID token.                             |
| 404    | No job with that ID **or** the job belongs to another user.       |
| 405    | Wrong method (`Allow: GET`).                                      |

### `GET /healthz`

Returns `200 OK` with body `ok`.

---

## Example session

Easiest to demo with `DEV_MODE=1` so you don't need a Firebase token:

```bash
$ DEV_MODE=1 go run ./cmd/httpserver

# 1. Submit a prompt (no auth header needed in dev mode)
$ curl -s -X POST http://localhost:8080/generate \
       -H 'Content-Type: application/json' \
       -d '{"prompt":"What is a goroutine?"}'
{"job_id":"9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11","status":"queued"}

# 2. Poll for the result
$ curl -s http://localhost:8080/jobs/9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11
{"job_id":"9f0c...","status":"processing"}

$ curl -s http://localhost:8080/jobs/9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11
{"job_id":"9f0c...","status":"done","response":"A goroutine is a lightweight thread..."}

# Health check
$ curl -s http://localhost:8080/healthz
ok
```

In production mode (`FIREBASE_CREDENTIALS_FILE=…`), every call adds the header:

```bash
ID_TOKEN="$(... your Firebase JS SDK gives this to the browser ...)"

curl -s -X POST http://localhost:8080/generate \
     -H "Authorization: Bearer $ID_TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"prompt":"What is a goroutine?"}'
```

To stress-test concurrency, fire 25 prompts at once (in dev mode for simplicity) — the first 10 hit workers immediately, the next 15 sit in the queue, and anything past `queueSize=100` outstanding gets a clean 503:

```bash
for i in $(seq 1 25); do
  curl -s -X POST http://localhost:8080/generate \
       -H 'Content-Type: application/json' \
       -d "{\"prompt\":\"say hi $i\"}" &
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
- **No CORS.** The backend assumes same-origin or non-browser callers. Add a CORS middleware before pointing a browser frontend at it cross-origin.
- **No revocation check.** `Middleware` calls `VerifyIDToken`, not `VerifyIDTokenAndCheckRevoked`. A token revoked via Firebase Admin remains valid here until it expires (Firebase tokens last 1 hour). Tighten this if your threat model requires immediate revocation.
- **Job retention is unbounded.** A long-running server's map grows without bound. Add TTL/eviction before going to production.
- **No retries on Ollama errors.** A failed Ollama call marks the job `failed`. If you want at-least-once behaviour, add retry-with-backoff inside `worker.process`.
