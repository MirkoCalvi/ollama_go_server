import { useState } from "react"
import { Check, CircleAlert, Copy, LoaderCircle, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { characterImageFor } from "@/lib/character-images"
import type { JobState } from "@/hooks/useJob"

type Props = {
  state: JobState
  onReset: () => void
}

export function JobResult({ state, onReset }: Props) {
  const [copied, setCopied] = useState(false)

  if (state.phase === "idle" || state.phase === "submitting") return null

  if (state.phase === "error") {
    return (
      <Card className="border-destructive/50">
        <CardHeader className="flex-row items-center gap-2 space-y-0">
          <CircleAlert className="h-4 w-4 text-destructive" />
          <CardTitle className="text-base">Request failed</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-destructive">{state.message}</p>
          <Button variant="outline" size="sm" onClick={onReset} className="mt-3">
            <RefreshCw className="h-4 w-4" /> Try again
          </Button>
        </CardContent>
      </Card>
    )
  }

  const { job, character } = state
  const isFailed = state.phase === "failed"
  const isPolling = state.phase === "polling"
  const text = job.response ?? ""
  const image = characterImageFor(character)

  const copy = async () => {
    if (!text) return
    await navigator.clipboard.writeText(text)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1500)
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div className="flex items-center gap-3">
          {image ? (
            <img
              src={image}
              alt={character}
              className="h-10 w-10 rounded-md object-cover"
            />
          ) : null}
          <div className="flex flex-col">
            <CardTitle className="text-base">{character}</CardTitle>
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <StatusPill phase={state.phase} />
              <span className="capitalize">{job.status}</span>
            </span>
          </div>
        </div>
        {!isPolling ? (
          <Button variant="ghost" size="sm" onClick={onReset}>
            <RefreshCw className="h-4 w-4" /> New prompt
          </Button>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {isPolling ? (
          <p className="text-sm text-muted-foreground">
            Job <span className="font-mono">{job.job_id.slice(0, 8)}</span> is running…
          </p>
        ) : null}
        {isFailed ? (
          <p className="text-sm text-destructive whitespace-pre-wrap">
            {job.error ?? "unknown error"}
          </p>
        ) : null}
        {text ? (
          <div className="flex flex-col gap-2">
            <pre className="whitespace-pre-wrap rounded-md border bg-muted p-3 text-sm">
              {text}
            </pre>
            <Button variant="outline" size="sm" onClick={copy} className="self-start">
              {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

function StatusPill({ phase }: { phase: JobState["phase"] }) {
  if (phase === "polling") {
    return <LoaderCircle className="h-3 w-3 animate-spin" />
  }
  if (phase === "done") {
    return <Check className="h-3 w-3 text-green-600" />
  }
  if (phase === "failed") {
    return <CircleAlert className="h-3 w-3 text-destructive" />
  }
  return null
}
