package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPURLKey(t *testing.T) {
	const token, key = "bearer-token", "url-key-0123456789abcdef0123456789abcdef"
	var gotQuery, gotAuth string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotAuth = r.URL.RawQuery, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", Bearer(token, false, inner))
	mux.Handle("/api/v1/", Bearer(token, false, inner))
	h := MCPURLKey(key, token, mux)

	cases := []struct {
		name, target, auth string
		want               int
	}{
		{"valid key", "/mcp?key=" + key, "", http.StatusNoContent},
		{"wrong key", "/mcp?key=nope", "", http.StatusUnauthorized},
		{"empty key", "/mcp?key=", "", http.StatusUnauthorized},
		{"bearer still works", "/mcp", "Bearer " + token, http.StatusNoContent},
		{"no credentials", "/mcp", "", http.StatusUnauthorized},
		{"key does not unlock API", "/api/v1/config?key=" + key, "", http.StatusUnauthorized},
		{"key does not unlock subpaths", "/mcp/x?key=" + key, "", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, c.target, strings.NewReader("{}"))
			if c.auth != "" {
				r.Header.Set("Authorization", c.auth)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Fatalf("status %d, want %d", w.Code, c.want)
			}
			if strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), token) {
				t.Fatal("response leaked a credential")
			}
		})
	}

	// The key is stripped before the handler sees the request; other params survive.
	r := httptest.NewRequest(http.MethodPost, "/mcp?a=1&key="+key, nil)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if gotQuery != "a=1" || gotAuth != "Bearer "+token {
		t.Fatalf("inner saw query %q auth %q", gotQuery, gotAuth)
	}
}

func TestMCPURLKeyDisabledWhenEmpty(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := MCPURLKey("", "token", Bearer("token", false, inner))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/mcp?key=", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", w.Code)
	}
}
