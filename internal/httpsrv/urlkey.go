package httpsrv

import (
	"crypto/subtle"
	"net/http"
)

// URLKeyParam is the query parameter MCPURLKey reads.
const URLKeyParam = "key"

// MCPURLKey lets an MCP client that cannot send an Authorization header (for
// example ChatGPT's custom connector with authentication "None") reach /mcp
// with a secret in the URL: /mcp?key=<MCP_URL_KEY>.
//
// The URL key is separate from the bearer token, so it can be rotated (or
// removed) without signing out the web UI or other clients, and it unlocks
// /mcp only — never the API or the UI. A matching key is removed from the
// request and replaced with the bearer header, so the existing bearer check
// stays the single gate. A wrong key is rejected outright. An empty urlKey
// disables the feature and returns next unchanged.
func MCPURLKey(urlKey, token string, next http.Handler) http.Handler {
	if urlKey == "" {
		return next
	}
	expected := []byte(urlKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" || r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		q := r.URL.Query()
		if !q.Has(URLKeyParam) {
			next.ServeHTTP(w, r)
			return
		}
		provided := []byte(q.Get(URLKeyParam))
		if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
			unauthorized(w)
			return
		}
		q.Del(URLKeyParam)
		r2 := r.Clone(r.Context())
		r2.URL.RawQuery = q.Encode()
		r2.RequestURI = r2.URL.RequestURI()
		r2.Header.Set("Authorization", "Bearer "+token)
		next.ServeHTTP(w, r2)
	})
}
