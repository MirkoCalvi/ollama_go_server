import { initializeApp, type FirebaseApp } from "firebase/app"
import { getAuth, GoogleAuthProvider, type Auth } from "firebase/auth"

export const DEV_MODE = import.meta.env.VITE_DEV_MODE === "1"

let appInstance: FirebaseApp | null = null
let authInstance: Auth | null = null

export function getFirebaseAuth(): Auth {
  if (DEV_MODE) {
    throw new Error("getFirebaseAuth() called in DEV_MODE — auth flow is bypassed")
  }
  if (!authInstance) {
    appInstance = initializeApp({
      apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
      authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
      projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
      appId: import.meta.env.VITE_FIREBASE_APP_ID,
    })
    authInstance = getAuth(appInstance)
  }
  return authInstance
}

export const googleProvider = new GoogleAuthProvider()
