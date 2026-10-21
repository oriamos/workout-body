package mcpsrv

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/fullfran/training-mcp/internal/training"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- in-workout prescriptions --------------------------------------------

type ProgressOut struct {
	SessionID int64                   `json:"session_id"`
	PlanName  string                  `json:"plan_name,omitempty"`
	Progress  []training.PlanProgress `json:"progress"`
}
type SessionItemInput struct {
	SessionID int64 `json:"session_id" jsonschema:"Id of the workout (session) to adjust."`
	PlanItemInput
}
type AdjustItemInput struct {
	SessionID      int64    `json:"session_id" jsonschema:"Id of the workout (session) to adjust."`
	Exercise       string   `json:"exercise" jsonschema:"Exercise already prescribed in this workout."`
	TargetSets     *int     `json:"target_sets,omitempty" jsonschema:"New planned set count, 1-20. Omit to leave unchanged."`
	RepMin         *int     `json:"rep_min,omitempty" jsonschema:"New lower bound of the rep range. Omit to leave unchanged."`
	RepMax         *int     `json:"rep_max,omitempty" jsonschema:"New upper bound of the rep range. Omit to leave unchanged."`
	TargetRPE      *float64 `json:"target_rpe,omitempty" jsonschema:"New target RPE 1-10. Omit to leave unchanged."`
	TargetWeightKG *float64 `json:"target_weight_kg,omitempty" jsonschema:"New target load in kilograms, 0-2000. Omit to leave unchanged."`
	TargetRIR      *float64 `json:"target_rir,omitempty" jsonschema:"New target reps in reserve 0-10. Omit to leave unchanged."`
	Skipped        *bool    `json:"skipped,omitempty" jsonschema:"Mark the exercise as consciously skipped today, or unmark it."`
}
type SwapItemInput struct {
	SessionID   int64  `json:"session_id" jsonschema:"Id of the workout (session) to adjust."`
	Exercise    string `json:"exercise" jsonschema:"Exercise currently prescribed in the workout."`
	Replacement string `json:"replacement" jsonschema:"Exercise to use instead, keeping the same prescription."`
}
type RemoveItemInput struct {
	SessionID int64  `json:"session_id" jsonschema:"Id of the workout (session) to adjust."`
	Exercise  string `json:"exercise" jsonschema:"Exercise to drop from this workout's prescription."`
}
type SaveAsPlanInput struct {
	SessionID int64  `json:"session_id" jsonschema:"Id of the workout (session) to turn into a plan."`
	Name      string `json:"name" jsonschema:"Name for the new plan."`
}

// ItemOut returns the workout's prescriptions after a change, so the caller
// sees what is now stored instead of an echo of its own arguments.
type ItemOut struct {
	SessionID int64                   `json:"session_id"`
	Exercise  string                  `json:"exercise"`
	Progress  []training.PlanProgress `json:"progress"`
}

func registerSessionPlanTools(r *registry, service *training.Service) {
	itemAfter := func(ctx context.Context, sessionID int64, exercise string, err error) (*mcp.CallToolResult, ItemOut, error) {
		out := ItemOut{SessionID: sessionID, Exercise: exercise}
		if err != nil {
			return nil, out, toolError(err)
		}
		out.Progress, err = service.SessionProgress(ctx, sessionID)
		return nil, out, toolError(err)
	}

	progressSchema := inputSchema[SessionInput]()
	setPositiveID(progressSchema, "session_id")
	addTool(r, &mcp.Tool{
		Name:        "session_progress",
		Description: "For a workout by session id: planned versus completed working sets per exercise, with the prescribed rep range, target load (kg), RPE and RIR. Exercises done off-plan are listed with target_sets 0. get_workout returns the same progress by date.",
		InputSchema: progressSchema,
		Annotations: readOnly("Workout progress"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SessionInput) (*mcp.CallToolResult, ProgressOut, error) {
		v, err := service.SessionProgress(ctx, in.SessionID)
		if err != nil {
			return nil, ProgressOut{SessionID: in.SessionID}, toolError(err)
		}
		out := ProgressOut{SessionID: in.SessionID, Progress: v}
		if sess, err := service.GetSession(ctx, in.SessionID); err == nil {
			out.PlanName = sess.PlanName
		}
		return nil, out, nil
	})

	setItemSchema := inputSchema[SessionItemInput]()
	setPositiveID(setItemSchema, "session_id")
	setNonEmpty(setItemSchema, "exercise")
	addTool(r, &mcp.Tool{
		Name:        "add_session_exercise",
		Description: "Add an exercise to one workout's prescription, or replace its prescription entirely (unlike log_sets prescriptions, which merge). Never creates completed sets and never edits the plan the workout came from. " + planPrescriptionRule,
		InputSchema: setItemSchema,
		Annotations: destructive("Set workout prescription", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SessionItemInput) (*mcp.CallToolResult, ItemOut, error) {
		return itemAfter(ctx, in.SessionID, in.Exercise, service.SetSessionItem(ctx, in.SessionID, in.PlanItemInput.toItem()))
	})

	adjustSchema := inputSchema[AdjustItemInput]()
	setPositiveID(adjustSchema, "session_id")
	setNonEmpty(adjustSchema, "exercise")
	adjustSchema.MinProperties = jsonschema.Ptr(3)
	addTool(r, &mcp.Tool{
		Name:        "adjust_session_exercise",
		Description: "Change part of one exercise's prescription in a workout: set count, rep range, target load (kg), RPE, RIR, or whether it is skipped. Omitted fields stay as they are. Never creates completed sets.",
		InputSchema: adjustSchema,
		Annotations: destructive("Adjust workout prescription", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AdjustItemInput) (*mcp.CallToolResult, ItemOut, error) {
		return itemAfter(ctx, in.SessionID, in.Exercise, service.AdjustSessionItem(ctx, in.SessionID, in.Exercise, training.SessionItemPatch{
			TargetSets: in.TargetSets, RepMin: in.RepMin, RepMax: in.RepMax, TargetRPE: in.TargetRPE,
			TargetWeightKG: in.TargetWeightKG, TargetRIR: in.TargetRIR, Skipped: in.Skipped,
		}))
	})

	swapSchema := inputSchema[SwapItemInput]()
	setPositiveID(swapSchema, "session_id")
	setNonEmpty(swapSchema, "exercise", "replacement")
	addTool(r, &mcp.Tool{
		Name:        "swap_session_exercise",
		Description: "Substitute one exercise for another in a workout's prescription, keeping the same prescription. Use when the planned machine is taken.",
		InputSchema: swapSchema,
		Annotations: destructive("Swap workout exercise", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SwapItemInput) (*mcp.CallToolResult, ItemOut, error) {
		return itemAfter(ctx, in.SessionID, in.Replacement, service.SwapSessionItem(ctx, in.SessionID, in.Exercise, in.Replacement))
	})

	removeSchema := inputSchema[RemoveItemInput]()
	setPositiveID(removeSchema, "session_id")
	setNonEmpty(removeSchema, "exercise")
	addTool(r, &mcp.Tool{
		Name:        "remove_session_exercise",
		Description: "Drop an exercise from a workout's prescription. Completed sets already logged for it are kept and reappear as off-plan work.",
		InputSchema: removeSchema,
		Annotations: destructive("Remove workout exercise", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RemoveItemInput) (*mcp.CallToolResult, ItemOut, error) {
		return itemAfter(ctx, in.SessionID, in.Exercise, service.RemoveSessionItem(ctx, in.SessionID, in.Exercise))
	})

	saveSchema := inputSchema[SaveAsPlanInput]()
	setPositiveID(saveSchema, "session_id")
	setNonEmpty(saveSchema, "name")
	addTool(r, &mcp.Tool{
		Name:        "save_session_as_plan",
		Description: "Turn a workout's prescription, including in-workout adjustments and off-plan work, into a new reusable plan. Skipped exercises are left out.",
		InputSchema: saveSchema,
		Annotations: additive("Save workout as plan", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SaveAsPlanInput) (*mcp.CallToolResult, PlanOut, error) {
		v, err := service.SaveSessionAsPlan(ctx, in.SessionID, in.Name)
		return nil, PlanOut{Plan: v}, toolError(err)
	})
}

// ---- exercises -------------------------------------------------------------

type ExercisesOut struct {
	Exercises []training.Exercise `json:"exercises"`
}
type ExerciseOut struct {
	Exercise training.Exercise `json:"exercise"`
}
type ExerciseRefInput struct {
	ExerciseID int64  `json:"exercise_id,omitempty" jsonschema:"Exact id of the exercise. Give exercise_id or exercise."`
	Exercise   string `json:"exercise,omitempty" jsonschema:"Current canonical name or alias of the exercise."`
}
type AliasInput struct {
	ExerciseRefInput
	Alias string `json:"alias" jsonschema:"Other name that should resolve to this exercise, e.g. 'bp' for 'bench press'."`
}
type RemoveAliasInput struct {
	Alias string `json:"alias" jsonschema:"Alias to remove."`
}
type RemoveAliasOut struct {
	Alias   string `json:"alias"`
	Removed bool   `json:"removed"`
}
type RenameInput struct {
	ExerciseRefInput
	Name string `json:"name" jsonschema:"New canonical name."`
}

func resolveExerciseID(ctx context.Context, service *training.Service, in ExerciseRefInput) (int64, error) {
	if in.ExerciseID > 0 {
		return in.ExerciseID, nil
	}
	if in.Exercise == "" {
		return 0, invalidArg("exercise", "give exercise_id or exercise")
	}
	ex, err := service.GetExercise(ctx, in.Exercise)
	return ex.ID, err
}

func registerExerciseTools(r *registry, service *training.Service) {
	exerciseAfter := func(ctx context.Context, id int64, err error) (*mcp.CallToolResult, ExerciseOut, error) {
		if err != nil {
			return nil, ExerciseOut{}, toolError(err)
		}
		ex, err := service.GetExercise(ctx, strconv.FormatInt(id, 10))
		return nil, ExerciseOut{Exercise: ex}, toolError(err)
	}

	addTool(r, &mcp.Tool{
		Name:        "list_exercises",
		Description: "List every known exercise with its stable id, canonical name, aliases, muscle group and number of logged sets. Use it to match what the user says to an existing exercise before logging, so history is not split across near-duplicate names.",
		Annotations: readOnly("List exercises"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ExercisesOut, error) {
		v, err := service.Exercises(ctx)
		return nil, ExercisesOut{Exercises: v}, toolError(err)
	})

	aliasSchema := inputSchema[AliasInput]()
	setNonEmpty(aliasSchema, "alias")
	addTool(r, &mcp.Tool{
		Name:        "add_exercise_alias",
		Description: "Make another name resolve to an existing exercise. An alias that is already an exercise name or belongs to another exercise is rejected as a conflict; aliases never merge exercises.",
		InputSchema: aliasSchema,
		Annotations: additive("Add exercise alias", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AliasInput) (*mcp.CallToolResult, ExerciseOut, error) {
		id, err := resolveExerciseID(ctx, service, in.ExerciseRefInput)
		if err != nil {
			return nil, ExerciseOut{}, toolError(err)
		}
		return exerciseAfter(ctx, id, service.AddExerciseAlias(ctx, id, in.Alias))
	})

	removeAliasSchema := inputSchema[RemoveAliasInput]()
	setNonEmpty(removeAliasSchema, "alias")
	addTool(r, &mcp.Tool{
		Name:        "remove_exercise_alias",
		Description: "Delete an alias. Sets logged under it keep pointing at the exercise.",
		InputSchema: removeAliasSchema,
		Annotations: destructive("Remove exercise alias", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RemoveAliasInput) (*mcp.CallToolResult, RemoveAliasOut, error) {
		err := service.RemoveExerciseAlias(ctx, in.Alias)
		return nil, RemoveAliasOut{Alias: training.NormalizeExerciseName(in.Alias), Removed: err == nil}, toolError(err)
	})

	renameSchema := inputSchema[RenameInput]()
	setNonEmpty(renameSchema, "name")
	addTool(r, &mcp.Tool{
		Name:        "rename_exercise",
		Description: "Change an exercise's canonical name everywhere it is referenced (sets, plans). The old name does not become an alias automatically (add one with add_exercise_alias if wanted). A name that already belongs to another exercise is a conflict: renaming never merges exercises.",
		InputSchema: renameSchema,
		Annotations: destructive("Rename exercise", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RenameInput) (*mcp.CallToolResult, ExerciseOut, error) {
		id, err := resolveExerciseID(ctx, service, in.ExerciseRefInput)
		if err != nil {
			return nil, ExerciseOut{}, toolError(err)
		}
		return exerciseAfter(ctx, id, service.RenameExercise(ctx, id, in.Name))
	})

	noteSchema := inputSchema[NoteInput]()
	setNonEmpty(noteSchema, "exercise")
	addTool(r, &mcp.Tool{
		Name:        "set_exercise_note",
		Description: "Store a persistent setup reminder for an exercise, such as seat height or grip width. An empty note removes it.",
		InputSchema: noteSchema,
		Annotations: destructive("Set exercise note", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NoteInput) (*mcp.CallToolResult, NotesOut, error) {
		if err := service.SetExerciseNote(ctx, in.Exercise, in.Note); err != nil {
			return nil, NotesOut{}, toolError(err)
		}
		v, err := service.ExerciseNotes(ctx)
		return nil, NotesOut{Notes: v}, toolError(err)
	})
	addTool(r, &mcp.Tool{
		Name:        "list_exercise_notes",
		Description: "List every exercise that has a setup note.",
		Annotations: readOnly("List exercise notes"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, NotesOut, error) {
		v, err := service.ExerciseNotes(ctx)
		return nil, NotesOut{Notes: v}, toolError(err)
	})

	groupSchema := inputSchema[GroupInput]()
	setNonEmpty(groupSchema, "exercise")
	prop(groupSchema, "muscle_group").Enum = muscleGroupEnum()
	addTool(r, &mcp.Tool{
		Name:        "set_exercise_group",
		Description: "Assign an exercise to one muscle group so its sets count toward that group's volume. Re-assigning replaces the previous group.",
		InputSchema: groupSchema,
		Annotations: destructive("Set exercise muscle group", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GroupInput) (*mcp.CallToolResult, GroupOut, error) {
		err := service.SetExerciseGroup(ctx, in.Exercise, in.MuscleGroup)
		return nil, GroupOut{Exercise: training.NormalizeExerciseName(in.Exercise), MuscleGroup: in.MuscleGroup}, toolError(err)
	})
	addTool(r, &mcp.Tool{
		Name:        "list_exercise_groups",
		Description: "List every exercise that has a muscle group assigned, ordered by group.",
		Annotations: readOnly("List exercise groups"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, GroupsOut, error) {
		v, err := service.ExerciseGroups(ctx)
		return nil, GroupsOut{Groups: v}, toolError(err)
	})
}

type NoteInput struct {
	Exercise string `json:"exercise" jsonschema:"Exercise name or alias."`
	Note     string `json:"note" jsonschema:"Setup reminder, e.g. 'seat at 4, wide grip'. Empty string removes it."`
}
type NotesOut struct {
	Notes []training.ExerciseNote `json:"notes"`
}
type GroupInput struct {
	Exercise    string `json:"exercise" jsonschema:"Exercise name or alias."`
	MuscleGroup string `json:"muscle_group" jsonschema:"Muscle group the exercise trains; one of the supported values."`
}
type GroupOut struct {
	Exercise    string `json:"exercise"`
	MuscleGroup string `json:"muscle_group"`
}
type GroupsOut struct {
	Groups []training.ExerciseGroup `json:"groups"`
}

// ---- analytics -------------------------------------------------------------

type StatsInput struct {
	Exercise string `json:"exercise" jsonschema:"Exercise name, alias or numeric id."`
	From     string `json:"from,omitempty" jsonschema:"Inclusive earliest local date YYYY-MM-DD. Omit for no lower bound."`
	To       string `json:"to,omitempty" jsonschema:"Inclusive latest local date YYYY-MM-DD. Omit for no upper bound."`
}
type StatsOut struct {
	Stats training.ExerciseStats `json:"stats"`
}
type RangeInput struct {
	From string `json:"from,omitempty" jsonschema:"Inclusive earliest local date YYYY-MM-DD."`
	To   string `json:"to,omitempty" jsonschema:"Inclusive latest local date YYYY-MM-DD."`
}
type SummaryOut struct {
	Summary training.TrainingSummary `json:"summary"`
}
type HistoryInput struct {
	Exercise string `json:"exercise" jsonschema:"Exercise name or alias."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum sets to return; defaults to 50, accepts 1-500."`
}
type HistoryOut struct {
	History training.ExerciseHistory `json:"history"`
}
type WeeklyOut struct {
	Weeks []training.WeeklyVolume `json:"weeks"`
}
type VolumeOut struct {
	Volume []training.GroupVolume `json:"volume"`
}
type LoadInput struct {
	Exercise string `json:"exercise" jsonschema:"Exercise name or alias."`
}
type LoadSuggestionOut struct {
	Suggestion training.LoadSuggestion `json:"suggestion"`
}

func registerAnalyticsTools(r *registry, service *training.Service) {
	statsSchema := inputSchema[StatsInput]()
	setNonEmpty(statsSchema, "exercise")
	setDate(statsSchema, "from", "to")
	addTool(r, &mcp.Tool{
		Name: "exercise_stats",
		Description: "Progression for one exercise over an optional local date range: per-workout working sets, total reps, volume (kg x reps), top weight (kg) and best estimated 1RM, plus bests with the set that produced them. " +
			"Warm-ups are counted separately and excluded from volume and bests. Sets missing weight or reps count as sets but add no volume (reported as unrecorded_sets); nothing is estimated for them.",
		InputSchema: statsSchema,
		Annotations: readOnly("Exercise stats"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in StatsInput) (*mcp.CallToolResult, StatsOut, error) {
		v, err := service.ExerciseStats(ctx, in.Exercise, in.From, in.To)
		return nil, StatsOut{Stats: v}, toolError(err)
	})

	summarySchema := inputSchema[RangeInput]()
	setDate(summarySchema, "from", "to")
	addTool(r, &mcp.Tool{
		Name:        "training_summary",
		Description: "Overview of completed working sets over a local date range (default: the 28 days ending today in the configured timezone): workouts, working and warm-up sets, volume in kg, sets missing weight/reps, and breakdowns by exercise, muscle group and week (Monday start).",
		InputSchema: summarySchema,
		Annotations: readOnly("Training summary"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RangeInput) (*mcp.CallToolResult, SummaryOut, error) {
		v, err := service.TrainingSummary(ctx, in.From, in.To)
		return nil, SummaryOut{Summary: v}, toolError(err)
	})

	historySchema := inputSchema[HistoryInput]()
	setNonEmpty(historySchema, "exercise")
	prop(historySchema, "limit").Default = json.RawMessage("50")
	prop(historySchema, "limit").Minimum = jsonschema.Ptr(1.0)
	prop(historySchema, "limit").Maximum = jsonschema.Ptr(500.0)
	addTool(r, &mcp.Tool{
		Name:        "exercise_history",
		Description: "Recorded sets of one exercise, newest first, each with its date, values (null = not recorded) and estimated 1RM, plus the best set by estimated 1RM.",
		InputSchema: historySchema,
		Annotations: readOnly("Exercise history"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in HistoryInput) (*mcp.CallToolResult, HistoryOut, error) {
		v, err := service.ExerciseHistory(ctx, in.Exercise, in.Limit)
		return nil, HistoryOut{History: v}, toolError(err)
	})

	weeklySchema := inputSchema[RangeInput]()
	setDate(weeklySchema, "from", "to")
	addTool(r, &mcp.Tool{
		Name:        "weekly_volume",
		Description: "Legacy SI and set count per muscle group per training week (Monday start), newest week first. SI is derived from RPE and is 0 for sets without RPE. Prefer training_summary for kg volume.",
		InputSchema: weeklySchema,
		Annotations: readOnly("Weekly volume"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RangeInput) (*mcp.CallToolResult, WeeklyOut, error) {
		v, err := service.WeeklyVolume(ctx, training.ListFilter{From: in.From, To: in.To})
		return nil, WeeklyOut{Weeks: v}, toolError(err)
	})

	volumeSchema := inputSchema[RangeInput]()
	setDate(volumeSchema, "from", "to")
	addTool(r, &mcp.Tool{
		Name:        "volume_by_muscle",
		Description: "Legacy total SI and set count per muscle group within an optional local date range. Exercises with no group are reported under an empty muscle_group.",
		InputSchema: volumeSchema,
		Annotations: readOnly("Volume by muscle"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RangeInput) (*mcp.CallToolResult, VolumeOut, error) {
		v, err := service.VolumeByGroup(ctx, training.ListFilter{From: in.From, To: in.To})
		return nil, VolumeOut{Volume: v}, toolError(err)
	})

	loadSchema := inputSchema[LoadInput]()
	setNonEmpty(loadSchema, "exercise")
	addTool(r, &mcp.Tool{
		Name:        "suggest_load",
		Description: "Advisory only: suggest the next working weight (kg) for an exercise, judged against its prescribed rep range and RPE, explaining the reasoning. Never changes anything. Declines when there is no comparable set or no prescription.",
		InputSchema: loadSchema,
		Annotations: readOnly("Suggest load"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in LoadInput) (*mcp.CallToolResult, LoadSuggestionOut, error) {
		v, err := service.SuggestLoad(ctx, in.Exercise)
		return nil, LoadSuggestionOut{Suggestion: v}, toolError(err)
	})
}

// ---- feedback loop ----------------------------------------------------------

type FeedbackInput struct {
	SessionID   int64  `json:"session_id" jsonschema:"Id of the workout (session) being rated."`
	MuscleGroup string `json:"muscle_group" jsonschema:"Muscle group trained; one of the supported values."`
	Fatigue     int    `json:"fatigue" jsonschema:"Fatigue the muscle took, 0-3. 0 = barely noticed, 3 = severe."`
	Pump        int    `json:"pump" jsonschema:"Pump the muscle got, 0-3. 0 = none, 3 = extreme."`
	Recovery    int    `json:"recovery" jsonschema:"How sore/unrecovered it felt afterwards, 0-3. 0 = nothing, 3 = still wrecked days later."`
}
type FeedbackOut struct {
	SessionID int64               `json:"session_id"`
	Feedback  []training.Feedback `json:"feedback"`
}
type LandmarksInput struct {
	MuscleGroup string `json:"muscle_group" jsonschema:"Muscle group; one of the supported values."`
	MEV         int    `json:"mev,omitempty" jsonschema:"Minimum effective volume in weekly sets. 0 means unknown."`
	MRV         int    `json:"mrv,omitempty" jsonschema:"Maximum recoverable volume in weekly sets. 0 means unknown."`
}
type LandmarksOut struct {
	Landmarks []training.Landmarks `json:"landmarks"`
}
type RecommendOut struct {
	Recommendations []training.SetChange `json:"recommendations"`
}

func registerFeedbackTools(r *registry, service *training.Service) {
	feedbackSchema := inputSchema[FeedbackInput]()
	setPositiveID(feedbackSchema, "session_id")
	prop(feedbackSchema, "muscle_group").Enum = muscleGroupEnum()
	for _, dim := range []string{"fatigue", "pump", "recovery"} {
		prop(feedbackSchema, dim).Minimum = jsonschema.Ptr(0.0)
		prop(feedbackSchema, dim).Maximum = jsonschema.Ptr(3.0)
	}
	addTool(r, &mcp.Tool{
		Name:        "record_feedback",
		Description: "Rate how one muscle group responded to a workout: fatigue, pump and recovery, each 0-3, as stated by the user. Only rate groups actually trained; never invent ratings. Re-rating replaces the previous rating. Drives volume_recommendation.",
		InputSchema: feedbackSchema,
		Annotations: destructive("Record feedback", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in FeedbackInput) (*mcp.CallToolResult, FeedbackOut, error) {
		err := service.RecordFeedback(ctx, in.SessionID, training.Feedback{MuscleGroup: in.MuscleGroup, Fatigue: in.Fatigue, Pump: in.Pump, Recovery: in.Recovery})
		if err != nil {
			return nil, FeedbackOut{SessionID: in.SessionID}, toolError(err)
		}
		all, err := service.SessionFeedback(ctx, in.SessionID)
		return nil, FeedbackOut{SessionID: in.SessionID, Feedback: all}, toolError(err)
	})

	landmarksSchema := inputSchema[LandmarksInput]()
	prop(landmarksSchema, "muscle_group").Enum = muscleGroupEnum()
	prop(landmarksSchema, "mev").Minimum = jsonschema.Ptr(0.0)
	prop(landmarksSchema, "mrv").Minimum = jsonschema.Ptr(0.0)
	addTool(r, &mcp.Tool{
		Name:        "set_volume_landmarks",
		Description: "Record a muscle group's personal weekly-set boundaries: MEV (below which it stops growing) and MRV (above which fatigue outpaces recovery). Leave a value at 0 if unknown. Replaces previous values.",
		InputSchema: landmarksSchema,
		Annotations: destructive("Set volume landmarks", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in LandmarksInput) (*mcp.CallToolResult, LandmarksOut, error) {
		if err := service.SetLandmarks(ctx, training.Landmarks{MuscleGroup: in.MuscleGroup, MEV: in.MEV, MRV: in.MRV}); err != nil {
			return nil, LandmarksOut{}, toolError(err)
		}
		v, err := service.Landmarks(ctx)
		return nil, LandmarksOut{Landmarks: v}, toolError(err)
	})
	addTool(r, &mcp.Tool{
		Name:        "list_volume_landmarks",
		Description: "List the muscle groups that have personal volume landmarks recorded.",
		Annotations: readOnly("List volume landmarks"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, LandmarksOut, error) {
		v, err := service.Landmarks(ctx)
		return nil, LandmarksOut{Landmarks: v}, toolError(err)
	})
	addTool(r, &mcp.Tool{
		Name:        "volume_recommendation",
		Description: "Advisory set-count change per muscle group for next week, from recent feedback weighted toward the latest. Reports the sample count and confidence, declines to advise on a single rating, and never pushes volume past a recorded MEV or MRV. Changes nothing.",
		Annotations: readOnly("Volume recommendation"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, RecommendOut, error) {
		v, err := service.VolumeRecommendation(ctx)
		return nil, RecommendOut{Recommendations: v}, toolError(err)
	})
}
