package training

import "context"

type Store interface {
	AddSet(context.Context, AddSetInput) (Set, float64, error)
	UpdateSet(context.Context, int64, SetPatch) (Set, float64, error)
	DeleteSet(context.Context, int64, *int) (int64, float64, int, error)
	GetSession(context.Context, int64) (Session, error)
	ListSessions(context.Context, ListFilter) ([]SessionSummary, error)
	RecentExercises(context.Context, int) ([]ExerciseMemory, error)
	DeleteSession(context.Context, int64) (int, error)
	CreatePlan(context.Context, Plan) (Plan, error)
	ListPlans(context.Context) ([]Plan, error)
	GetPlan(context.Context, int64) (Plan, error)
	UpdatePlan(context.Context, int64, PlanPatch) error
	SetPlanItem(context.Context, int64, PlanItem) error
	RemovePlanItem(context.Context, int64, string) error
	MovePlanItem(context.Context, int64, string, int) error
	DeletePlan(context.Context, int64) error
	SessionProgress(context.Context, int64) ([]PlanProgress, error)
	SetSessionItem(context.Context, int64, PlanItem) error
	PatchSessionItem(context.Context, int64, string, SessionItemPatch) error
	SwapSessionItem(context.Context, int64, string, string) error
	RemoveSessionItem(ctx context.Context, sessionID int64, exercise string, expectedRevision *int) error
	SetFeedback(context.Context, int64, Feedback) error
	SessionFeedback(context.Context, int64) ([]Feedback, error)
	LatestFeedback(context.Context) (map[string]Feedback, error)
	RecentFeedback(context.Context, int) (map[string][]Feedback, error)
	SetLandmarks(context.Context, Landmarks) error
	AllLandmarks(context.Context) (map[string]Landmarks, error)
	ExerciseHistory(context.Context, string, int) (ExerciseHistory, error)
	WeeklyVolume(context.Context, ListFilter) ([]WeeklyVolume, error)
	SetExerciseGroup(context.Context, ExerciseGroup) error
	ExerciseGroups(context.Context) ([]ExerciseGroup, error)
	VolumeByGroup(context.Context, ListFilter) ([]GroupVolume, error)
	Snapshot(context.Context, string) error
	SetExerciseNote(context.Context, ExerciseNote) error
	ExerciseNotes(context.Context) ([]ExerciseNote, error)
	SupersetFor(context.Context, int64, string) (string, error)

	// Engine v2.
	//
	// GetOrCreateWorkout is atomic: concurrent callers for one date all get the
	// same workout. WorkoutByDate returns ErrNotFound when there is none.
	GetOrCreateWorkout(ctx context.Context, date string) (Session, bool, error)
	WorkoutByDate(context.Context, string) (Session, error)
	// LogBatch applies a whole BatchInput in one transaction, including the
	// idempotency record.
	LogBatch(context.Context, BatchInput) (BatchResult, error)
	GetSet(context.Context, int64) (Set, error)
	// AttachPlan copies a plan into an existing workout that follows no plan.
	AttachPlan(ctx context.Context, sessionID, planID int64) error

	Exercises(context.Context) ([]Exercise, error)
	GetExercise(ctx context.Context, idOrName string) (Exercise, error)
	AddExerciseAlias(ctx context.Context, exerciseID int64, alias string) error
	RemoveExerciseAlias(ctx context.Context, alias string) error
	RenameExercise(ctx context.Context, exerciseID int64, newName string) error

	// ImportPlan stores a validated plan in one transaction. With dryRun it
	// reports the outcome and rolls back.
	ImportPlan(ctx context.Context, p Plan, onConflict string, dryRun bool) (PlanImportResult, error)
	// ReplacePlan swaps a plan's name, notes and items atomically.
	ReplacePlan(ctx context.Context, id int64, p Plan, expectedRevision *int) (Plan, error)
	DeletePlanExpected(ctx context.Context, id int64, expectedRevision *int) error

	SetRecords(ctx context.Context, from, to string) ([]SetRecord, error)

	// SetPlanSchedule stores (nil clears) a plan's schedule and bumps its revision.
	SetPlanSchedule(ctx context.Context, id int64, s *Schedule, expectedRevision *int) error
	// WorkoutsInRange lists stored workouts with their plan and set counts.
	WorkoutsInRange(ctx context.Context, from, to string) ([]WorkoutDay, error)
}
