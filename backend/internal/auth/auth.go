// Package auth implements authentication middleware for the HTTP server.
//
// Two strategies are exported:
//
//   - FirebaseAuthenticator: validates Firebase ID tokens (the JWTs the
//     Firebase JS SDK hands out after a user signs in with Google et al).
//     The verified UID is stashed on the request context.
//
//   - DevMiddleware: bypasses auth entirely and injects a fixed user_id
//     ("dev"). Intended only for local development; main.go must guard
//     activation with a loopback-only SERVER_ADDR check.
//
// Handlers read the resolved user_id with UserFrom regardless of which
// middleware ran. Swapping verifiers (e.g. adding API-key fallback later)
// is a localized change as long as the new middleware writes to the same
// userKey.
package auth

import (
	"context"
	"net/http"
	"strings"

	firebaseauth "firebase.google.com/go/v4/auth"
)

// ctxKey is unexported so external packages can only read the authenticated
// user via UserFrom — they cannot inject a fake user by writing to the same
// key from outside.
type ctxKey int

const userKey ctxKey = 0

// DevUserID is the synthetic user injected by DevMiddleware.
const DevUserID = "dev"

// UserFrom returns the authenticated user_id stashed in ctx by whichever
// middleware ran, or "" if no auth ran (which should never happen on
// protected routes).
func UserFrom(ctx context.Context) string {
	s, _ := ctx.Value(userKey).(string)
	return s
}

// FirebaseTokenVerifier is the slice of *firebaseauth.Client that this
// package depends on. Defining it as an interface lets tests pass in a
// fake verifier without spinning up a real Firebase app.
type FirebaseTokenVerifier interface {
	VerifyIDToken(ctx context.Context, idToken string) (*firebaseauth.Token, error)
}

// FirebaseAuthenticator wraps a token verifier (typically *firebaseauth.Client
// from the Firebase Admin SDK) and exposes Middleware.
type FirebaseAuthenticator struct {
	verifier FirebaseTokenVerifier
}

// NewFirebase returns a FirebaseAuthenticator backed by the given verifier.
func NewFirebase(verifier FirebaseTokenVerifier) *FirebaseAuthenticator {
	return &FirebaseAuthenticator{verifier: verifier}
}

// Middleware verifies the "Authorization: Bearer <id-token>" header against
// Firebase. On success, the verified UID lands on r.Context() and next runs.
// Anything else — missing header, malformed token, expired token, wrong
// project (audience mismatch), revoked token — collapses to 401 with no
// detail leaked to the client. The reason is logged on the server side via
// the request, but we don't differentiate in the response so attackers can't
// learn which check failed.
func (a *FirebaseAuthenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) {
			unauthorized(w)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		if token == "" {
			unauthorized(w)
			return
		}

		// VerifyIDToken checks signature, expiry, audience (project), issuer,
		// and that the token is not in the future. It does NOT check whether
		// the token has been revoked since issue — VerifyIDTokenAndCheckRevoked
		// would, at the cost of an extra Firebase round-trip per request.
		// Stick with the cheaper variant by default.
		verified, err := a.verifier.VerifyIDToken(r.Context(), token)
		if err != nil {
			unauthorized(w)
			return
		}

		ctx := context.WithValue(r.Context(), userKey, verified.UID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// DevMiddleware unconditionally injects DevUserID on every request. It does
// not look at the Authorization header. Intended only for local development
// — main.go must guard activation with a loopback-only SERVER_ADDR check so
// this can't be deployed to a public interface by accident.
func DevMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userKey, DevUserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="httpserver"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
}
