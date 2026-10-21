// Package mediastore keeps uploaded video files on the local filesystem.
package mediastore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

// FS stores blobs as regular files in one directory. Keys are random hex names
// plus an extension; a key never contains a path separator, so it cannot escape
// the directory.
type FS struct{ dir string }

var _ training.BlobStore = (*FS)(nil)

// New creates the directory (mode 0700) if needed.
func New(dir string) (*FS, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o700); err != nil {
		return nil, err
	}
	return &FS{dir: dir}, nil
}

func validKey(key string) bool {
	if key == "" || strings.ContainsAny(key, `/\`) || strings.HasPrefix(key, ".") {
		return false
	}
	return key == filepath.Base(key)
}

func (f *FS) Put(ctx context.Context, r io.Reader, ext string, max int64) (string, int64, string, error) {
	if ext != ".mp4" && ext != ".mov" && ext != ".webm" {
		return "", 0, "", fmt.Errorf("unsupported extension %q", ext)
	}
	tmp, err := os.CreateTemp(filepath.Join(f.dir, "tmp"), "upload-*")
	if err != nil {
		return "", 0, "", err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }

	h := sha256.New()
	// Read one byte past the limit so "exactly max" is accepted and "max+1" is not.
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(&ctxReader{ctx, r}, max+1))
	if err != nil {
		cleanup()
		return "", 0, "", err
	}
	if n > max {
		cleanup()
		return "", 0, "", training.ErrTooLarge
	}
	if err = tmp.Sync(); err != nil {
		cleanup()
		return "", 0, "", err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", 0, "", err
	}
	var rnd [16]byte
	if _, err = rand.Read(rnd[:]); err != nil {
		os.Remove(tmpName)
		return "", 0, "", err
	}
	key := hex.EncodeToString(rnd[:]) + ext
	if err = os.Rename(tmpName, filepath.Join(f.dir, key)); err != nil {
		os.Remove(tmpName)
		return "", 0, "", err
	}
	return key, n, hex.EncodeToString(h.Sum(nil)), nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func (f *FS) Open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	if !validKey(key) {
		return nil, training.ErrNotFound
	}
	file, err := os.Open(filepath.Join(f.dir, key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: the video file is missing from the media directory", training.ErrNotFound)
	}
	return file, err
}

func (f *FS) Delete(_ context.Context, key string) error {
	if !validKey(key) {
		return training.ErrNotFound
	}
	err := os.Remove(filepath.Join(f.dir, key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (f *FS) Keys(context.Context) ([]string, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && validKey(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
