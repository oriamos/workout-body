package integration

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fullfran/training-mcp/internal/mediastore"
	"github.com/fullfran/training-mcp/internal/training"
)

// mp4 returns bytes that look like an MP4 container (ftyp box) of the given size.
func mp4(size int) []byte {
	b := make([]byte, size)
	copy(b, []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'})
	return b
}

func mediaEnv(t *testing.T, max int64) (*env, string) {
	t.Helper()
	e := newEnv(t)
	dir := filepath.Join(t.TempDir(), "media")
	fs, err := mediastore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.svc.WithMedia(e.st, fs, max)
	return e, dir
}

func filesIn(t *testing.T, dir string) int {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func logOne(t *testing.T, e *env) training.Set {
	t.Helper()
	res, err := e.svc.LogSets(ctx(), batch("", set("bench press", 60, 8)))
	if err != nil {
		t.Fatal(err)
	}
	return res.Sets[0]
}

func TestAttachVideoToSetAndStream(t *testing.T) {
	e, dir := mediaEnv(t, 1<<20)
	s := logOne(t, e)
	data := mp4(2048)
	m, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "../../etc/set one.mp4", "form check", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if m.OwnerType != "set" || m.OwnerID != s.ID || m.ContentType != "video/mp4" || m.SizeBytes != 2048 {
		t.Fatalf("unexpected media: %+v", m)
	}
	if m.Filename != "set one.mp4" {
		t.Fatalf("filename must be stripped of path components, got %q", m.Filename)
	}
	if filesIn(t, dir) != 1 {
		t.Fatal("expected one stored file")
	}
	_, rc, err := e.svc.OpenMedia(ctx(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatal("stored bytes differ from uploaded bytes")
	}
	list, _ := e.svc.SetMedia(ctx(), s.ID)
	if len(list) != 1 || list[0].ID != m.ID {
		t.Fatalf("list: %+v", list)
	}
}

func TestVideoFormatIsSniffedNotTrusted(t *testing.T) {
	e, dir := mediaEnv(t, 1<<20)
	logOne(t, e)
	_, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "evil.mp4", "", strings.NewReader("#!/bin/sh\necho hi\n"))
	if !errors.Is(err, training.ErrValidation) {
		t.Fatalf("a non-video renamed .mp4 must be rejected, got %v", err)
	}
	if filesIn(t, dir) != 0 {
		t.Fatal("rejected upload left a file")
	}
	webm := append(append([]byte{0x1A, 0x45, 0xDF, 0xA3}, []byte("....webm")...), make([]byte, 64)...)
	m, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "x", "", bytes.NewReader(webm))
	if err != nil || m.ContentType != "video/webm" {
		t.Fatalf("webm: %v %+v", err, m)
	}
	mov := mp4(64)
	copy(mov[8:12], "qt  ")
	m, err = e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "x", "", bytes.NewReader(mov))
	if err != nil || m.ContentType != "video/quicktime" {
		t.Fatalf("mov: %v %+v", err, m)
	}
}

func TestVideoSizeAndCountLimits(t *testing.T) {
	e, dir := mediaEnv(t, 1000)
	logOne(t, e)
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "big.mp4", "", bytes.NewReader(mp4(1001))); !errors.Is(err, training.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if filesIn(t, dir) != 0 {
		t.Fatal("oversize upload left a file")
	}
	for i := 0; i < training.MaxMediaPerSet; i++ {
		if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "v.mp4", "", bytes.NewReader(mp4(100))); err != nil {
			t.Fatalf("video %d: %v", i, err)
		}
	}
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "v.mp4", "", bytes.NewReader(mp4(100))); !errors.Is(err, training.ErrConflict) {
		t.Fatalf("over the per-set limit must conflict, got %v", err)
	}
	if filesIn(t, dir) != training.MaxMediaPerSet {
		t.Fatalf("the refused upload must not leave a file: %d files", filesIn(t, dir))
	}
}

func TestVideoForMissingOwnerLeavesNothing(t *testing.T) {
	e, dir := mediaEnv(t, 1<<20)
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "99", "v.mp4", "", bytes.NewReader(mp4(100))); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerExercise, "no such exercise", "v.mp4", "", bytes.NewReader(mp4(100))); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if filesIn(t, dir) != 0 {
		t.Fatal("left a file")
	}
}

func TestDeletingSetRemovesItsVideos(t *testing.T) {
	e, dir := mediaEnv(t, 1<<20)
	s := logOne(t, e)
	m, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "v.mp4", "", bytes.NewReader(mp4(100)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := e.svc.DeleteSet(ctx(), s.ID, nil); err != nil {
		t.Fatal(err)
	}
	if filesIn(t, dir) != 0 {
		t.Fatal("the video file must be removed with its set")
	}
	if _, err := e.svc.GetMedia(ctx(), m.ID); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("media row must be gone, got %v", err)
	}
}

func TestDeleteMediaAndOrphanPurge(t *testing.T) {
	e, dir := mediaEnv(t, 1<<20)
	logOne(t, e)
	m, _ := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "v.mp4", "", bytes.NewReader(mp4(100)))
	if err := e.svc.DeleteMedia(ctx(), m.ID); err != nil {
		t.Fatal(err)
	}
	if filesIn(t, dir) != 0 {
		t.Fatal("file not removed")
	}
	// A file with no row (crash between write and insert) is collected.
	orphan := filepath.Join(dir, "deadbeef.mp4")
	if err := os.WriteFile(orphan, mp4(10), 0o600); err != nil {
		t.Fatal(err)
	}
	// A fresh orphan may be a file whose row is about to commit: it is spared.
	young := filepath.Join(dir, "cafebabe.mp4")
	if err := os.WriteFile(young, mp4(10), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * training.OrphanGrace)
	if err := os.Chtimes(orphan, past, past); err != nil {
		t.Fatal(err)
	}
	keep, _ := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "v.mp4", "", bytes.NewReader(mp4(100)))
	n, err := e.svc.PurgeOrphanBlobs(ctx())
	if err != nil || n != 1 {
		t.Fatalf("purged %d, err %v", n, err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Fatal("a file inside the grace period must not be collected")
	}
	if _, rc, err := e.svc.OpenMedia(ctx(), keep.ID); err != nil {
		t.Fatalf("purge removed a referenced file: %v", err)
	} else {
		rc.Close()
	}
}

func TestExerciseGuideInstructionsAndVideos(t *testing.T) {
	e, _ := mediaEnv(t, 1<<20)
	logOne(t, e)
	if err := e.svc.AddExerciseAlias(ctx(), 1, "bp"); err != nil {
		t.Fatal(err)
	}
	g, err := e.svc.SetExerciseInstructions(ctx(), "bp", "  Retract shoulder blades. Lower to mid-chest.  ")
	if err != nil {
		t.Fatal(err)
	}
	if g.Exercise != "bench press" || g.Instructions != "Retract shoulder blades. Lower to mid-chest." {
		t.Fatalf("guide: %+v", g)
	}
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerExercise, "bench press", "how-to.mp4", "setup", bytes.NewReader(mp4(300))); err != nil {
		t.Fatal(err)
	}
	g, _ = e.svc.ExerciseGuide(ctx(), "bp")
	if len(g.Videos) != 1 || g.Videos[0].Caption != "setup" || g.Videos[0].OwnerType != "exercise" {
		t.Fatalf("guide videos: %+v", g.Videos)
	}
	if _, err := e.svc.SetExerciseInstructions(ctx(), "bp", strings.Repeat("x", training.MaxInstructionsLen+1)); !errors.Is(err, training.ErrValidation) {
		t.Fatalf("too-long instructions must be rejected, got %v", err)
	}
	if _, err := e.svc.SetExerciseInstructions(ctx(), "unknown lift", "x"); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("instructions must not create exercises, got %v", err)
	}
	// Clearing.
	g, _ = e.svc.SetExerciseInstructions(ctx(), "bench press", "")
	if g.Instructions != "" {
		t.Fatal("empty text must clear")
	}
}

func TestMediaDisabledIsExplicit(t *testing.T) {
	e := newEnv(t)
	logOne(t, e)
	if _, err := e.svc.AttachVideo(ctx(), training.OwnerSet, "1", "v.mp4", "", bytes.NewReader(mp4(10))); !errors.Is(err, training.ErrMediaDisabled) {
		t.Fatalf("want ErrMediaDisabled, got %v", err)
	}
	// Reading a guide still works without media storage.
	if _, err := e.svc.ExerciseGuide(ctx(), "bench press"); err != nil {
		t.Fatal(err)
	}
}
