package httpsrv

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// SessionCookie is the browser session of the built-in web UI. Its value is
// derived from the bearer token, so the browser never holds the token itself,
// and changing the token signs every browser out.
const SessionCookie = "wb_session"

// SessionValue is the cookie value that proves a browser logged in with token.
func SessionValue(token string) string {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte("workout-body web session v1"))
	return hex.EncodeToString(m.Sum(nil))
}

func validSession(r *http.Request, token string) bool {
	c, err := r.Cookie(SessionCookie)
	if err != nil || token == "" {
		return false
	}
	want := SessionValue(token)
	return len(c.Value) == len(want) && subtle.ConstantTimeCompare([]byte(c.Value), []byte(want)) == 1
}

// crossSite reports a request that a browser marked as coming from another
// site. The cookie is SameSite=Strict already; this is a second line of
// defence for state-changing requests.
func crossSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return false
	}
	return true
}

// BearerOrSession accepts the bearer token (assistants, scripts) or the web
// UI's session cookie (the built-in UI in a browser).
func BearerOrSession(token string, disabled bool, next http.Handler) http.Handler {
	bearer := Bearer(token, disabled, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if disabled || strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || !validSession(r, token) {
			bearer.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && crossSite(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LoggedIn reports whether a browser request carries a valid UI session.
func LoggedIn(r *http.Request, token string, disabled bool) bool {
	return disabled || validSession(r, token)
}
