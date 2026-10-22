package uisrv

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fullfran/training-mcp/internal/httpsrv"
)

const token = "0123456789abcdef0123456789abcdef"

var files = fstest.MapFS{
	"index.html":       {Data: []byte(`<!doctype html><html><head><script>document.documentElement.dataset.theme="light"</script></head><body><div id="root"></div><script type="module" src="/assets/app-1.js"></script></body></html>`)},
	"assets/app-1.js":  {Data: []byte(`console.log("app")`)},
	"assets/app-1.css": {Data: []byte(`body{}`)},
}

func newServer(t *testing.T, fsys fstest.MapFS) (*httptest.Server, *http.Client) {
	t.Helper()
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"ok":true}`) })
	mcp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "mcp") })
	srv := httptest.NewServer(httpsrv.RoutesWithUI(mcp, api, New(fsys, token, false), false, token, nil))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return srv, c
}

func get(t *testing.T, c *http.Client, u string, hdr ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", u, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func login(t *testing.T, c *http.Client, base, tok string) *http.Response {
	t.Helper()
	res, err := c.PostForm(base+"/login", url.Values{"token": {tok}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestLoginGatesTheUIAndTheSessionUnlocksTheAPI(t *testing.T) {
	srv, c := newServer(t, files)
	if res, _ := get(t, c, srv.URL+"/"); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/login" {
		t.Fatalf("anonymous / must redirect to the login page: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if res, _ := get(t, c, srv.URL+"/assets/app-1.js"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous asset: %d", res.StatusCode)
	}
	if res, _ := get(t, c, srv.URL+"/api/v1/x"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous API: %d", res.StatusCode)
	}
	res, body := get(t, c, srv.URL+"/login")
	if res.StatusCode != 200 || !strings.Contains(body, `name="token"`) || !strings.Contains(res.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("login page: %d %s", res.StatusCode, body)
	}
	if r := login(t, c, srv.URL, "wrong"); r.StatusCode != http.StatusUnauthorized || len(r.Cookies()) != 0 {
		t.Fatalf("wrong token: %d %v", r.StatusCode, r.Cookies())
	}
	r := login(t, c, srv.URL, token)
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" {
		t.Fatalf("login: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	ck := r.Cookies()[0]
	if ck.Name != httpsrv.SessionCookie || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || strings.Contains(ck.Value, token) {
		t.Fatalf("cookie: %+v", ck)
	}
	res, body = get(t, c, srv.URL+"/")
	csp := res.Header.Get("Content-Security-Policy")
	if res.StatusCode != 200 || !strings.Contains(body, `id="root"`) || !strings.Contains(csp, "'sha256-") || res.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("index after login: %d csp=%q %s", res.StatusCode, csp, body)
	}
	if res, _ := get(t, c, srv.URL+"/assets/app-1.js"); res.StatusCode != 200 || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %s", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	if res, body := get(t, c, srv.URL+"/api/v1/x"); res.StatusCode != 200 || body != `{"ok":true}` {
		t.Fatalf("API with session: %d %s", res.StatusCode, body)
	}
	// The session never opens /mcp: assistants use the bearer token.
	req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader("{}"))
	if res, _ := c.Do(req); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/mcp with only a session: %d", res.StatusCode)
	}
	// A cross-site state change with the cookie is refused.
	req, _ = http.NewRequest("POST", srv.URL+"/api/v1/x", strings.NewReader("{}"))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if res, _ := c.Do(req); res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("POST", srv.URL+"/api/v1/x", strings.NewReader("{}"))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if res, _ := c.Do(req); res.StatusCode != 200 {
		t.Fatalf("same-origin POST: %d", res.StatusCode)
	}
	if res, _ := get(t, c, srv.URL+"/missing.js"); res.StatusCode != 404 {
		t.Fatalf("missing file: %d", res.StatusCode)
	}
	// Logout clears the session.
	res2, err := c.PostForm(srv.URL+"/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res, _ := get(t, c, srv.URL+"/api/v1/x"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("API after logout: %d", res.StatusCode)
	}
}

func TestSessionFromAnotherTokenIsRejected(t *testing.T) {
	srv, c := newServer(t, files)
	u, _ := url.Parse(srv.URL)
	c.Jar.SetCookies(u, []*http.Cookie{{Name: httpsrv.SessionCookie, Value: httpsrv.SessionValue("a-different-token-a-different-tok")}})
	if res, _ := get(t, c, srv.URL+"/api/v1/x"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale session after a token change: %d", res.StatusCode)
	}
	// Bearer keeps working alongside.
	if res, _ := get(t, c, srv.URL+"/api/v1/x", "Authorization", "Bearer "+token); res.StatusCode != 200 {
		t.Fatalf("bearer: %d", res.StatusCode)
	}
}

func TestNotBuiltBinaryExplainsHow(t *testing.T) {
	srv, c := newServer(t, fstest.MapFS{".gitkeep": {}})
	login(t, c, srv.URL, token)
	if res, body := get(t, c, srv.URL+"/"); res.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, "build-ui.sh") {
		t.Fatalf("not built: %d %s", res.StatusCode, body)
	}
}

func TestSecureCookieBehindHTTPSProxy(t *testing.T) {
	srv, c := newServer(t, files)
	req, _ := http.NewRequest("POST", srv.URL+"/login", strings.NewReader("token="+token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if ck := res.Cookies(); len(ck) != 1 || !ck[0].Secure {
		t.Fatalf("cookie behind TLS-terminating proxy must be Secure: %+v", ck)
	}
}
