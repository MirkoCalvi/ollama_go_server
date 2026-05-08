import { useState } from "react"
import { LoaderCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function LoginScreen({ onSignIn }: { onSignIn: () => Promise<void> }) {
  const [signingIn, setSigningIn] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handle = async () => {
    setSigningIn(true)
    setError(null)
    try {
      await onSignIn()
    } catch (e) {
      setError(e instanceof Error ? e.message : "sign-in failed")
    } finally {
      setSigningIn(false)
    }
  }

  return (
    <div className="min-h-screen w-full flex items-center justify-center p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>Talk to a character</CardTitle>
          <CardDescription>Sign in with Google to get started.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Button onClick={handle} disabled={signingIn} size="lg">
            {signingIn ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {signingIn ? "Signing in…" : "Sign in with Google"}
          </Button>
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
        </CardContent>
      </Card>
    </div>
  )
}
