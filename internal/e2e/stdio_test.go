package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The documented stdio setup: the client launches the binary, no token, no port.
func TestStdioModeWithRealBinary(t *testing.T) {
	db := filepath.Join(t.TempDir(), "stdio.db")
	cmd := exec.Command(binary, "--stdio")
	cmd.Env = append(os.Environ(), "TRAINING_DB_PATH="+db, "WORKOUT_TIMEZONE=Asia/Jerusalem",
		"MCP_BEARER_TOKEN=", "MCP_AUTH_DISABLED=", "MEDIA_DIR="+filepath.Join(filepath.Dir(db), "media"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e-stdio", Version: "1"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect over stdio (no token must be required): %v", err)
	}
	defer cs.Close()

	w := tool(t, cs, "get_workout", map[string]any{})
	if w["timezone"] != "Asia/Jerusalem" || w["exists"] != false {
		t.Fatalf("get_workout: %v", w)
	}
	r := tool(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8}}})
	if r["created"] != true || len(list(r["sets"])) != 1 {
		t.Fatalf("log_sets over stdio: %v", r)
	}
	again := tool(t, cs, "get_workout", map[string]any{})
	if again["exists"] != true {
		t.Fatalf("the write must persist: %v", again)
	}
}
