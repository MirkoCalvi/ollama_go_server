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
