package training

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrTooLarge means an upload exceeded the configured size limit.
var ErrTooLarge = errors.New("file too large")

// ErrMediaDisabled means media storage is not configured on this server.
var ErrMediaDisabled = errors.New("media storage is not configured")

// Media owner kinds.
const (
	OwnerSet      = "set"
	OwnerExercise = "exercise"
)

// Limits, documented in docs/media.md.
const (
	DefaultMaxVideoBytes = 200 << 20
	MaxMediaPerSet       = 5
	MaxMediaPerExercise  = 10
	maxCaptionLen        = 200
	MaxInstructionsLen   = 5000
)

// Media is a stored video. The bytes are addressed by StorageKey, which is
// internal and never exposed; clients use ID.
type Media struct {
	ID          int64  `json:"id"`
	OwnerType   string `json:"owner_type"` // "set" or "exercise"
	OwnerID     int64  `json:"owner_id"`
	Kind        string `json:"kind"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	Caption     string `json:"caption,omitempty"`
	CreatedAt   string `json:"created_at"`
	StorageKey  string `json:"-"`
}

// MediaStore persists media metadata and exercise instructions.
type MediaStore interface {
	// AddMedia inserts a row after checking the owner exists and the per-owner
	// limit, atomically.
	AddMedia(context.Context, Media) (Media, error)
	GetMedia(context.Context, int64) (Media, error)
	ListMedia(ctx context.Context, ownerType string, ownerID int64) ([]Media, error)
	DeleteMedia(context.Context, int64) (Media, error)
	// KeysForSet / KeysForWorkout list storage keys so files can be removed
	// after the rows are cascaded away.
	KeysForSet(context.Context, int64) ([]string, error)
	KeysForWorkout(context.Context, int64) ([]string, error)
	AllKeys(context.Context) (map[string]bool, error)
	ExerciseInstructions(ctx context.Context, exerciseID int64) (string, error)
	SetExerciseInstructions(ctx context.Context, exerciseID int64, text string) error
}

// BlobStore keeps the video bytes.
type BlobStore interface {
	// Put streams r into a new blob, failing with ErrTooLarge past max bytes.
	// ext is a file extension such as ".mp4".
	Put(ctx context.Context, r io.Reader, ext string, max int64) (key string, size int64, sha256hex string, err error)
	Open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	Delete(ctx context.Context, key string) error
	Keys(ctx context.Context) ([]string, error)
}

// ExerciseGuide is everything known about how to perform an exercise.
type ExerciseGuide struct {
	ExerciseID   int64   `json:"exercise_id"`
	Exercise     string  `json:"exercise"`
	Instructions string  `json:"instructions"`
	Videos       []Media `json:"videos"`
}

// WithMedia enables media features. ms stores metadata and exercise
// instructions; bs stores the video bytes and may be nil, in which case videos
// are disabled but exercise instructions still work. maxBytes <= 0 selects the
// default limit.
func (s *Service) WithMedia(ms MediaStore, bs BlobStore, maxBytes int64) *Service {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxVideoBytes
	}
	s.media, s.blobs, s.maxVideo = ms, bs, maxBytes
	if bs == nil {
		s.blobs = nil // avoid a typed-nil interface
	}
	return s
}

// MediaEnabled reports whether video storage is configured.
func (s *Service) MediaEnabled() bool { return s.media != nil && s.blobs != nil }

// MaxVideoBytes is the upload size limit.
func (s *Service) MaxVideoBytes() int64 { return s.maxVideo }

// sniffVideo identifies the container from magic bytes. The client-declared
// content type and file name are never trusted.
func sniffVideo(head []byte) (contentType, ext string, ok bool) {
	switch {
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		if string(head[8:12]) == "qt  " {
			return "video/quicktime", ".mov", true
		}
		return "video/mp4", ".mp4", true
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "video/webm", ".webm", true
	}
	return "", "", false
}

func cleanFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) {
		name = ""
	}
	var b strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len([]rune(out)) > 120 {
		out = string([]rune(out)[:120])
	}
	return out
}

// AttachVideo stores a video for a set or an exercise (ref = id, name or
// alias). The container format is verified from the bytes: MP4, MOV (QuickTime)
// and WebM are accepted.
func (s *Service) AttachVideo(ctx context.Context, ownerType string, ownerRef string, filename, caption string, r io.Reader) (Media, error) {
	if !s.MediaEnabled() {
		return Media{}, ErrMediaDisabled
	}
	caption = strings.TrimSpace(caption)
	if tooLong(caption, maxCaptionLen) {
		return Media{}, invalid("caption", "must be at most %d characters", maxCaptionLen)
	}
	var ownerID int64
	switch ownerType {
	case OwnerSet:
		id, err := strconv.ParseInt(strings.TrimSpace(ownerRef), 10, 64)
		if err != nil || id <= 0 {
			return Media{}, invalid("set_id", "must be a positive integer")
		}
		if _, err := s.store.GetSet(ctx, id); err != nil {
			return Media{}, err
		}
		ownerID = id
	case OwnerExercise:
		ex, err := s.GetExercise(ctx, ownerRef)
		if err != nil {
			return Media{}, err
		}
		ownerID = ex.ID
	default:
		return Media{}, invalid("owner_type", "must be %q or %q", OwnerSet, OwnerExercise)
	}

	br := bufio.NewReaderSize(r, 64)
	head, _ := br.Peek(16)
	ct, ext, ok := sniffVideo(head)
	if !ok {
		return Media{}, invalid("file", "not a supported video; use MP4, MOV (QuickTime) or WebM")
	}
	key, size, sum, err := s.blobs.Put(ctx, br, ext, s.maxVideo)
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return Media{}, fmt.Errorf("%w: the limit is %d MB", ErrTooLarge, s.maxVideo>>20)
		}
		return Media{}, err
	}
	if size == 0 {
		_ = s.blobs.Delete(ctx, key)
		return Media{}, invalid("file", "is empty")
	}
	fn := cleanFilename(filename)
	if fn == "" {
		fn = "video" + ext
	}
	m, err := s.media.AddMedia(ctx, Media{
		OwnerType: ownerType, OwnerID: ownerID, Kind: "video", Filename: fn,
		ContentType: ct, SizeBytes: size, SHA256: sum, Caption: caption, StorageKey: key,
	})
	if err != nil {
		_ = s.blobs.Delete(ctx, key) // never leave a blob without a row
		return Media{}, err
	}
	return m, nil
}

// OpenMedia returns metadata and a seekable reader (HTTP range requests need
// seeking). The caller closes the reader.
func (s *Service) OpenMedia(ctx context.Context, id int64) (Media, io.ReadSeekCloser, error) {
	if !s.MediaEnabled() {
		return Media{}, nil, ErrMediaDisabled
	}
	if id <= 0 {
		return Media{}, nil, invalid("media_id", "must be positive")
	}
	m, err := s.media.GetMedia(ctx, id)
	if err != nil {
		return Media{}, nil, err
	}
	rc, err := s.blobs.Open(ctx, m.StorageKey)
	return m, rc, err
}

// GetMedia returns metadata only.
func (s *Service) GetMedia(ctx context.Context, id int64) (Media, error) {
	if !s.MediaEnabled() {
		return Media{}, ErrMediaDisabled
	}
	if id <= 0 {
		return Media{}, invalid("media_id", "must be positive")
	}
	return s.media.GetMedia(ctx, id)
}

// SetMedia lists the videos of a performed set.
func (s *Service) SetMedia(ctx context.Context, setID int64) ([]Media, error) {
	if !s.MediaEnabled() {
		return nil, ErrMediaDisabled
	}
	if _, err := s.store.GetSet(ctx, setID); err != nil {
		return nil, err
	}
	out, err := s.media.ListMedia(ctx, OwnerSet, setID)
	if err == nil && out == nil {
		out = []Media{}
	}
	return out, err
}

// DeleteMedia removes a video row and its file.
func (s *Service) DeleteMedia(ctx context.Context, id int64) error {
	if !s.MediaEnabled() {
		return ErrMediaDisabled
	}
	if id <= 0 {
		return invalid("media_id", "must be positive")
	}
	m, err := s.media.DeleteMedia(ctx, id)
	if err != nil {
		return err
	}
	return s.blobs.Delete(ctx, m.StorageKey)
}

// ExerciseGuide returns the written instructions and how-to videos of an
// exercise (id, name or alias). It works without media storage: the videos
// list is then empty.
func (s *Service) ExerciseGuide(ctx context.Context, ref string) (ExerciseGuide, error) {
	ex, err := s.GetExercise(ctx, ref)
	if err != nil {
		return ExerciseGuide{}, err
	}
	g := ExerciseGuide{ExerciseID: ex.ID, Exercise: ex.Name, Videos: []Media{}}
	if s.media == nil {
		return g, nil
	}
	if g.Instructions, err = s.media.ExerciseInstructions(ctx, ex.ID); err != nil {
		return g, err
	}
	if s.blobs != nil {
		if g.Videos, err = s.media.ListMedia(ctx, OwnerExercise, ex.ID); err != nil {
			return g, err
		}
		if g.Videos == nil {
			g.Videos = []Media{}
		}
	}
	return g, nil
}

// SetExerciseInstructions stores the written how-to for an exercise; an empty
// text clears it. The exercise must already exist (it is never created here).
func (s *Service) SetExerciseInstructions(ctx context.Context, ref, text string) (ExerciseGuide, error) {
	if s.media == nil {
		return ExerciseGuide{}, ErrMediaDisabled
	}
	text = strings.TrimSpace(text)
	if tooLong(text, MaxInstructionsLen) {
		return ExerciseGuide{}, invalid("instructions", "must be at most %d characters", MaxInstructionsLen)
	}
	ex, err := s.GetExercise(ctx, ref)
	if err != nil {
		return ExerciseGuide{}, err
	}
	if err := s.media.SetExerciseInstructions(ctx, ex.ID, text); err != nil {
		return ExerciseGuide{}, err
	}
	return s.ExerciseGuide(ctx, fmt.Sprint(ex.ID))
}

// removeBlobs deletes files best-effort. A leftover file is harmless and is
// collected by PurgeOrphanBlobs.
func (s *Service) removeBlobs(ctx context.Context, keys []string) {
	if s.blobs == nil {
		return
	}
	for _, k := range keys {
		_ = s.blobs.Delete(ctx, k)
	}
}

// PurgeOrphanBlobs deletes stored files that no media row references (for
// example after a crash between writing a file and recording it). It returns
// how many were removed.
func (s *Service) PurgeOrphanBlobs(ctx context.Context) (int, error) {
	if !s.MediaEnabled() {
		return 0, ErrMediaDisabled
	}
	live, err := s.media.AllKeys(ctx)
	if err != nil {
		return 0, err
	}
	keys, err := s.blobs.Keys(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range keys {
		if !live[k] {
			if err := s.blobs.Delete(ctx, k); err == nil {
				n++
			}
		}
	}
	return n, nil
}
