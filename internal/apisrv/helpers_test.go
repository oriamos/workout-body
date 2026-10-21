package apisrv

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/httpsrv"
	"github.com/fullfran/training-mcp/internal/sqlitestore"
	"github.com/fullfran/training-mcp/internal/training"
)

const testToken = "test-token-0123456789"

var jerusalem = func() *time.Location {
	l, err := time.LoadLocation("Asia/Jerusalem")
	if err != nil {
		panic(err)
	}
	return l
}()

// env is the API over a real SQLite-backed service on a temp file, mounted the
// way production mounts it: outer mux at /api/v1/ behind MaxBody and Bearer.
type env struct {
	t      *testing.T
	svc    *training.Service
	st     *sqlitestore.Store
	api    *Server
	srv    *httptest.Server
	logBuf *bytes.Buffer
	// validate checks every response against openapi.yaml.
	validate bool
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC) // 14:00 in Jerusalem
	svc := training.NewService(st, func() time.Time { return now }).WithLocation(jerusalem)
	api := New(svc)
	buf := &bytes.Buffer{}
	api.logger = log.New(buf, "", 0)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", httpsrv.MaxBody(httpsrv.Bearer(testToken, false, api.Handler())))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{t: t, svc: svc, st: st, api: api, srv: srv, logBuf: buf, validate: true}
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

// h builds a header map from key, value pairs.
func h(kv ...string) map[string]string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// do sends a request. body may be nil, a string/[]byte (sent raw) or any value
// (JSON-encoded). Content-Type defaults to application/json when there is a
// body; a header value of "" removes the header.
func (e *env) do(method, path string, body any, headers map[string]string) resp {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	out := resp{status: res.StatusCode, header: res.Header, body: data}
	if e.validate {
		checkAgainstSpec(e.t, method, req.URL.EscapedPath(), out)
	}
	return out
}

func (r resp) obj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, r.body)
	}
	return m
}

func (r resp) etag() string { return r.header.Get("ETag") }

func expect(t *testing.T, r resp, status int) map[string]any {
	t.Helper()
	if r.status != status {
		t.Fatalf("status = %d, want %d; body: %s", r.status, status, r.body)
	}
	if len(bytes.TrimSpace(r.body)) == 0 {
		return nil
	}
	if !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") {
		return nil
	}
	return r.obj(t)
}

// expectErr asserts an error envelope with code and returns it.
func expectErr(t *testing.T, r resp, status int, code string) map[string]any {
	t.Helper()
	m := expect(t, r, status)
	e, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope: %s", r.body)
	}
	if e["code"] != code {
		t.Fatalf("error code = %v, want %s; body: %s", e["code"], code, r.body)
	}
	if s, _ := e["message"].(string); s == "" {
		t.Fatalf("error without message: %s", r.body)
	}
	return e
}

// issueFields lists the field of every issue in an error envelope.
func issueFields(e map[string]any) []string {
	var out []string
	list, _ := e["issues"].([]any)
	for _, i := range list {
		out = append(out, i.(map[string]any)["field"].(string))
	}
	return out
}

func hasIssue(t *testing.T, e map[string]any, field string) {
	t.Helper()
	for _, f := range issueFields(e) {
		if f == field {
			return
		}
	}
	t.Fatalf("no issue for %q in %v (%v)", field, issueFields(e), e)
}

func num(v any) int { f, _ := v.(float64); return int(f) }

func quoted(n int) string { return strconv.Quote(strconv.Itoa(n)) }

func list(m map[string]any, key string) []any { l, _ := m[key].([]any); return l }

func samplePlan(name string) map[string]any {
	return map[string]any{
		"format": "workout-plan", "version": 1, "name": name, "notes": "synthetic",
		"items": []any{
			map[string]any{"exercise": "Bench Press", "sets": 3, "rep_min": 6, "rep_max": 8, "target_rpe": 8},
			map[string]any{"exercise": "Cable Row", "sets": 2, "rep_min": 10, "rep_max": 12, "target_weight_kg": 40.5, "target_rir": 2},
		},
	}
}
