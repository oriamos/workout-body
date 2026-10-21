// Package mcpsrv exposes the training engine as MCP tools. It is a thin
// adapter over *training.Service: it maps tool arguments onto service calls,
// maps results back, and turns engine errors into actionable tool errors. It
// holds no business rules of its own.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is the MCP server for one training service.
type Server struct {
	mcp     *mcp.Server
	handler *mcp.StreamableHTTPHandler
	names   []string
}

// Shared description fragments, so every tool states the same contract in the
// same words.
const (
	workoutIsSession = "A 'workout' and a 'session' are the same thing: there is exactly one per local calendar date."
	dateRule         = "Dates are local calendar dates in exact YYYY-MM-DD format, in the server's configured timezone (get_workout reports the timezone and today's date); omit date to mean today."
	unitsRule        = "Units: weight_kg in kilograms; rpe is rate of perceived exertion 1-10; rir is reps in reserve 0-10."
	notRecordedRule  = "An omitted value means 'not recorded' and is stored as unknown: never invent, estimate or default a weight, reps, RPE or RIR the user did not state. weight_kg 0 means an explicit zero load, not unknown."
	idempotencyRule  = "idempotency_key (optional, at most 128 characters) is global to this single-user server, not scoped per date or tool. Retrying with the same key and the same payload replays the stored result (replayed=true) without writing anything again; the same key with a different payload is rejected as a conflict. Reuse a key only to retry the same intended write; use a new key for every new write."
	revisionRule     = "expected_revision (optional) makes the write fail with a conflict if the target changed since you read it; on conflict re-read and retry."
)

const serverInstructions = `Personal workout log for a single user. ` + workoutIsSession + ` ` + dateRule + ` ` + unitsRule + ` ` + notRecordedRule + `
Typical flow: get_workout to read today's state (sets with stable ids, set_number, revision, planned-vs-completed progress) -> log_sets to append completed sets and/or planned prescriptions -> update_set / delete_set by set id to correct. Prescriptions are plans and never create completed sets. ` + idempotencyRule

// New builds the MCP server and its stateless Streamable HTTP handler.
func New(service *training.Service) *Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "training-mcp", Version: "0.2.0"}, &mcp.ServerOptions{Instructions: serverInstructions})
	r := &registry{server: s}
	registerWorkoutTools(r, service)
	registerPlanTools(r, service)
	registerSessionPlanTools(r, service)
	registerExerciseTools(r, service)
	registerAnalyticsTools(r, service)
	registerFeedbackTools(r, service)

	// Stateless: the server keeps no per-client session state, so restarting the
	// container never invalidates a live conversation. These tools are plain
	// request/response with no server-initiated notifications, so nothing is lost.
	h := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return s },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	names := append([]string(nil), r.names...)
	sort.Strings(names)
	return &Server{mcp: s, handler: h, names: names}
}

// Handler serves MCP over Streamable HTTP. Authentication is the caller's
// job (see httpsrv.Bearer).
func (s *Server) Handler() http.Handler { return s.handler }

// RunStdio serves MCP over stdin/stdout until ctx is cancelled or the client
// disconnects. stdout carries protocol messages only; log to stderr.
func (s *Server) RunStdio(ctx context.Context) error {
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

// ToolNames lists every registered tool, sorted.
func (s *Server) ToolNames() []string { return append([]string(nil), s.names...) }

// registry records tool names as tools are added, so ToolNames can never drift
// from what is actually registered.
type registry struct {
	server *mcp.Server
	names  []string
}

func addTool[In, Out any](r *registry, tool *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	if tool.Annotations == nil {
		panic("mcpsrv: tool " + tool.Name + " has no annotations")
	}
	if tool.InputSchema == nil {
		tool.InputSchema = inputSchema[In]()
	}
	mcp.AddTool(r.server, tool, h)
	r.names = append(r.names, tool.Name)
}

// ---- annotations ----------------------------------------------------------

func boolPtr(v bool) *bool { return &v }

// readOnly marks a tool that never modifies stored data.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

// additive marks a tool that only adds data and never overwrites or removes it.
func additive(title string, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: boolPtr(false), IdempotentHint: idempotent, OpenWorldHint: boolPtr(false)}
}

// destructive marks a tool that may overwrite or remove stored data.
func destructive(title string, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: boolPtr(true), IdempotentHint: idempotent, OpenWorldHint: boolPtr(false)}
}

// ---- schemas --------------------------------------------------------------

// inputSchema infers a schema from T and makes optional pointer fields plain
// (non-nullable) types: omit a field instead of sending null.
func inputSchema[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	nonNullable(schema)
	return schema
}

func nonNullable(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Types) > 0 {
		var kept []string
		for _, t := range s.Types {
			if t != "null" {
				kept = append(kept, t)
			}
		}
		if len(kept) == 1 {
			s.Type, s.Types = kept[0], nil
		} else {
			s.Types = kept
		}
	}
	for _, p := range s.Properties {
		nonNullable(p)
	}
	nonNullable(s.Items)
}

const datePattern = `^\d{4}-\d{2}-\d{2}$`

func prop(s *jsonschema.Schema, name string) *jsonschema.Schema {
	p := s.Properties[name]
	if p == nil {
		panic("mcpsrv: schema has no property " + name)
	}
	return p
}

func setDate(s *jsonschema.Schema, names ...string) {
	for _, n := range names {
		prop(s, n).Pattern = datePattern
	}
}

func setPositiveID(s *jsonschema.Schema, names ...string) {
	for _, n := range names {
		prop(s, n).Minimum = jsonschema.Ptr(1.0)
	}
}

func setNonEmpty(s *jsonschema.Schema, names ...string) {
	for _, n := range names {
		prop(s, n).Pattern = `.*\S.*`
	}
}

func enumOf(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

func muscleGroupEnum() []any { return enumOf(training.MuscleGroups) }

// ---- errors ---------------------------------------------------------------

// toolError turns an engine error into a tool error the model can act on.
// Validation, conflict, not-found and ambiguity details are always passed
// through; only unexpected internal errors are masked, so storage details never
// leak.
func toolError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, training.ErrValidation):
		if iss := training.IssuesOf(err); len(iss) > 0 {
			parts := make([]string, 0, len(iss))
			for _, i := range iss {
				if i.Field == "" {
					parts = append(parts, i.Message)
				} else {
					parts = append(parts, i.Field+": "+i.Message)
				}
			}
			return fmt.Errorf("validation error: %s. Nothing was written. Fix these fields and call the tool again; never fill a missing value with a guess - omit it instead", strings.Join(parts, "; "))
		}
		return fmt.Errorf("%s. Nothing was written. Check the arguments against this tool's input schema and description, then call it again", err.Error())
	case errors.Is(err, training.ErrConflict):
		return fmt.Errorf("%s. Nothing was written. Re-read the current state (get_workout, get_set or get_plan) and retry with the current revision; if an idempotency_key was rejected, use a new key only if this is a genuinely different write", err.Error())
	case errors.Is(err, training.ErrNotFound):
		return fmt.Errorf("%s. Check the id, date or name (get_workout, list_sessions, list_plans or list_exercises list what exists)", err.Error())
	case errors.Is(err, training.ErrAmbiguous):
		return fmt.Errorf("%s. Use exercise_id or a more specific name; ask the user if it is unclear which one they mean", err.Error())
	}
	return errors.New("internal tool error")
}

func invalidArg(field, msg string) error {
	return &training.ValidationError{Issues: []training.Issue{{Field: field, Message: msg}}}
}
