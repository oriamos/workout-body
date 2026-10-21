package mcpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/fullfran/training-mcp/internal/training"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- shared set fields ----------------------------------------------------

// SetInput is one completed set. Every metric is optional: omitted means not
// recorded.
type SetInput struct {
	Exercise   string   `json:"exercise,omitempty" jsonschema:"Exercise name or alias, e.g. 'bench press'. Normalized to lower case. An unknown name creates a new exercise and is reported in new_exercises with similar existing names. Give exercise or exercise_id."`
	ExerciseID int64    `json:"exercise_id,omitempty" jsonschema:"Exact id of an existing exercise (from list_exercises or a previous result). Wins over exercise when both are given."`
	WeightKG   *float64 `json:"weight_kg,omitempty" jsonschema:"Load in kilograms, 0-2000. Omit if the user did not state it; 0 means an explicit zero load (e.g. bodyweight with no added load)."`
	Reps       *int     `json:"reps,omitempty" jsonschema:"Completed repetitions, 1-1000. Omit if not stated."`
	RPE        *float64 `json:"rpe,omitempty" jsonschema:"Rate of perceived exertion, 1-10 (decimals allowed). Omit if not stated."`
	RIR        *float64 `json:"rir,omitempty" jsonschema:"Reps in reserve, 0-10. Omit if not stated."`
	SetType    string   `json:"set_type,omitempty" jsonschema:"One of warmup, working, backoff, drop, failure. Defaults to working. Warm-ups do not count as completed working sets in progress or analytics."`
	Notes      string   `json:"notes,omitempty" jsonschema:"Free-text note for this set, at most 500 characters."`
	Technique  string   `json:"technique,omitempty" jsonschema:"Optional intensity technique, e.g. 'drop set', 'rest-pause', 'myo-reps'. Omit for a normal straight set."`
}

func (in SetInput) toAdd(sessionID int64) training.AddSetInput {
	return training.AddSetInput{
		SessionID: sessionID, ExerciseID: in.ExerciseID, Exercise: in.Exercise,
		WeightKG: in.WeightKG, Reps: in.Reps, RPE: in.RPE, RIR: in.RIR,
		SetType: in.SetType, Notes: in.Notes, Technique: in.Technique,
	}
}

// PrescriptionInput is what is planned for one exercise in a workout. It never
// creates completed sets.
type PrescriptionInput struct {
	Exercise       string   `json:"exercise" jsonschema:"Exercise name or alias the prescription is for."`
	Sets           int      `json:"sets" jsonschema:"Planned number of sets, 1-20. Replaces the previously planned count for this exercise."`
	RepMin         int      `json:"rep_min,omitempty" jsonschema:"Lower end of the target rep range. Omit to keep any stored value / not prescribed."`
	RepMax         int      `json:"rep_max,omitempty" jsonschema:"Upper end of the target rep range. For a fixed target use rep_min = rep_max."`
	TargetWeightKG *float64 `json:"target_weight_kg,omitempty" jsonschema:"Planned load in kilograms, 0-2000. Omit if not prescribed."`
	TargetRPE      *float64 `json:"target_rpe,omitempty" jsonschema:"Target RPE 1-10. Omit if not prescribed."`
	TargetRIR      *float64 `json:"target_rir,omitempty" jsonschema:"Target reps in reserve 0-10. Omit if not prescribed."`
	Notes          string   `json:"notes,omitempty" jsonschema:"Cues or intent for this exercise."`
	Superset       string   `json:"superset,omitempty" jsonschema:"Label grouping exercises done back to back, e.g. 'A'."`
}

func (p PrescriptionInput) toItem() training.PlanItem {
	it := training.PlanItem{
		Exercise: p.Exercise, TargetSets: p.Sets, RepMin: p.RepMin, RepMax: p.RepMax,
		TargetWeightKG: p.TargetWeightKG, TargetRIR: p.TargetRIR, Notes: p.Notes, Superset: p.Superset,
	}
	if p.TargetRPE != nil {
		it.TargetRPE = *p.TargetRPE
	}
	return it
}

func setTypeEnum(s *jsonschema.Schema) { s.Enum = enumOf(training.SetTypes) }

// ---- inputs and outputs ---------------------------------------------------

type GetWorkoutInput struct {
	Date string `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD in the configured timezone. Omit for today."`
}
type WorkoutOut struct {
	Exists   bool              `json:"exists"`
	Date     string            `json:"date"`
	Today    string            `json:"today"`
	Timezone string            `json:"timezone"`
	Workout  *training.Session `json:"workout,omitempty"`
}

type LogSetsInput struct {
	Date             string              `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD of the workout. Omit for today. Sets cannot be logged for a future date."`
	Sets             []SetInput          `json:"sets,omitempty" jsonschema:"Completed sets to append, in the order performed (at most 100). Only include sets the user actually did."`
	Prescriptions    []PrescriptionInput `json:"prescriptions,omitempty" jsonschema:"Planned work to add or merge into this workout's prescription (at most 40). Merging keeps stored fields you omit. Never creates completed sets."`
	PlanID           int64               `json:"plan_id,omitempty" jsonschema:"Optional plan to copy into the workout when it follows no plan yet. A workout already following a different plan is a conflict."`
	IdempotencyKey   string              `json:"idempotency_key,omitempty" jsonschema:"Optional retry key, global to this user. Same key + same payload replays; same key + different payload is a conflict. New write = new key."`
	ExpectedRevision *int                `json:"expected_revision,omitempty" jsonschema:"Optional workout revision from your last read; the write is rejected with a conflict if the workout changed since. Ignored when the workout does not exist yet."`
}

type LogSetInput struct {
	Date           string   `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD of the workout. Omit for today."`
	Exercise       string   `json:"exercise,omitempty" jsonschema:"Exercise name or alias. Give exercise or exercise_id."`
	ExerciseID     int64    `json:"exercise_id,omitempty" jsonschema:"Exact id of an existing exercise; wins over exercise."`
	WeightKG       *float64 `json:"weight_kg,omitempty" jsonschema:"Load in kilograms, 0-2000. Omit if not stated."`
	Reps           *int     `json:"reps,omitempty" jsonschema:"Completed repetitions per set, 1-1000. Omit if not stated."`
	RPE            *float64 `json:"rpe,omitempty" jsonschema:"RPE 1-10. Omit if not stated."`
	RIR            *float64 `json:"rir,omitempty" jsonschema:"Reps in reserve 0-10. Omit if not stated."`
	SetType        string   `json:"set_type,omitempty" jsonschema:"warmup, working (default), backoff, drop or failure."`
	Notes          string   `json:"notes,omitempty" jsonschema:"Free-text note copied onto each set."`
	Technique      string   `json:"technique,omitempty" jsonschema:"Optional intensity technique, e.g. 'drop set'."`
	Count          int      `json:"count,omitempty" jsonschema:"How many identical sets to record, 1-20; defaults to 1. Use it for '3x10 at 80kg'."`
	IdempotencyKey string   `json:"idempotency_key,omitempty" jsonschema:"Optional retry key, global to this user. Same key + same payload replays; same key + different payload is a conflict."`
}

type StartInput struct {
	Date   string `json:"date,omitempty" jsonschema:"Local date YYYY-MM-DD. Omit for today."`
	PlanID int64  `json:"plan_id,omitempty" jsonschema:"Optional plan to copy into the workout. Omit for a free-form workout."`
}
type SessionOut struct {
	Session training.Session `json:"session"`
}

type AddInput struct {
	SessionID  int64    `json:"session_id" jsonschema:"Id of the existing workout (session)."`
	Exercise   string   `json:"exercise,omitempty" jsonschema:"Exercise name or alias. Give exercise or exercise_id."`
	ExerciseID int64    `json:"exercise_id,omitempty" jsonschema:"Exact id of an existing exercise; wins over exercise."`
	WeightKG   *float64 `json:"weight_kg,omitempty" jsonschema:"Load in kilograms, 0-2000. Omit if not stated."`
	Reps       *int     `json:"reps,omitempty" jsonschema:"Completed repetitions, 1-1000. Omit if not stated."`
	RPE        *float64 `json:"rpe,omitempty" jsonschema:"RPE 1-10. Omit if not stated."`
	RIR        *float64 `json:"rir,omitempty" jsonschema:"Reps in reserve 0-10. Omit if not stated."`
	SetType    string   `json:"set_type,omitempty" jsonschema:"warmup, working (default), backoff, drop or failure."`
	Notes      string   `json:"notes,omitempty" jsonschema:"Free-text note for this set."`
	Technique  string   `json:"technique,omitempty" jsonschema:"Optional intensity technique, e.g. 'drop set'."`
}
type SetOut struct {
	Set     training.Set `json:"set"`
	TotalSI float64      `json:"total_si"`
}

type UpdateInput struct {
	SetID            int64    `json:"set_id" jsonschema:"Stable id of the set to change (from get_workout or a log result). Never use set_number or position as an id."`
	Exercise         *string  `json:"exercise,omitempty" jsonschema:"Move the set to this exercise name or alias."`
	ExerciseID       *int64   `json:"exercise_id,omitempty" jsonschema:"Move the set to this existing exercise id."`
	WeightKG         *float64 `json:"weight_kg,omitempty" jsonschema:"New load in kilograms, 0-2000."`
	Reps             *int     `json:"reps,omitempty" jsonschema:"New repetition count, 1-1000."`
	RPE              *float64 `json:"rpe,omitempty" jsonschema:"New RPE 1-10."`
	RIR              *float64 `json:"rir,omitempty" jsonschema:"New reps in reserve 0-10."`
	SetType          *string  `json:"set_type,omitempty" jsonschema:"New set type: warmup, working, backoff, drop or failure."`
	Notes            *string  `json:"notes,omitempty" jsonschema:"New note; empty string clears it."`
	Technique        *string  `json:"technique,omitempty" jsonschema:"New technique; empty string clears it back to a straight set."`
	Clear            []string `json:"clear,omitempty" jsonschema:"Metrics to reset to 'not recorded': any of weight_kg, reps, rpe, rir. Cannot be combined with a new value for the same field."`
	ExpectedRevision *int     `json:"expected_revision,omitempty" jsonschema:"The set's revision from your last read; rejected with a conflict if it changed since."`
}

type DeleteInput struct {
	SetID            int64 `json:"set_id" jsonschema:"Stable id of the set to delete."`
	ExpectedRevision *int  `json:"expected_revision,omitempty" jsonschema:"The set's revision from your last read; rejected with a conflict if it changed since."`
}
type DeleteOut struct {
	DeletedSetID int64   `json:"deleted_set_id"`
	SessionID    int64   `json:"session_id"`
	TotalSI      float64 `json:"total_si"`
	Remaining    int     `json:"remaining"`
}

type SetIDInput struct {
	SetID int64 `json:"set_id" jsonschema:"Stable id of the set."`
}
type GetSetOut struct {
	Set training.Set `json:"set"`
}

type SessionInput struct {
	SessionID int64 `json:"session_id" jsonschema:"Id of the existing workout (session)."`
}
type ListInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum workouts to return; defaults to 20, accepts 1-100."`
	From  string `json:"from,omitempty" jsonschema:"Inclusive earliest local date YYYY-MM-DD."`
	To    string `json:"to,omitempty" jsonschema:"Inclusive latest local date YYYY-MM-DD."`
}
type ListOut struct {
	Sessions []training.SessionSummary `json:"sessions"`
}
type DeleteSessionOut struct {
	SessionID   int64 `json:"session_id"`
	DeletedSets int   `json:"deleted_sets"`
}

// ---- registration ---------------------------------------------------------

func registerWorkoutTools(r *registry, service *training.Service) {
	getSchema := inputSchema[GetWorkoutInput]()
	setDate(getSchema, "date")
	addTool(r, &mcp.Tool{
		Name: "get_workout",
		Description: "Read the workout for a local date without creating anything. Returns exists=false when there is no workout for that date. " +
			"Always returns the configured timezone and today's local date, so 'today' and 'yesterday' can be resolved correctly. " +
			"The workout lists every completed set with its stable id (use it for update_set/delete_set), set_number (1-based order within its exercise; for reading only), revision and values (null = not recorded), " +
			"plus progress: planned (target_sets, rep range, target load/RPE/RIR) versus completed working sets (done_sets) per exercise. Planned sets are never counted as done. " +
			workoutIsSession + " " + dateRule,
		InputSchema: getSchema,
		Annotations: readOnly("Get workout"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetWorkoutInput) (*mcp.CallToolResult, WorkoutOut, error) {
		out := WorkoutOut{Today: service.Today(), Timezone: service.Location().String()}
		date, err := service.ResolveDate(in.Date)
		if err != nil {
			return nil, out, toolError(err)
		}
		out.Date = date
		w, err := service.GetWorkout(ctx, date)
		if errors.Is(err, training.ErrNotFound) {
			return nil, out, nil
		}
		if err != nil {
			return nil, out, toolError(err)
		}
		out.Exists, out.Workout = true, &w
		return nil, out, nil
	})

	logSetsSchema := inputSchema[LogSetsInput]()
	setDate(logSetsSchema, "date")
	setTypeEnum(prop(prop(logSetsSchema, "sets").Items, "set_type"))
	addTool(r, &mcp.Tool{
		Name: "log_sets",
		Description: "Primary write for incremental logging. In one atomic transaction: finds or creates the workout for the date, appends the given completed sets in order, and merges the given prescriptions (planned work). Nothing is written if any part is invalid. " +
			"Prescriptions describe what is planned and never create completed sets: 'planning 3 sets of squats' is a prescription, '130 kg x 4' is a set. " +
			"Returns the persisted sets with their ids and stored values, the stored prescriptions, the updated workout (revision, progress), created=true if the workout was new, " +
			"new_exercises for names that did not exist yet (each with similar existing names, so a typo can be caught and fixed with update_set or rename_exercise), and replayed=true when served from an earlier identical request. " +
			unitsRule + " " + notRecordedRule + " " + idempotencyRule + " " + revisionRule + " " + workoutIsSession + " " + dateRule,
		InputSchema: logSetsSchema,
		Annotations: additive("Log sets and prescriptions", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in LogSetsInput) (*mcp.CallToolResult, training.BatchResult, error) {
		b := training.BatchInput{Date: in.Date, PlanID: in.PlanID, IdempotencyKey: in.IdempotencyKey, ExpectedRevision: in.ExpectedRevision}
		for _, s := range in.Sets {
			b.Sets = append(b.Sets, s.toAdd(0))
		}
		for _, p := range in.Prescriptions {
			b.Prescriptions = append(b.Prescriptions, p.toItem())
		}
		res, err := service.LogSets(ctx, b)
		return nil, res, toolError(err)
	})

	logSchema := inputSchema[LogSetInput]()
	setDate(logSchema, "date")
	setTypeEnum(prop(logSchema, "set_type"))
	prop(logSchema, "count").Default = json.RawMessage("1")
	prop(logSchema, "count").Minimum = jsonschema.Ptr(1.0)
	prop(logSchema, "count").Maximum = jsonschema.Ptr(20.0)
	addTool(r, &mcp.Tool{
		Name: "log_set",
		Description: "Convenience for one exercise: record count (1-20) identical completed sets into the workout for the date (today when omitted), creating the workout if needed. Equivalent to log_sets with count copies of one set; returns the same result. " +
			"Use log_sets for several exercises or for prescriptions. " + unitsRule + " " + notRecordedRule + " " + idempotencyRule + " " + dateRule,
		InputSchema: logSchema,
		Annotations: additive("Log identical sets", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in LogSetInput) (*mcp.CallToolResult, training.BatchResult, error) {
		set := SetInput{Exercise: in.Exercise, ExerciseID: in.ExerciseID, WeightKG: in.WeightKG, Reps: in.Reps, RPE: in.RPE, RIR: in.RIR, SetType: in.SetType, Notes: in.Notes, Technique: in.Technique}
		res, err := service.LogSet(ctx, in.Date, set.toAdd(0), in.Count, in.IdempotencyKey)
		return nil, res, toolError(err)
	})

	startSchema := inputSchema[StartInput]()
	setDate(startSchema, "date")
	addTool(r, &mcp.Tool{
		Name: "start_session",
		Description: "Get-or-create the workout for a local date (today when omitted), optionally copying a plan into it. Calling it for a date that already has a workout returns that workout; nothing is duplicated. " +
			"A workout already following a different plan is a conflict. You rarely need this: log_sets creates the workout on first write, and get_workout reads it without creating. " +
			workoutIsSession + " " + dateRule,
		InputSchema: startSchema,
		Annotations: additive("Start or get workout", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in StartInput) (*mcp.CallToolResult, SessionOut, error) {
		v, err := service.StartPlannedSession(ctx, in.Date, in.PlanID)
		return nil, SessionOut{Session: v}, toolError(err)
	})

	addSchema := inputSchema[AddInput]()
	setPositiveID(addSchema, "session_id")
	setTypeEnum(prop(addSchema, "set_type"))
	addTool(r, &mcp.Tool{
		Name: "add_set",
		Description: "Append one completed set to an existing workout addressed by session id, returning the persisted set (with its id) and the workout's total SI. Prefer log_sets, which needs no id and supports idempotent retries. " +
			unitsRule + " " + notRecordedRule,
		InputSchema: addSchema,
		Annotations: additive("Add set by session id", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AddInput) (*mcp.CallToolResult, SetOut, error) {
		set := SetInput{Exercise: in.Exercise, ExerciseID: in.ExerciseID, WeightKG: in.WeightKG, Reps: in.Reps, RPE: in.RPE, RIR: in.RIR, SetType: in.SetType, Notes: in.Notes, Technique: in.Technique}
		v, total, err := service.AddSet(ctx, set.toAdd(in.SessionID))
		return nil, SetOut{Set: v, TotalSI: total}, toolError(err)
	})

	updateSchema := inputSchema[UpdateInput]()
	setPositiveID(updateSchema, "set_id")
	setTypeEnum(prop(updateSchema, "set_type"))
	prop(updateSchema, "clear").Items.Enum = enumOf([]string{"weight_kg", "reps", "rpe", "rir"})
	updateSchema.MinProperties = jsonschema.Ptr(2)
	addTool(r, &mcp.Tool{
		Name: "update_set",
		Description: "Correct one completed set addressed by its stable set_id (read it from get_workout or a log result; never guess it from set_number). Only the fields given change; omitted fields stay as stored. " +
			"Use clear to turn a recorded weight_kg/reps/rpe/rir back into 'not recorded'. Returns the persisted set with its new revision. " +
			unitsRule + " " + revisionRule,
		InputSchema: updateSchema,
		Annotations: destructive("Update set", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in UpdateInput) (*mcp.CallToolResult, SetOut, error) {
		p := training.SetPatch{
			ExerciseID: in.ExerciseID, Exercise: in.Exercise, WeightKG: in.WeightKG, Reps: in.Reps, RPE: in.RPE, RIR: in.RIR,
			SetType: in.SetType, Notes: in.Notes, Technique: in.Technique, ExpectedRevision: in.ExpectedRevision,
		}
		for i, c := range in.Clear {
			switch c {
			case "weight_kg":
				p.ClearWeight = true
			case "reps":
				p.ClearReps = true
			case "rpe":
				p.ClearRPE = true
			case "rir":
				p.ClearRIR = true
			default:
				return nil, SetOut{}, toolError(invalidArg("clear["+strconv.Itoa(i)+"]", "must be one of weight_kg, reps, rpe, rir"))
			}
		}
		v, total, err := service.UpdateSet(ctx, in.SetID, p)
		return nil, SetOut{Set: v, TotalSI: total}, toolError(err)
	})

	deleteSchema := inputSchema[DeleteInput]()
	setPositiveID(deleteSchema, "set_id")
	addTool(r, &mcp.Tool{
		Name:        "delete_set",
		Description: "Permanently delete one completed set by its stable set_id. Irreversible: confirm with the user first unless they explicitly asked for this deletion. Remaining sets keep their ids. " + revisionRule,
		InputSchema: deleteSchema,
		Annotations: destructive("Delete set", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DeleteInput) (*mcp.CallToolResult, DeleteOut, error) {
		sid, total, n, err := service.DeleteSet(ctx, in.SetID, in.ExpectedRevision)
		return nil, DeleteOut{DeletedSetID: in.SetID, SessionID: sid, TotalSI: total, Remaining: n}, toolError(err)
	})

	getSetSchema := inputSchema[SetIDInput]()
	setPositiveID(getSetSchema, "set_id")
	addTool(r, &mcp.Tool{
		Name:        "get_set",
		Description: "Read one completed set by its stable id, including its current revision. Values are null when not recorded.",
		InputSchema: getSetSchema,
		Annotations: readOnly("Get set"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SetIDInput) (*mcp.CallToolResult, GetSetOut, error) {
		v, err := service.GetSet(ctx, in.SetID)
		return nil, GetSetOut{Set: v}, toolError(err)
	})

	sessionSchema := inputSchema[SessionInput]()
	setPositiveID(sessionSchema, "session_id")
	addTool(r, &mcp.Tool{
		Name:        "get_session",
		Description: "Read a workout by its session id with its ordered sets (stable ids, values; null = not recorded), revision and total SI. Use get_workout to read by date with set numbers and progress. " + workoutIsSession,
		InputSchema: sessionSchema,
		Annotations: readOnly("Get session by id"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SessionInput) (*mcp.CallToolResult, SessionOut, error) {
		v, err := service.GetSession(ctx, in.SessionID)
		return nil, SessionOut{Session: v}, toolError(err)
	})

	listSchema := inputSchema[ListInput]()
	prop(listSchema, "limit").Default = json.RawMessage("20")
	prop(listSchema, "limit").Minimum = jsonschema.Ptr(1.0)
	prop(listSchema, "limit").Maximum = jsonschema.Ptr(100.0)
	setDate(listSchema, "from", "to")
	addTool(r, &mcp.Tool{
		Name:        "list_sessions",
		Description: "List workouts newest first, optionally within inclusive local date bounds (from must not be after to). " + workoutIsSession,
		InputSchema: listSchema,
		Annotations: readOnly("List workouts"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListOut, error) {
		v, err := service.ListSessions(ctx, training.ListFilter{Limit: in.Limit, From: in.From, To: in.To})
		return nil, ListOut{Sessions: v}, toolError(err)
	})

	deleteSessionSchema := inputSchema[SessionInput]()
	setPositiveID(deleteSessionSchema, "session_id")
	addTool(r, &mcp.Tool{
		Name:        "delete_session",
		Description: "Permanently delete a workout and every set in it. Irreversible: confirm with the user first. Returns how many sets were destroyed. " + workoutIsSession,
		InputSchema: deleteSessionSchema,
		Annotations: destructive("Delete workout", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SessionInput) (*mcp.CallToolResult, DeleteSessionOut, error) {
		n, err := service.DeleteSession(ctx, in.SessionID)
		return nil, DeleteSessionOut{SessionID: in.SessionID, DeletedSets: n}, toolError(err)
	})
}
