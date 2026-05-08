import { LoaderCircle, LogOut } from "lucide-react"
import { Button } from "@/components/ui/button"
import { JobResult } from "@/components/JobResult"
import { LoginScreen } from "@/components/LoginScreen"
import { PromptForm } from "@/components/PromptForm"
import { useAuth } from "@/hooks/useAuth"
import { useJob } from "@/hooks/useJob"

export default function App() {
  const auth = useAuth()
  const job = useJob()

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
  const busy = job.state.phase === "submitting" || job.state.phase === "polling"

  return (
    <div className="min-h-screen w-full">
      <header className="border-b">
        <div className="mx-auto flex max-w-2xl items-center justify-between px-6 py-4">
          <div className="flex flex-col">
            <span className="text-sm font-medium">Talk to a character</span>
            <span className="text-xs text-muted-foreground">
              {user.displayName ?? user.email ?? user.uid}
            </span>
          </div>
          <Button variant="ghost" size="sm" onClick={auth.signOut}>
            <LogOut className="h-4 w-4" /> Sign out
          </Button>
        </div>
      </header>

      <main className="mx-auto flex max-w-2xl flex-col gap-6 px-6 py-8">
        <PromptForm busy={busy} onSubmit={job.submit} />
        <JobResult state={job.state} onReset={job.reset} />
      </main>
    </div>
  )
}
