import { DEV_MODE, getFirebaseAuth } from "@/lib/firebase"

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080"

export type JobStatus = "queued" | "processing" | "done" | "failed"

export type JobResponse = {
  job_id: string
  status: JobStatus
  response?: string
  error?: string
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
    this.name = "ApiError"
  }
}

async function authedFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...((init.headers as Record<string, string>) ?? {}),
  }
  if (!DEV_MODE) {
    const user = getFirebaseAuth().currentUser
    if (!user) throw new ApiError(401, "not signed in")
    // Firebase JS SDK refreshes the token automatically when within ~5 min of expiry.
    headers.Authorization = `Bearer ${await user.getIdToken()}`
  }
  return fetch(`${API_BASE}${path}`, { ...init, headers })
}

async function handle<T>(res: Response): Promise<T> {
  if (res.ok) return res.json() as Promise<T>
  let msg = res.statusText
  try {
    const body = (await res.json()) as { error?: string }
    if (body.error) msg = body.error
  } catch {
    // body wasn't JSON; keep statusText
  }
  throw new ApiError(res.status, msg)
}

export function fetchCharacters(): Promise<string[]> {
  // Public endpoint — no auth needed, no Authorization header.
  return fetch(`${API_BASE}/characters`).then((r) => handle<string[]>(r))
}

export function submitJob(prompt: string, character: string): Promise<JobResponse> {
  return authedFetch("/generate", {
    method: "POST",
    body: JSON.stringify({ prompt, character }),
  }).then((r) => handle<JobResponse>(r))
}

export function fetchJob(id: string): Promise<JobResponse> {
  return authedFetch(`/jobs/${encodeURIComponent(id)}`).then((r) => handle<JobResponse>(r))
}
