import { useCallback, useEffect, useRef, useState } from "react"
import { ApiError, fetchJob, submitJob, type JobResponse } from "@/lib/api"

const POLL_INTERVAL_MS = 1000

// `character` is threaded through every non-idle phase because the backend
// JobResponse doesn't echo the chosen character back — the UI keeps it
// client-side so JobResult can render the right avatar.
export type JobState =
  | { phase: "idle" }
  | { phase: "submitting"; character: string }
  | { phase: "polling"; job: JobResponse; character: string }
  | { phase: "done"; job: JobResponse; character: string }
  | { phase: "failed"; job: JobResponse; character: string }
  | { phase: "error"; message: string; character: string }

function isTerminal(status: JobResponse["status"]): boolean {
  return status === "done" || status === "failed"
}

export function useJob() {
  const [state, setState] = useState<JobState>({ phase: "idle" })
  const timerRef = useRef<number | null>(null)
  const jobIdRef = useRef<string | null>(null)
  const characterRef = useRef<string>("")

  const stopPolling = useCallback(() => {
    if (timerRef.current !== null) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const tick = useCallback(async () => {
    const id = jobIdRef.current
    if (!id || document.hidden) return
    const character = characterRef.current
    try {
      const job = await fetchJob(id)
      if (job.status === "done") {
        stopPolling()
        setState({ phase: "done", job, character })
      } else if (job.status === "failed") {
        stopPolling()
        setState({ phase: "failed", job, character })
      } else {
        setState({ phase: "polling", job, character })
      }
    } catch (err) {
      stopPolling()
      setState({
        phase: "error",
        message: err instanceof ApiError ? err.message : "network error",
        character,
      })
    }
  }, [stopPolling])

  useEffect(() => {
    return () => stopPolling()
  }, [stopPolling])

  const submit = useCallback(
    async (prompt: string, character: string) => {
      stopPolling()
      characterRef.current = character
      setState({ phase: "submitting", character })
      try {
        const job = await submitJob(prompt, character)
        jobIdRef.current = job.job_id
        if (isTerminal(job.status)) {
          setState({
            phase: job.status === "done" ? "done" : "failed",
            job,
            character,
          })
          return
        }
        setState({ phase: "polling", job, character })
        timerRef.current = window.setInterval(tick, POLL_INTERVAL_MS)
      } catch (err) {
        setState({
          phase: "error",
          message: err instanceof ApiError ? err.message : "network error",
          character,
        })
      }
    },
    [stopPolling, tick],
  )

  const reset = useCallback(() => {
    stopPolling()
    jobIdRef.current = null
    characterRef.current = ""
    setState({ phase: "idle" })
  }, [stopPolling])

  return { state, submit, reset }
}
