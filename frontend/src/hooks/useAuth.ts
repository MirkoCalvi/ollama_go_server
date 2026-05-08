import { useEffect, useState } from "react"
import {
  onAuthStateChanged,
  signInWithPopup,
  signOut,
  type User,
} from "firebase/auth"
import { DEV_MODE, getFirebaseAuth, googleProvider } from "@/lib/firebase"

export type AuthState =
  | { status: "loading" }
  | { status: "signed-out" }
  | { status: "signed-in"; user: { uid: string; displayName: string | null; email: string | null } }

const DEV_USER = {
  status: "signed-in" as const,
  user: { uid: "dev", displayName: "Dev User", email: null },
}

export function useAuth(): {
  state: AuthState
  signIn: () => Promise<void>
  signOut: () => Promise<void>
} {
  const [state, setState] = useState<AuthState>(DEV_MODE ? DEV_USER : { status: "loading" })

  useEffect(() => {
    if (DEV_MODE) return
    return onAuthStateChanged(getFirebaseAuth(), (user: User | null) => {
      setState(
        user
          ? {
              status: "signed-in",
              user: { uid: user.uid, displayName: user.displayName, email: user.email },
            }
          : { status: "signed-out" },
      )
    })
  }, [])

  return {
    state,
    signIn: async () => {
      if (DEV_MODE) return
      await signInWithPopup(getFirebaseAuth(), googleProvider)
    },
    signOut: async () => {
      if (DEV_MODE) return
      await signOut(getFirebaseAuth())
    },
  }
}
