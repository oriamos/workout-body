// Package e2e runs the acceptance flow against the real compiled binary: a
// real SQLite database, real MCP client calls over Streamable HTTP, and plain
// HTTP requests to the JSON API.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const token = "e2e-test-token-not-a-secret"

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "workout-e2e-bin")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "training-mcp")
	out, err := exec.Command("go", "build", "-o", binary, "../../cmd/training-mcp").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.Exit(2)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// server is one running instance of the binary.
type server struct {
	t    *testing.T
	cmd  *exec.Cmd
	base string
	db   string
	logs *bytes.Buffer
}

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// start launches the binary on db (a fresh path when empty) and waits for /health.
func start(t *testing.T, db string, extraEnv ...string) *server {
	t.Helper()
	if db == "" {
		db = filepath.Join(t.TempDir(), "e2e.db")
	}
	addr := freeAddr(t)
	s := &server{t: t, base: "http://" + addr, db: db, logs: &bytes.Buffer{}}
	s.cmd = exec.Command(binary, "--addr", addr)
	s.cmd.Env = append(os.Environ(),
		"MCP_BEARER_TOKEN="+token,
		"TRAINING_DB_PATH="+db,
		"MEDIA_DIR="+filepath.Join(filepath.Dir(db), "media"),
		"WORKOUT_TIMEZONE=Asia/Jerusalem",
		"MCP_AUTH_DISABLED=", "WEB_BASE_PATH=",
	)
	s.cmd.Env = append(s.cmd.Env, extraEnv...)
	s.cmd.Stdout, s.cmd.Stderr = s.logs, s.logs
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.stop() })
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if r, err := http.Get(s.base + "/health"); err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return s
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server did not become healthy:\n%s", s.logs.String())
	return nil
}

// stop shuts the server down gracefully (SIGTERM) and waits for it to exit.
func (s *server) stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
	s.cmd = nil
}

// resp is a decoded HTTP response.
type resp struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r resp) JSON(t testing.TB, out any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, out); err != nil {
		t.Fatalf("decode %q: %v", r.Body, err)
	}
}

func (r resp) Map(t testing.TB) map[string]any {
	t.Helper()
	var m map[string]any
	r.JSON(t, &m)
	return m
}

// do sends an authenticated request. A nil body sends none.
func (s *server) do(method, path string, body any, headers ...string) resp {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case []byte:
			rd = bytes.NewReader(b)
		default:
			data, err := json.Marshal(body)
			if err != nil {
				s.t.Fatal(err)
			}
			rd = bytes.NewReader(data)
		}
	}
	req, err := http.NewRequest(method, s.base+path, rd)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "-" {
			req.Header.Del(headers[i])
		} else {
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return resp{Status: res.StatusCode, Header: res.Header, Body: data}
}

// mcpClient connects a real MCP client over Streamable HTTP with the bearer token.
func (s *server) mcpClient() *mcp.ClientSession {
	s.t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   s.base + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{http.DefaultTransport}},
	}, nil)
	if err != nil {
		s.t.Fatalf("mcp connect: %v\n%s", err, s.logs.String())
	}
	s.t.Cleanup(func() { cs.Close() })
	return cs
}

type bearer struct{ base http.RoundTripper }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	return b.base.RoundTrip(r)
}

// tool calls an MCP tool, fails on a tool error, and returns the structured result.
func tool(t testing.TB, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: tool error: %s", name, toolText(res))
	}
	data, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: decode %s: %v", name, data, err)
	}
	return out
}

// toolErr calls a tool expecting an error and returns its text.
func toolErr(t testing.TB, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("%s: expected an error, got %s", name, toolText(res))
	}
	return toolText(res)
}

func toolText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// --- small accessors over decoded JSON ---

func get(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func toolParams(name string, args map[string]any) *mcp.CallToolParams {
	return &mcp.CallToolParams{Name: name, Arguments: args}
}
