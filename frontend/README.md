# frontend

React + Vite + TypeScript app for the Go [backend](../backend/README.md). Sign
in with Google via Firebase, pick a character, send a prompt, watch the job
poll to completion.

> All commands below assume `cd frontend` first.

## Quick start

```bash
cp .env.example .env.local
# edit .env.local — see "Run modes" below
npm install
npm run dev    # http://localhost:5173
```

The Vite dev server expects the backend on `VITE_API_BASE_URL` (default
`http://localhost:8080`). Boot the backend first; see
[backend/README.md](../backend/README.md) for run instructions.

## Run modes

### 1. Dev mode (no Firebase, fastest)

For iterating on UI without a Firebase account. Run the backend with
`DEV_MODE=1` (it binds loopback only and ignores the `Authorization` header)
and set `VITE_DEV_MODE=1`:

```bash
# in backend/
DEV_MODE=1 go run ./cmd/httpserver
```

```env
# in frontend/.env.local
VITE_API_BASE_URL=http://localhost:8080
VITE_DEV_MODE=1
```

The frontend skips Firebase entirely, treats the user as `dev`, and sends no
`Authorization` header. The login screen is bypassed; the prompt UI shows
immediately.

> Don't ship `VITE_DEV_MODE=1` to a real deployment — every request will get
> 401 because production runs Firebase auth.

### 2. Firebase mode (real Google sign-in)

Run the backend with a Firebase service-account JSON, fill in the Web App
config in `.env.local`:

```bash
# in backend/
FIREBASE_CREDENTIALS_FILE=~/secrets/firebase-admin.json go run ./cmd/httpserver
```

```env
# in frontend/.env.local
VITE_API_BASE_URL=http://localhost:8080
VITE_DEV_MODE=0
VITE_FIREBASE_API_KEY=...
VITE_FIREBASE_AUTH_DOMAIN=your-project.firebaseapp.com
VITE_FIREBASE_PROJECT_ID=your-project
VITE_FIREBASE_APP_ID=1:1234567890:web:abcdef
```

You get those values from **Firebase console → Project settings → Your apps →
Web app → SDK setup**. They're public; safe to commit to a `.env.example` and
ship to the browser.

If you don't have a registered Web App yet:

1. Firebase console → Project settings → Your apps → **Add app → Web**.
2. Give it a nickname (e.g. "httpserver-web"); skip Firebase Hosting.
3. Copy the `firebaseConfig` object into `.env.local` (one var per field).
4. **Authentication → Sign-in method →** enable **Google**.
5. **Authentication → Settings → Authorized domains →** make sure `localhost`
   is listed (it's there by default for new projects).

Sign-in uses `signInWithPopup` with `GoogleAuthProvider`. The popup may be
blocked the first time — allow popups for the dev origin and retry.

## Scripts

- `npm run dev` — Vite dev server with HMR.
- `npm run build` — type-check (`tsc -b`) + production build to `dist/`.
- `npm run preview` — serve `dist/` locally for a sanity check.
- `npm run lint` — ESLint.

## Source layout

```
src/
├── App.tsx                    routes between LoginScreen and the prompt UI
├── main.tsx                   React root (StrictMode wrapper)
├── index.css                  Tailwind directives + shadcn CSS variables
├── assets/                    character images (one PNG per backend character)
├── lib/
│   ├── api.ts                 typed API client (submitJob, fetchJob, fetchCharacters,
│   │                          authedFetch with VITE_DEV_MODE shortcut, ApiError)
│   ├── firebase.ts            initializeApp, GoogleAuthProvider, lazy auth instance
│   ├── character-images.ts    static name → image-URL map; missing names fall back to a letter tile
│   └── utils.ts               cn() helper (clsx + tailwind-merge)
├── hooks/
│   ├── useAuth.ts             onAuthStateChanged subscription + sign-in/out actions
│   └── useJob.ts              submit + 1s poll until done|failed; pauses when tab hidden;
│                              threads selected character through every state so JobResult
│                              can render the right avatar (the API doesn't echo it back)
└── components/
    ├── LoginScreen.tsx        centered card, "Sign in with Google" button
    ├── PromptForm.tsx         loads /characters, image-card grid for character selection,
    │                          textarea + send button
    ├── JobResult.tsx          character avatar + status pill, response card, copy + reset
    └── ui/                    shadcn-style primitives (button, card, label, textarea)
```

UI primitives in `components/ui/` follow the shadcn convention — they're
copy-paste components, not a library, so you can edit them in place. They
share the CSS variables defined in `index.css` for theming.

## How auth + polling work

The full happy-path flow:

```
1. App.tsx mounts → useAuth subscribes to onAuthStateChanged.
   ── DEV_MODE=1: instantly resolves to {uid:"dev"}; LoginScreen is skipped.
   ── otherwise: shows LoginScreen → user clicks button → signInWithPopup
       → onAuthStateChanged fires with the User → state becomes "signed-in".

2. PromptForm mounts → fetchCharacters() (public, no token) → populates <select>.

3. User submits → useJob.submit(prompt, character):
   a. authedFetch("/generate") → grabs await user.getIdToken() from Firebase
      (the SDK auto-refreshes when within ~5min of the 1h expiry) → POST.
   b. Backend returns 202 + {job_id, status:"queued"}.
   c. Hook starts setInterval(1000ms) → each tick calls authedFetch("/jobs/{id}").
   d. On status="done" or "failed": clearInterval, render JobResult.

4. document.visibilitychange (tab backgrounded) → tick is a no-op until the
   tab is visible again. Stops hammering the backend from inactive tabs.
```

`useJob` does not own a request cancellation mechanism — if you submit while a
poll is running, the previous interval is cleared and replaced. Errors during
polling stop the loop and surface via the `error` phase.

## What's not implemented

Deliberate v1 omissions, in line with the backend's scope:

- **Job history.** No backend list endpoint; the FE only knows about the
  current job. Add a route + endpoint together when needed.
- **Streaming.** Backend returns one response when the worker finishes; no
  per-token streaming. Adding it requires a real backend design change (SSE
  or WebSocket); see "Out of scope" in `backend/README.md`.
- **Retry on failure.** Failed jobs show the error and a "Try again" button
  that resets the form; there's no automatic retry.
- **Persistence across reloads.** Refreshing the page loses the in-flight
  job ID. The backend store is also in-memory, so this is symmetric.
- **Multi-character chat / conversation history.** Each prompt is a fresh
  request with no prior turns; the backend does not maintain per-user chat
  state.
