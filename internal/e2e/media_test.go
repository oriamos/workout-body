package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func fakeMP4(n int) []byte {
	b := make([]byte, n)
	copy(b, []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'})
	for i := 12; i < n; i++ {
		b[i] = byte(i % 251)
	}
	return b
}

func filesIn(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

// Videos for a set and how-to material for an exercise, through the real
// binary: HTTP upload and streaming, MCP reads, cleanup on disk.
func TestSetVideoAndExerciseHowTo(t *testing.T) {
	srv := start(t, "")
	mediaDir := filepath.Join(filepath.Dir(srv.db), "media")
	cs := srv.mcpClient()

	logged := tool(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"exercise": "bench press", "weight_kg": 60, "reps": 8}}})
	setID := int(num(list(logged["sets"])[0].(map[string]any)["id"]))

	// --- a video of the set, uploaded over HTTP
	video := fakeMP4(4096)
	up := srv.do("POST", "/api/v1/sets/"+strconv.Itoa(setID)+"/videos?filename=bench-set-1.mp4&caption=bar+path", video, "Content-Type", "video/mp4")
	if up.Status != 201 {
		t.Fatalf("upload: %d %s", up.Status, up.Body)
	}
	m := up.Map(t)
	mediaID := int(num(m["id"]))
	if m["content_type"] != "video/mp4" || num(m["size_bytes"]) != 4096 || m["owner_type"] != "set" || int(num(m["owner_id"])) != setID {
		t.Fatalf("media metadata: %v", m)
	}
	if _, leaked := m["storage_key"]; leaked {
		t.Fatalf("the internal storage key must never be exposed: %v", m)
	}
	if filesIn(t, mediaDir) != 1 {
		t.Fatal("expected one stored file")
	}

	// --- streaming, including a Range request (what a phone player sends)
	full := srv.do("GET", "/api/v1/media/"+strconv.Itoa(mediaID), nil)
	if full.Status != 200 || !bytes.Equal(full.Body, video) || full.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("stream: %d type=%q len=%d", full.Status, full.Header.Get("Content-Type"), len(full.Body))
	}
	part := srv.do("GET", "/api/v1/media/"+strconv.Itoa(mediaID), nil, "Range", "bytes=100-199")
	if part.Status != 206 || !bytes.Equal(part.Body, video[100:200]) {
		t.Fatalf("range: %d len=%d", part.Status, len(part.Body))
	}
	if r := srv.do("GET", "/api/v1/media/"+strconv.Itoa(mediaID), nil, "Authorization", "-"); r.Status != 401 {
		t.Fatalf("media must require the token, got %d", r.Status)
	}

	// --- not a video: refused, nothing stored
	bad := srv.do("POST", "/api/v1/sets/"+strconv.Itoa(setID)+"/videos?filename=x.mp4", []byte("#!/bin/sh\nrm -rf /\n"), "Content-Type", "video/mp4")
	if bad.Status != 400 {
		t.Fatalf("a script renamed .mp4 must be rejected, got %d %s", bad.Status, bad.Body)
	}
	if filesIn(t, mediaDir) != 1 {
		t.Fatal("a rejected upload left a file behind")
	}

	// --- MCP sees metadata (never content)
	vids := tool(t, cs, "list_set_videos", map[string]any{"set_id": setID})
	if l := list(vids["videos"]); len(l) != 1 || int(num(l[0].(map[string]any)["id"])) != mediaID {
		t.Fatalf("list_set_videos: %v", vids)
	}

	// --- exercise how-to: written instructions (via MCP) + a how-to video (via HTTP)
	tool(t, cs, "set_exercise_instructions", map[string]any{"exercise": "bench press", "instructions": "Retract shoulder blades; lower to mid-chest."})
	mp := srv.do("POST", "/api/v1/exercises/bench%20press/videos?filename=howto.mp4", fakeMP4(2048), "Content-Type", "video/mp4")
	if mp.Status != 201 {
		t.Fatalf("exercise video: %d %s", mp.Status, mp.Body)
	}
	g := srv.do("GET", "/api/v1/exercises/bench%20press/guide", nil).Map(t)
	if g["instructions"] != "Retract shoulder blades; lower to mid-chest." || len(list(g["videos"])) != 1 {
		t.Fatalf("guide over HTTP: %v", g)
	}
	mg := tool(t, cs, "get_exercise_guide", map[string]any{"exercise": "bench press"})
	if gg, _ := mg["guide"].(map[string]any); gg == nil || gg["instructions"] != g["instructions"] || len(list(gg["videos"])) != 1 {
		t.Fatalf("guide over MCP must match HTTP: %v", mg)
	}
	if filesIn(t, mediaDir) != 2 {
		t.Fatal("expected two stored files")
	}

	// --- deleting the set removes ITS video (not the exercise's)
	del := srv.do("GET", "/api/v1/sets/"+strconv.Itoa(setID), nil)
	rd := srv.do("DELETE", "/api/v1/sets/"+strconv.Itoa(setID), nil, "If-Match", del.Header.Get("ETag"))
	if rd.Status != 200 && rd.Status != 204 {
		t.Fatalf("delete set: %d %s", rd.Status, rd.Body)
	}
	if r := srv.do("GET", "/api/v1/media/"+strconv.Itoa(mediaID), nil); r.Status != 404 {
		t.Fatalf("the set's video must be gone, got %d", r.Status)
	}
	if filesIn(t, mediaDir) != 1 {
		t.Fatalf("the set's file must be removed from disk, %d files remain", filesIn(t, mediaDir))
	}
	// the how-to video survives and still streams
	g = srv.do("GET", "/api/v1/exercises/bench%20press/guide", nil).Map(t)
	howto := list(g["videos"])
	if len(howto) != 1 {
		t.Fatalf("how-to video must survive: %v", g)
	}
	id := int(num(howto[0].(map[string]any)["id"]))
	if r := srv.do("GET", "/api/v1/media/"+strconv.Itoa(id), nil); r.Status != 200 {
		t.Fatalf("how-to stream: %d", r.Status)
	}

	// --- and it all persists across a restart
	srv.stop()
	srv2 := start(t, srv.db)
	if r := srv2.do("GET", "/api/v1/media/"+strconv.Itoa(id), nil); r.Status != 200 || len(r.Body) != 2048 {
		t.Fatalf("after restart: %d len=%d", r.Status, len(r.Body))
	}
}

func TestOversizeUploadRefusedAndLeavesNothing(t *testing.T) {
	srv := start(t, "", "MAX_VIDEO_MB=1")
	mediaDir := filepath.Join(filepath.Dir(srv.db), "media")
	cs := srv.mcpClient()
	logged := tool(t, cs, "log_sets", map[string]any{"sets": []any{map[string]any{"exercise": "row", "weight_kg": 50, "reps": 10}}})
	setID := int(num(list(logged["sets"])[0].(map[string]any)["id"]))
	r := srv.do("POST", "/api/v1/sets/"+strconv.Itoa(setID)+"/videos", fakeMP4(1<<20+1), "Content-Type", "video/mp4")
	if r.Status != 413 {
		t.Fatalf("one byte over the limit must be 413, got %d %s", r.Status, r.Body)
	}
	if filesIn(t, mediaDir) != 0 {
		t.Fatal("an oversize upload left a file behind")
	}
	if r := srv.do("POST", "/api/v1/sets/"+strconv.Itoa(setID)+"/videos", fakeMP4(1<<20), "Content-Type", "video/mp4"); r.Status != 201 {
		t.Fatalf("a file exactly at the limit must be accepted, got %d %s", r.Status, r.Body)
	}
}
