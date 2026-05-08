# FrigidLLM

A small chat-with-a-character app: a Go HTTP backend that fronts a local
[Ollama](https://ollama.com) instance with an asynchronous job queue, and a
React + Vite + TypeScript frontend that signs users in with Google via Firebase
and talks to that backend.

```
┌─────────────────────────────┐         ┌─────────────────────────────┐         ┌──────────────┐
│  frontend/  (Vite, :5173)   │         │  backend/  (Go, :8080)      │         │   Ollama     │
│                             │  HTTPS  │                             │  HTTP   │   :11434     │
│  Google sign-in (Firebase)  ├────────►│  POST /generate  ──┐        ├────────►│              │
│  POST prompt + character    │  Bearer │   ↓                │        │         │              │
│  Poll GET /jobs/{id}        │  <UID>  │  worker pool (10)  │        │         │              │
│                             │         │                    └─► chat │         │              │
└─────────────────────────────┘         └─────────────────────────────┘         └──────────────┘
```

The backend treats prompt → response as an async **job**: clients get a
`job_id` immediately and poll until status is `done` or `failed`. The frontend
hides that with a 1-second poll loop.

## Repo layout

```
.
├── backend/    Go HTTP server. Worker pool, Firebase ID-token auth, Ollama client.
│              See backend/README.md.
├── frontend/   React + Vite + TS web app. Firebase Google sign-in + job polling.
│              See frontend/README.md.
├── README.md   you are here
└── CLAUDE.md   guidance for Claude Code working in this repo
```

The two halves communicate **only** over HTTP — the frontend never imports the
backend, and vice versa. CORS is enforced backend-side (single origin, set via
`CORS_ORIGIN`, default `http://localhost:5173`).

## Quick start

You need three things running locally:

1. **Ollama** with a model pulled.
   ```bash
   ollama pull phi3
   # systemd usually starts ollama automatically; otherwise:  ollama serve
   ```

2. **Backend** (in dev mode — bypasses Firebase, binds loopback only):
   ```bash
   cd backend
   DEV_MODE=1 go run ./cmd/httpserver
   ```

3. **Frontend** (talks to the dev-mode backend, no Firebase setup needed):
   ```bash
   cd frontend
   cp .env.example .env.local         # set VITE_DEV_MODE=1 in .env.local
   npm install
   npm run dev                        # http://localhost:5173
   ```

For real Google sign-in, swap the backend to `FIREBASE_CREDENTIALS_FILE=...`
and fill in the Firebase Web App keys in `frontend/.env.local`. Both READMEs
walk through it.

## Subproject docs

- **[backend/README.md](backend/README.md)** — API contract, environment
  variables, auth (Firebase + dev mode), worker-pool concurrency model,
  graceful shutdown, intentional out-of-scope items.
- **[frontend/README.md](frontend/README.md)** — run modes, source layout,
  how the polling hook works, what's deliberately not implemented yet.

## Status

This is a personal project. There is no test suite yet, no production
deployment, and no persistence — restarting the backend loses all jobs. The
scope is deliberately small; see the "Out of scope" sections in each subREADME
for what's been considered and left out (persistence, retries, streaming,
multi-origin CORS, etc.).
