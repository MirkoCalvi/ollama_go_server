# backend

The Go HTTP server. An asynchronous job queue that fronts a local
[Ollama](https://ollama.com) instance: clients submit a prompt + character,
get a `job_id` immediately, and poll a separate endpoint to fetch the result
once a worker has finished calling Ollama.

> All commands below assume `cd backend` first. The Go module lives at
> `backend/go.mod`; module path is `github.com/MirkoCalvi/httpserver/backend`.

## Routes

| Route               | Method | Auth      | Purpose                                                    |
| ------------------- | ------ | --------- | ---------------------------------------------------------- |
| `/generate`         | POST   | required  | Enqueue a prompt for a character; returns 202 + `job_id`.  |
| `/jobs/{id}`        | GET    | required  | Read a job's current state. 404 if not yours or unknown.   |
| `/characters`       | GET    | none      | List of registered character names.                        |
| `/healthz`          | GET    | none      | Liveness probe — returns `200 ok`.                         |

CORS is enforced for browser clients via single-origin echo (default
`http://localhost:5173`, override with `CORS_ORIGIN`); preflight `OPTIONS`
requests on protected routes are handled.

## Features

- Bounded **worker pool of 10 goroutines** consuming from a buffered channel queue (size 100).
- Per-job `context.WithTimeout` for the Ollama call (default 180s, configurable).
- **Firebase ID-token auth** (`Authorization: Bearer <id-token>`); the verified UID is the job owner. `DEV_MODE=1` bypasses auth for local hacking.
- **Per-character system prompts and sampling parameters** (temperature, top-p, top-k) — see `internal/ollama/personalities/`.
- Thread-safe in-memory job store (`sync.RWMutex` + map).
- Structured JSON logging to stdout.
- Graceful shutdown: stops accepting new HTTP requests, then drains queued jobs.
- Validates input, caps request bodies at 1 MiB, returns 503 when the queue is saturated.
- Standard Go layout (`cmd/` + `internal/`).

## Project layout

```
backend/
├── cmd/
│   └── httpserver/      main package — entrypoint, wiring, graceful shutdown,
│                        CORS middleware, auth strategy selection
├── internal/
│   ├── handlers/        HTTP handlers (POST /generate, GET /jobs/{id}, GET /characters)
│   ├── worker/          worker pool: buffered channel + N goroutines
│   ├── ollama/          thin client for http://localhost:11434/api/chat
│   │   └── personalities/  registered Character definitions (Frank, Olivia, August, …)
│   ├── auth/            Firebase ID-token middleware + dev bypass; ctx plumbing
│   ├── models/          Job, JobStatus, thread-safe JobStore
│   └── logger/          tiny JSON-line structured logger
├── docs/                walkthrough + personality notes
├── go.mod               module github.com/MirkoCalvi/httpserver/backend
└── go.sum
```

The dependency graph is one-way:

```
cmd/httpserver
   ├─> internal/auth       (middleware wraps protected routes)
   └─> internal/handlers ──> internal/worker ──> internal/ollama
                       └──> internal/auth      (reads UID from ctx)
                       └──> internal/models
                       └──> internal/ollama/personalities  (resolves Character by name)
                       └──> internal/logger
```

Nothing depends on `handlers`, so swapping HTTP for, say, gRPC later is a
localized change.

## Requirements

- **Go 1.21 or newer.** `go.mod` declares `go 1.25.0` (Firebase Admin SDK transitively requires recent Go); with Go ≥1.21 the `GOTOOLCHAIN=auto` default fetches the right toolchain on first build.
- **Ollama** running locally on port 11434 with the configured model pulled (default `phi3`).
- **Firebase service-account JSON** for production auth — Firebase Console → ⚙️ Project settings → Service accounts → "Generate new private key". Save it on disk and `chmod 600`. Not needed in `DEV_MODE`.

```bash
ollama pull phi3
```

## Run

The server **requires an auth strategy** at startup; setting neither, or both,
makes it exit 1 (fail-closed). Pick exactly one:

```bash
# Local hacking — bypasses auth, forces loopback bind, every request runs as user "dev".
DEV_MODE=1 go run ./cmd/httpserver

# Production path — verify Firebase ID tokens.
chmod 600 ~/secrets/firebase-admin.json
FIREBASE_CREDENTIALS_FILE=~/secrets/firebase-admin.json go run ./cmd/httpserver
```

Build instead of `go run`:

```bash
go build -o bin/httpserver ./cmd/httpserver
DEV_MODE=1 ./bin/httpserver
```

Whole-module check before committing:

```bash
go mod tidy && go build ./... && go vet ./...
```

## Configuration

All knobs are environment variables with sensible defaults. Worker count and
queue size are intentionally **compile-time constants** (the spec is "max 10
workers"); change `cmd/httpserver/main.go` if you need different values.

| Variable                    | Default                  | Meaning                                                                              |
| --------------------------- | ------------------------ | ------------------------------------------------------------------------------------ |
| `DEV_MODE`                  | unset                    | If `1`, bypass auth and force-bind loopback. Set this **or** `FIREBASE_CREDENTIALS_FILE`. |
| `FIREBASE_CREDENTIALS_FILE` | unset                    | Path to the Firebase service-account JSON. Required for prod.                        |
| `SERVER_ADDR`               | `:8080`                  | HTTP listen address (rewritten to `127.0.0.1:PORT` under `DEV_MODE`).                |
| `OLLAMA_URL`                | `http://localhost:11434` | Base URL of the Ollama server.                                                       |
| `OLLAMA_MODEL`              | `phi3`                   | Model to invoke.                                                                     |
| `JOB_TIMEOUT_SECONDS`       | `180`                    | Per-job timeout for the Ollama call.                                                 |
| `CORS_ORIGIN`               | `http://localhost:5173`  | Single allowed browser origin. Override for prod.                                    |

Example:

```bash
FIREBASE_CREDENTIALS_FILE=~/secrets/firebase-admin.json \
SERVER_ADDR=:9000 OLLAMA_MODEL=llama3.2 JOB_TIMEOUT_SECONDS=120 \
CORS_ORIGIN=https://chat.example.com \
  go run ./cmd/httpserver
```

## Authentication

Every request to `/generate` and `/jobs/{id}` must carry a **Firebase ID
token**:

```
Authorization: Bearer <firebase-id-token>
```

The token is the JWT the Firebase JS SDK gives the browser after a user signs
in (e.g. with Google). The backend verifies signature, expiry, audience (must
be your Firebase project), and issuer with the Firebase Admin SDK, then stores
the verified UID on the request context. Handlers see the resolved UID via
`auth.UserFrom(ctx)`; the body cannot override it.

- Missing, malformed, expired, wrong-project, or signature-invalid token → **401 Unauthorized** with `WWW-Authenticate: Bearer realm="httpserver"`. The response is identical regardless of which check failed, so attackers can't probe failure modes.
- `GET /jobs/{id}` returns **404** if the job doesn't exist *or* belongs to a different user — intentionally indistinguishable so non-owners can't enumerate IDs.
- `/characters` and `/healthz` are exempt — the frontend needs the character list before sign-in, and liveness probes don't carry tokens.

The job's `UserID` is the Firebase UID, opaque and stable. To switch to email
or another claim, edit the assignment in `internal/auth/auth.go`'s
`Middleware`. Don't trust email without checking
`verified.Claims["email_verified"]`.

### Dev mode

`DEV_MODE=1` skips auth entirely — useful when iterating on the backend
without spinning up Firebase:

```bash
DEV_MODE=1 go run ./cmd/httpserver
curl -X POST http://localhost:8080/generate \
     -H 'Content-Type: application/json' \
     -d '{"prompt":"hi","character":"Frank"}'
```

Every request runs as user `dev`. As a safety net, dev mode **refuses to bind
non-loopback interfaces**: `SERVER_ADDR=:PORT` (or unset) is rewritten to
`127.0.0.1:PORT`, and an explicit non-loopback host like `0.0.0.0` makes the
server exit at startup.

## API

### `POST /generate`

**Request**

```json
{
  "prompt": "Explain channels in Go in two sentences.",
  "character": "Frank"
}
```

`character` must be one of the names returned by `GET /characters`. The body
does **not** carry `user_id` — the server reads it from the verified Firebase
token (or `dev` in dev mode). Sending `user_id` returns 400 (unknown field).

**Response — 202 Accepted**

```json
{
  "job_id": "9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11",
  "status": "queued"
}
```

**Errors**

| Status | When                                                                                                  |
| ------ | ----------------------------------------------------------------------------------------------------- |
| 400    | Body is not valid JSON, contains an unknown field, exceeds 1 MiB, or `prompt`/`character` is missing. |
| 400    | `character` value is not a registered name.                                                           |
| 401    | Missing or invalid Firebase ID token.                                                                 |
| 405    | Wrong method (`Allow: POST`).                                                                         |
| 503    | Worker queue is full — retry later.                                                                   |

### `GET /jobs/{id}`

**Response — 200 OK** (queued or processing)

```json
{ "job_id": "9f0c...", "status": "processing" }
```

**Response — 200 OK** (done)

```json
{
  "job_id": "9f0c...",
  "status": "done",
  "response": "Channels are typed conduits..."
}
```

**Response — 200 OK** (failed)

```json
{
  "job_id": "9f0c...",
  "status": "failed",
  "error": "call ollama: dial tcp 127.0.0.1:11434: connect: connection refused"
}
```

`failed` is an internal extension of the public spec (`queued|processing|done`)
— without it, a crashed Ollama call would leave a job stuck in `processing`
forever.

**Errors**

| Status | When                                                              |
| ------ | ----------------------------------------------------------------- |
| 401    | Missing or invalid Firebase ID token.                             |
| 404    | No job with that ID **or** the job belongs to another user.       |
| 405    | Wrong method (`Allow: GET`).                                      |

### `GET /characters`

Returns a sorted JSON array of registered character names. No auth.

```json
["August","Frank","James","Olivia","Robert"]
```

Each name resolves to a `Character` (system prompt + sampling parameters)
defined in `internal/ollama/personalities/`. To add a character: drop a new
file in that directory and register it in the `byName` map in `personalities.go`.

### `GET /healthz`

Returns `200 OK` with body `ok`. No auth.

## Example session

Easiest with `DEV_MODE=1` so you don't need a Firebase token:

```bash
$ DEV_MODE=1 go run ./cmd/httpserver

# 1. See which characters exist
$ curl -s http://localhost:8080/characters
["August","Frank","James","Olivia","Robert"]

# 2. Submit a prompt
$ curl -s -X POST http://localhost:8080/generate \
       -H 'Content-Type: application/json' \
       -d '{"prompt":"What is a goroutine?","character":"Frank"}'
{"job_id":"9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11","status":"queued"}

# 3. Poll for the result
$ curl -s http://localhost:8080/jobs/9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11
{"job_id":"9f0c...","status":"processing"}

$ curl -s http://localhost:8080/jobs/9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11
{"job_id":"9f0c...","status":"done","response":"A goroutine is..."}
```

In production mode (`FIREBASE_CREDENTIALS_FILE=…`), every protected call adds:

```bash
ID_TOKEN="$(... your Firebase JS SDK gives this to the browser ...)"

curl -s -X POST http://localhost:8080/generate \
     -H "Authorization: Bearer $ID_TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"prompt":"What is a goroutine?","character":"Frank"}'
```

Stress-test concurrency by firing 25 prompts at once — the first 10 hit
workers immediately, the next 15 sit in the queue, and anything past the
queue cap (100 outstanding) gets a clean 503:

```bash
for i in $(seq 1 25); do
  curl -s -X POST http://localhost:8080/generate \
       -H 'Content-Type: application/json' \
       -d "{\"prompt\":\"say hi $i\",\"character\":\"Frank\"}" &
done
wait
```

## Concurrency model

```
HTTP handler              Buffered channel (cap 100)            Worker pool (10 goroutines)
─────────────             ─────────────────────────             ────────────────────────────
POST /generate ──> Submit ──> [ job, job, job, ... ] ──> recv ──> worker calls Ollama
                  (non-blocking; 503 if full)                     ──> store.SetResult / SetError
```

- **Submit is non-blocking.** A full queue surfaces as HTTP 503; request goroutines never block on pool capacity.
- **Each job has its own context.** Per-job `context.WithTimeout` rooted at `context.Background()` — graceful shutdown does **not** cancel in-flight calls, it just stops accepting new HTTP requests and waits for the queue to drain.
- **Store access is RWMutex-guarded.** Reads (`GET /jobs/{id}`) take an RLock; writes (`Create`, `UpdateStatus`, `SetResult`, `SetError`) take a Lock. `JobStore.Get` returns a value copy, so callers can't mutate stored state without going through a setter.

## Graceful shutdown

On `SIGINT` / `SIGTERM`:

1. `http.Server.Shutdown` stops accepting new connections and lets in-flight HTTP requests finish (capped by `shutdownTimeout = 15s`).
2. `pool.Stop()` closes the jobs channel and `wg.Wait`s for the 10 workers to drain remaining queued jobs.
3. Process exits.

In-flight Ollama calls are not cancelled by shutdown — they have their own
per-job timeout — so a job that has just started will run to completion (or
hit `JOB_TIMEOUT_SECONDS`).

## Notes & out-of-scope

These are common asks that have been considered and left out — re-introduce
them only if a real need arises:

- **Persistence.** `JobStore` is in-memory; restart loses everything. Surface area is small (`Create`, `Get`, `UpdateStatus`, `SetResult`, `SetError`) so a Redis/Postgres backend is a localized change.
- **Multi-origin CORS.** Current middleware allows exactly one origin via `CORS_ORIGIN`. Multi-origin support means accepting a comma-separated list and checking membership.
- **Token revocation check.** `Middleware` calls `VerifyIDToken`, not `VerifyIDTokenAndCheckRevoked`. A token revoked via Firebase Admin remains valid here until it expires (1 hour TTL). Tighten if your threat model requires immediate revocation.
- **Job retention / eviction.** A long-running server's map grows unbounded. Add TTL/eviction before going to production.
- **Retries on Ollama errors.** A failed call marks the job `failed`; no backoff/retry. If you want at-least-once behaviour, add retry inside `worker.process`.
- **Streaming responses.** The Ollama client reads the full response before writing to the store. Per-token streaming requires a real backend redesign (handler holds connection, store grows incrementally, FE switches to SSE).
- **API-key fallback for service-to-service.** Was prototyped and removed. If CI/ops scripts later need to call non-interactively, reintroduce a parallel middleware that runs *before* Firebase verification.
