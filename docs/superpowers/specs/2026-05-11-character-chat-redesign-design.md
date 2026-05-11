# Character chat — frontend redesign

**Date:** 2026-05-11
**Scope:** frontend UI/UX overhaul + small backend change to `/characters`

## Goal

Replace the current one-page form (character grid + textarea + result card) with two views:

1. **Homepage** — a list of all characters as `[image] [name + description]` rows. Click a row to select.
2. **Chat view** — split layout: the chosen character's image on the left, a multi-turn chat on the right. A Back button returns to the homepage and clears the conversation.

## Decisions (locked)

| # | Question | Decision |
|---|---|---|
| 1 | Where do descriptions come from? | Extend backend `/characters` to return `[{name, description}, …]`. |
| 2 | What does "chat" mean? | Multi-turn, frontend-only memory. Each `/generate` call is still independent on the backend; the UI keeps the visible message history until Back is pressed. |
| 3 | What does Back do to history? | Clears it. Re-selecting the same character starts an empty chat. |

## Architecture

Two views, swapped by a single piece of local state in `App.tsx`:

```
App state: selected: string | null
  null     → <CharacterList />              (homepage)
  "Frank"  → <ChatView character="Frank" /> (split view)
```

No router. Only two views; local state is enough. Back = `setSelected(null)`.

`LoginScreen` is unchanged. `PromptForm.tsx` and `JobResult.tsx` are deleted — their logic is absorbed into `CharacterList` and `ChatView`. `useJob` is unchanged.

## Backend change — `/characters`

`ollama.Character` gains a `Description string` field. Each personality file in
`backend/internal/ollama/personalities/*.go` sets a one-sentence blurb describing the character.

`personalities.List() []ollama.Character` is added. `personalities.Names()` may be removed if no remaining caller needs it (audit at implementation time).

`backend/internal/handlers/handlers.go` `/characters` response changes from:

```json
["August","Frank","James","Olivia","Robert"]
```

to:

```json
[
  {"name":"August","description":"…"},
  {"name":"Frank","description":"…"},
  …
]
```

A small DTO struct (`{ Name, Description }`) keeps only the public fields in the
response — the system prompt and Ollama parameters stay internal. Sorted alphabetically
by name (same stable order as today).

`/characters` remains unauthenticated (matches existing behavior; the list is not sensitive).

Frontend `fetchCharacters()` return type changes from `Promise<string[]>` to
`Promise<Character[]>` where `Character = { name: string; description: string }`.

## Frontend — Homepage (`CharacterList`)

```
┌─────────────────────────────────────────────────────────┐
│  Talk to a character                    [Sign out]      │
├─────────────────────────────────────────────────────────┤
│  ┌──────┐  August                                       │
│  │ img  │  <one-sentence description>                   │
│  └──────┘                                               │
│                                                         │
│  ┌──────┐  Frank                                        │
│  │ img  │  <one-sentence description>                   │
│  └──────┘                                               │
│  …                                                      │
└─────────────────────────────────────────────────────────┘
```

- Single column, max-width container (`max-w-2xl`, matching today's layout).
- Each row is a `button` (a11y: real button, keyboard-focusable). Whole row is clickable.
- Rounded image ~80×80px on the left; name (heading) + description (muted body) on the right.
- Loading and error states follow today's pattern (text under header).
- Selecting a row calls the `onSelect(name)` prop, which sets `App.selected`.

## Frontend — Chat view (`ChatView`)

```
┌─────────────────────────────────────────────────────────────┐
│  [← Back]    Frank                            [Sign out]    │
├──────────────────────┬──────────────────────────────────────┤
│                      │  ┌────────────────────────────────┐  │
│                      │  │ user: tell me about Tuesdays   │  │
│   ┌──────────────┐   │  └────────────────────────────────┘  │
│   │   Frank      │   │  ┌────────────────────────────────┐  │
│   │   (image)    │   │  │ Frank: eh, Tuesdays are…       │  │
│   └──────────────┘   │  └────────────────────────────────┘  │
│   <description>      │  ┌────────────────────────────────┐  │
│                      │  │ Frank is typing… ⟳             │  │
│                      │  └────────────────────────────────┘  │
│                      │  ─────────────────────────────────── │
│                      │  [ textarea…           ] [ Send ▶ ]  │
└──────────────────────┴──────────────────────────────────────┘
```

### State

```ts
type Message =
  | { role: "user"; text: string }
  | { role: "assistant"; text: string }
  | { role: "assistant"; error: string }

const [messages, setMessages] = useState<Message[]>([])
const job = useJob()
```

`useJob` (existing) drives the one currently-in-flight assistant reply. `ChatView`
orchestrates: turns are pushed onto `messages` as they complete.

### Flow

1. User types in the textarea, hits **Send** (or Cmd/Ctrl+Enter).
2. `ChatView` appends `{ role: "user", text }` to `messages`, clears the textarea,
   calls `job.submit(text, character)`.
3. While `job.state.phase === "polling"` (or `"submitting"`), a single "typing…"
   bubble is rendered at the bottom of the message list *in addition to* the
   persisted messages.
4. An effect watches `job.state.phase`:
   - `"done"` → append `{ role: "assistant", text: job.state.job.response }` and call `job.reset()`.
   - `"failed"` → append `{ role: "assistant", error: job.state.job.error ?? "unknown error" }` and call `job.reset()`.
   - `"error"` → append `{ role: "assistant", error: job.state.message }` and call `job.reset()`.
5. Send is disabled while a reply is in flight (preserves the existing
   "one job at a time" invariant — no client-side queueing).

### Behavior

- Each `/generate` request is independent (the backend has no conversation
  context). Matches decision #2.
- Auto-scroll the message list to the bottom on every append (user message,
  assistant message, typing indicator). No "user-scrolled-up" detection in v1 —
  simplest behavior; revisit if it gets annoying in practice.
- Errors render as inline assistant bubbles styled as `destructive`.
- Copy button on assistant bubbles (optional polish; keep if cheap, drop if it
  bloats the message component).

### Back

`onBack` sets `App.selected = null`. `ChatView` unmounts, so `messages` and any
in-flight `useJob` are discarded. Matches decision #3.

### Responsive

- **Desktop (≥ `sm` breakpoint):** two columns as drawn above. Left column ~⅓ width.
- **Mobile (< `sm`):** stacked — compact header with avatar + name on top,
  full-height chat below; the large image / description block is hidden.

## Files added / changed / removed

| File | Change |
|------|--------|
| `backend/internal/ollama/character.go` | add `Description string` to `Character` |
| `backend/internal/ollama/personalities/august.go` | set `Description` |
| `backend/internal/ollama/personalities/frank.go` | set `Description` |
| `backend/internal/ollama/personalities/james.go` | set `Description` |
| `backend/internal/ollama/personalities/olivia.go` | set `Description` |
| `backend/internal/ollama/personalities/robert.go` | set `Description` |
| `backend/internal/ollama/personalities/personalities.go` | add `List()`; remove `Names()` if unused after change |
| `backend/internal/handlers/handlers.go` | `/characters` returns `[]characterDTO{name, description}` |
| `frontend/src/lib/api.ts` | `fetchCharacters` returns `Character[]` |
| `frontend/src/components/CharacterList.tsx` | **new** — homepage list |
| `frontend/src/components/ChatView.tsx` | **new** — split view + multi-turn chat |
| `frontend/src/App.tsx` | holds `selected` state, routes between views |
| `frontend/src/components/PromptForm.tsx` | **delete** |
| `frontend/src/components/JobResult.tsx` | **delete** |
| `frontend/src/hooks/useJob.ts` | **unchanged** — one in-flight job at a time |

## Testing

No test suite exists in this repo (per `CLAUDE.md`). Verification is manual:

- `cd backend && DEV_MODE=1 go run ./cmd/httpserver` (loopback only)
- `cd frontend && npm run dev` (Vite on http://localhost:5173)
- Walk the golden path: load homepage → see 5 character rows with descriptions →
  click one → split view appears → send a prompt → typing indicator → assistant
  bubble appears → send another → conversation accumulates → press Back →
  homepage → re-select same character → chat is empty.
- Edge cases: backend down (load error), submission error (network failure mid-job),
  failed Ollama job (status: failed), narrow viewport (mobile layout).
- Also run `go build ./... && go vet ./...` from `backend/` before commit.

## Out of scope (explicit)

- Conversation context sent to the backend (decision #2: frontend-only memory).
- Conversation persistence across reloads or per-user storage (decision #3).
- Multi-job concurrency in the UI.
- Routing library (`react-router-dom`). Local state covers the two views.
- Streaming responses (the existing job model is async-polled; no change here).

## Risks

- **Description copy quality.** Bad blurbs make the homepage feel cheap. Treat
  description text as part of the deliverable, not boilerplate. Each one should
  capture the character's hook in a single sentence.
- **`personalities.Names()` removal.** If any other backend code still calls it,
  remove only after a grep — otherwise leave it as a thin wrapper around `List()`.
- **Mobile layout regression.** The current single-column form works on every
  width; the new split view must collapse cleanly. Verify at < 400px viewport.
