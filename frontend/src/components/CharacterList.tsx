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
