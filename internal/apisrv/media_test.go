package apisrv

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"
)

func logSet(t *testing.T, e *env, exercise string) int {
	t.Helper()
	m := expect(t, e.do("POST", "/api/v1/workouts/today/sets", map[string]any{"sets": []any{
		map[string]any{"exercise": exercise, "weight_kg": 60, "reps": 8},
	}}, nil), 201)
	return num(list(m, "sets")[0].(map[string]any)["id"])
}

func video(ct string) map[string]string { return h("Content-Type", ct) }

// multipartBody builds a form with an optional caption part before the file.
func multipartBody(t *testing.T, caption *string, fileField, filename string, data []byte, extra ...string) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for i := 0; i+1 < len(extra); i += 2 {
		mw.WriteField(extra[i], extra[i+1])
	}
	if caption != nil {
		mw.WriteField("caption", *caption)
	}
	if fileField != "" {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, fileField, filename))
		hdr.Set("Content-Type", "video/mp4") // declared type is never trusted
		pw, err := mw.CreatePart(hdr)
		if err != nil {
			t.Fatal(err)
		}
		pw.Write(data)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestVideoRawUploadStreamAndRange(t *testing.T) {
	e := newMediaEnv(t, 1<<20)
	cfg := expect(t, e.do("GET", "/api/v1/config", nil, nil), 200)
	if md := cfg["media"].(map[string]any); md["enabled"] != true || num(md["max_video_bytes"]) != 1<<20 {
		t.Fatalf("config media = %v", md)
	}
	setID := logSet(t, e, "squat")
	data := mp4(2048)
	r := e.do("POST", fmt.Sprintf("/api/v1/sets/%d/videos?filename=../form%%20check.mp4&caption=depth", setID), data, video("application/octet-stream"))
	m := expect(t, r, 201)
	id := num(m["id"])
	if m["owner_type"] != "set" || num(m["owner_id"]) != setID || m["content_type"] != "video/mp4" || num(m["size_bytes"]) != 2048 ||
		m["filename"] != "form check.mp4" || m["caption"] != "depth" {
		t.Fatalf("media = %v", m)
	}
	if r.header.Get("Location") != fmt.Sprintf("/api/v1/media/%d", id) {
		t.Fatalf("Location = %q", r.header.Get("Location"))
	}
	l := expect(t, e.do("GET", fmt.Sprintf("/api/v1/sets/%d/videos", setID), nil, nil), 200)
	if len(list(l, "videos")) != 1 {
		t.Fatalf("videos = %v", l)
	}

	mediaPath := fmt.Sprintf("/api/v1/media/%d", id)
	r = e.do("GET", mediaPath, nil, nil)
	if r.status != 200 || !bytes.Equal(r.body, data) {
		t.Fatalf("stream: %d, %d bytes", r.status, len(r.body))
	}
	for k, want := range map[string]string{
		"Content-Type":        "video/mp4",
		"ETag":                `"` + m["sha256"].(string) + `"`,
		"Cache-Control":       "private, max-age=3600",
		"Content-Disposition": `inline; filename="form check.mp4"`,
		"Accept-Ranges":       "bytes",
	} {
		if got := r.header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	r = e.do("GET", mediaPath, nil, h("Range", "bytes=100-199"))
	if r.status != 206 || !bytes.Equal(r.body, data[100:200]) || r.header.Get("Content-Range") != "bytes 100-199/2048" {
		t.Fatalf("range: %d %q %d bytes", r.status, r.header.Get("Content-Range"), len(r.body))
	}
	r = e.do("GET", mediaPath, nil, h("Range", "bytes=-10"))
	if r.status != 206 || !bytes.Equal(r.body, data[2038:]) {
		t.Fatalf("suffix range: %d", r.status)
	}
	if r = e.do("GET", mediaPath, nil, h("If-None-Match", r.header.Get("ETag"))); r.status != 304 {
		t.Fatalf("If-None-Match: %d", r.status)
	}
	hasIssue(t, expectErr(t, e.do("GET", "/api/v1/media/x", nil, nil), 400, "validation_error"), "id")
	expectErr(t, e.do("GET", "/api/v1/media/999", nil, nil), 404, "not_found")

	// Upload to missing owners leaves nothing behind.
	expectErr(t, e.do("POST", "/api/v1/sets/999/videos", data, video("video/mp4")), 404, "not_found")
	expectErr(t, e.do("POST", "/api/v1/exercises/nope/videos", data, video("video/mp4")), 404, "not_found")
	if n := e.filesOnDisk(); n != 1 {
		t.Fatalf("%d files on disk, want 1", n)
	}
}

func TestVideoMultipartUploadAndGuide(t *testing.T) {
	e := newMediaEnv(t, 1<<20)
	logSet(t, e, "Front Squat")
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 500)...)
	caption := "  cue: knees out "
	body, ct := multipartBody(t, &caption, "file", "how to.webm", webm)
	r := e.do("POST", "/api/v1/exercises/front%20squat/videos?caption=ignored", body, h("Content-Type", ct))
	m := expect(t, r, 201)
	if m["owner_type"] != "exercise" || m["content_type"] != "video/webm" || m["filename"] != "how to.webm" || m["caption"] != "cue: knees out" {
		t.Fatalf("media = %v", m)
	}
	l := expect(t, e.do("GET", "/api/v1/exercises/front%20squat/videos", nil, nil), 200)
	if len(list(l, "videos")) != 1 {
		t.Fatalf("videos = %v", l)
	}
	if r := e.do("GET", fmt.Sprintf("/api/v1/media/%d", num(m["id"])), nil, nil); r.status != 200 || !bytes.Equal(r.body, webm) || r.header.Get("Content-Type") != "video/webm" {
		t.Fatalf("stream: %d %q", r.status, r.header.Get("Content-Type"))
	}

	// Guide and instructions.
	g := expect(t, e.do("PUT", "/api/v1/exercises/front%20squat/instructions", map[string]any{"instructions": "  Elbows high.  "}, nil), 200)
	if g["instructions"] != "Elbows high." || g["exercise"] != "front squat" || len(list(g, "videos")) != 1 {
		t.Fatalf("guide = %v", g)
	}
	g = expect(t, e.do("GET", "/api/v1/exercises/front%20squat/guide", nil, nil), 200)
	if g["instructions"] != "Elbows high." {
		t.Fatalf("guide = %v", g)
	}
	g = expect(t, e.do("PUT", "/api/v1/exercises/front%20squat/instructions", map[string]any{"instructions": ""}, nil), 200)
	if g["instructions"] != "" {
		t.Fatalf("cleared guide = %v", g)
	}
	hasIssue(t, expectErr(t, e.do("PUT", "/api/v1/exercises/front%20squat/instructions", map[string]any{}, nil), 400, "validation_error"), "instructions")
	hasIssue(t, expectErr(t, e.do("PUT", "/api/v1/exercises/front%20squat/instructions", map[string]any{"instructions": strings.Repeat("x", 5001)}, nil), 400, "validation_error"), "instructions")
	expectErr(t, e.do("PUT", "/api/v1/exercises/nope/instructions", map[string]any{"instructions": "x"}, nil), 404, "not_found")
	expectErr(t, e.do("GET", "/api/v1/exercises/nope/guide", nil, nil), 404, "not_found")

	// Malformed forms.
	body, ct = multipartBody(t, &caption, "", "", nil)
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/exercises/front%20squat/videos", body, h("Content-Type", ct)), 400, "validation_error"), "file")
	body, ct = multipartBody(t, nil, "file", "a.mp4", mp4(100), "colour", "red")
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/exercises/front%20squat/videos", body, h("Content-Type", ct)), 400, "validation_error"), "colour")
	long := strings.Repeat("c", 201)
	body, ct = multipartBody(t, &long, "file", "a.mp4", mp4(100))
	hasIssue(t, expectErr(t, e.do("POST", "/api/v1/exercises/front%20squat/videos", body, h("Content-Type", ct)), 400, "validation_error"), "caption")
	expectErr(t, e.do("POST", "/api/v1/exercises/front%20squat/videos", "--x--", h("Content-Type", "multipart/form-data")), 400, "bad_request")
	if n := e.filesOnDisk(); n != 1 {
		t.Fatalf("%d files on disk, want 1", n)
	}
}

func TestVideoContentIsSniffed(t *testing.T) {
	e := newMediaEnv(t, 1<<20)
	setID := logSet(t, e, "bench")
	path := fmt.Sprintf("/api/v1/sets/%d/videos?filename=evil.mp4", setID)
	script := "#!/bin/sh\necho owned\n"
	hasIssue(t, expectErr(t, e.do("POST", path, script, video("video/mp4")), 400, "validation_error"), "file")
	body, ct := multipartBody(t, nil, "file", "evil.mp4", []byte(script))
	hasIssue(t, expectErr(t, e.do("POST", path, body, h("Content-Type", ct)), 400, "validation_error"), "file")
	expectErr(t, e.do("POST", path, mp4(100), video("text/plain")), 415, "unsupported_media_type")
	expectErr(t, e.do("POST", path, mp4(100), video("")), 415, "unsupported_media_type")
	expectErr(t, e.do("POST", path, mp4(100), video("application/json")), 415, "unsupported_media_type")
	if n := e.filesOnDisk(); n != 0 {
		t.Fatalf("rejected uploads left %d files", n)
	}
	// A real MOV declared as octet-stream is stored as QuickTime.
	mov := mp4(64)
	copy(mov[8:12], "qt  ")
	if m := expect(t, e.do("POST", path, mov, video("application/octet-stream")), 201); m["content_type"] != "video/quicktime" {
		t.Fatalf("mov = %v", m)
	}
}

func TestVideoTooLarge(t *testing.T) {
	e := newMediaEnv(t, 1000)
	setID := logSet(t, e, "bench")
	path := fmt.Sprintf("/api/v1/sets/%d/videos", setID)
	expectErr(t, e.do("POST", path, mp4(1001), video("video/mp4")), 413, "payload_too_large")
	body, ct := multipartBody(t, nil, "file", "big.mp4", mp4(1001))
	expectErr(t, e.do("POST", path, body, h("Content-Type", ct)), 413, "payload_too_large")
	// Far past the transport cap as well.
	expectErr(t, e.do("POST", path, mp4(200<<10), video("video/mp4")), 413, "payload_too_large")
	if n := e.filesOnDisk(); n != 0 {
		t.Fatalf("oversize uploads left %d files", n)
	}
	expect(t, e.do("POST", path, mp4(1000), video("video/mp4")), 201)
}

func TestVideoPerSetLimitAndDeletion(t *testing.T) {
	e := newMediaEnv(t, 1<<20)
	setID := logSet(t, e, "bench")
	path := fmt.Sprintf("/api/v1/sets/%d/videos", setID)
	var ids []int
	for i := range 5 {
		m := expect(t, e.do("POST", path, mp4(100+i), video("video/mp4")), 201)
		ids = append(ids, num(m["id"]))
	}
	expectErr(t, e.do("POST", path, mp4(100), video("video/mp4")), 409, "conflict")
	if n := e.filesOnDisk(); n != 5 {
		t.Fatalf("%d files, want 5", n)
	}

	// DELETE /media/{id}.
	first := fmt.Sprintf("/api/v1/media/%d", ids[0])
	if r := e.do("DELETE", first, nil, nil); r.status != 204 {
		t.Fatalf("delete media: %d %s", r.status, r.body)
	}
	expectErr(t, e.do("GET", first, nil, nil), 404, "not_found")
	expectErr(t, e.do("DELETE", first, nil, nil), 404, "not_found")

	// Deleting the set removes its remaining videos and files.
	expect(t, e.do("DELETE", fmt.Sprintf("/api/v1/sets/%d", setID), nil, h("If-Match", "*")), 200)
	expectErr(t, e.do("GET", fmt.Sprintf("/api/v1/media/%d", ids[1]), nil, nil), 404, "not_found")
	expectErr(t, e.do("GET", path, nil, nil), 404, "not_found")
	if n := e.filesOnDisk(); n != 0 {
		t.Fatalf("%d files left after deleting the set", n)
	}
}

func TestMediaDisabled(t *testing.T) {
	e := newEnv(t)
	setID := logSet(t, e, "bench")
	expectErr(t, e.do("POST", fmt.Sprintf("/api/v1/sets/%d/videos", setID), mp4(100), video("video/mp4")), 501, "media_disabled")
	expectErr(t, e.do("POST", "/api/v1/exercises/bench/videos", mp4(100), video("video/mp4")), 501, "media_disabled")
	expectErr(t, e.do("GET", fmt.Sprintf("/api/v1/sets/%d/videos", setID), nil, nil), 501, "media_disabled")
	expectErr(t, e.do("GET", "/api/v1/exercises/bench/videos", nil, nil), 501, "media_disabled")
	expectErr(t, e.do("GET", "/api/v1/media/1", nil, nil), 501, "media_disabled")
	expectErr(t, e.do("DELETE", "/api/v1/media/1", nil, nil), 501, "media_disabled")
	expectErr(t, e.do("PUT", "/api/v1/exercises/bench/instructions", map[string]any{"instructions": "x"}, nil), 501, "media_disabled")
	g := expect(t, e.do("GET", "/api/v1/exercises/bench/guide", nil, nil), 200)
	if len(list(g, "videos")) != 0 || g["instructions"] != "" {
		t.Fatalf("guide without media = %v", g)
	}
}

func TestMediaRequiresAuth(t *testing.T) {
	e := newMediaEnv(t, 1<<20)
	setID := logSet(t, e, "bench")
	m := expect(t, e.do("POST", fmt.Sprintf("/api/v1/sets/%d/videos", setID), mp4(100), video("video/mp4")), 201)
	noAuth := h("Authorization", "")
	if r := e.do("GET", fmt.Sprintf("/api/v1/media/%d", num(m["id"])), nil, noAuth); r.status != 401 || bytes.Contains(r.body, []byte("ftyp")) {
		t.Fatalf("unauthenticated GET /media: %d", r.status)
	}
	if r := e.do("POST", fmt.Sprintf("/api/v1/sets/%d/videos", setID), mp4(100), h("Authorization", "", "Content-Type", "video/mp4")); r.status != 401 {
		t.Fatalf("unauthenticated upload: %d", r.status)
	}
	if n := e.filesOnDisk(); n != 1 {
		t.Fatalf("%d files, want 1", n)
	}
}

func TestContentDisposition(t *testing.T) {
	for in, want := range map[string]string{
		"a.mp4":            `inline; filename=a.mp4`,
		`x"; evil=1.mp4`:   `inline; evil=1.mp4"`,
		"día\r\n.mp4":      `inline; filename*=utf-8''d%C3%ADa.mp4`,
		"":                 "inline",
		"form check.mp4":   `inline; filename="form check.mp4"`,
		"../../etc/passwd": `inline; filename=....etcpasswd`,
	} {
		got := contentDisposition(in)
		if in == `x"; evil=1.mp4` {
			if strings.Contains(got, `"; evil`) {
				t.Errorf("header injection: %q", got)
			}
			continue
		}
		if got != want {
			t.Errorf("contentDisposition(%q) = %q, want %q", in, got, want)
		}
	}
}
