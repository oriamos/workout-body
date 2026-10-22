package apisrv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

// ---- errors ---------------------------------------------------------------

const (
	codeValidation           = "validation_error"
	codeNotFound             = "not_found"
	codeConflict             = "conflict"
	codePreconditionFailed   = "precondition_failed"
	codePreconditionRequired = "precondition_required"
	codeBadRequest           = "bad_request"
	codeInternal             = "internal"
	codeMethodNotAllowed     = "method_not_allowed"
	codeUnsupportedMedia     = "unsupported_media_type"
	codePayloadTooLarge      = "payload_too_large"
	codeMediaDisabled        = "media_disabled"
	codeNutritionDisabled    = "nutrition_disabled"
	codeWeightDisabled       = "weight_disabled"
)

type errorBody struct {
	Error apiError `json:"error"`
}

// apiError is both the wire shape of an error and an error a handler can
// return to choose the status explicitly.
type apiError struct {
	status  int
	Code    string           `json:"code"`
	Message string           `json:"message"`
	Issues  []training.Issue `json:"issues,omitempty"`
}

func (e *apiError) Error() string { return e.Message }

func badRequest(format string, args ...any) error {
	return &apiError{status: http.StatusBadRequest, Code: codeBadRequest, Message: fmt.Sprintf(format, args...)}
}

func invalidField(field, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return &apiError{status: http.StatusBadRequest, Code: codeValidation, Message: field + ": " + msg,
		Issues: []training.Issue{{Field: field, Message: msg}}}
}

func invalidIssues(iss []training.Issue) error {
	parts := make([]string, 0, len(iss))
	for _, i := range iss {
		parts = append(parts, i.Field+": "+i.Message)
	}
	return &apiError{status: http.StatusBadRequest, Code: codeValidation, Message: "validation error: " + strings.Join(parts, "; "), Issues: iss}
}

// orNotFound gives a bare training.ErrNotFound a useful message; wrapped
// errors already carry one.
func orNotFound(err error, format string, args ...any) error {
	if err == training.ErrNotFound {
		return &apiError{status: http.StatusNotFound, Code: codeNotFound, Message: fmt.Sprintf(format, args...)}
	}
	return err
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &ae):
		// already shaped
	case errors.Is(err, training.ErrTooLarge):
		ae = &apiError{status: http.StatusRequestEntityTooLarge, Code: codePayloadTooLarge, Message: err.Error()}
	case errors.As(err, &mbe):
		ae = &apiError{status: http.StatusRequestEntityTooLarge, Code: codePayloadTooLarge, Message: fmt.Sprintf("request body exceeds %d bytes", mbe.Limit)}
	case errors.Is(err, training.ErrMediaDisabled):
		ae = &apiError{status: http.StatusNotImplemented, Code: codeMediaDisabled, Message: "video storage is not configured on this server"}
	case errors.Is(err, training.ErrNutritionDisabled):
		ae = &apiError{status: http.StatusNotImplemented, Code: codeNutritionDisabled, Message: "nutrition tracking is not available on this server"}
	case errors.Is(err, training.ErrWeightDisabled):
		ae = &apiError{status: http.StatusNotImplemented, Code: codeWeightDisabled, Message: "weight tracking is not available on this server"}
	case errors.Is(err, training.ErrValidation):
		ae = &apiError{status: http.StatusBadRequest, Code: codeValidation, Message: err.Error(), Issues: training.IssuesOf(err)}
	case errors.Is(err, training.ErrNotFound):
		ae = &apiError{status: http.StatusNotFound, Code: codeNotFound, Message: err.Error()}
	case errors.Is(err, training.ErrStale):
		ae = &apiError{status: http.StatusPreconditionFailed, Code: codePreconditionFailed, Message: err.Error()}
	case errors.Is(err, training.ErrConflict):
		ae = &apiError{status: http.StatusConflict, Code: codeConflict, Message: err.Error()}
	case errors.Is(err, training.ErrAmbiguous):
		ae = &apiError{status: http.StatusConflict, Code: codeConflict, Message: err.Error()}
	default:
		s.logger.Printf("apisrv: %s %s: %v", r.Method, r.URL.Path, err)
		ae = &apiError{status: http.StatusInternalServerError, Code: codeInternal, Message: "internal server error"}
	}
	if ae.status == 0 {
		ae.status = http.StatusBadRequest
	}
	writeJSON(w, ae.status, errorBody{Error: *ae})
}

// ---- responses ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"code":"internal","message":"internal server error"}}`+"\n")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func setETag(w http.ResponseWriter, revision int) {
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(revision)))
}

// ---- requests -------------------------------------------------------------

func requireJSON(r *http.Request, extra ...string) error {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return &apiError{status: http.StatusUnsupportedMediaType, Code: codeUnsupportedMedia, Message: "Content-Type: application/json is required"}
	}
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return &apiError{status: http.StatusUnsupportedMediaType, Code: codeUnsupportedMedia, Message: "malformed Content-Type"}
	}
	if cs, ok := params["charset"]; ok && !strings.EqualFold(cs, "utf-8") {
		return &apiError{status: http.StatusUnsupportedMediaType, Code: codeUnsupportedMedia, Message: "only charset=utf-8 is accepted"}
	}
	if mt == "application/json" {
		return nil
	}
	for _, e := range extra {
		if mt == e {
			return nil
		}
	}
	return &apiError{status: http.StatusUnsupportedMediaType, Code: codeUnsupportedMedia, Message: fmt.Sprintf("Content-Type %q is not supported; use application/json", mt)}
}

// readBody returns the raw JSON body, bounded in size.
func readBody(w http.ResponseWriter, r *http.Request, extraTypes ...string) ([]byte, error) {
	if err := requireJSON(r, extraTypes...); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, &apiError{status: http.StatusRequestEntityTooLarge, Code: codePayloadTooLarge, Message: fmt.Sprintf("request body exceeds %d bytes", maxBodyBytes)}
		}
		return nil, badRequest("could not read request body")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, badRequest("request body is empty; send a JSON object")
	}
	return data, nil
}

// decodeBody strictly decodes one JSON object into dst: unknown fields,
// trailing data and type mismatches are rejected.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	data, err := readBody(w, r)
	if err != nil {
		return err
	}
	return decodeStrict(data, dst)
}

func decodeStrict(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return badRequest("request body must contain a single JSON value")
	}
	return nil
}

func decodeError(err error) error {
	var se *json.SyntaxError
	var ute *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		return badRequest("malformed JSON at byte %d: %v", se.Offset, err)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return badRequest("malformed JSON: unexpected end of input")
	case errors.As(err, &ute):
		field := ute.Field
		if field == "" {
			return badRequest("request body must be a JSON object")
		}
		return invalidField(field, "expected %s, got %s", jsonType(ute.Type.Kind().String()), ute.Value)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		name, uerr := strconv.Unquote(strings.TrimPrefix(err.Error(), "json: unknown field "))
		if uerr != nil {
			name = strings.TrimPrefix(err.Error(), "json: unknown field ")
		}
		return invalidField(name, "unknown field")
	default:
		return badRequest("invalid JSON body: %v", err)
	}
}

func jsonType(kind string) string {
	switch kind {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return "integer"
	case "float32", "float64":
		return "number"
	case "bool":
		return "boolean"
	case "slice", "array":
		return "array"
	case "map", "struct":
		return "object"
	default:
		return kind
	}
}

// ifMatch parses an If-Match header. present reports whether the header was
// sent; rev is nil for "*" (any current revision).
func ifMatch(r *http.Request) (rev *int, present bool, err error) {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	if v == "" {
		return nil, false, nil
	}
	if v == "*" {
		return nil, true, nil
	}
	n, perr := training.ParseRevision(v)
	if perr != nil || n == nil {
		return nil, true, &apiError{status: http.StatusBadRequest, Code: codeBadRequest,
			Message: fmt.Sprintf("If-Match %q is not a valid entity tag; send the ETag you read, e.g. \"3\"", v)}
	}
	return n, true, nil
}

// requireIfMatch is ifMatch for mutations that must not be blind.
func requireIfMatch(r *http.Request) (*int, error) {
	rev, present, err := ifMatch(r)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, &apiError{status: http.StatusPreconditionRequired, Code: codePreconditionRequired,
			Message: "If-Match is required: send the ETag (revision) from your last read of this resource"}
	}
	return rev, nil
}

func pathID(r *http.Request, name string) (int64, error) {
	v := r.PathValue(name)
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, invalidField(name, "%q is not a valid id (positive integer)", v)
	}
	return n, nil
}

// date resolves a local calendar date: "today" or YYYY-MM-DD.
func (s *Server) date(field, v string) (string, error) {
	if v == "today" {
		return s.svc.Today(), nil
	}
	d, err := s.svc.ResolveDate(v)
	if err != nil {
		return "", invalidField(field, "%q is not a valid calendar date; use YYYY-MM-DD or today", v)
	}
	return d, nil
}

// optDate is date for an optional query parameter; empty stays empty.
func (s *Server) optDate(r *http.Request, name string) (string, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return "", nil
	}
	return s.date(name, v)
}

func queryInt(r *http.Request, name string) (int, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, invalidField(name, "%q is not an integer", v)
	}
	return n, nil
}

func queryBool(r *http.Request, name string) (bool, error) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	switch strings.ToLower(v) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, invalidField(name, "%q is not a boolean; use true or false", v)
}

// ---- merge-patch helpers ----------------------------------------------------

// patchObject decodes a JSON object into raw members, rejecting keys not in
// allowed. A member whose value is JSON null is reported in nulls.
func patchObject(data []byte, allowed map[string]bool) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := decodeStrict(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, badRequest("request body must be a JSON object")
	}
	var iss []training.Issue
	for k := range raw {
		if !allowed[k] {
			iss = append(iss, training.Issue{Field: k, Message: "unknown field"})
		}
	}
	if len(iss) > 0 {
		sortIssues(iss)
		return nil, invalidIssues(iss)
	}
	if len(raw) == 0 {
		return nil, &apiError{status: http.StatusBadRequest, Code: codeValidation, Message: "provide at least one field to change"}
	}
	return raw, nil
}

func isNull(v json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(v), []byte("null")) }

// member decodes one patch member into dst, reporting a typed issue.
func member(raw map[string]json.RawMessage, key string, dst any, iss *[]training.Issue) bool {
	v, ok := raw[key]
	if !ok {
		return false
	}
	if err := json.Unmarshal(v, dst); err != nil {
		var ute *json.UnmarshalTypeError
		msg := "has the wrong type"
		if errors.As(err, &ute) {
			msg = fmt.Sprintf("expected %s, got %s", jsonType(ute.Type.Kind().String()), ute.Value)
		}
		*iss = append(*iss, training.Issue{Field: key, Message: msg})
		return false
	}
	return true
}

func sortIssues(iss []training.Issue) {
	for i := 1; i < len(iss); i++ {
		for j := i; j > 0 && iss[j].Field < iss[j-1].Field; j-- {
			iss[j], iss[j-1] = iss[j-1], iss[j]
		}
	}
}
