package apisrv

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/fullfran/training-mcp/internal/training"
)

// uploadOverhead is allowed on top of the video limit for multipart framing and
// the caption part.
const uploadOverhead = 64 << 10

// maxCaptionPart bounds how much of a caption part is read; the service
// enforces the real caption limit.
const maxCaptionPart = 4 << 10

var rawVideoTypes = map[string]bool{
	"video/mp4": true, "video/quicktime": true, "video/webm": true, "application/octet-stream": true,
}

func (s *Server) uploadSetVideo(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	return s.upload(w, r, training.OwnerSet, strconv.FormatInt(id, 10))
}

func (s *Server) uploadExerciseVideo(w http.ResponseWriter, r *http.Request) error {
	return s.upload(w, r, training.OwnerExercise, r.PathValue("ref"))
}

// upload streams a video to the service, either as the raw request body or as
// the `file` part of a multipart form. Nothing is buffered in memory; the
// container format is verified by the service from the bytes, so the declared
// Content-Type only selects how the body is framed.
func (s *Server) upload(w http.ResponseWriter, r *http.Request, ownerType, ownerRef string) error {
	if !s.svc.MediaEnabled() {
		return training.ErrMediaDisabled
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.svc.MaxVideoBytes()+uploadOverhead)
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return &apiError{status: http.StatusUnsupportedMediaType, Code: codeUnsupportedMedia,
			Message: "Content-Type is required: multipart/form-data, or the raw video as video/mp4, video/quicktime, video/webm or application/octet-stream"}
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return &apiError{status: http.StatusUnsupportedMediaType, Code: codeUnsupportedMedia, Message: "malformed Content-Type"}
	}
	q := r.URL.Query()
	var m training.Media
	switch {
	case rawVideoTypes[mt]:
		m, err = s.svc.AttachVideo(r.Context(), ownerType, ownerRef, q.Get("filename"), q.Get("caption"), r.Body)
	case mt == "multipart/form-data":
		m, err = s.uploadMultipart(r, ownerType, ownerRef, q.Get("caption"))
	default:
		return &apiError{status: http.StatusUnsupportedMediaType, Code: codeUnsupportedMedia,
			Message: fmt.Sprintf("Content-Type %q is not accepted for uploads; use multipart/form-data or a video/* type", mt)}
	}
	if err != nil {
		if ownerType == training.OwnerSet {
			return orNotFound(err, "set %s does not exist", ownerRef)
		}
		return orNotFound(err, "exercise %q does not exist", ownerRef)
	}
	w.Header().Set("Location", Prefix+"/media/"+strconv.FormatInt(m.ID, 10))
	writeJSON(w, http.StatusCreated, m)
	return nil
}

// uploadMultipart reads parts in order: an optional `caption` text part, then
// the `file` part, which is streamed straight into storage. Parts after the
// file are not read.
func (s *Server) uploadMultipart(r *http.Request, ownerType, ownerRef, caption string) (training.Media, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return training.Media{}, badRequest("malformed multipart body: %v", err)
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return training.Media{}, invalidField("file", "is required: send the video as a multipart part named \"file\"")
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				return training.Media{}, err
			}
			return training.Media{}, badRequest("malformed multipart body: %v", err)
		}
		switch part.FormName() {
		case "caption":
			b, err := io.ReadAll(io.LimitReader(part, maxCaptionPart+1))
			part.Close()
			if err != nil {
				return training.Media{}, badRequest("could not read the caption part")
			}
			if len(b) > maxCaptionPart {
				return training.Media{}, invalidField("caption", "is too long")
			}
			caption = string(b)
		case "file":
			defer part.Close()
			return s.svc.AttachVideo(r.Context(), ownerType, ownerRef, part.FileName(), caption, part)
		default:
			part.Close()
			return training.Media{}, invalidField(part.FormName(), "unknown multipart part; send \"caption\" (optional) then \"file\"")
		}
	}
}

func (s *Server) listSetVideos(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	list, err := s.svc.SetMedia(r.Context(), id)
	if err != nil {
		return orNotFound(err, "set %d does not exist", id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"videos": list})
	return nil
}

func (s *Server) listExerciseVideos(w http.ResponseWriter, r *http.Request) error {
	if !s.svc.MediaEnabled() {
		return training.ErrMediaDisabled
	}
	ref := r.PathValue("ref")
	g, err := s.svc.ExerciseGuide(r.Context(), ref)
	if err != nil {
		return orNotFound(err, "exercise %q does not exist", ref)
	}
	writeJSON(w, http.StatusOK, map[string]any{"videos": g.Videos})
	return nil
}

// getMedia streams a video. http.ServeContent provides Range (206), If-Range
// and If-None-Match (304) handling.
func (s *Server) getMedia(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	m, rc, err := s.svc.OpenMedia(r.Context(), id)
	if err != nil {
		return orNotFound(err, "media %d does not exist", id)
	}
	defer rc.Close()
	h := w.Header()
	h.Set("Content-Type", m.ContentType)
	h.Set("Content-Disposition", contentDisposition(m.Filename))
	h.Set("ETag", strconv.Quote(m.SHA256))
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", parseTime(m.CreatedAt), rc)
	return nil
}

// parseTime reads a stored timestamp for Last-Modified; zero (header omitted)
// when it is in an unknown format.
func parseTime(v string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

// contentDisposition builds an inline disposition whose filename cannot break
// out of the header: control characters, quotes and backslashes are dropped,
// and non-ASCII names are RFC 2231-encoded by mime.FormatMediaType.
func contentDisposition(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '"' || r == '\\' || r == '/' {
			continue
		}
		b.WriteRune(r)
	}
	clean := strings.TrimSpace(b.String())
	if clean == "" {
		return "inline"
	}
	if v := mime.FormatMediaType("inline", map[string]string{"filename": clean}); v != "" {
		return v
	}
	return "inline"
}

func (s *Server) deleteMedia(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.svc.DeleteMedia(r.Context(), id); err != nil {
		return orNotFound(err, "media %d does not exist", id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) exerciseGuide(w http.ResponseWriter, r *http.Request) error {
	ref := r.PathValue("ref")
	g, err := s.svc.ExerciseGuide(r.Context(), ref)
	if err != nil {
		return orNotFound(err, "exercise %q does not exist", ref)
	}
	writeJSON(w, http.StatusOK, g)
	return nil
}

func (s *Server) putInstructions(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Instructions *string `json:"instructions"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}
	if body.Instructions == nil {
		return invalidField("instructions", "is required (an empty string clears them)")
	}
	ref := r.PathValue("ref")
	g, err := s.svc.SetExerciseInstructions(r.Context(), ref, *body.Instructions)
	if err != nil {
		return orNotFound(err, "exercise %q does not exist", ref)
	}
	writeJSON(w, http.StatusOK, g)
	return nil
}
