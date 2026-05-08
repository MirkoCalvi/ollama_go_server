import { useEffect, useState } from "react"
import { Check, LoaderCircle, Send } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { fetchCharacters } from "@/lib/api"
import { characterImageFor } from "@/lib/character-images"
import { cn } from "@/lib/utils"

type Props = {
  busy: boolean
  onSubmit: (prompt: string, character: string) => void
}

export function PromptForm({ busy, onSubmit }: Props) {
  const [characters, setCharacters] = useState<string[] | null>(null)
  const [character, setCharacter] = useState<string>("")
  const [prompt, setPrompt] = useState<string>("")
  const [loadError, setLoadError] = useState<string | null>(null)

  useEffect(() => {
    fetchCharacters()
      .then((list) => {
        setCharacters(list)
        setCharacter(list[0] ?? "")
      })
      .catch((e) => setLoadError(e instanceof Error ? e.message : "failed to load characters"))
  }, [])

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!prompt.trim() || !character) return
    onSubmit(prompt.trim(), character)
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <Label>Character</Label>
        {loadError ? (
          <p className="text-sm text-destructive">Couldn't load characters: {loadError}</p>
        ) : characters === null ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : (
          <div
            role="radiogroup"
            aria-label="Character"
            className="grid grid-cols-2 gap-3 sm:grid-cols-3"
          >
            {characters.map((c) => (
              <CharacterCard
                key={c}
                name={c}
                selected={c === character}
                disabled={busy}
                onSelect={() => setCharacter(c)}
              />
            ))}
          </div>
        )}
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="prompt">Prompt</Label>
        <Textarea
          id="prompt"
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="Ask something…"
          rows={5}
          disabled={busy}
          required
        />
      </div>

      <Button type="submit" disabled={busy || !prompt.trim() || !character}>
        {busy ? (
          <LoaderCircle className="h-4 w-4 animate-spin" />
        ) : (
          <Send className="h-4 w-4" />
        )}
        {busy ? "Working…" : "Send"}
      </Button>
    </form>
  )
}

type CardProps = {
  name: string
  selected: boolean
  disabled: boolean
  onSelect: () => void
}

function CharacterCard({ name, selected, disabled, onSelect }: CardProps) {
  const image = characterImageFor(name)
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      disabled={disabled}
      onClick={onSelect}
      className={cn(
        "group relative overflow-hidden rounded-md border bg-card text-left transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        "disabled:cursor-not-allowed disabled:opacity-50",
        selected ? "border-primary ring-2 ring-primary" : "hover:border-foreground/30",
      )}
    >
      {image ? (
        <img
          src={image}
          alt=""
          className="aspect-square w-full object-cover"
          loading="lazy"
        />
      ) : (
        <div className="flex aspect-square w-full items-center justify-center bg-muted text-3xl font-semibold text-muted-foreground">
          {name.slice(0, 1)}
        </div>
      )}
      <div className="flex items-center justify-between gap-2 px-3 py-2 text-sm font-medium">
        <span>{name}</span>
        {selected ? <Check className="h-4 w-4 text-primary" /> : null}
      </div>
    </button>
  )
}
