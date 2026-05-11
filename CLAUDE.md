# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repo layout

This repo is split into two top-level directories:

- `backend/` — the Go HTTP server (this is the Go module; module path is `github.com/MirkoCalvi/httpserver/backend`).
- `frontend/` — the React + Vite + TypeScript web app (Firebase Google sign-in → calls the backend).

All Go commands below assume `cd backend` first. Frontend commands assume `cd frontend`.

## Commands

```bash
# Backend (run from repo root with `cd backend &&` or from inside backend/):
cd backend
DEV_MODE=1 go run ./cmd/httpserver # bypass auth, loopback only
FIREBASE_CREDENTIALS_FILE=~/secrets/firebase-admin.json go run ./cmd/httpserver       # verify Firebase ID tokens

# Build the binary
go build -o bin/httpserver ./cmd/httpserver

# Whole-module check before committing (from backend/):
go mod tidy && go build ./... && go vet ./...

# Frontend:
cd frontend
npm install
npm run dev   # Vite dev server on http://localhost:5173
```

### Git Hub 
- allowed to any gitHub commnads except pushing
- never put yourself (claude code) as co-autor 
- never include CLAUDE.md and .claude in commits (use `git add [...] :!CLAUDE.md  :!.claude` )

**Go version:** the module's `go` directive is whatever `go mod tidy` resolves to (currently `go 1.25.0` because Firebase Admin SDK transitively requires recent Go). Anyone with Go ≥1.21 installed gets the right toolchain auto-fetched via `GOTOOLCHAIN=auto` (the default). Local dev installed Go 1.22 to `~/sdk/go1.22.10/` — use `PATH=~/sdk/go1.22.10/bin:$PATH` if the system Go is older.

There is no test suite yet. If you add `_test.go` files, run with `go test ./...` (from `backend/`) and a single test with `go test ./internal/<pkg> -run TestName`.

The server expects Ollama running locally with the configured model pulled (`ollama pull phi3` for the default).

Configuration is environment-driven. Auth: exactly one of `DEV_MODE=1` or `FIREBASE_CREDENTIALS_FILE=/path/to/service-account.json` (none → fail closed; both → fail closed). Other knobs: `SERVER_ADDR`, `OLLAMA_URL`, `OLLAMA_MODEL`, `JOB_TIMEOUT_SECONDS`, `CORS_ORIGIN` (defaults in `backend/cmd/httpserver/main.go`). Worker count (10) and queue size (100) are intentionally compile-time constants — the spec is "max 10 workers".

## Architecture

This is an asynchronous job-queue server fronting Ollama. The big picture is **a buffered channel between the HTTP layer and a fixed worker pool**, with all job state kept in an in-memory, mutex-guarded map.

```
POST /generate  ──┐  auth.Middleware                ┌──> worker (goroutine #0)
                  │  (Bearer key → user_id in ctx)  │     calls ollama, writes store
GET /jobs/{id}    │     buffered chan *Job (100)    ├──> worker (goroutine #1) ...
                  │                                 │
handlers ─────────┴──> pool.Submit (non-blocking) ──┴──> 10 workers total
                  │
                  └──> store.Create / store.Get  ◄────────── (workers update)
```

Key invariants and design choices a future change should respect:

- **Module path is `github.com/MirkoCalvi/httpserver/backend`** (the `go.mod` lives at `backend/go.mod`, not the repo root, since the repo is now a backend+frontend split). Application code lives under `backend/internal/` (Go enforces that nothing outside this module can import it); the only `main` package is `backend/cmd/httpserver`. Don't move packages out of `internal/` unless you genuinely intend them to be a public API.

- **`Pool.Submit` is non-blocking** (`select` with `default`). A full queue returns `false` and the handler responds 503. Don't change this to a blocking send — that would tie up HTTP request goroutines and cascade backpressure into the request layer in a way callers can't observe.

- **Per-job context is rooted at `context.Background()`, not the application context.** This is deliberate: graceful shutdown (`pool.Stop`) closes the jobs channel and waits for workers to drain. Queued/in-flight Ollama calls finish on their own timeout rather than being cancelled mid-flight. If you ever want shutdown to abort in-flight work, that's a real design change — read `backend/internal/worker/worker.go`'s package comment first.

- **Shutdown order is HTTP-first, then pool.** `srv.Shutdown` runs before `pool.Stop` so no new jobs can be enqueued while the pool is draining. Reversing this order would race.

- **`JobStore.Get` returns a value copy**, not `*Job`. Callers cannot mutate stored state without going through the setters (`UpdateStatus`, `SetResult`, `SetError`). Preserve this when adding new accessors.

- **`StatusFailed` is an internal extension.** The public spec advertises `queued|processing|done`, but errors need somewhere to land — without `failed`, a crashed Ollama call would leave a job stuck in `processing` forever. The handler exposes `failed` jobs with status 200 plus an `error` field. Don't remove `StatusFailed` without first deciding how errors are otherwise reported.

- **Method enforcement is in handlers, not the mux.** Holdover from when the project targeted Go 1.18 (before pattern routing). Now that the toolchain is 1.22+, you can switch to pattern-routed `ServeMux` (`mux.HandleFunc("POST /generate", ...)`) and remove the in-handler `r.Method` checks — currently kept to minimize churn while the Firebase work landed.

- **`user_id` comes from the auth middleware, never the body.** `backend/internal/auth.FirebaseAuthenticator.Middleware` calls `client.VerifyIDToken(ctx, token)` from the Firebase Admin SDK, then stashes the verified UID on `r.Context()` via an unexported context key. Handlers read it with `auth.UserFrom(ctx)`. The `generateRequest` struct deliberately has no UserID field — `DisallowUnknownFields` rejects clients that still send one. The body must never decide identity.

- **Per-request model options use pointers to distinguish "unset" from "zero."** `Job.Temperature` and `ollama.Options.Temperature` are `*float64`. The handler resolves a server-side default (`defaultTemperature = 0.7`) when the client doesn't send one, so by the time a Job reaches the worker its `Temperature` is always non-nil — but the pointer plumbing is preserved at the `ollama` package boundary so that layer remains agnostic about whether *anyone* picked a value. Future tunables (top_p, top_k, num_predict, seed, …) should follow the same pointer pattern. The handler validates `temperature` is in `[0, 2]` and rejects NaN.

- **`GET /jobs/{id}` returns 404 on ownership mismatch, not 403.** The handler checks `job.UserID != auth.UserFrom(ctx)` and treats unknown-id and not-yours identically. This is intentional — distinguishing them lets non-owners enumerate which job IDs exist. Don't "improve" this to a more informative error.

- **An auth strategy is required at startup; the server fails closed.** `buildAuth` in `backend/cmd/httpserver/main.go` selects exactly one of `DEV_MODE=1` or `FIREBASE_CREDENTIALS_FILE=...`. Setting both is rejected. None set → exit 1.

- **`DEV_MODE` is forced to bind loopback.** When `DEV_MODE=1`, `loopbackOnly` rewrites `SERVER_ADDR=":PORT"` to `127.0.0.1:PORT` and refuses any explicit non-loopback host. This is the safety net that makes `auth.DevMiddleware` (which injects `user_id="dev"` without checking any header) hard to expose by accident. Don't soften this — the middleware is only safe under that constraint.

- **Firebase auth uses `VerifyIDToken`, not `VerifyIDTokenAndCheckRevoked`.** The cheap variant validates signature/expiry/audience/issuer locally; the expensive one makes a Firebase round-trip per request to check the user's revocation timestamp. Default to cheap; only switch if your threat model requires immediate revocation (Firebase tokens are 1-hour TTL anyway). Failure modes (expired, revoked-not-yet-detected, wrong-project, malformed) are intentionally collapsed to a single `401 unauthorized` response so attackers can't probe which check failed.

- **Job's `UserID` is the Firebase UID** (`token.UID`) — opaque, stable, never reassigned. If you'd prefer email or another claim, change the assignment in `backend/internal/auth/auth.go`'s `Middleware`. Don't trust email without checking `verified.Claims["email_verified"]`.

- **The Firebase Admin SDK pulls in a heavy dep tree** (cloud.google.com/go, opentelemetry, gRPC). That's why `go.mod` has so many indirect dependencies and why the `go` directive ended up at `1.25.0` after `go mod tidy` — some transitive dep requires that version. With Go ≥1.21 and the default `GOTOOLCHAIN=auto`, this is invisible to users; older Gos must upgrade.

- **`internal/logger` is now redundant.** The toolchain is on Go 1.22+ and the standard `log/slog` (1.21+) does the same job. The wrapper's API was deliberately slog-shaped, so migrating is mechanical — replacing `*logger.Logger` with `*slog.Logger` and changing `New()` to `slog.New(slog.NewJSONHandler(os.Stdout, nil))` is the bulk of it. Same for the prefix-routed mux: switch to pattern routing (`mux.HandleFunc("POST /generate", ...)`, `r.PathValue("id")`) and drop the in-handler method checks.

- **Body size cap is 1 MiB** (`maxBodyBytes` in `backend/internal/handlers/handlers.go`) and `DisallowUnknownFields` is on. Both are deliberate input hygiene; raise the cap if you legitimately need larger prompts but understand it widens the abuse surface.

- **CORS is single-origin and env-driven.** `corsMiddleware` in `backend/cmd/httpserver/main.go` echoes `Access-Control-Allow-Origin` only when the request's `Origin` header matches `CORS_ORIGIN` (default `http://localhost:5173`). It does not reflect arbitrary origins back. Allowed methods: `GET, POST, OPTIONS`. Allowed headers: `Authorization, Content-Type`. Override `CORS_ORIGIN` for prod.

- **`GET /characters` is unauthenticated.** It returns the sorted JSON array of registered character names (`internal/ollama/personalities.Names()`). The frontend needs this on its first render to populate a character `<Select>`, before a Firebase token is available. The list is not sensitive — keep it public alongside `/healthz`.

## What's intentionally out of scope

These are common asks that have been considered and excluded — re-introduce them only if the user actually needs them:

- **Persistence.** `JobStore` is in-memory; restart loses everything. The interface is small (`Create`, `Get`, `UpdateStatus`, `SetResult`, `SetError`) so a Redis/Postgres-backed implementation is a localized change.
- **Multi-origin CORS.** The current middleware allows exactly one origin via `CORS_ORIGIN`. Supporting a list (e.g. dev + staging + prod) means a small change to `corsMiddleware` to accept a comma-separated env var and check membership.
- **Token revocation check.** See above — `VerifyIDToken` not `VerifyIDTokenAndCheckRevoked`.
- **API-key fallback for service-to-service.** Was prototyped (commits in this repo's history might still show it) but removed at the user's request. If CI/ops scripts later need to call the backend non-interactively, reintroduce a parallel middleware that runs *before* Firebase verification and checks an API key.
- **Retention/eviction.** The map grows unbounded.
- **Retries.** A failed Ollama call marks the job `failed` once; no backoff/retry.
