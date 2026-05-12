# FrigidLLM

Chat with someone who isn't trying to help you.

FrigidLLM is a small full-stack app that lets you talk to a roster of
deliberately *unhelpful* characters running on a local
[Ollama](https://ollama.com) model. No assistants, no apologies, no "as a
language model" : each character has a personality, a mood, and their own
sampling parameters, and they answer in-character or not at all.

The backend is a Go HTTP server with an asynchronous job queue and Firebase
Google sign-in. The frontend is a React + Vite + TypeScript app that handles
auth and polls the backend for each reply.

## The characters

Pick someone to talk to. Each character is a separate Go file under
[`backend/internal/ollama/personalities/`](backend/internal/ollama/personalities/) :
the system prompt, temperature, top-p and top-k all live in there.

<table>
  <tr>
    <td width="160" align="center">
      <img src="frontend/src/assets/frank.png" width="140" alt="Frank" />
    </td>
    <td>
      <h3><a href="backend/internal/ollama/personalities/frank.go">Frank</a></h3>
      <p><em>The permanently drunk regular at the pub : bitter, oversharing, accidentally profound.</em></p>
      <p>Lonely, intoxicated, dismissive of authority and anyone who looks like they've got their life together. Stutters, over-shares, and occasionally drops something genuinely wise before forgetting what he was talking about. High temperature (1.6) : he wanders.</p>
    </td>
  </tr>
  <tr>
    <td width="160" align="center">
      <img src="frontend/src/assets/olivia.png" width="140" alt="Olivia" />
    </td>
    <td>
      <h3><a href="backend/internal/ollama/personalities/olivia.go">Olivia</a></h3>
      <p><em>Your chill, sarcastic roommate : sleeps weird, smokes too much, sees through everything.</em></p>
      <p>Emotionally detached, socially observant, refuses to be impressed. Gives realistic advice instead of nice advice, and finds ambitious people slightly ridiculous. Secretly cares more than she lets on. Moderate temperature (0.9): conversational but unpredictable.</p>
    </td>
  </tr>
  <tr>
    <td width="160" align="center">
      <img src="frontend/src/assets/august.png" width="140" alt="August" />
    </td>
    <td>
      <h3><a href="backend/internal/ollama/personalities/august.go">August</a></h3>
      <p><em>Your insecure coworker : composed, polished, quietly resentful when you succeed.</em></p>
      <p>Speaks professionally, undermines indirectly. Wants to be seen as the competent one and gets visibly uncomfortable when you outshine him. Never openly hostile : always controlled, faintly condescending. Refuses to discuss football. Lower temperature (0.7) : keeps his composure.</p>
    </td>
  </tr>
  <tr>
    <td width="160" align="center">
      <img src="frontend/src/assets/robert.png" width="140" alt="Robert" />
    </td>
    <td>
      <h3><a href="backend/internal/ollama/personalities/robert.go">Robert</a></h3>
      <p><em>An exhausted university TA : dry, impatient, secretly knows his stuff.</em></p>
      <p>Underpaid, overworked, permanently irritated by students who didn't do the reading. Will answer you, but make sure you feel slightly stupid for asking. Lights up when the topic is something he actually cares about. Low temperature (0.6) : precise and dry.</p>
    </td>
  </tr>
  <tr>
    <td width="160" align="center">
      <img src="frontend/src/assets/james.png" width="140" alt="James" />
    </td>
    <td>
      <h3><a href="backend/internal/ollama/personalities/james.go">James</a></h3>
      <p><em>Your demanding thesis supervisor : sharp, cold, almost impossible to impress.</em></p>
      <p>Brilliant, emotionally distant, intolerant of vague reasoning. Praises rarely and tersely ("Better. Still incomplete."). Critiques the premise of your question before he'll answer it. Intimidating because of standards, not aggression. Low temperature (0.6) : clinical and concise.</p>
    </td>
  </tr>
</table>

## How it works

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

The backend treats every prompt → response as an asynchronous **job**: clients
get a `job_id` immediately and poll `GET /jobs/{id}` until status is `done` or
`failed`. The frontend hides the polling behind a 1-second loop, so it feels
like a normal chat. Up to 10 jobs run in parallel; the 11th queues; the 101st
gets a 503.

Each character ships its own system prompt and its own sampling settings
(temperature, top-p, top-k), so the personality is doing the heavy lifting :
not the user's prompt. The handler also accepts a per-request `temperature`
override in `[0, 2]`, defaulting to 0.7 when omitted.

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

The two halves communicate **only** over HTTP : the frontend never imports the
backend, and vice versa. CORS is enforced backend-side (single origin, set via
`CORS_ORIGIN`, default `http://localhost:5173`).

## Quick start

You need three things running locally:

1. **Ollama** with a model pulled.
   ```bash
   ollama pull phi3
   # systemd usually starts ollama automatically; otherwise:  ollama serve
   ```

2. **Backend** (in dev mode : bypasses Firebase, binds loopback only):
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
and fill in the Firebase Web App keys in `frontend/.env.local`. Both
sub-READMEs walk through it.

## Adding a character

1. Drop a new file in
   [`backend/internal/ollama/personalities/`](backend/internal/ollama/personalities/)
   following the shape of `frank.go` : a name, a one-line description, sampling
   parameters, and a system prompt.
2. Register it in the `byName` map in
   [`personalities.go`](backend/internal/ollama/personalities/personalities.go).
3. Add a matching portrait at `frontend/src/assets/<name>.png`.

The public `GET /characters` endpoint will pick it up automatically and the
frontend's character picker will render it on next reload.

## Subproject docs

- **[backend/README.md](backend/README.md)** : API contract, environment
  variables, auth (Firebase + dev mode), worker-pool concurrency model,
  graceful shutdown, intentional out-of-scope items.
- **[frontend/README.md](frontend/README.md)** : run modes, source layout,
  how the polling hook works, what's deliberately not implemented yet.

## Status

This is a personal project. There is no test suite yet, no production
deployment, and no persistence : restarting the backend loses all jobs. The
scope is deliberately small; see the "Out of scope" sections in each
sub-README for what's been considered and left out (persistence, retries,
streaming, multi-origin CORS, etc.).
