package mediastore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fullfran/training-mcp/internal/training"
)

func TestPutOpenDelete(t *testing.T) {
	fs, err := New(filepath.Join(t.TempDir(), "media"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key, n, sum, err := fs.Put(ctx, bytes.NewReader([]byte("hello video")), ".mp4", 100)
	if err != nil || n != 11 || len(sum) != 64 {
		t.Fatalf("put: %v %d %q", err, n, sum)
	}
	rc, err := fs.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "hello video" {
		t.Fatalf("got %q", b)
	}
	keys, _ := fs.Keys(ctx)
	if len(keys) != 1 || keys[0] != key {
		t.Fatalf("keys: %v", keys)
	}
	if err := fs.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := fs.Delete(ctx, key); err != nil {
		t.Fatalf("deleting twice must be harmless: %v", err)
	}
	if _, err := fs.Open(ctx, key); !errors.Is(err, training.ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestSizeLimitBoundary(t *testing.T) {
	fs, _ := New(t.TempDir())
	ctx := context.Background()
	if _, _, _, err := fs.Put(ctx, bytes.NewReader(make([]byte, 10)), ".mp4", 10); err != nil {
		t.Fatalf("exactly at the limit must be accepted: %v", err)
	}
	if _, _, _, err := fs.Put(ctx, bytes.NewReader(make([]byte, 11)), ".mp4", 10); !errors.Is(err, training.ErrTooLarge) {
		t.Fatalf("one byte over must be ErrTooLarge, got %v", err)
	}
	keys, _ := fs.Keys(ctx)
	if len(keys) != 1 {
		t.Fatalf("a rejected upload must leave no file: %v", keys)
	}
	tmp, _ := os.ReadDir(filepath.Join(fs.dir, "tmp"))
	if len(tmp) != 0 {
		t.Fatalf("temp files left behind: %v", tmp)
	}
}

func TestKeysCannotEscapeDirectory(t *testing.T) {
	fs, _ := New(t.TempDir())
	for _, k := range []string{"../x.mp4", "a/b.mp4", `..\x`, "", ".hidden"} {
		if _, err := fs.Open(context.Background(), k); !errors.Is(err, training.ErrNotFound) {
			t.Errorf("Open(%q) = %v, want not found", k, err)
		}
		if err := fs.Delete(context.Background(), k); !errors.Is(err, training.ErrNotFound) {
			t.Errorf("Delete(%q) = %v, want not found", k, err)
		}
	}
}

func TestRejectsUnknownExtension(t *testing.T) {
	fs, _ := New(t.TempDir())
	if _, _, _, err := fs.Put(context.Background(), bytes.NewReader([]byte("x")), ".sh", 10); err == nil {
		t.Fatal("expected error")
	}
}
