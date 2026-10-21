package training

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// ---- validation errors ----------------------------------------------------

// Issue is one actionable validation problem. Field is a path such as
// "sets[1].reps" so a caller can point at the exact value that was rejected.
type Issue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError carries every problem found, not just the first. It matches
// ErrValidation under errors.Is, so existing callers keep working.
type ValidationError struct {
	Issues []Issue `json:"issues"`
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, i := range e.Issues {
		if i.Field == "" {
			parts = append(parts, i.Message)
		} else {
			parts = append(parts, i.Field+": "+i.Message)
		}
	}
	return "validation error: " + strings.Join(parts, "; ")
}

func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// issues turns collected problems into an error, or nil when there are none.
func issues(list []Issue) error {
	if len(list) == 0 {
		return nil
	}
	return &ValidationError{Issues: list}
}

// invalid builds a single-issue validation error.
func invalid(field, format string, args ...any) error {
	return &ValidationError{Issues: []Issue{{Field: field, Message: fmt.Sprintf(format, args...)}}}
}

// IssuesOf extracts the issues of a validation error, if err is one.
func IssuesOf(err error) []Issue {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Issues
	}
	return nil
}

// ---- set types ------------------------------------------------------------

const (
	SetTypeWarmup  = "warmup"
	SetTypeWorking = "working"
	SetTypeBackoff = "backoff"
	SetTypeDrop    = "drop"
	SetTypeFailure = "failure"
)

// SetTypes lists the accepted values. Every type except warmup counts as
// completed working work in analytics.
var SetTypes = []string{SetTypeWarmup, SetTypeWorking, SetTypeBackoff, SetTypeDrop, SetTypeFailure}

func validSetType(v string) bool {
	for _, t := range SetTypes {
		if t == v {
			return true
		}
	}
	return false
}

// Limits, documented in docs/engine.md.
const (
	MaxWeightKG    = 2000
	MaxReps        = 1000
	maxSetNotes    = 500
	maxExerciseLen = 100
	MaxBatchSets   = 100
	MaxBatchItems  = 40
	MaxKeyLen      = 128
)

// ---- exercise identity ----------------------------------------------------

// Exercise is a stable exercise identity. Name is the canonical normalized
// name; Aliases are other names that resolve to it.
type Exercise struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases"`
	MuscleGroup string   `json:"muscle_group,omitempty"`
	SetCount    int      `json:"set_count"`
}

// NewExercise reports an exercise that did not exist and was created by a
// write. Similar lists existing exercises with a close name, so a typo can be
// noticed and corrected instead of silently fragmenting history.
type NewExercise struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Similar []string `json:"similar,omitempty"`
}

// NormalizeExerciseName lowercases, trims and collapses inner whitespace.
// Nothing else is changed: merging "bench-press" into "bench press" would be a
// guess, so near matches are reported, never merged.
func NormalizeExerciseName(v string) string {
	return strings.Join(strings.Fields(strings.ToLower(v)), " ")
}

func exerciseKey(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SimilarNames returns existing names that look like name without being it:
// same letters ignoring punctuation and spacing, one containing the other, or a
// small edit distance for longer names.
func SimilarNames(name string, existing []string) []string {
	key := exerciseKey(name)
	var out []string
	for _, e := range existing {
		if e == name {
			continue
		}
		ek := exerciseKey(e)
		if ek == "" || key == "" {
			continue
		}
		switch {
		case ek == key,
			len(key) >= 4 && len(ek) >= 4 && (strings.Contains(ek, key) || strings.Contains(key, ek)),
			len(key) >= 6 && len(ek) >= 6 && editDistance(key, ek) <= 2:
			out = append(out, e)
		}
	}
	return out
}

func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur := make([]int, len(br)+1)
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(br)]
}

// ---- batch logging --------------------------------------------------------

// BatchInput is one atomic write to a workout: any number of performed sets
// and any number of prescription updates. Date is a local calendar date
// (YYYY-MM-DD); empty means today in the configured timezone.
type BatchInput struct {
	Date           string
	PlanID         int64 // optionally follow this plan when the workout is created or unplanned
	Sets           []AddSetInput
	Prescriptions  []PlanItem
	IdempotencyKey string
	// ExpectedRevision, when non-nil, requires the workout to still be at this
	// revision (checked inside the transaction).
	ExpectedRevision *int
}

// BatchResult reports what was persisted.
type BatchResult struct {
	Workout       Session       `json:"workout"`
	Created       bool          `json:"created"`
	Sets          []Set         `json:"sets"`
	Prescriptions []PlanItem    `json:"prescriptions"`
	NewExercises  []NewExercise `json:"new_exercises,omitempty"`
	// Replayed is true when this response was returned from an earlier request
	// with the same idempotency key; nothing new was written.
	Replayed bool `json:"replayed"`
}

// ---- plan documents -------------------------------------------------------

const (
	PlanFormat  = "workout-plan"
	PlanVersion = 1
)

// PlanDocument is the portable plan format (docs/plan-format.md).
type PlanDocument struct {
	Format  string             `json:"format"`
	Version int                `json:"version"`
	Name    string             `json:"name"`
	Notes   string             `json:"notes,omitempty"`
	Items   []PlanDocumentItem `json:"items"`
}

// PlanDocumentItem prescribes one exercise. Nil/zero means "not prescribed".
type PlanDocumentItem struct {
	Exercise       string   `json:"exercise"`
	Sets           int      `json:"sets"`
	RepMin         int      `json:"rep_min,omitempty"`
	RepMax         int      `json:"rep_max,omitempty"`
	TargetWeightKG *float64 `json:"target_weight_kg,omitempty"`
	TargetRPE      float64  `json:"target_rpe,omitempty"`
	TargetRIR      *float64 `json:"target_rir,omitempty"`
	Superset       string   `json:"superset,omitempty"`
	Notes          string   `json:"notes,omitempty"`
}

// Import conflict policies.
const (
	OnConflictError   = "error"
	OnConflictReplace = "replace"
)

// PlanImportResult describes what an import did (or would do when DryRun).
type PlanImportResult struct {
	Plan    Plan `json:"plan"`
	Created bool `json:"created"`
	// Replaced is true when an existing plan with the same name had its
	// exercises replaced.
	Replaced     bool          `json:"replaced"`
	DryRun       bool          `json:"dry_run"`
	NewExercises []NewExercise `json:"new_exercises,omitempty"`
}

// ---- analytics ------------------------------------------------------------

// SetRecord is one performed set joined with its workout date and muscle
// group, the raw input of the statistics.
type SetRecord struct {
	SetID      int64
	SessionID  int64
	Date       string
	ExerciseID int64
	Exercise   string
	Muscle     string
	WeightKG   *float64
	Reps       *int
	RPE        *float64
	RIR        *float64
	SetType    string
}

func (r SetRecord) working() bool { return r.SetType != SetTypeWarmup }

// loadKG is weight x reps, defined only when both were recorded.
func (r SetRecord) loadKG() (float64, bool) {
	if r.WeightKG == nil || r.Reps == nil {
		return 0, false
	}
	return *r.WeightKG * float64(*r.Reps), true
}

// ExerciseStatsPoint summarizes one exercise on one workout date.
type ExerciseStatsPoint struct {
	Date        string  `json:"date"`
	WorkingSets int     `json:"working_sets"`
	WarmupSets  int     `json:"warmup_sets"`
	TotalReps   int     `json:"total_reps"`
	VolumeKG    float64 `json:"volume_kg"`
	// TopWeightKG is the heaviest working set with a recorded weight.
	TopWeightKG *float64 `json:"top_weight_kg"`
	BestE1RM    float64  `json:"best_e1rm"`
	// UnrecordedSets counts working sets missing weight or reps; they count as
	// sets but add nothing to volume.
	UnrecordedSets int `json:"unrecorded_sets"`
}

// ExerciseBest is a personal best with the set that produced it.
type ExerciseBest struct {
	Value float64 `json:"value"`
	Date  string  `json:"date"`
	SetID int64   `json:"set_id"`
}

// ExerciseStats is progression for one exercise over a date range.
type ExerciseStats struct {
	ExerciseID   int64                `json:"exercise_id"`
	Exercise     string               `json:"exercise"`
	From         string               `json:"from,omitempty"`
	To           string               `json:"to,omitempty"`
	Workouts     int                  `json:"workouts"`
	WorkingSets  int                  `json:"working_sets"`
	WarmupSets   int                  `json:"warmup_sets"`
	VolumeKG     float64              `json:"volume_kg"`
	MaxWeight    *ExerciseBest        `json:"max_weight_kg"`
	BestE1RM     *ExerciseBest        `json:"best_e1rm"`
	MaxRepsAtTop *ExerciseBest        `json:"max_reps"`
	Points       []ExerciseStatsPoint `json:"points"`
}

// GroupSummary aggregates working sets by muscle group.
type GroupSummary struct {
	MuscleGroup string  `json:"muscle_group"`
	WorkingSets int     `json:"working_sets"`
	VolumeKG    float64 `json:"volume_kg"`
}

// ExerciseSummary aggregates working sets of one exercise.
type ExerciseSummary struct {
	ExerciseID  int64   `json:"exercise_id"`
	Exercise    string  `json:"exercise"`
	WorkingSets int     `json:"working_sets"`
	VolumeKG    float64 `json:"volume_kg"`
}

// WeekSummary aggregates one training week (Monday start).
type WeekSummary struct {
	WeekStart   string  `json:"week_start"`
	Workouts    int     `json:"workouts"`
	WorkingSets int     `json:"working_sets"`
	VolumeKG    float64 `json:"volume_kg"`
}

// TrainingSummary is the overview of a date range.
type TrainingSummary struct {
	From           string            `json:"from"`
	To             string            `json:"to"`
	Workouts       int               `json:"workouts"`
	WorkingSets    int               `json:"working_sets"`
	WarmupSets     int               `json:"warmup_sets"`
	VolumeKG       float64           `json:"volume_kg"`
	UnrecordedSets int               `json:"unrecorded_sets"`
	ByExercise     []ExerciseSummary `json:"by_exercise"`
	ByMuscle       []GroupSummary    `json:"by_muscle_group"`
	ByWeek         []WeekSummary     `json:"by_week"`
}

// ---- helpers --------------------------------------------------------------

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func tooLong(v string, max int) bool { return utf8.RuneCountInString(v) > max }
