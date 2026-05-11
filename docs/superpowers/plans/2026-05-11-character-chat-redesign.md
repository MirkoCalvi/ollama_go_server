# Character Chat Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single-page form with a two-view UI — a homepage list of characters (image + name + description) and a split chat view (character image on left, multi-turn chat on right with a Back button). Includes a small backend change so `/characters` returns `{name, description}` per character.

**Architecture:** Two views in `App.tsx` swapped by local `selected: string | null` state — no router. The backend extends `ollama.Character` with a `Description` field; a tiny DTO in the handler exposes only `{name, description}` publicly. The chat view orchestrates the existing `useJob` hook: each completed (or failed) job is appended to a frontend-only `messages: Message[]` array; "Back" unmounts the view and discards everything (per spec decision: clear-on-back, frontend-only memory).

**Tech Stack:** Go 1.22+ HTTP server (backend), React + Vite + TypeScript + Tailwind + shadcn-ui primitives (frontend). No new dependencies.

**Verification strategy:** This repo has no test suite (per `CLAUDE.md`) and the spec did not introduce one. The plan uses `go build ./... && go vet ./...` and `curl` for backend verification, and `npm run dev` + browser walkthrough for frontend verification — matching the existing project style. Adding a test framework is out of scope.

**Spec:** `docs/superpowers/specs/2026-05-11-character-chat-redesign-design.md`

**Commit conventions (from `CLAUDE.md`):**
- No co-author trailers.
- Never include `CLAUDE.md` or `.claude` in commits — always `git add` explicit paths (never `git add .` or `git add -A`).
- All Go commands run from `backend/`. All `npm` commands run from `frontend/`.

---

## File Structure

### Backend
- **Modify** `backend/internal/ollama/character.go` — add `Description string` to `Character`.
- **Modify** `backend/internal/ollama/personalities/{august,frank,james,olivia,robert}.go` — set `Description` on each character literal.
- **Modify** `backend/internal/ollama/personalities/personalities.go` — replace `Names()` with `List()` returning `[]ollama.Character`.
- **Modify** `backend/internal/handlers/handlers.go` — `/characters` handler returns `[]characterDTO{Name, Description}` instead of `[]string`.

### Frontend
- **Modify** `frontend/src/lib/api.ts` — `fetchCharacters` returns `Character[]` where `Character = { name: string; description: string }`.
- **Create** `frontend/src/components/CharacterList.tsx` — homepage list (image + name + description rows).
- **Create** `frontend/src/components/ChatView.tsx` — split layout, message list, input bar, Back button, orchestrates `useJob`.
- **Modify** `frontend/src/App.tsx` — holds `selected` state; routes between `LoginScreen` / `CharacterList` / `ChatView`.
- **Delete** `frontend/src/components/PromptForm.tsx`.
- **Delete** `frontend/src/components/JobResult.tsx`.
- **Unchanged** `frontend/src/hooks/useJob.ts` — one in-flight job at a time (existing contract).

---

## Phase 1 — Backend

### Task 1: Add `Description` field to `Character`

**Files:**
- Modify: `backend/internal/ollama/character.go`

- [ ] **Step 1: Add the field**

Replace the `Character` struct definition with the version below (adds a single `Description` field). Field is documented because it travels to the public `/characters` response.

```go
// Character pairs a sampling profile with a system prompt that defines the
// persona. Concrete characters are defined in internal/ollama/personalities.
//
// Description is a short, user-facing blurb (one sentence) shown on the
// frontend homepage. It is the only persona text the public /characters
// endpoint exposes — the SystemPrompt and Parameters stay internal.
type Character struct {
	Name         string
	Description  string
	Parameters   PersonalityParameters
	SystemPrompt string
}
```

- [ ] **Step 2: Verify the module still builds**

Run from `backend/`:

```bash
go build ./... && go vet ./...
```

Expected: no output, exit 0. (Adding a struct field is backward-compatible — existing personality literals leave it as the zero value `""`.)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/ollama/character.go
git commit -m "ollama: add Description field to Character"
```

---

### Task 2: Set `Description` on each personality

**Files:**
- Modify: `backend/internal/ollama/personalities/august.go`
- Modify: `backend/internal/ollama/personalities/frank.go`
- Modify: `backend/internal/ollama/personalities/james.go`
- Modify: `backend/internal/ollama/personalities/olivia.go`
- Modify: `backend/internal/ollama/personalities/robert.go`

The blurbs below were drafted from each character's existing `SystemPrompt` — one sentence, captures the hook, fits comfortably on a card row.

- [ ] **Step 1: Set August's description**

In `august.go`, immediately after the `Name: "August",` line inside the `ollama.Character{...}` literal, insert:

```go
	Description: "Your insecure coworker — composed, polished, quietly resentful when you succeed.",
```

- [ ] **Step 2: Set Frank's description**

In `frank.go`, after `Name: "Frank",`, insert:

```go
	Description: "The permanently drunk regular at the pub — bitter, oversharing, accidentally profound.",
```

- [ ] **Step 3: Set James's description**

In `james.go`, after `Name: "Professor James",` (or whatever `Name` is set to in that file — verify before editing), insert:

```go
	Description: "Your demanding thesis supervisor — sharp, cold, almost impossible to impress.",
```

Note: the existing `Name` field in `james.go` may not match the map key `"James"` in `personalities.go`. Do NOT change `Name` in this task — that's a separate concern unrelated to this redesign. Only add `Description`.

- [ ] **Step 4: Set Olivia's description**

In `olivia.go`, after `Name: "Olivia",`, insert:

```go
	Description: "Your chill, sarcastic roommate — sleeps weird, smokes too much, sees through everything.",
```

- [ ] **Step 5: Set Robert's description**

In `robert.go`, after `Name: "Robert",`, insert:

```go
	Description: "An exhausted university TA — dry, impatient, secretly knows his stuff.",
```

- [ ] **Step 6: Verify build**

Run from `backend/`:

```bash
go build ./... && go vet ./...
```

Expected: no output, exit 0.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/ollama/personalities/august.go \
        backend/internal/ollama/personalities/frank.go \
        backend/internal/ollama/personalities/james.go \
        backend/internal/ollama/personalities/olivia.go \
        backend/internal/ollama/personalities/robert.go
git commit -m "personalities: one-line description for each character"
```

---

### Task 3: Add `personalities.List()` and update `/characters` handler

**Files:**
- Modify: `backend/internal/ollama/personalities/personalities.go`
- Modify: `backend/internal/handlers/handlers.go`

Audit confirms the only caller of `personalities.Names()` in this repo is `handlers.Characters`. After this task `Names()` is dead and we delete it.

- [ ] **Step 1: Replace `Names()` with `List()`**

Open `backend/internal/ollama/personalities/personalities.go`. Replace the `Names()` function with `List()`:

```go
// List returns the registered characters in sorted order by name. Sorted so
// the public /characters endpoint has a stable response that doesn't depend
// on map iteration order.
func List() []ollama.Character {
	out := make([]ollama.Character, 0, len(byName))
	for _, c := range byName {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
```

Leave the `Get(name)` function untouched.

- [ ] **Step 2: Update the `/characters` handler**

Open `backend/internal/handlers/handlers.go`. Find the `Characters` method (currently calls `personalities.Names()` on line 158). Replace it with:

```go
// characterDTO is the public projection of an ollama.Character. The
// SystemPrompt and Parameters are intentionally NOT exposed — the public
// /characters endpoint advertises only what a UI needs to pick a character.
type characterDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Characters handles GET /characters. Returns the list of available
// characters (name + short description) a client may pass as the "character"
// field on POST /generate. Unauthenticated — the list is not sensitive and a
// frontend needs it on its first render, before a Firebase token is available.
func (h *Handler) Characters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	chars := personalities.List()
	out := make([]characterDTO, 0, len(chars))
	for _, c := range chars {
		out = append(out, characterDTO{Name: c.Name, Description: c.Description})
	}
	writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 3: Verify build and vet**

Run from `backend/`:

```bash
go build ./... && go vet ./...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/ollama/personalities/personalities.go \
        backend/internal/handlers/handlers.go
git commit -m "characters: expose name+description via /characters"
```

---

### Task 4: Manually verify `/characters` returns the new shape

**Files:** (none — verification only)

- [ ] **Step 1: Start the server in DEV_MODE**

Run from `backend/`:

```bash
DEV_MODE=1 go run ./cmd/httpserver
```

Server listens on `127.0.0.1:8080` by default (DEV_MODE forces loopback).

- [ ] **Step 2: Curl `/characters`**

In a second terminal:

```bash
curl -s http://127.0.0.1:8080/characters | python3 -m json.tool
```

Expected output (descriptions match the strings inserted in Task 2; the `Name` field for James reflects whatever is set in `james.go`, e.g. `"Professor James"`):

```json
[
    {
        "name": "August",
        "description": "Your insecure coworker — composed, polished, quietly resentful when you succeed."
    },
    {
        "name": "Frank",
        "description": "The permanently drunk regular at the pub — bitter, oversharing, accidentally profound."
    },
    ...
]
```

If any description is missing or empty, return to Task 2 and fix the offending file.

- [ ] **Step 3: Stop the server**

`Ctrl+C` in the server terminal.

(No commit — verification only.)

---

## Phase 2 — Frontend

### Task 5: Update `fetchCharacters` return type

**Files:**
- Modify: `frontend/src/lib/api.ts`

- [ ] **Step 1: Add the `Character` type and update `fetchCharacters`**

In `frontend/src/lib/api.ts`, find the existing `fetchCharacters` function (currently returns `Promise<string[]>`) and replace it. Add the `Character` type export immediately above it:

```ts
export type Character = {
  name: string
  description: string
}

export function fetchCharacters(): Promise<Character[]> {
  // Public endpoint — no auth needed, no Authorization header.
  return fetch(`${API_BASE}/characters`).then((r) => handle<Character[]>(r))
}
```

- [ ] **Step 2: Verify nothing else in the frontend imports the old shape**

Run from `frontend/`:

```bash
grep -rn "fetchCharacters" src/
```

Expected: callers found in `src/components/PromptForm.tsx` (to be deleted in Task 9) — that's fine; it'll be removed before the type mismatch matters. No other callers.

Run a build to confirm there are no other type errors (PromptForm will continue to compile against `string[]` until we change it in Task 9, which is fine because we don't actually touch it here):

```bash
npx tsc --noEmit
```

Expected: type error from `PromptForm.tsx` because it now sees `Character[]` instead of `string[]`. This is **expected and acceptable** for this task — PromptForm is being deleted in Task 9. If you see ONLY errors originating in `src/components/PromptForm.tsx`, proceed. If you see errors in any other file, stop and investigate.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/api.ts
git commit -m "api: fetchCharacters returns Character[] with description"
```

---

### Task 6: Create `CharacterList` component

**Files:**
- Create: `frontend/src/components/CharacterList.tsx`

- [ ] **Step 1: Write the component**

Create `frontend/src/components/CharacterList.tsx` with the following contents:

```tsx
import { useEffect, useState } from "react"
import { LogOut } from "lucide-react"
import { Button } from "@/components/ui/button"
import { fetchCharacters, type Character } from "@/lib/api"
import { characterImageFor } from "@/lib/character-images"
import { cn } from "@/lib/utils"

type Props = {
  userLabel: string
  onSignOut: () => void
  onSelect: (name: string) => void
}

export function CharacterList({ userLabel, onSignOut, onSelect }: Props) {
  const [characters, setCharacters] = useState<Character[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    fetchCharacters()
      .then(setCharacters)
      .catch((e) =>
        setLoadError(e instanceof Error ? e.message : "failed to load characters"),
      )
  }, [])

  return (
    <div className="min-h-screen w-full">
      <header className="border-b">
        <div className="mx-auto flex max-w-2xl items-center justify-between px-6 py-4">
          <div className="flex flex-col">
            <span className="text-sm font-medium">Talk to a character</span>
            <span className="text-xs text-muted-foreground">{userLabel}</span>
          </div>
          <Button variant="ghost" size="sm" onClick={onSignOut}>
            <LogOut className="h-4 w-4" /> Sign out
          </Button>
        </div>
      </header>

      <main className="mx-auto flex max-w-2xl flex-col gap-3 px-6 py-8">
        {loadError ? (
          <p className="text-sm text-destructive">
            Couldn't load characters: {loadError}
          </p>
        ) : characters === null ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : (
          characters.map((c) => (
            <CharacterRow key={c.name} character={c} onSelect={() => onSelect(c.name)} />
          ))
        )}
      </main>
    </div>
  )
}

type RowProps = {
  character: Character
  onSelect: () => void
}

function CharacterRow({ character, onSelect }: RowProps) {
  const image = characterImageFor(character.name)
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "group flex w-full items-center gap-4 rounded-md border bg-card p-3 text-left transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        "hover:border-foreground/30",
      )}
    >
      {image ? (
        <img
          src={image}
          alt=""
          className="h-20 w-20 shrink-0 rounded-md object-cover"
          loading="lazy"
        />
      ) : (
        <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-md bg-muted text-2xl font-semibold text-muted-foreground">
          {character.name.slice(0, 1)}
        </div>
      )}
      <div className="flex flex-col gap-1">
        <span className="text-base font-medium">{character.name}</span>
        <span className="text-sm text-muted-foreground">{character.description}</span>
      </div>
    </button>
  )
}
```

- [ ] **Step 2: Verify it type-checks in isolation**

Run from `frontend/`:

```bash
npx tsc --noEmit
```

Expected: the only remaining errors are still in `src/components/PromptForm.tsx` (same as Task 5). If `CharacterList.tsx` produces an error, fix before continuing.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/CharacterList.tsx
git commit -m "frontend: add CharacterList homepage component"
```

---

### Task 7: Create `ChatView` component

**Files:**
- Create: `frontend/src/components/ChatView.tsx`

- [ ] **Step 1: Write the component**

Create `frontend/src/components/ChatView.tsx` with the following contents. The component owns the `messages` array and an effect that watches `useJob`'s phase transitions to append a new message + reset the hook for the next turn.

```tsx
import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { ArrowLeft, CircleAlert, LoaderCircle, LogOut, Send } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { characterImageFor } from "@/lib/character-images"
import { cn } from "@/lib/utils"
import { useJob } from "@/hooks/useJob"

type Message =
  | { role: "user"; text: string }
  | { role: "assistant"; text: string }
  | { role: "assistant"; error: string }

type Props = {
  character: string
  description: string
  userLabel: string
  onBack: () => void
  onSignOut: () => void
}

export function ChatView({ character, description, userLabel, onBack, onSignOut }: Props) {
  const job = useJob()
  const [messages, setMessages] = useState<Message[]>([])
  const [draft, setDraft] = useState("")
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const image = characterImageFor(character)

  const busy = job.state.phase === "submitting" || job.state.phase === "polling"

  // Watch useJob's phase: each terminal transition becomes one assistant
  // message appended to the chat, then useJob is reset so the next turn can
  // start fresh.
  useEffect(() => {
    if (job.state.phase === "done") {
      const text = job.state.job.response ?? ""
      setMessages((m) => [...m, { role: "assistant", text }])
      job.reset()
    } else if (job.state.phase === "failed") {
      const err = job.state.job.error ?? "unknown error"
      setMessages((m) => [...m, { role: "assistant", error: err }])
      job.reset()
    } else if (job.state.phase === "error") {
      const err = job.state.message
      setMessages((m) => [...m, { role: "assistant", error: err }])
      job.reset()
    }
  }, [job.state.phase])

  // Auto-scroll on every message-list change (including the typing
  // indicator). No "user-scrolled-up" detection in v1 — simplest behavior.
  useLayoutEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight })
  }, [messages.length, busy])

  const send = () => {
    const text = draft.trim()
    if (!text || busy) return
    setMessages((m) => [...m, { role: "user", text }])
    setDraft("")
    job.submit(text, character)
  }

  const onKeyDown: React.KeyboardEventHandler<HTMLTextAreaElement> = (e) => {
    // Cmd/Ctrl+Enter sends. Plain Enter inserts a newline (default).
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault()
      send()
    }
  }

  return (
    <div className="flex min-h-screen w-full flex-col">
      <header className="border-b">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between px-6 py-4">
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={onBack}>
              <ArrowLeft className="h-4 w-4" /> Back
            </Button>
            <span className="text-sm font-medium">{character}</span>
          </div>
          <div className="flex items-center gap-3">
            <span className="hidden text-xs text-muted-foreground sm:inline">
              {userLabel}
            </span>
            <Button variant="ghost" size="sm" onClick={onSignOut}>
              <LogOut className="h-4 w-4" /> Sign out
            </Button>
          </div>
        </div>
      </header>

      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-4 px-6 py-6 sm:flex-row">
        {/* Left: character pane (hidden on mobile) */}
        <aside className="hidden w-1/3 shrink-0 flex-col gap-3 sm:flex">
          {image ? (
            <img
              src={image}
              alt={character}
              className="aspect-square w-full rounded-md border object-cover"
            />
          ) : (
            <div className="flex aspect-square w-full items-center justify-center rounded-md border bg-muted text-4xl font-semibold text-muted-foreground">
              {character.slice(0, 1)}
            </div>
          )}
          <div className="flex flex-col gap-1">
            <span className="text-base font-medium">{character}</span>
            <span className="text-sm text-muted-foreground">{description}</span>
          </div>
        </aside>

        {/* Right: chat */}
        <section className="flex min-h-0 flex-1 flex-col gap-3">
          <div
            ref={scrollRef}
            className="flex min-h-[400px] flex-1 flex-col gap-2 overflow-y-auto rounded-md border bg-card p-3"
          >
            {messages.length === 0 && !busy ? (
              <p className="m-auto text-sm text-muted-foreground">
                Say something to start the conversation.
              </p>
            ) : null}
            {messages.map((m, i) => (
              <MessageBubble key={i} message={m} character={character} />
            ))}
            {busy ? <TypingBubble character={character} /> : null}
          </div>

          <div className="flex items-end gap-2">
            <Textarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={onKeyDown}
              placeholder={`Message ${character}…`}
              rows={2}
              disabled={busy}
              className="flex-1"
            />
            <Button onClick={send} disabled={busy || !draft.trim()}>
              {busy ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Send className="h-4 w-4" />
              )}
              Send
            </Button>
          </div>
        </section>
      </main>
    </div>
  )
}

function MessageBubble({
  message,
  character,
}: {
  message: Message
  character: string
}) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="max-w-[80%] rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground whitespace-pre-wrap">
          {message.text}
        </div>
      </div>
    )
  }
  if ("error" in message) {
    return (
      <div className="flex justify-start">
        <div
          className={cn(
            "flex max-w-[80%] items-start gap-2 rounded-md border border-destructive/50 px-3 py-2",
            "text-sm text-destructive whitespace-pre-wrap",
          )}
        >
          <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <span>
            <span className="sr-only">{character} (error): </span>
            {message.error}
          </span>
        </div>
      </div>
    )
  }
  return (
    <div className="flex justify-start">
      <div className="max-w-[80%] rounded-md border bg-muted px-3 py-2 text-sm whitespace-pre-wrap">
        <span className="sr-only">{character}: </span>
        {message.text}
      </div>
    </div>
  )
}

function TypingBubble({ character }: { character: string }) {
  return (
    <div className="flex justify-start">
      <div className="flex max-w-[80%] items-center gap-2 rounded-md border bg-muted px-3 py-2 text-sm text-muted-foreground">
        <LoaderCircle className="h-3 w-3 animate-spin" />
        {character} is typing…
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify type-check**

Run from `frontend/`:

```bash
npx tsc --noEmit
```

Expected: only the pre-existing `PromptForm.tsx` errors remain. `ChatView.tsx` and its consumers compile clean.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ChatView.tsx
git commit -m "frontend: add ChatView split layout + multi-turn chat"
```

---

### Task 8: Rewire `App.tsx` to switch between views

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Replace `App.tsx` contents**

Replace the entire contents of `frontend/src/App.tsx` with:

```tsx
import { useEffect, useState } from "react"
import { LoaderCircle } from "lucide-react"
import { CharacterList } from "@/components/CharacterList"
import { ChatView } from "@/components/ChatView"
import { LoginScreen } from "@/components/LoginScreen"
import { useAuth } from "@/hooks/useAuth"
import { fetchCharacters, type Character } from "@/lib/api"

export default function App() {
  const auth = useAuth()
  const [selected, setSelected] = useState<string | null>(null)
  const [characters, setCharacters] = useState<Character[] | null>(null)

  // App fetches the character list once so ChatView can look up the selected
  // character's description without re-fetching. CharacterList re-fetches
  // independently in its own effect (cheap; happens once per homepage visit).
  useEffect(() => {
    if (auth.state.status !== "signed-in") return
    fetchCharacters()
      .then(setCharacters)
      .catch(() => {
        // Silent fallback: ChatView will display an empty description rather
        // than blocking the chat flow on a metadata fetch failure.
      })
  }, [auth.state.status])

  if (auth.state.status === "loading") {
    return (
      <div className="min-h-screen w-full flex items-center justify-center">
        <LoaderCircle className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (auth.state.status === "signed-out") {
    return <LoginScreen onSignIn={auth.signIn} />
  }

  const user = auth.state.user
  const userLabel = user.displayName ?? user.email ?? user.uid

  if (selected === null) {
    return (
      <CharacterList
        userLabel={userLabel}
        onSignOut={auth.signOut}
        onSelect={setSelected}
      />
    )
  }

  const description =
    characters?.find((c) => c.name === selected)?.description ?? ""

  return (
    <ChatView
      character={selected}
      description={description}
      userLabel={userLabel}
      onBack={() => setSelected(null)}
      onSignOut={auth.signOut}
    />
  )
}
```

- [ ] **Step 2: Type-check**

Run from `frontend/`:

```bash
npx tsc --noEmit
```

Expected: only the `PromptForm.tsx` errors remain (they're cleared in Task 9).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "frontend: route App between CharacterList and ChatView"
```

---

### Task 9: Delete `PromptForm` and `JobResult`

**Files:**
- Delete: `frontend/src/components/PromptForm.tsx`
- Delete: `frontend/src/components/JobResult.tsx`

- [ ] **Step 1: Remove the files**

```bash
rm frontend/src/components/PromptForm.tsx frontend/src/components/JobResult.tsx
```

- [ ] **Step 2: Confirm nothing imports them**

Run from `frontend/`:

```bash
grep -rn "PromptForm\|JobResult" src/
```

Expected: no matches. If there are matches, an earlier task missed a reference — fix it.

- [ ] **Step 3: Type-check is now clean**

```bash
npx tsc --noEmit
```

Expected: no output, exit 0.

- [ ] **Step 4: Run the lint/build the project actually uses**

```bash
npm run build
```

Expected: succeeds. (If the project uses a different build/lint command, run that — `package.json`'s `scripts` block is authoritative.)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/PromptForm.tsx frontend/src/components/JobResult.tsx
git commit -m "frontend: remove old PromptForm and JobResult"
```

(Note: `git add` on a deleted file stages the deletion.)

---

### Task 10: Manual end-to-end verification

**Files:** (none — verification only)

Verifies the spec's golden path in a real browser. No commit at the end.

- [ ] **Step 1: Start backend**

In one terminal, from `backend/`:

```bash
DEV_MODE=1 go run ./cmd/httpserver
```

(DEV_MODE bypasses Firebase auth and forces loopback binding. Ollama must be running with the configured model pulled — `ollama pull phi3` if it isn't already.)

- [ ] **Step 2: Start frontend**

In a second terminal, from `frontend/`:

```bash
npm run dev
```

Expected: Vite reports listening on `http://localhost:5173`.

- [ ] **Step 3: Walk the golden path**

Open `http://localhost:5173` in a browser. Verify:

1. **Login → homepage:** Sign in (DEV_MODE accepts anything; the Firebase login flow may still be presented depending on `lib/firebase.ts`'s DEV branch — match whatever the existing app does).
2. **Homepage shows 5 character rows.** Each row has an image, a name, and a description sentence. The descriptions match those set in Task 2.
3. **Click a character (e.g. Frank).** The page transitions to the split layout: Frank's image and description on the left, an empty chat area + textarea on the right.
4. **Send a prompt.** The user message bubble appears immediately on the right; a "Frank is typing…" indicator follows; when the job completes, the assistant bubble replaces the indicator.
5. **Send a second prompt.** Both prior turns remain visible; the new pair is appended.
6. **Click Back.** Returns to the homepage.
7. **Re-select Frank.** The chat is empty — history was cleared (matches spec decision: clear on Back).
8. **Click a different character (e.g. Olivia).** The left pane updates to Olivia's image and description.

- [ ] **Step 4: Check edge cases**

- **Narrow viewport:** Resize the browser to < 640px (the `sm` breakpoint). The left character pane should hide; the chat fills the width.
- **Empty input:** With an empty textarea, the Send button is disabled.
- **Backend down:** Stop the backend (`Ctrl+C` in its terminal). Send a message. The error should appear as a destructive-styled assistant bubble. Restart the backend. The next message succeeds.

- [ ] **Step 5: Stop both servers**

`Ctrl+C` in both terminals.

(No commit — verification only. If any step failed, return to the relevant earlier task and fix.)

---

## Self-Review Notes (written as part of the plan, kept for reference)

- **Spec coverage:** Each spec section maps to a task:
  - Backend `/characters` shape → Tasks 1, 2, 3, 4.
  - Homepage (`CharacterList`) → Task 6.
  - Chat view (`ChatView`) including state, flow, behavior, Back, responsive → Task 7.
  - App routing → Task 8.
  - File deletions → Task 9.
  - Testing/verification → Tasks 4, 10.
- **Placeholders:** None. Every code step contains the actual code; every command has expected output.
- **Type consistency:** `Character` is defined in `api.ts` (Task 5), consumed in `CharacterList` (Task 6) and `App` (Task 8). `Message` is local to `ChatView` (Task 7). `useJob` is unchanged.
- **The `Name` field in `james.go` does not match the map key `"James"`.** Task 2 explicitly leaves this alone — it's a pre-existing inconsistency unrelated to this redesign; flagging it here so a reviewer doesn't get confused. If it bothers you, file a separate cleanup.
