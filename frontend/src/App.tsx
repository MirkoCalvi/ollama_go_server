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
