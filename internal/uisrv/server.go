// Package uisrv serves the built web UI (web/, copied into dist/ by
// scripts/build-ui.sh before `go build`) from the binary, behind a login page.
// Logging in with the bearer token sets a session cookie that the JSON API also
// accepts (httpsrv.BearerOrSession), so the browser never stores the token.
package uisrv

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/fullfran/training-mcp/internal/httpsrv"
)

//go:embed all:dist
var embedded embed.FS

// Embedded is the UI compiled into the binary (possibly only a placeholder).
func Embedded() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

const sessionDays = 180

// Server serves the UI files and the login/logout endpoints.
type Server struct {
	files    fs.FS
	token    string
	disabled bool
	index    []byte // nil when the UI was not built into the binary
	csp      string
}

var inlineScript = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// New serves files (the contents of web/dist). token is the bearer token that
// unlocks the UI; disabled mirrors MCP_AUTH_DISABLED.
func New(files fs.FS, token string, disabled bool) *Server {
	s := &Server{files: files, token: token, disabled: disabled}
	if b, err := fs.ReadFile(files, "index.html"); err == nil {
		s.index = b
	}
	// Allow exactly the inline scripts shipped in index.html (theme bootstrap).
	var hashes []string
	for _, m := range inlineScript.FindAllSubmatch(s.index, -1) {
		sum := sha256.Sum256(m[1])
		hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	s.csp = "default-src 'self'; script-src 'self' " + strings.Join(hashes, " ") +
		"; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self';" +
		" object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	return s
}

// Built reports whether a real UI (index.html) is embedded.
func (s *Server) Built() bool { return s.index != nil }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	switch r.URL.Path {
	case "/login":
		s.login(w, r)
		return
	case "/logout":
		s.logout(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !httpsrv.LoggedIn(r, s.token, s.disabled) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		} else {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
		return
	}
	if s.index == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = notBuilt.Execute(w, nil)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" || p == "index.html" {
		h.Set("Content-Security-Policy", s.csp)
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(s.index))
		return
	}
	if _, err := fs.Stat(s.files, p); err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(p, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable") // content-hashed names
	}
	http.ServeFileFS(w, r, s.files, p)
}

func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if httpsrv.LoggedIn(r, s.token, s.disabled) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		s.loginPage(w, http.StatusOK, false)
	case http.MethodPost:
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		given := []byte(strings.TrimSpace(r.PostFormValue("token")))
		want := []byte(s.token)
		if s.token == "" || len(given) != len(want) || subtle.ConstantTimeCompare(given, want) != 1 {
			time.Sleep(500 * time.Millisecond) // slow down guessing
			s.loginPage(w, http.StatusUnauthorized, true)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: httpsrv.SessionCookie, Value: httpsrv.SessionValue(s.token), Path: "/",
			MaxAge: sessionDays * 24 * 3600, HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: httpsrv.SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) loginPage(w http.ResponseWriter, status int, failed bool) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	_ = loginTmpl.Execute(w, struct{ Failed bool }{failed})
}

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="he" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark"><title>כניסה · אימונים</title>
<style>
:root{--bg:#eef1f6;--card:#fff;--text:#14171c;--muted:#5d6573;--line:#d9dee6;--accent:#b9e04a;--accent-ink:#13200a;--err:#b42318}
@media (prefers-color-scheme:dark){:root{--bg:#0f1216;--card:#181c22;--text:#eef1f5;--muted:#9aa3b2;--line:#2a313b;--err:#ff8a7a}}
*{box-sizing:border-box}body{margin:0;min-height:100dvh;display:grid;place-items:center;background:var(--bg);color:var(--text);font:16px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;padding:16px}
form{width:100%;max-width:360px;background:var(--card);border:1px solid var(--line);border-radius:20px;padding:28px 24px;display:grid;gap:14px}
h1{margin:0;font-size:22px}p{margin:0;color:var(--muted);font-size:14px}
label{font-weight:600;font-size:14px}input{width:100%;font:inherit;padding:12px 14px;border:1px solid var(--line);border-radius:12px;background:transparent;color:inherit;direction:ltr;text-align:left}
button{font:inherit;font-weight:700;padding:12px;border:0;border-radius:999px;background:var(--accent);color:var(--accent-ink);cursor:pointer}
.err{color:var(--err);font-weight:600}
</style></head><body>
<form method="post" action="/login">
<h1>כניסה</h1>
<p>הזן את הטוקן של השרת (MCP_BEARER_TOKEN). הדפדפן יישאר מחובר, והטוקן עצמו לא נשמר בו.</p>
{{if .Failed}}<p class="err" role="alert">הטוקן שגוי.</p>{{end}}
<label for="token">טוקן</label>
<input id="token" name="token" type="password" autocomplete="current-password" required autofocus>
<button type="submit">כניסה</button>
</form></body></html>`))

var notBuilt = template.Must(template.New("nb").Parse(`<!doctype html><html lang="he" dir="rtl"><head><meta charset="utf-8"><title>הממשק לא נבנה</title></head>
<body style="font-family:system-ui;padding:24px"><h1>הממשק לא נכלל בבינארי הזה</h1>
<p>בנה אותו והטמע אותו: <code dir="ltr">scripts/build-ui.sh</code> ואז <code dir="ltr">go build ./cmd/training-mcp</code>, או השתמש בתמונת ה־Docker.</p>
<p>ה־API וה־MCP עובדים כרגיל.</p></body></html>`))
