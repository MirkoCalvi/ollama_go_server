# Code Walkthrough

A step-by-step trace of what happens inside this server, from `go run ./cmd/httpserver` to a finished job. Each section points at the actual file and explains *why* the code is shaped that way, with concrete inputs and outputs you can reproduce.

> Reading order: this guide assumes you already skimmed `README.md`. If something feels under-explained here, check there first.

---

## Step 0 — The mental model in one picture

```
              ┌────────────────┐    ┌──────────────────────────┐    ┌──────────────────┐
HTTP client ──▶  HTTP handler  │───▶│ buffered chan *Job (100) │───▶│   worker pool    │──▶ Ollama
              │  /generate     │    └──────────────────────────┘    │  (10 goroutines) │
              │  /jobs/{id}    │              ▲    │                └────────┬─────────┘
              └────────┬───────┘              │    │                         │
                       │                      │    │                         │
                       ▼                      │    └──── workers receive ────┘
                ┌──────────────┐              │
                │   JobStore   │◀─────────────┴──── handlers write,
                │  (map+mutex) │                    workers update
                └──────────────┘
```

Two players write to the store: HTTP handlers (when the job is created) and workers (when the status or result changes). They coordinate via the channel and a `sync.RWMutex`.

---

## Step 1 — Server starts

**Entrypoint:** `cmd/httpserver/main.go`

When you run `go run ./cmd/httpserver`, this happens, in order:

1. **Logger** — `logger.New()` wraps `log.Logger` and emits one JSON object per line to stdout. (`internal/logger/logger.go`)
2. **Config** — `loadConfig` reads four environment variables with defaults:
   - `SERVER_ADDR` → `:8080`
   - `OLLAMA_URL` → `http://localhost:11434`
   - `OLLAMA_MODEL` → `phi3`
   - `JOB_TIMEOUT_SECONDS` → `60`
3. **JobStore** — `models.NewJobStore()` returns an empty `map[string]*Job` guarded by `sync.RWMutex`.
4. **Ollama client** — `ollama.NewClient(url, model)` is just a struct holding the URL, model name, and an `http.Client{}`. No network calls yet.
5. **Worker pool** — `worker.NewPool(10, 100, ...)` allocates the buffered channel `make(chan *models.Job, 100)`. Then `pool.Start()` spawns 10 goroutines, each running:
   ```go
   for job := range p.jobs { p.process(job, id) }
   ```
   All 10 are immediately **blocked** on the channel receive — there is nothing to do yet.
6. **Routes** — three handlers registered on a plain `http.ServeMux`:
   - `/generate` → `h.Generate`
   - `/jobs/` → `h.GetJob` (prefix match — the `{id}` is parsed inside the handler)
   - `/healthz` → returns `200 ok`
7. **HTTP server** — wrapped in `loggingMiddleware`, started in a goroutine.
8. **Signal handler** — `signal.NotifyContext(ctx, SIGINT, SIGTERM)` watches for shutdown; main blocks on `<-ctx.Done()`.

**What you see in the logs:**

```json
{"level":"INFO","msg":"worker started","worker_id":0,"time":"..."}
{"level":"INFO","msg":"worker started","worker_id":1,"time":"..."}
... (10 of these)
{"level":"INFO","msg":"http server starting","addr":":8080","workers":10,"queue_size":100,"ollama_model":"phi3","ollama_url":"http://localhost:11434","time":"..."}
```

The 10 "worker started" lines may interleave because the goroutines race to log; this is normal.

---

## Step 2 — Client sends `POST /generate`

```bash
curl -s -X POST http://localhost:8080/generate \
     -H 'Content-Type: application/json' \
     -d '{"user_id":"alice","prompt":"What is a goroutine?"}'
```

Inside the server, the request flows through:

### 2a. Logging middleware — `cmd/httpserver/main.go`

```go
rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
next.ServeHTTP(rec, r)
log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, ...)
```

`statusRecorder` is a tiny wrapper that captures the response status (otherwise the middleware can't see it) without changing handler behaviour.

### 2b. Mux routes to `h.Generate` — `internal/handlers/handlers.go`

```go
if r.Method != http.MethodPost {
    w.Header().Set("Allow", http.MethodPost)
    writeError(w, http.StatusMethodNotAllowed, "method not allowed")
    return
}
```

A `GET /generate` would short-circuit here with `405` and `Allow: POST`. The mux can't do this for us on Go 1.18 (no pattern routing), so each handler enforces its own method.

### 2c. Body cap and JSON decoding

```go
r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes) // 1 MiB
dec := json.NewDecoder(r.Body)
dec.DisallowUnknownFields()
if err := dec.Decode(&req); err != nil { ... 400 ... }
```

- `MaxBytesReader` is **input hygiene** — without it, an attacker could send a 10 GB body and exhaust memory.
- `DisallowUnknownFields` rejects payloads with stray keys (e.g. `{"user_id":"a","prompt":"b","sudo":true}`). This catches client bugs early instead of silently ignoring them.

**Bad inputs you can try:**

| Curl | Status | Reason |
|------|--------|--------|
| `-d '{'` | 400 | invalid JSON |
| `-d '{"user_id":"alice"}'` | 400 | empty `prompt` after trim |
| `-d '{"user_id":"alice","prompt":"hi","extra":1}'` | 400 | unknown field |
| `-d "$(head -c 2000000 /dev/zero)"` | 413 | over 1 MiB |

### 2d. Validation

```go
if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Prompt) == "" {
    writeError(w, http.StatusBadRequest, "user_id and prompt are required")
    return
}
```

Whitespace-only values count as empty — `{"user_id":"   ","prompt":"hi"}` is rejected.

### 2e. Create the Job and store it

```go
job := &models.Job{
    ID:        uuid.NewString(),
    UserID:    req.UserID,
    Prompt:    req.Prompt,
    Status:    models.StatusQueued,
    CreatedAt: now,
    UpdatedAt: now,
}
h.store.Create(job)
```

`uuid.NewString()` returns something like `9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11`. `store.Create` takes a write lock and inserts:

```go
// internal/models/models.go
func (s *JobStore) Create(job *Job) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.jobs[job.ID] = job
}
```

**The job exists in the store *before* it goes into the queue.** This ordering matters: it means a client can `GET /jobs/{id}` immediately after the 202 response and always find the record (status `queued` until a worker picks it up).

### 2f. Hand it to the pool — non-blocking

```go
if !h.pool.Submit(job) {
    h.store.SetError(job.ID, "queue full")
    writeError(w, http.StatusServiceUnavailable, "server is at capacity, try again later")
    return
}
```

```go
// internal/worker/worker.go
func (p *Pool) Submit(job *models.Job) bool {
    select {
    case p.jobs <- job:
        return true
    default:
        return false
    }
}
```

This is the most important concurrency primitive in the project. `select` with a `default` case turns a blocking channel send into a non-blocking one:

- If the buffered channel has room, the job is enqueued and `Submit` returns `true`.
- If the buffer is full (100 unprocessed jobs already queued), `default` runs immediately and `Submit` returns `false`.

The handler then marks the job `failed` (so a later `GET /jobs/{id}` reflects reality) and replies 503. The client gets a clear "back off and retry" signal instead of hanging.

### 2g. Reply 202 Accepted

```go
writeJSON(w, http.StatusAccepted, jobResponse{
    JobID:  job.ID,
    Status: job.Status, // "queued"
})
```

What the client sees:

```json
{"job_id":"9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11","status":"queued"}
```

What the logs show:

```json
{"level":"INFO","msg":"job queued","job_id":"9f0c...","user_id":"alice","time":"..."}
{"level":"INFO","msg":"http","method":"POST","path":"/generate","status":202,"duration_ms":1,"time":"..."}
```

The `duration_ms` is single-digit because the handler does no I/O — it just enqueues and returns.

---

## Step 3 — A worker picks up the job

A worker goroutine that's been blocked on `<-p.jobs` since startup unblocks the moment Step 2f sends. Exactly one worker receives — Go's runtime picks one of the waiting goroutines.

**Inside the worker — `internal/worker/worker.go`:**

```go
func (p *Pool) worker(id int) {
    defer p.wg.Done()
    for job := range p.jobs {        // <— the receive
        p.process(job, id)
    }
}

func (p *Pool) process(job *models.Job, workerID int) {
    p.store.UpdateStatus(job.ID, models.StatusProcessing)   // (a)
    p.log.Info("processing", "job_id", job.ID, ...)

    ctx, cancel := context.WithTimeout(context.Background(), p.timeout)  // (b)
    defer cancel()

    response, err := p.client.Generate(ctx, job.Prompt)     // (c)
    if err != nil {
        p.log.Error("ollama call failed", "job_id", job.ID, "error", err.Error())
        p.store.SetError(job.ID, err.Error())               // (d) failed
        return
    }
    p.store.SetResult(job.ID, response)                     // (d) done
}
```

**(a) Mark `processing`.** A client polling between Step 2g and now sees `queued`; once this line runs, polls see `processing`.

**(b) Per-job context.** Rooted at `context.Background()`, **not** the application context. Why?

> If we used the app context, then on `SIGTERM` every in-flight Ollama call would be cancelled mid-flight — your user would lose their result for a graceful restart. Instead, shutdown stops accepting new HTTP requests, then waits for the pool to drain. In-flight calls finish or hit the per-job timeout (`JOB_TIMEOUT_SECONDS`, default 60s).

**(c) The actual Ollama call.**

```go
// internal/ollama/ollama.go
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
    body, _ := json.Marshal(generateRequest{
        Model:  c.model,    // e.g. "phi3"
        Prompt: prompt,     // user's prompt
        Stream: false,      // we want a single full response, not a stream
    })
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := c.http.Do(req)
    ...
}
```

Wire-level, this is what hits Ollama:

```http
POST /api/generate HTTP/1.1
Host: localhost:11434
Content-Type: application/json

{"model":"phi3","prompt":"What is a goroutine?","stream":false}
```

Ollama replies with JSON like:

```json
{
  "model":"phi3",
  "created_at":"2026-05-06T...",
  "response":"A goroutine is a lightweight thread...",
  "done":true,
  ...
}
```

We only decode `response` and `done`:

```go
type generateResponse struct {
    Response string `json:"response"`
    Done     bool   `json:"done"`
}
```

Extra fields are ignored — `encoding/json` only reads the keys we name.

**(d) Write the result.** On success, `SetResult` flips status to `done` and stores the text. On error, `SetError` flips it to `failed` and stores the error string.

```go
// internal/models/models.go
func (s *JobStore) SetResult(id, response string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if j, ok := s.jobs[id]; ok {
        j.Status = StatusDone
        j.Response = response
        j.UpdatedAt = time.Now()
    }
}
```

The job pointer in the map is updated in place. Because `JobStore.Get` returns a **copy** (not a pointer), readers can't witness a torn struct — they either see the state before this lock or after, never mid-update.

**Logs from this whole sequence:**

```json
{"level":"INFO","msg":"processing","job_id":"9f0c...","user_id":"alice","worker_id":3,"time":"..."}
{"level":"INFO","msg":"job done","job_id":"9f0c...","worker_id":3,"time":"..."}
```

Or, if Ollama is down:

```json
{"level":"INFO","msg":"processing","job_id":"9f0c...","worker_id":3,"time":"..."}
{"level":"ERROR","msg":"ollama call failed","job_id":"9f0c...","error":"call ollama: dial tcp 127.0.0.1:11434: connect: connection refused","time":"..."}
```

---

## Step 4 — Client polls `GET /jobs/{id}`

```bash
curl -s http://localhost:8080/jobs/9f0c3d2e-7a1c-4d8a-9b94-3a8b0e1f2c11
```

### 4a. Path parsing — `internal/handlers/handlers.go`

```go
id := strings.TrimPrefix(r.URL.Path, "/jobs/")
if id == "" || strings.Contains(id, "/") {
    writeError(w, http.StatusNotFound, "not found")
    return
}
```

The mux is registered for the **prefix** `/jobs/`, so every URL starting with that string lands here. `TrimPrefix` extracts the ID. The `Contains(id, "/")` check rejects nested paths like `/jobs/abc/extra` — those are not valid IDs.

| URL | Result |
|-----|--------|
| `/jobs/9f0c...`           | extracts `9f0c...`, looks it up |
| `/jobs/`                  | empty id → 404 |
| `/jobs/abc/def`           | id contains `/` → 404 |
| `/jobs/unknown-uuid`      | not in store → 404 |

### 4b. Read from the store

```go
job, ok := h.store.Get(id)
if !ok {
    writeError(w, http.StatusNotFound, "job not found")
    return
}
```

`Get` takes an **RLock** — multiple GET requests can read in parallel; only writers block.

```go
// internal/models/models.go
func (s *JobStore) Get(id string) (Job, bool) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    j, ok := s.jobs[id]
    if !ok { return Job{}, false }
    return *j, true   // value copy
}
```

The value copy (`*j`) is the safety net: even if the worker mutates the underlying `*Job` a microsecond after we unlock, the copy we return is frozen.

### 4c. Reply

```go
writeJSON(w, http.StatusOK, jobResponse{
    JobID:    job.ID,
    Status:   job.Status,
    Response: job.Response,  // omitempty — only present if set
    Error:    job.Error,     // omitempty — only present if set
})
```

The four states a client will see:

```json
// queued (just enqueued, no worker yet)
{"job_id":"9f0c...","status":"queued"}

// processing (a worker has it, ollama call in flight)
{"job_id":"9f0c...","status":"processing"}

// done (success)
{"job_id":"9f0c...","status":"done","response":"A goroutine is a lightweight thread..."}

// failed (ollama errored, queue was full, etc.)
{"job_id":"9f0c...","status":"failed","error":"call ollama: dial tcp 127.0.0.1:11434: connect: connection refused"}
```

---

## Step 5 — Concurrency in action

To see the pool actually under load, fire 25 prompts at once:

```bash
for i in $(seq 1 25); do
  curl -s -X POST http://localhost:8080/generate \
       -H 'Content-Type: application/json' \
       -d "{\"user_id\":\"u$i\",\"prompt\":\"hi $i\"}" &
done
wait
```

What happens internally (assuming each Ollama call takes ~3s):

```
t=0s   25 requests arrive, 25 jobs created, all 25 sent to channel.
       Channel capacity 100, so all 25 fit immediately.
       10 workers each receive one job → 10 jobs enter "processing".
       15 jobs sit in the channel buffer with status "queued".

t=0s   All 25 POST /generate handlers return 202.

t=3s   First 10 workers finish, write results, loop back to receive again.
       10 more jobs enter "processing" (now 20 done/in-progress, 5 queued).

t=6s   Next 10 finish. The remaining 5 enter "processing".

t=9s   Last 5 finish. All workers idle, blocked on channel receive again.
```

A client polling job #20 in this scenario would see:

```
t=0s    queued
t=3s    processing
t=6s    done
```

Job #1 (early in the batch) would be `processing` immediately and `done` by t=3s.

### What "max 10 workers" actually means

The channel is the queue. Submission is bounded by **channel capacity (100)**, not worker count. Worker count only bounds **concurrent Ollama calls**. So with 10 workers and a 100-deep queue:

- 1–100 in-flight → all accepted, queued or processing
- 101st → 503 (queue full)

If you wanted to also reject when the queue starts to back up, you'd lower `queueSize` in `cmd/httpserver/main.go`.

---

## Step 6 — Backpressure: queue full

If callers submit faster than workers drain, the channel buffer fills. The 101st submission hits this branch:

```go
// internal/worker/worker.go
func (p *Pool) Submit(job *models.Job) bool {
    select {
    case p.jobs <- job:    // would block — buffer full
        return true
    default:               // taken instead
        return false
    }
}
```

Back in the handler:

```go
if !h.pool.Submit(job) {
    h.store.SetError(job.ID, "queue full")
    writeError(w, http.StatusServiceUnavailable, "server is at capacity, try again later")
    return
}
```

Two things happen:

1. The job stays in the store (it was created before `Submit`). Its status is now `failed` with `error: "queue full"`. The client *can* poll its `job_id` and see why it was rejected — useful for debugging.
2. The HTTP response is 503. A well-behaved client backs off and retries.

---

## Step 7 — Graceful shutdown

Press `Ctrl-C` (sends `SIGINT`) or `kill -TERM <pid>` (sends `SIGTERM`).

```go
// cmd/httpserver/main.go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

select {
case err := <-serverErr: ...
case <-ctx.Done():
    log.Info("shutdown signal received")
}

// 1. Stop accepting new HTTP requests.
shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout) // 15s
defer cancel()
srv.Shutdown(shutdownCtx)

// 2. Drain the worker pool.
pool.Stop()
log.Info("server stopped")
```

What each step does:

1. **`srv.Shutdown(shutdownCtx)`** — stops the listener (no new connections), waits up to 15s for in-flight HTTP handlers to finish. Note that `POST /generate` returns in milliseconds, so this almost never has to wait.
2. **`pool.Stop()`** — closes the jobs channel:
   ```go
   func (p *Pool) Stop() {
       close(p.jobs)
       p.wg.Wait()
   }
   ```
   Workers are running `for job := range p.jobs`. When the channel is closed and drained, the range loop exits, `wg.Done()` fires, and `wg.Wait()` unblocks.
3. The worker that was *currently* processing a job finishes its Ollama call (or hits its 60s timeout) before checking for the next iteration. **No in-flight work is dropped.**

**Why this order matters:** if you `pool.Stop()` first, a client that snuck a request in between `Stop()` and `Shutdown()` would see the handler call `pool.Submit(job)` on a closed channel — that panics. Stopping HTTP first guarantees no new submitters.

Logs you'll see:

```json
{"level":"INFO","msg":"shutdown signal received","time":"..."}
{"level":"INFO","msg":"worker stopped","worker_id":3,"time":"..."}
{"level":"INFO","msg":"worker stopped","worker_id":7,"time":"..."}
... (10 of these)
{"level":"INFO","msg":"server stopped","time":"..."}
```

---

## Step 8 — Errors and where they surface

| Failure                              | Surface                              | Code path                                    |
|--------------------------------------|--------------------------------------|----------------------------------------------|
| Wrong method                         | HTTP 405 with `Allow:` header        | `handlers.Generate`/`GetJob` first lines     |
| Body too large                       | HTTP 413                             | `MaxBytesReader`                             |
| Malformed JSON / unknown field       | HTTP 400                             | `dec.Decode` returns error                   |
| Empty user_id or prompt              | HTTP 400                             | post-decode validation                       |
| Queue full                           | HTTP 503; job marked `failed`        | `pool.Submit` returns false                  |
| Ollama unreachable / non-200         | Job marked `failed` (poll to see)    | `client.Generate` returns error              |
| Job timeout (>JOB_TIMEOUT_SECONDS)   | Job marked `failed`, error mentions context deadline | `context.WithTimeout` cancels the HTTP call  |
| Unknown job_id                       | HTTP 404                             | `store.Get` returns `ok=false`               |

Note the asymmetry: **HTTP errors** (400/405/413/503) are returned synchronously from `/generate`, **Ollama errors** are reported via `/jobs/{id}` polling because by the time we know about them the HTTP handler has long since returned 202.

---

## Where to go next

- Add `_test.go` files. The store and pool are easy to unit-test in isolation; the Ollama client wants an `httptest.Server`.
- Replace `JobStore` with a Redis or Postgres backing if you need persistence — the interface is just `Create / Get / UpdateStatus / SetResult / SetError`.
- If you upgrade to Go 1.22+, you can swap `internal/logger` for `log/slog` (the API is intentionally compatible) and replace the prefix-routed mux with pattern routing (`mux.HandleFunc("POST /generate", ...)` and `r.PathValue("id")`), removing the in-handler method checks.
