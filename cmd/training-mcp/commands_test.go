package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fullfran/training-mcp/internal/sqlitestore"
)

func TestParseArgs(t *testing.T) {
	for name, tc := range map[string]struct {
		args    []string
		want    options
		wantErr string
	}{
		"default":  {nil, options{addr: ":8080", command: "serve"}, ""},
		"stdio":    {[]string{"--stdio"}, options{addr: ":8080", stdio: true, command: "serve"}, ""},
		"backup":   {[]string{"backup", "/tmp/x.db"}, options{addr: ":8080", command: "backup", arg: "/tmp/x.db"}, ""},
		"media-gc": {[]string{"media-gc"}, options{addr: ":8080", command: "media-gc"}, ""},
		"no dest":  {[]string{"backup"}, options{}, "usage"},
		"unknown":  {[]string{"frobnicate"}, options{}, "unknown command"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %+v err %v want %+v", got, err, tc.want)
			}
		})
	}
}

func TestBackupCommandWritesOpenableSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRAINING_DB_PATH", filepath.Join(dir, "live.db"))
	t.Setenv("MCP_BEARER_TOKEN", "")
	t.Setenv("MCP_AUTH_DISABLED", "")
	dest := filepath.Join(dir, "backup.db")
	// A backup needs no bearer token: it never opens a network endpoint.
	if err := runWithContext(context.Background(), []string{"backup", dest}); err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.Open(dest)
	if err != nil {
		t.Fatalf("snapshot does not open: %v", err)
	}
	st.Close()
	if err := runWithContext(context.Background(), []string{"backup", dest}); err == nil {
		t.Fatal("must refuse to overwrite an existing backup")
	}
}

func TestMediaGCRuns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TRAINING_DB_PATH", filepath.Join(dir, "live.db"))
	t.Setenv("MEDIA_DIR", filepath.Join(dir, "media"))
	t.Setenv("MCP_BEARER_TOKEN", "")
	orphan := filepath.Join(dir, "media", "0123456789abcdef0123456789abcdef.mp4")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runWithContext(context.Background(), []string{"media-gc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan file should have been removed")
	}
}

func TestServeStillRequiresTokenButStdioDoesNot(t *testing.T) {
	t.Setenv("TRAINING_DB_PATH", filepath.Join(t.TempDir(), "x.db"))
	t.Setenv("MCP_BEARER_TOKEN", "")
	t.Setenv("MCP_AUTH_DISABLED", "")
	if err := runWithContext(context.Background(), []string{"--addr", "127.0.0.1:0"}); err == nil || !strings.Contains(err.Error(), "MCP_BEARER_TOKEN") {
		t.Fatalf("serve without a token must fail, got %v", err)
	}
}

func TestUnusableMediaDirDoesNotStopTheServer(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file-not-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRAINING_DB_PATH", filepath.Join(dir, "x.db"))
	t.Setenv("MEDIA_DIR", filepath.Join(blocker, "media")) // cannot be created: parent is a file
	t.Setenv("MCP_BEARER_TOKEN", "test-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runWithContext(ctx, []string{"--addr", "127.0.0.1:0"}); err != nil {
		t.Fatalf("workout logging must not depend on the video directory: %v", err)
	}
}
