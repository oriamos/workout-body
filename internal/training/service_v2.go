package training

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- exercises ------------------------------------------------------------

// Exercises lists every known exercise with its aliases.
func (s *Service) Exercises(ctx context.Context) ([]Exercise, error) {
	out, err := s.store.Exercises(ctx)
	if err == nil && out == nil {
		out = []Exercise{}
	}
	return out, err
}

// GetExercise finds an exercise by numeric id, canonical name or alias. An
// unknown name is ErrNotFound; this never creates anything.
func (s *Service) GetExercise(ctx context.Context, ref string) (Exercise, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Exercise{}, invalid("exercise", "is required")
	}
	return s.store.GetExercise(ctx, ref)
}

// AddExerciseAlias makes alias resolve to an existing exercise. An alias that
// is already an exercise name or belongs to another exercise is rejected, never
// silently reassigned.
func (s *Service) AddExerciseAlias(ctx context.Context, exerciseID int64, alias string) error {
	alias = NormalizeExerciseName(alias)
	if exerciseID <= 0 {
		return invalid("exercise_id", "must be positive")
	}
	if alias == "" || tooLong(alias, maxExerciseLen) {
		return invalid("alias", "must be 1-%d characters", maxExerciseLen)
	}
	return s.store.AddExerciseAlias(ctx, exerciseID, alias)
}

// RemoveExerciseAlias deletes an alias; sets keep pointing at the exercise.
func (s *Service) RemoveExerciseAlias(ctx context.Context, alias string) error {
	alias = NormalizeExerciseName(alias)
	if alias == "" {
		return invalid("alias", "is required")
	}
	return s.store.RemoveExerciseAlias(ctx, alias)
}

// RenameExercise changes an exercise's canonical name everywhere it is
// referenced (sets, plans, catalogue). The old name does not become an alias
// automatically. A name that already belongs to another exercise is a conflict:
// merging is never implicit.
func (s *Service) RenameExercise(ctx context.Context, exerciseID int64, newName string) error {
	newName = NormalizeExerciseName(newName)
	if exerciseID <= 0 {
		return invalid("exercise_id", "must be positive")
	}
	if newName == "" || tooLong(newName, maxExerciseLen) {
		return invalid("name", "must be 1-%d characters", maxExerciseLen)
	}
	return s.store.RenameExercise(ctx, exerciseID, newName)
}

// ---- plan JSON ------------------------------------------------------------

var (
	planTopKeys  = map[string]bool{"format": true, "version": true, "name": true, "notes": true, "items": true}
	planItemKeys = map[string]bool{"exercise": true, "sets": true, "rep_min": true, "rep_max": true,
		"target_weight_kg": true, "target_rpe": true, "target_rir": true, "superset": true, "notes": true}
)

// ParsePlanDocument decodes and validates plan JSON without touching storage.
// Every problem found is reported with its JSON path.
func ParsePlanDocument(data []byte) (PlanDocument, error) {
	var doc PlanDocument
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		if err == nil {
			err = errors.New("null")
		}
		return doc, invalid("", "not a valid JSON object: %v", jsonProblem(err))
	}
	// Every field is decoded on its own so that ALL type errors are reported,
	// each with its exact path (items[1].sets), instead of only the first one
	// encoding/json would return with an index-less path.
	var iss []Issue
	typeErr := map[string]bool{}
	field := func(path string, msg json.RawMessage, dst any) {
		// The published schema has no nullable fields: "not prescribed" is
		// expressed by leaving the field out.
		if bytes.Equal(bytes.TrimSpace(msg), []byte("null")) {
			typeErr[path] = true
			iss = append(iss, Issue{Field: path, Message: "must not be null; omit the field instead"})
			return
		}
		if err := json.Unmarshal(msg, dst); err != nil {
			typeErr[path] = true
			iss = append(iss, Issue{Field: path, Message: typeProblem(err)})
		}
	}
	for k, v := range raw {
		switch k {
		case "format":
			field(k, v, &doc.Format)
		case "version":
			field(k, v, &doc.Version)
		case "name":
			field(k, v, &doc.Name)
		case "notes":
			field(k, v, &doc.Notes)
		case "items":
			var items []json.RawMessage
			if err := json.Unmarshal(v, &items); err != nil {
				typeErr[k] = true
				iss = append(iss, Issue{Field: k, Message: "expected an array of exercises"})
				continue
			}
			doc.Items = make([]PlanDocumentItem, len(items))
			for i, rawItem := range items {
				var m map[string]json.RawMessage
				if err := json.Unmarshal(rawItem, &m); err != nil || m == nil {
					iss = append(iss, Issue{Field: fmt.Sprintf("items[%d]", i), Message: "expected an object"})
					continue
				}
				it := &doc.Items[i]
				for ik, iv := range m {
					p := fmt.Sprintf("items[%d].%s", i, ik)
					switch ik {
					case "exercise":
						field(p, iv, &it.Exercise)
					case "sets":
						field(p, iv, &it.Sets)
					case "rep_min":
						field(p, iv, &it.RepMin)
						zeroMeansOmit(p, &it.RepMin, typeErr, &iss)
					case "rep_max":
						field(p, iv, &it.RepMax)
						zeroMeansOmit(p, &it.RepMax, typeErr, &iss)
					case "target_weight_kg":
						field(p, iv, &it.TargetWeightKG)
					case "target_rpe":
						field(p, iv, &it.TargetRPE)
						if !typeErr[p] && it.TargetRPE == 0 {
							iss = append(iss, Issue{Field: p, Message: "must be between 1 and 10; omit the field for 'not prescribed'"})
						}
					case "target_rir":
						field(p, iv, &it.TargetRIR)
					case "superset":
						field(p, iv, &it.Superset)
					case "notes":
						field(p, iv, &it.Notes)
					default:
						iss = append(iss, Issue{Field: p, Message: "unknown field"})
					}
				}
			}
		default:
			iss = append(iss, Issue{Field: k, Message: "unknown field"})
		}
	}
	if doc.Format != PlanFormat && !typeErr["format"] {
		iss = append(iss, Issue{Field: "format", Message: fmt.Sprintf("must be %q", PlanFormat)})
	}
	if doc.Version != PlanVersion && !typeErr["version"] {
		iss = append(iss, Issue{Field: "version", Message: fmt.Sprintf("must be %d (this server reads version %d)", PlanVersion, PlanVersion)})
	}
	if p := doc.toPlan(); true {
		if err := validatePlan(&p, "sets"); err != nil {
			for _, i := range IssuesOf(err) {
				// A value that failed to decode was already reported; do not
				// add a second, misleading "must be between" issue for it.
				if !typeErr[i.Field] && !(typeErr["items"] && i.Field == "items") {
					iss = append(iss, i)
				}
			}
		}
	}
	sort.SliceStable(iss, func(a, b int) bool { return iss[a].Field < iss[b].Field })
	return doc, issues(iss)
}

func typeProblem(err error) string {
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return fmt.Sprintf("expected %s, got %s", ute.Type, ute.Value)
	}
	return jsonProblem(err)
}

// zeroMeansOmit rejects an explicit 0 for a count that the schema requires to
// be at least 1. (The engine stores "not prescribed" as 0 internally, but the
// document format says to leave the field out.)
func zeroMeansOmit(path string, v *int, typeErr map[string]bool, iss *[]Issue) {
	if !typeErr[path] && *v == 0 {
		*iss = append(*iss, Issue{Field: path, Message: "must be at least 1; omit the field for 'not prescribed'"})
	}
}

func jsonProblem(err error) string {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return fmt.Sprintf("syntax error at byte %d: %v", se.Offset, err)
	}
	return err.Error()
}

func (d PlanDocument) toPlan() Plan {
	p := Plan{Name: d.Name, Notes: d.Notes}
	for _, it := range d.Items {
		p.Items = append(p.Items, PlanItem{
			Exercise: it.Exercise, TargetSets: it.Sets, RepMin: it.RepMin, RepMax: it.RepMax,
			TargetWeightKG: it.TargetWeightKG, TargetRPE: it.TargetRPE, TargetRIR: it.TargetRIR,
			Superset: it.Superset, Notes: it.Notes,
		})
	}
	return p
}

// PlanToDocument converts a stored plan to the portable format.
func PlanToDocument(p Plan) PlanDocument {
	d := PlanDocument{Format: PlanFormat, Version: PlanVersion, Name: p.Name, Notes: p.Notes, Items: []PlanDocumentItem{}}
	for _, it := range p.Items {
		d.Items = append(d.Items, PlanDocumentItem{
			Exercise: it.Exercise, Sets: it.TargetSets, RepMin: it.RepMin, RepMax: it.RepMax,
			TargetWeightKG: it.TargetWeightKG, TargetRPE: it.TargetRPE, TargetRIR: it.TargetRIR,
			Superset: it.Superset, Notes: it.Notes,
		})
	}
	return d
}

// ExportPlan returns a plan in the portable JSON format. Importing the result
// recreates the same prescriptions.
func (s *Service) ExportPlan(ctx context.Context, id int64) (PlanDocument, error) {
	p, err := s.GetPlan(ctx, id)
	if err != nil {
		return PlanDocument{}, err
	}
	return PlanToDocument(p), nil
}

// ImportPlan validates plan JSON and stores it atomically.
//
// A plan is identified by its name (case-insensitive). If no plan has that
// name, one is created. If one exists, onConflict decides: "error" (default)
// rejects the import with ErrConflict, "replace" swaps the existing plan's
// notes and exercises in place, keeping its id. Workouts already created from
// the plan keep their own copied prescription and are never changed. With
// dryRun the result describes the outcome and nothing is written.
func (s *Service) ImportPlan(ctx context.Context, data []byte, onConflict string, dryRun bool) (PlanImportResult, error) {
	doc, err := ParsePlanDocument(data)
	if err != nil {
		return PlanImportResult{}, err
	}
	switch onConflict = strings.ToLower(strings.TrimSpace(onConflict)); onConflict {
	case "":
		onConflict = OnConflictError
	case OnConflictError, OnConflictReplace:
	default:
		return PlanImportResult{}, invalid("on_conflict", "must be %q or %q", OnConflictError, OnConflictReplace)
	}
	plan := doc.toPlan()
	if err := validatePlan(&plan, "sets"); err != nil {
		return PlanImportResult{}, err
	}
	res, err := s.store.ImportPlan(ctx, plan, onConflict, dryRun)
	if err != nil {
		return res, err
	}
	s.annotateNew(ctx, res.NewExercises)
	return res, nil
}

// ReplacePlan replaces a plan's name, notes and exercises from a validated
// document, atomically, optionally guarded by the revision the caller read.
func (s *Service) ReplacePlan(ctx context.Context, id int64, data []byte, expectedRevision *int) (Plan, error) {
	if id <= 0 {
		return Plan{}, invalid("plan_id", "must be positive")
	}
	doc, err := ParsePlanDocument(data)
	if err != nil {
		return Plan{}, err
	}
	plan := doc.toPlan()
	if err := validatePlan(&plan, "sets"); err != nil {
		return Plan{}, err
	}
	return s.store.ReplacePlan(ctx, id, plan, expectedRevision)
}

// DeletePlanChecked deletes a plan, optionally guarded by its revision.
// Workouts that followed it keep their copied prescription and plan name.
func (s *Service) DeletePlanChecked(ctx context.Context, id int64, expectedRevision *int) error {
	if id <= 0 {
		return invalid("plan_id", "must be positive")
	}
	return s.store.DeletePlanExpected(ctx, id, expectedRevision)
}

// ---- analytics ------------------------------------------------------------

// maxE1RMReps is the highest rep count for which an estimate is produced.
// Beyond it the Epley formula extrapolates too far to be believable.
const maxE1RMReps = 12

func e1rm(w *float64, r *int) float64 {
	if w == nil || r == nil || *w <= 0 || *r > maxE1RMReps {
		return 0
	}
	return Epley1RM(*w, *r)
}

func (s *Service) checkRange(from, to string) error {
	if from != "" && !validDate(from) {
		return invalid("from", "%q is not a valid date; use YYYY-MM-DD", from)
	}
	if to != "" && !validDate(to) {
		return invalid("to", "%q is not a valid date; use YYYY-MM-DD", to)
	}
	if from != "" && to != "" && from > to {
		return invalid("from", "must not be after to")
	}
	return nil
}

// ExerciseStats reports progression for one exercise (name, alias or numeric
// id) over an optional date range. Warm-up sets are counted separately and
// excluded from volume and bests.
func (s *Service) ExerciseStats(ctx context.Context, ref, from, to string) (ExerciseStats, error) {
	if err := s.checkRange(from, to); err != nil {
		return ExerciseStats{}, err
	}
	ex, err := s.GetExercise(ctx, ref)
	if err != nil {
		return ExerciseStats{}, err
	}
	records, err := s.store.SetRecords(ctx, from, to)
	if err != nil {
		return ExerciseStats{}, err
	}
	out := ExerciseStats{ExerciseID: ex.ID, Exercise: ex.Name, From: from, To: to, Points: []ExerciseStatsPoint{}}
	byDate := map[string]*ExerciseStatsPoint{}
	var dates []string
	for _, r := range records {
		if r.ExerciseID != ex.ID {
			continue
		}
		pt := byDate[r.Date]
		if pt == nil {
			pt = &ExerciseStatsPoint{Date: r.Date}
			byDate[r.Date] = pt
			dates = append(dates, r.Date)
		}
		if !r.working() {
			pt.WarmupSets++
			out.WarmupSets++
			continue
		}
		pt.WorkingSets++
		out.WorkingSets++
		if r.Reps != nil {
			pt.TotalReps += *r.Reps
			if r.Technique == "" && (out.MaxRepsAtTop == nil || float64(*r.Reps) > out.MaxRepsAtTop.Value) {
				out.MaxRepsAtTop = &ExerciseBest{Value: float64(*r.Reps), Date: r.Date, SetID: r.SetID}
			}
		}
		if load, ok := r.loadKG(); ok {
			pt.VolumeKG += load
		} else {
			pt.UnrecordedSets++
		}
		// Sets done with an intensity technique (drop set, rest-pause, …) count
		// toward volume but never become a record, matching ExerciseHistory.
		if r.Technique != "" {
			continue
		}
		if r.WeightKG != nil {
			if pt.TopWeightKG == nil || *r.WeightKG > *pt.TopWeightKG {
				w := *r.WeightKG
				pt.TopWeightKG = &w
			}
			if out.MaxWeight == nil || *r.WeightKG > out.MaxWeight.Value {
				out.MaxWeight = &ExerciseBest{Value: *r.WeightKG, Date: r.Date, SetID: r.SetID}
			}
		}
		if e := e1rm(r.WeightKG, r.Reps); e > 0 {
			if e > pt.BestE1RM {
				pt.BestE1RM = e
			}
			if out.BestE1RM == nil || e > out.BestE1RM.Value {
				out.BestE1RM = &ExerciseBest{Value: e, Date: r.Date, SetID: r.SetID}
			}
		}
	}
	sort.Strings(dates)
	for _, d := range dates {
		pt := byDate[d]
		pt.VolumeKG = round2(pt.VolumeKG)
		out.VolumeKG += pt.VolumeKG
		out.Points = append(out.Points, *pt)
	}
	out.VolumeKG = round2(out.VolumeKG)
	out.Workouts = len(dates)
	return out, nil
}

// TrainingSummary aggregates completed working sets over a date range
// (default: the 28 days ending today). Warm-ups are counted separately.
func (s *Service) TrainingSummary(ctx context.Context, from, to string) (TrainingSummary, error) {
	if to == "" {
		to = s.Today()
	}
	if from == "" {
		t, err := time.Parse("2006-01-02", to)
		if err != nil {
			return TrainingSummary{}, invalid("to", "%q is not a valid date; use YYYY-MM-DD", to)
		}
		from = t.AddDate(0, 0, -27).Format("2006-01-02")
	}
	if err := s.checkRange(from, to); err != nil {
		return TrainingSummary{}, err
	}
	records, err := s.store.SetRecords(ctx, from, to)
	if err != nil {
		return TrainingSummary{}, err
	}
	out := TrainingSummary{From: from, To: to, ByExercise: []ExerciseSummary{}, ByMuscle: []GroupSummary{}, ByWeek: []WeekSummary{}}
	workouts := map[string]bool{} // dates with at least one completed working set
	exercises := map[int64]*ExerciseSummary{}
	groups := map[string]*GroupSummary{}
	weeks := map[string]*WeekSummary{}
	weekWorkouts := map[string]map[string]bool{}
	for _, r := range records {
		if !r.working() {
			out.WarmupSets++
			continue
		}
		workouts[r.Date] = true
		wk := weekStart(r.Date)
		w := weeks[wk]
		if w == nil {
			w = &WeekSummary{WeekStart: wk}
			weeks[wk] = w
			weekWorkouts[wk] = map[string]bool{}
		}
		weekWorkouts[wk][r.Date] = true
		e := exercises[r.ExerciseID]
		if e == nil {
			e = &ExerciseSummary{ExerciseID: r.ExerciseID, Exercise: r.Exercise}
			exercises[r.ExerciseID] = e
		}
		g := groups[r.Muscle]
		if g == nil {
			g = &GroupSummary{MuscleGroup: r.Muscle}
			groups[r.Muscle] = g
		}
		out.WorkingSets++
		e.WorkingSets++
		g.WorkingSets++
		w.WorkingSets++
		if load, ok := r.loadKG(); ok {
			out.VolumeKG += load
			e.VolumeKG += load
			g.VolumeKG += load
			w.VolumeKG += load
		} else {
			out.UnrecordedSets++
		}
	}
	out.Workouts = len(workouts)
	out.VolumeKG = round2(out.VolumeKG)
	for _, e := range exercises {
		e.VolumeKG = round2(e.VolumeKG)
		out.ByExercise = append(out.ByExercise, *e)
	}
	sort.Slice(out.ByExercise, func(i, j int) bool {
		if out.ByExercise[i].WorkingSets != out.ByExercise[j].WorkingSets {
			return out.ByExercise[i].WorkingSets > out.ByExercise[j].WorkingSets
		}
		return out.ByExercise[i].Exercise < out.ByExercise[j].Exercise
	})
	for _, g := range groups {
		g.VolumeKG = round2(g.VolumeKG)
		out.ByMuscle = append(out.ByMuscle, *g)
	}
	sort.Slice(out.ByMuscle, func(i, j int) bool {
		if out.ByMuscle[i].WorkingSets != out.ByMuscle[j].WorkingSets {
			return out.ByMuscle[i].WorkingSets > out.ByMuscle[j].WorkingSets
		}
		return out.ByMuscle[i].MuscleGroup < out.ByMuscle[j].MuscleGroup
	})
	for k, w := range weeks {
		w.Workouts = len(weekWorkouts[k])
		w.VolumeKG = round2(w.VolumeKG)
		out.ByWeek = append(out.ByWeek, *w)
	}
	sort.Slice(out.ByWeek, func(i, j int) bool { return out.ByWeek[i].WeekStart < out.ByWeek[j].WeekStart })
	return out, nil
}

// weekStart returns the Monday on or before date.
func weekStart(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	back := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -back).Format("2006-01-02")
}

// ParseRevision parses an optional revision from a string such as an If-Match
// value. Empty means "no precondition".
func ParseRevision(v string) (*int, error) {
	v = strings.Trim(strings.TrimSpace(v), `"`)
	v = strings.TrimPrefix(v, "W/")
	v = strings.Trim(v, `"`)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return nil, invalid("revision", "%q is not a valid revision", v)
	}
	return &n, nil
}
