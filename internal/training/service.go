package training

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func NormalizeSI(si float64) float64 {
	return math.Round(si*10) / 10
}

func CalculateSI(rpe float64) float64 {
	v := 0.2*rpe - 0.6
	if v < 0 {
		return 0
	}
	return NormalizeSI(v)
}

type Service struct {
	store Store
	clock Clock
	loc   *time.Location
}

func NewService(store Store, clock Clock) *Service { return &Service{store: store, clock: clock} }

// WithLocation sets the timezone that decides which calendar date "today" is.
// Without it the clock's own location is used.
func (s *Service) WithLocation(loc *time.Location) *Service {
	s.loc = loc
	return s
}

// Location is the timezone used to resolve local dates.
func (s *Service) Location() *time.Location {
	if s.loc != nil {
		return s.loc
	}
	return s.clock().Location()
}

// Today is the current local calendar date (YYYY-MM-DD) in the configured
// timezone, never the server's UTC date.
func (s *Service) Today() string { return s.clock().In(s.Location()).Format("2006-01-02") }

// ResolveDate returns date if it is a valid YYYY-MM-DD calendar date, or today
// when date is empty.
func (s *Service) ResolveDate(date string) (string, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return s.Today(), nil
	}
	if !validDate(date) {
		return "", invalid("date", "%q is not a valid calendar date; use YYYY-MM-DD", date)
	}
	return date, nil
}

func (s *Service) StartSession(ctx context.Context, date string) (Session, error) {
	return s.StartPlannedSession(ctx, date, 0)
}

// StartPlannedSession returns the workout for date, creating it if needed, and
// optionally makes it follow a plan. There is only ever one workout per local
// date, so starting a date that already has one returns it. planID 0 means no
// plan. An existing workout that already follows a different plan is a
// conflict rather than being silently re-planned.
func (s *Service) StartPlannedSession(ctx context.Context, date string, planID int64) (Session, error) {
	date, err := s.ResolveDate(date)
	if err != nil {
		return Session{}, err
	}
	if planID < 0 {
		return Session{}, invalid("plan_id", "must be positive")
	}
	if planID > 0 {
		if _, err := s.store.GetPlan(ctx, planID); err != nil {
			return Session{}, err
		}
	}
	w, _, err := s.store.GetOrCreateWorkout(ctx, date)
	if err != nil {
		return Session{}, err
	}
	if planID > 0 && w.PlanID != planID {
		if w.PlanID != 0 {
			return Session{}, fmt.Errorf("%w: workout %s already follows plan %d (%s)", ErrConflict, date, w.PlanID, w.PlanName)
		}
		if err := s.store.AttachPlan(ctx, w.ID, planID); err != nil {
			return Session{}, err
		}
		return s.store.GetSession(ctx, w.ID)
	}
	return w, nil
}

// CreatePlan stores a reusable workout template. Exercise names are normalized
// exactly as AddSet normalizes them, so a plan item always matches the sets
// logged against it.
func (s *Service) CreatePlan(ctx context.Context, p Plan) (Plan, error) {
	if err := validatePlan(&p); err != nil {
		return Plan{}, err
	}
	return s.store.CreatePlan(ctx, p)
}

func (s *Service) ListPlans(ctx context.Context) ([]Plan, error) {
	out, err := s.store.ListPlans(ctx)
	if err == nil && out == nil {
		out = []Plan{}
	}
	return out, err
}

func (s *Service) GetPlan(ctx context.Context, id int64) (Plan, error) {
	if id <= 0 {
		return Plan{}, ErrValidation
	}
	return s.store.GetPlan(ctx, id)
}

// UpdatePlan edits a plan's name or notes without touching its exercises.
func (s *Service) UpdatePlan(ctx context.Context, id int64, p PlanPatch) error {
	if id <= 0 || p.Empty() {
		return ErrValidation
	}
	if p.Name != nil {
		v := strings.TrimSpace(*p.Name)
		if v == "" {
			return ErrValidation
		}
		p.Name = &v
	}
	if p.Notes != nil {
		v := strings.TrimSpace(*p.Notes)
		if len(v) > maxPlanNotesLen {
			return ErrValidation
		}
		p.Notes = &v
	}
	return s.store.UpdatePlan(ctx, id, p)
}

// validPlanItem holds the rules a prescription must satisfy, shared by plan
// creation, plan editing and in-session adjustments so none can drift.
func validPlanItem(it *PlanItem) error {
	return issues(planItemIssues(it, ""))
}

func planItemIssues(it *PlanItem, path string) []Issue {
	f := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}
	it.Exercise = NormalizeExerciseName(it.Exercise)
	it.Notes = strings.TrimSpace(it.Notes)
	it.Superset = normalizeSuperset(it.Superset)
	var out []Issue
	add := func(field, format string, args ...any) {
		out = append(out, Issue{Field: f(field), Message: fmt.Sprintf(format, args...)})
	}
	if it.Exercise == "" {
		add("exercise", "is required")
	} else if tooLong(it.Exercise, maxExerciseLen) {
		add("exercise", "must be at most %d characters", maxExerciseLen)
	}
	if it.TargetSets <= 0 || it.TargetSets > 20 {
		add("sets", "must be between 1 and 20, got %d", it.TargetSets)
	}
	if it.RepMin < 0 || it.RepMax < 0 || it.RepMin > MaxReps || it.RepMax > MaxReps {
		add("reps", "rep_min and rep_max must be between 0 and %d (0 means not prescribed)", MaxReps)
	} else if it.RepMax > 0 && it.RepMin > it.RepMax {
		add("reps", "rep_min (%d) must not exceed rep_max (%d)", it.RepMin, it.RepMax)
	}
	if it.TargetRPE != 0 && (!finite(it.TargetRPE) || it.TargetRPE < 1 || it.TargetRPE > 10) {
		add("target_rpe", "must be between 1 and 10 (omit it for no target)")
	}
	if it.TargetWeightKG != nil && (!finite(*it.TargetWeightKG) || *it.TargetWeightKG < 0 || *it.TargetWeightKG > MaxWeightKG) {
		add("target_weight_kg", "must be between 0 and %d kg (omit it for no target)", MaxWeightKG)
	}
	if it.TargetRIR != nil && (!finite(*it.TargetRIR) || *it.TargetRIR < 0 || *it.TargetRIR > 10) {
		add("target_rir", "must be between 0 and 10 (omit it for no target)")
	}
	if tooLong(it.Notes, maxPlanNotesLen) {
		add("notes", "must be at most %d characters", maxPlanNotesLen)
	}
	if tooLong(it.Superset, maxTechniqueLen) {
		add("superset", "must be at most %d characters", maxTechniqueLen)
	}
	return out
}

// validatePlan normalizes and checks a whole plan, collecting every problem.
// Exercises must be unique within a plan.
func validatePlan(p *Plan) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Notes = strings.TrimSpace(p.Notes)
	var out []Issue
	if p.Name == "" {
		out = append(out, Issue{Field: "name", Message: "is required"})
	} else if tooLong(p.Name, 100) {
		out = append(out, Issue{Field: "name", Message: "must be at most 100 characters"})
	}
	if tooLong(p.Notes, maxPlanNotesLen) {
		out = append(out, Issue{Field: "notes", Message: fmt.Sprintf("must be at most %d characters", maxPlanNotesLen)})
	}
	if len(p.Items) == 0 || len(p.Items) > MaxBatchItems {
		out = append(out, Issue{Field: "items", Message: fmt.Sprintf("must contain between 1 and %d exercises", MaxBatchItems)})
	}
	seen := map[string]int{}
	for i := range p.Items {
		out = append(out, planItemIssues(&p.Items[i], fmt.Sprintf("items[%d]", i))...)
		if first, dup := seen[p.Items[i].Exercise]; dup && p.Items[i].Exercise != "" {
			out = append(out, Issue{Field: fmt.Sprintf("items[%d].exercise", i),
				Message: fmt.Sprintf("duplicates items[%d] (%q); list each exercise once and use sets for volume", first, p.Items[i].Exercise)})
		}
		seen[p.Items[i].Exercise] = i
	}
	return issues(out)
}

// SetPlanItem adds an exercise to a saved plan or replaces its prescription.
// Editing a template does not touch sessions already started from it.
func (s *Service) SetPlanItem(ctx context.Context, planID int64, it PlanItem) error {
	if planID <= 0 {
		return ErrValidation
	}
	if err := validPlanItem(&it); err != nil {
		return err
	}
	return s.store.SetPlanItem(ctx, planID, it)
}

func (s *Service) RemovePlanItem(ctx context.Context, planID int64, exercise string) error {
	exercise = s.canon(ctx, exercise)
	if planID <= 0 || exercise == "" {
		return ErrValidation
	}
	return s.store.RemovePlanItem(ctx, planID, exercise)
}

// MovePlanItem shifts an exercise one place up or down in the plan's order.
func (s *Service) MovePlanItem(ctx context.Context, planID int64, exercise string, delta int) error {
	exercise = s.canon(ctx, exercise)
	if planID <= 0 || exercise == "" || (delta != 1 && delta != -1) {
		return ErrValidation
	}
	return s.store.MovePlanItem(ctx, planID, exercise, delta)
}

func (s *Service) DeletePlan(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrValidation
	}
	return s.store.DeletePlan(ctx, id)
}

// SetSessionItem adds an exercise to today's session plan or replaces its
// prescription. Adjusting a session never touches the plan it came from.
func (s *Service) SetSessionItem(ctx context.Context, sessionID int64, it PlanItem) error {
	if sessionID <= 0 {
		return ErrValidation
	}
	if err := validPlanItem(&it); err != nil {
		return err
	}
	return s.store.SetSessionItem(ctx, sessionID, it)
}

// AdjustSessionItem changes only what is given: the set count, the rep range,
// the RPE, or whether the exercise is skipped today.
func (s *Service) AdjustSessionItem(ctx context.Context, sessionID int64, exercise string, p SessionItemPatch) error {
	exercise = s.canon(ctx, exercise)
	if sessionID <= 0 || exercise == "" || p.Empty() {
		return ErrValidation
	}
	if p.TargetSets != nil && (*p.TargetSets <= 0 || *p.TargetSets > 20) {
		return ErrValidation
	}
	if p.RepMin != nil && *p.RepMin < 0 {
		return ErrValidation
	}
	if p.RepMax != nil && *p.RepMax < 0 {
		return ErrValidation
	}
	if p.RepMin != nil && p.RepMax != nil && *p.RepMax > 0 && *p.RepMin > *p.RepMax {
		return fmt.Errorf("%w: rep range is inverted", ErrValidation)
	}
	if p.TargetRPE != nil && *p.TargetRPE != 0 && (*p.TargetRPE < 1 || *p.TargetRPE > 10) {
		return ErrValidation
	}
	if p.TargetWeightKG != nil && (!finite(*p.TargetWeightKG) || *p.TargetWeightKG < 0 || *p.TargetWeightKG > MaxWeightKG) {
		return invalid("target_weight_kg", "must be between 0 and %d kg", MaxWeightKG)
	}
	if p.TargetRIR != nil && (!finite(*p.TargetRIR) || *p.TargetRIR < 0 || *p.TargetRIR > 10) {
		return invalid("target_rir", "must be between 0 and 10")
	}
	return s.store.PatchSessionItem(ctx, sessionID, exercise, p)
}

// SwapSessionItem substitutes an exercise in today's session, keeping its
// prescription. Use it when the planned machine is taken.
func (s *Service) SwapSessionItem(ctx context.Context, sessionID int64, from, to string) error {
	from = s.canon(ctx, from)
	to = s.canon(ctx, to)
	if sessionID <= 0 || from == "" || to == "" || from == to {
		return ErrValidation
	}
	return s.store.SwapSessionItem(ctx, sessionID, from, to)
}

// RemoveSessionItem drops an exercise from today's plan. Sets already logged
// against it are kept and reappear as off-plan work.
func (s *Service) RemoveSessionItem(ctx context.Context, sessionID int64, exercise string) error {
	exercise = s.canon(ctx, exercise)
	if sessionID <= 0 || exercise == "" {
		return ErrValidation
	}
	return s.store.RemoveSessionItem(ctx, sessionID, exercise)
}

// SaveSessionAsPlan promotes what a session actually planned — after any
// in-session adjustments — into a reusable plan.
func (s *Service) SaveSessionAsPlan(ctx context.Context, sessionID int64, name string) (Plan, error) {
	progress, err := s.SessionProgress(ctx, sessionID)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{Name: name}
	for _, p := range progress {
		if p.Skipped {
			continue
		}
		sets := p.TargetSets
		if sets == 0 {
			// Off-plan work becomes part of the template at the volume it was
			// actually performed.
			sets = p.DoneSets
		}
		if sets == 0 {
			continue
		}
		plan.Items = append(plan.Items, PlanItem{
			Exercise: p.Exercise, TargetSets: sets,
			RepMin: p.RepMin, RepMax: p.RepMax, TargetRPE: p.TargetRPE,
			TargetWeightKG: p.TargetWeightKG, TargetRIR: p.TargetRIR, Superset: p.Superset, Notes: p.Notes,
		})
	}
	return s.CreatePlan(ctx, plan)
}

// RecordFeedback stores how one muscle group responded to a session. Only the
// groups actually trained need rating, which keeps this a few taps.
func (s *Service) RecordFeedback(ctx context.Context, sessionID int64, f Feedback) error {
	f.MuscleGroup = strings.ToLower(strings.TrimSpace(f.MuscleGroup))
	if sessionID <= 0 || !f.Valid() {
		return ErrValidation
	}
	return s.store.SetFeedback(ctx, sessionID, f)
}

func (s *Service) SessionFeedback(ctx context.Context, sessionID int64) ([]Feedback, error) {
	if sessionID <= 0 {
		return nil, ErrValidation
	}
	out, err := s.store.SessionFeedback(ctx, sessionID)
	if err == nil && out == nil {
		out = []Feedback{}
	}
	return out, err
}

// SetLandmarks records a muscle group's personal weekly-set boundaries.
func (s *Service) SetLandmarks(ctx context.Context, l Landmarks) error {
	l.MuscleGroup = strings.ToLower(strings.TrimSpace(l.MuscleGroup))
	if !ValidMuscleGroup(l.MuscleGroup) || l.MEV < 0 || l.MRV < 0 {
		return ErrValidation
	}
	if l.MEV > 0 && l.MRV > 0 && l.MEV > l.MRV {
		return fmt.Errorf("%w: MEV must not exceed MRV", ErrValidation)
	}
	return s.store.SetLandmarks(ctx, l)
}

func (s *Service) Landmarks(ctx context.Context) ([]Landmarks, error) {
	all, err := s.store.AllLandmarks(ctx)
	if err != nil {
		return nil, err
	}
	out := []Landmarks{}
	for _, g := range MuscleGroups {
		if l, ok := all[g]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

// VolumeRecommendation turns recent feedback into a set change for next week.
//
// It weights recent sessions over older ones instead of reading only the last,
// says how many ratings it is based on, declines to advise on a single one, and
// refuses to push volume past a muscle group's known landmarks.
func (s *Service) VolumeRecommendation(ctx context.Context) ([]SetChange, error) {
	history, err := s.store.RecentFeedback(ctx, len(feedbackWeights))
	if err != nil {
		return nil, err
	}
	landmarks, err := s.store.AllLandmarks(ctx)
	if err != nil {
		return nil, err
	}
	weeks, err := s.store.WeeklyVolume(ctx, ListFilter{})
	if err != nil {
		return nil, err
	}
	lastSets := map[string]int{}
	seen := map[string]bool{}
	for _, w := range weeks {
		if !seen[w.MuscleGroup] {
			seen[w.MuscleGroup] = true
			lastSets[w.MuscleGroup] = w.Sets
		}
	}

	out := []SetChange{}
	for _, group := range MuscleGroups {
		rated, ok := history[group]
		if !ok || len(rated) == 0 {
			continue
		}
		magnitude := WeightedMagnitude(rated)
		change := SetChange{
			MuscleGroup:  group,
			Magnitude:    magnitude,
			LastWeekSets: lastSets[group],
			Samples:      len(rated),
			Confidence:   ConfidenceFor(len(rated)),
			MEV:          landmarks[group].MEV,
			MRV:          landmarks[group].MRV,
		}
		if len(rated) < 2 {
			// One rated session is an anecdote. Say so rather than dress it up.
			change.Advice = "Solo una sesión valorada: entrena otra y vuelve a valorar antes de cambiar el volumen."
			out = append(out, change)
			continue
		}
		delta, advice := RecommendSets(int(math.Round(magnitude)))
		change.SetsDelta, change.Advice = delta, advice
		change.SetsDelta, change.Clamped = clampToLandmarks(change.LastWeekSets, delta, change.MEV, change.MRV)
		if change.Clamped {
			change.Advice = clampAdvice(change.LastWeekSets, change.SetsDelta, change.MEV, change.MRV)
		}
		out = append(out, change)
	}
	return out, nil
}

// clampToLandmarks refuses to push weekly volume past what this muscle group is
// known to tolerate, or below what it needs to grow.
func clampToLandmarks(current, delta, mev, mrv int) (int, bool) {
	proposed := current + delta
	switch {
	case mrv > 0 && proposed > mrv:
		return mrv - current, true
	case mev > 0 && proposed < mev:
		return mev - current, true
	default:
		return delta, false
	}
}

// clampAdvice describes what to actually do, not what the raw rule wanted. When
// the current volume is already past a landmark the clamped delta points the
// other way, and saying "do not go up" while advising a cut would be wrong.
func clampAdvice(current, delta, mev, mrv int) string {
	switch {
	case delta < 0:
		return fmt.Sprintf("Estás en %d series y tu MRV es %d: baja %d aunque el estímulo pida más.", current, mrv, -delta)
	case delta > 0:
		return fmt.Sprintf("Estás en %d series y tu MEV es %d: sube %d para volver al mínimo efectivo.", current, mev, delta)
	case mrv > 0 && current >= mrv:
		return fmt.Sprintf("Ya estás en tu MRV (%d series): mantén el volumen.", mrv)
	default:
		return fmt.Sprintf("Ya estás en tu MEV (%d series): mantén el volumen.", mev)
	}
}

// Snapshot writes a consistent copy of the database to path, for backup.
func (s *Service) Snapshot(ctx context.Context, path string) error {
	if strings.TrimSpace(path) == "" {
		return ErrValidation
	}
	return s.store.Snapshot(ctx, path)
}

// SuggestLoad advises the next working weight for an exercise, judged against
// the prescription it currently carries in today's session, falling back to the
// most recent plan that prescribes it.
func (s *Service) SuggestLoad(ctx context.Context, exercise string) (LoadSuggestion, error) {
	exercise = s.canon(ctx, exercise)
	if exercise == "" {
		return LoadSuggestion{}, ErrValidation
	}
	history, err := s.ExerciseHistory(ctx, exercise, 100)
	if err != nil {
		return LoadSuggestion{}, err
	}
	// Sets carrying an intensity technique are not comparable to straight sets,
	// so they never drive the advice.
	var last *ExerciseSet
	for i := range history.Sets {
		if history.Sets[i].Technique == "" {
			last = &history.Sets[i]
			break
		}
	}
	repMin, repMax, targetRPE := s.prescriptionFor(ctx, exercise)
	out := SuggestLoad(last, repMin, repMax, targetRPE)
	out.Exercise = exercise
	return out, nil
}

// prescriptionFor finds the rep range and RPE an exercise is prescribed at,
// preferring today's session over any saved plan.
func (s *Service) prescriptionFor(ctx context.Context, exercise string) (int, int, float64) {
	today := s.Today()
	if sessions, err := s.ListSessions(ctx, ListFilter{From: today, To: today, Limit: 1}); err == nil && len(sessions) > 0 {
		if progress, err := s.SessionProgress(ctx, sessions[0].ID); err == nil {
			for _, p := range progress {
				if p.Exercise == exercise && p.TargetSets > 0 {
					return p.RepMin, p.RepMax, p.TargetRPE
				}
			}
		}
	}
	plans, err := s.ListPlans(ctx)
	if err != nil {
		return 0, 0, 0
	}
	for _, summary := range plans {
		plan, err := s.GetPlan(ctx, summary.ID)
		if err != nil {
			continue
		}
		for _, it := range plan.Items {
			if it.Exercise == exercise {
				return it.RepMin, it.RepMax, it.TargetRPE
			}
		}
	}
	return 0, 0, 0
}

// SessionProgress reports planned versus completed sets for a session.
func (s *Service) SessionProgress(ctx context.Context, sessionID int64) ([]PlanProgress, error) {
	if sessionID <= 0 {
		return nil, ErrValidation
	}
	out, err := s.store.SessionProgress(ctx, sessionID)
	if err == nil && out == nil {
		out = []PlanProgress{}
	}
	return out, err
}

// LogSet is the one-call entry path: it finds the workout for date (today when
// empty), creates it if needed, and records count copies of the set. Callers
// never handle a workout id, which is what makes conversational logging a
// single step. It is a convenience over LogSets.
func (s *Service) LogSet(ctx context.Context, date string, in AddSetInput, count int, key string) (BatchResult, error) {
	if count <= 0 {
		count = 1
	}
	if count > 20 {
		return BatchResult{}, invalid("count", "must be between 1 and 20, got %d", count)
	}
	sets := make([]AddSetInput, count)
	for i := range sets {
		sets[i] = in
	}
	return s.LogSets(ctx, BatchInput{Date: date, Sets: sets, IdempotencyKey: key})
}

// normalizeSet validates and normalizes one performed set. path prefixes the
// reported field names, e.g. "sets[2]".
func normalizeSet(in *AddSetInput, path string) []Issue {
	f := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}
	var out []Issue
	add := func(field, format string, args ...any) {
		out = append(out, Issue{Field: f(field), Message: fmt.Sprintf(format, args...)})
	}
	in.Exercise = NormalizeExerciseName(in.Exercise)
	in.Technique = normalizeTechnique(in.Technique)
	in.Superset = normalizeSuperset(in.Superset)
	in.Notes = strings.TrimSpace(in.Notes)
	in.SetType = strings.ToLower(strings.TrimSpace(in.SetType))
	if in.SetType == "" {
		in.SetType = SetTypeWorking
	}
	if in.Exercise == "" && in.ExerciseID <= 0 {
		add("exercise", "is required (a name, or exercise_id of an existing exercise)")
	}
	if tooLong(in.Exercise, maxExerciseLen) {
		add("exercise", "must be at most %d characters", maxExerciseLen)
	}
	if in.WeightKG != nil && (!finite(*in.WeightKG) || *in.WeightKG < 0 || *in.WeightKG > MaxWeightKG) {
		add("weight_kg", "must be between 0 and %d kg; omit it if the weight is unknown (0 means an explicit zero load)", MaxWeightKG)
	}
	if in.Reps != nil && (*in.Reps < 1 || *in.Reps > MaxReps) {
		add("reps", "must be a whole number between 1 and %d; omit it if unknown", MaxReps)
	}
	if in.RPE != nil && (!finite(*in.RPE) || *in.RPE < 1 || *in.RPE > 10) {
		add("rpe", "must be between 1 and 10; omit it if unknown")
	}
	if in.RIR != nil && (!finite(*in.RIR) || *in.RIR < 0 || *in.RIR > 10) {
		add("rir", "must be between 0 and 10; omit it if unknown")
	}
	if !validSetType(in.SetType) {
		add("set_type", "%q is not valid; use one of %s", in.SetType, strings.Join(SetTypes, ", "))
	}
	if tooLong(in.Notes, maxSetNotes) {
		add("notes", "must be at most %d characters", maxSetNotes)
	}
	if tooLong(in.Technique, maxTechniqueLen) {
		add("technique", "must be at most %d characters", maxTechniqueLen)
	}
	if tooLong(in.Superset, maxTechniqueLen) {
		add("superset", "must be at most %d characters", maxTechniqueLen)
	}
	return out
}

// LogSets is the primary write path for incremental logging. In one
// transaction it finds or creates the workout for the date, appends the given
// performed sets, merges the given prescriptions, and records the idempotency
// key. Nothing is applied if any part is invalid.
//
// Prescriptions describe what is planned and never create completed sets.
func (s *Service) LogSets(ctx context.Context, in BatchInput) (BatchResult, error) {
	date, err := s.ResolveDate(in.Date)
	if err != nil {
		return BatchResult{}, err
	}
	in.Date = date
	var iss []Issue
	if in.PlanID < 0 {
		iss = append(iss, Issue{Field: "plan_id", Message: "must be positive"})
	}
	if len(in.Sets) == 0 && len(in.Prescriptions) == 0 {
		iss = append(iss, Issue{Field: "sets", Message: "provide at least one set or prescription"})
	}
	if len(in.Sets) > MaxBatchSets {
		iss = append(iss, Issue{Field: "sets", Message: fmt.Sprintf("at most %d sets per request", MaxBatchSets)})
	}
	if len(in.Prescriptions) > MaxBatchItems {
		iss = append(iss, Issue{Field: "prescriptions", Message: fmt.Sprintf("at most %d prescriptions per request", MaxBatchItems)})
	}
	if len(in.IdempotencyKey) > MaxKeyLen {
		iss = append(iss, Issue{Field: "idempotency_key", Message: fmt.Sprintf("must be at most %d characters", MaxKeyLen)})
	}
	if len(in.Sets) > 0 && date > s.Today() {
		iss = append(iss, Issue{Field: "date", Message: fmt.Sprintf("%s is in the future; sets can only be logged for today or earlier (today is %s)", date, s.Today())})
	}
	sets := make([]AddSetInput, len(in.Sets))
	copy(sets, in.Sets)
	for i := range sets {
		iss = append(iss, normalizeSet(&sets[i], fmt.Sprintf("sets[%d]", i))...)
	}
	in.Sets = sets
	pres := make([]PlanItem, len(in.Prescriptions))
	copy(pres, in.Prescriptions)
	seen := map[string]int{}
	for i := range pres {
		iss = append(iss, planItemIssues(&pres[i], fmt.Sprintf("prescriptions[%d]", i))...)
		if first, dup := seen[pres[i].Exercise]; dup && pres[i].Exercise != "" {
			iss = append(iss, Issue{Field: fmt.Sprintf("prescriptions[%d].exercise", i), Message: fmt.Sprintf("duplicates prescriptions[%d]", first)})
		}
		seen[pres[i].Exercise] = i
	}
	in.Prescriptions = pres
	if err := issues(iss); err != nil {
		return BatchResult{}, err
	}
	res, err := s.store.LogBatch(ctx, in)
	if err != nil {
		return BatchResult{}, err
	}
	s.annotateNew(ctx, res.NewExercises)
	if res.Workout.ID > 0 {
		if res.Workout, err = s.decorate(ctx, res.Workout); err != nil {
			return res, err
		}
	}
	return res, nil
}

// annotateNew fills in near-miss names for exercises a write just created.
func (s *Service) annotateNew(ctx context.Context, created []NewExercise) {
	if len(created) == 0 {
		return
	}
	all, err := s.store.Exercises(ctx)
	if err != nil {
		return
	}
	var names []string
	for _, e := range all {
		names = append(names, e.Name)
		names = append(names, e.Aliases...)
	}
	for i := range created {
		created[i].Similar = SimilarNames(created[i].Name, names)
	}
}

// decorate adds derived read-only data to a workout: per-exercise set numbers
// and planned-versus-completed progress.
func (s *Service) decorate(ctx context.Context, w Session) (Session, error) {
	w = withSetNumbers(w)
	progress, err := s.store.SessionProgress(ctx, w.ID)
	if err != nil {
		return w, err
	}
	if progress == nil {
		progress = []PlanProgress{}
	}
	w.Progress = progress
	return w, nil
}

func withSetNumbers(w Session) Session {
	n := map[int64]int{}
	for i := range w.Sets {
		n[w.Sets[i].ExerciseID]++
		w.Sets[i].SetNumber = n[w.Sets[i].ExerciseID]
	}
	return w
}

// GetWorkout returns the workout for a date (today when empty) with its sets
// and planned-versus-completed progress. It does not create anything: a date
// without a workout is ErrNotFound.
func (s *Service) GetWorkout(ctx context.Context, date string) (Session, error) {
	date, err := s.ResolveDate(date)
	if err != nil {
		return Session{}, err
	}
	w, err := s.store.WorkoutByDate(ctx, date)
	if err != nil {
		return Session{}, err
	}
	return s.decorate(ctx, w)
}

// EnsureWorkout is get-or-create by local date. It is safe to call
// concurrently; all callers for one date receive the same workout.
func (s *Service) EnsureWorkout(ctx context.Context, date string) (Session, bool, error) {
	date, err := s.ResolveDate(date)
	if err != nil {
		return Session{}, false, err
	}
	w, created, err := s.store.GetOrCreateWorkout(ctx, date)
	if err != nil {
		return w, created, err
	}
	w, err = s.decorate(ctx, w)
	return w, created, err
}

// AddSet records one set in an existing workout, by id. Prefer LogSets for
// conversational logging.
func (s *Service) AddSet(ctx context.Context, in AddSetInput) (Set, float64, error) {
	if in.SessionID <= 0 {
		return Set{}, 0, invalid("session_id", "must be positive")
	}
	if err := issues(normalizeSet(&in, "")); err != nil {
		return Set{}, 0, err
	}
	if in.Superset == "" && in.Exercise != "" {
		// Derive the round from the session's plan, so logging a superset set
		// never costs an extra field.
		label, err := s.store.SupersetFor(ctx, in.SessionID, in.Exercise)
		if err != nil {
			return Set{}, 0, err
		}
		in.Superset = label
	}
	return s.store.AddSet(ctx, in)
}

// SetExerciseNote stores a persistent setup reminder for an exercise. An empty
// note removes it.
func (s *Service) SetExerciseNote(ctx context.Context, exercise, note string) error {
	exercise = s.canon(ctx, exercise)
	note = strings.TrimSpace(note)
	if exercise == "" || len(note) > maxNoteLen {
		return ErrValidation
	}
	return s.store.SetExerciseNote(ctx, ExerciseNote{Exercise: exercise, Note: note})
}

func (s *Service) ExerciseNotes(ctx context.Context) ([]ExerciseNote, error) {
	out, err := s.store.ExerciseNotes(ctx)
	if err == nil && out == nil {
		out = []ExerciseNote{}
	}
	return out, err
}

// normalizeTechnique lowercases and trims so "Drop Set" and "drop set" are the
// same technique, the same rule exercise names follow.
func normalizeTechnique(v string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(v))), " ")
}

// normalizeSuperset uppercases instead. A round label is an identifier, so "a"
// and "A" must group together, but it reads as "A" everywhere it is shown.
func normalizeSuperset(v string) string {
	return strings.Join(strings.Fields(strings.ToUpper(strings.TrimSpace(v))), " ")
}

// UpdateSet changes only the fields given on one set, addressed by its stable
// id. Set a Clear* flag to turn a recorded value back into "not recorded".
func (s *Service) UpdateSet(ctx context.Context, id int64, p SetPatch) (Set, float64, error) {
	if id <= 0 {
		return Set{}, 0, invalid("set_id", "must be positive")
	}
	var iss []Issue
	empty := p.Exercise == nil && p.ExerciseID == nil && p.WeightKG == nil && p.Reps == nil && p.RPE == nil && p.RIR == nil &&
		p.SetType == nil && p.Notes == nil && p.Technique == nil && !p.ClearWeight && !p.ClearReps && !p.ClearRPE && !p.ClearRIR
	if empty {
		iss = append(iss, Issue{Message: "provide at least one field to change"})
	}
	conflict := func(set *bool, flag bool, name string) {
		if set != nil && *set && flag {
			iss = append(iss, Issue{Field: name, Message: "cannot both set a value and clear it"})
		}
	}
	t, f := true, false
	has := func(b bool) *bool {
		if b {
			return &t
		}
		return &f
	}
	conflict(has(p.WeightKG != nil), p.ClearWeight, "weight_kg")
	conflict(has(p.Reps != nil), p.ClearReps, "reps")
	conflict(has(p.RPE != nil), p.ClearRPE, "rpe")
	conflict(has(p.RIR != nil), p.ClearRIR, "rir")
	if p.Exercise != nil {
		v := NormalizeExerciseName(*p.Exercise)
		if v == "" || tooLong(v, maxExerciseLen) {
			iss = append(iss, Issue{Field: "exercise", Message: fmt.Sprintf("must be 1-%d characters", maxExerciseLen)})
		}
		p.Exercise = &v
	}
	if p.WeightKG != nil && (!finite(*p.WeightKG) || *p.WeightKG < 0 || *p.WeightKG > MaxWeightKG) {
		iss = append(iss, Issue{Field: "weight_kg", Message: fmt.Sprintf("must be between 0 and %d kg", MaxWeightKG)})
	}
	if p.Reps != nil && (*p.Reps < 1 || *p.Reps > MaxReps) {
		iss = append(iss, Issue{Field: "reps", Message: fmt.Sprintf("must be a whole number between 1 and %d", MaxReps)})
	}
	if p.RPE != nil && (!finite(*p.RPE) || *p.RPE < 1 || *p.RPE > 10) {
		iss = append(iss, Issue{Field: "rpe", Message: "must be between 1 and 10"})
	}
	if p.RIR != nil && (!finite(*p.RIR) || *p.RIR < 0 || *p.RIR > 10) {
		iss = append(iss, Issue{Field: "rir", Message: "must be between 0 and 10"})
	}
	if p.SetType != nil {
		v := strings.ToLower(strings.TrimSpace(*p.SetType))
		if !validSetType(v) {
			iss = append(iss, Issue{Field: "set_type", Message: fmt.Sprintf("%q is not valid; use one of %s", v, strings.Join(SetTypes, ", "))})
		}
		p.SetType = &v
	}
	if p.Notes != nil {
		v := strings.TrimSpace(*p.Notes)
		if tooLong(v, maxSetNotes) {
			iss = append(iss, Issue{Field: "notes", Message: fmt.Sprintf("must be at most %d characters", maxSetNotes)})
		}
		p.Notes = &v
	}
	if p.Technique != nil {
		v := normalizeTechnique(*p.Technique)
		if len(v) > maxTechniqueLen {
			iss = append(iss, Issue{Field: "technique", Message: fmt.Sprintf("must be at most %d characters", maxTechniqueLen)})
		}
		p.Technique = &v
	}
	if err := issues(iss); err != nil {
		return Set{}, 0, err
	}
	return s.store.UpdateSet(ctx, id, p)
}

// DeleteSet removes one set by id. expectedRevision, when non-nil, must match
// the set's current revision. Remaining sets keep their ids; positions are
// compacted.
func (s *Service) DeleteSet(ctx context.Context, id int64, expectedRevision *int) (int64, float64, int, error) {
	if id <= 0 {
		return 0, 0, 0, invalid("set_id", "must be positive")
	}
	return s.store.DeleteSet(ctx, id, expectedRevision)
}

// GetSet returns one set by id.
func (s *Service) GetSet(ctx context.Context, id int64) (Set, error) {
	if id <= 0 {
		return Set{}, invalid("set_id", "must be positive")
	}
	return s.store.GetSet(ctx, id)
}
func (s *Service) GetSession(ctx context.Context, id int64) (Session, error) {
	if id <= 0 {
		return Session{}, ErrValidation
	}
	return s.store.GetSession(ctx, id)
}
func (s *Service) ListSessions(ctx context.Context, f ListFilter) ([]SessionSummary, error) {
	if f.Limit < 0 || f.Limit > 100 || (f.From != "" && !validDate(f.From)) || (f.To != "" && !validDate(f.To)) {
		return nil, ErrValidation
	}
	if f.From != "" && f.To != "" && f.From > f.To {
		return nil, fmt.Errorf("%w: invalid date range: from must not be after to", ErrValidation)
	}
	if f.Limit == 0 {
		f.Limit = 20
	}
	sessions, err := s.store.ListSessions(ctx, f)
	if err == nil && sessions == nil {
		sessions = []SessionSummary{}
	}
	return sessions, err
}

// RecentExercises returns the most recently used exercises, newest first, each
// carrying the values of its last recorded set.
func (s *Service) RecentExercises(ctx context.Context, limit int) ([]ExerciseMemory, error) {
	if limit < 0 || limit > 50 {
		return nil, ErrValidation
	}
	if limit == 0 {
		limit = 8
	}
	out, err := s.store.RecentExercises(ctx, limit)
	if err == nil && out == nil {
		out = []ExerciseMemory{}
	}
	return out, err
}

// DeleteSession removes a session and every set in it. Irreversible, so the
// number of sets destroyed is returned for the caller to report.
func (s *Service) DeleteSession(ctx context.Context, id int64) (int, error) {
	if id <= 0 {
		return 0, ErrValidation
	}
	return s.store.DeleteSession(ctx, id)
}

// ExerciseHistory returns one exercise's sets newest first, plus its best set
// by estimated 1RM.
func (s *Service) ExerciseHistory(ctx context.Context, exercise string, limit int) (ExerciseHistory, error) {
	exercise = s.canon(ctx, exercise)
	if exercise == "" || limit < 0 || limit > 500 {
		return ExerciseHistory{}, ErrValidation
	}
	if limit == 0 {
		limit = 50
	}
	out, err := s.store.ExerciseHistory(ctx, exercise, limit)
	if err == nil && out.Sets == nil {
		out.Sets = []ExerciseSet{}
	}
	return out, err
}

// WeeklyVolume returns SI per muscle group bucketed by training week.
func (s *Service) WeeklyVolume(ctx context.Context, f ListFilter) ([]WeeklyVolume, error) {
	if (f.From != "" && !validDate(f.From)) || (f.To != "" && !validDate(f.To)) {
		return nil, ErrValidation
	}
	if f.From != "" && f.To != "" && f.From > f.To {
		return nil, fmt.Errorf("%w: invalid date range: from must not be after to", ErrValidation)
	}
	out, err := s.store.WeeklyVolume(ctx, f)
	if err == nil && out == nil {
		out = []WeeklyVolume{}
	}
	return out, err
}

// SetExerciseGroup assigns an exercise to a muscle group. The exercise name is
// normalized the same way AddSet normalizes it, so the catalogue key always
// matches what is stored on sets.
func (s *Service) SetExerciseGroup(ctx context.Context, exercise, group string) error {
	exercise = s.canon(ctx, exercise)
	group = strings.ToLower(strings.TrimSpace(group))
	if exercise == "" || !ValidMuscleGroup(group) {
		return ErrValidation
	}
	return s.store.SetExerciseGroup(ctx, ExerciseGroup{Exercise: exercise, MuscleGroup: group})
}

func (s *Service) ExerciseGroups(ctx context.Context) ([]ExerciseGroup, error) {
	out, err := s.store.ExerciseGroups(ctx)
	if err == nil && out == nil {
		out = []ExerciseGroup{}
	}
	return out, err
}

// VolumeByGroup totals SI per muscle group over an optional date range.
func (s *Service) VolumeByGroup(ctx context.Context, f ListFilter) ([]GroupVolume, error) {
	if (f.From != "" && !validDate(f.From)) || (f.To != "" && !validDate(f.To)) {
		return nil, ErrValidation
	}
	if f.From != "" && f.To != "" && f.From > f.To {
		return nil, fmt.Errorf("%w: invalid date range: from must not be after to", ErrValidation)
	}
	out, err := s.store.VolumeByGroup(ctx, f)
	if err == nil && out == nil {
		out = []GroupVolume{}
	}
	return out, err
}

func validDate(v string) bool { _, err := time.Parse("2006-01-02", v); return err == nil }

// canon normalizes an exercise name and resolves an alias to its canonical
// name, so every lookup behaves the same whichever spelling the caller used.
// An unknown name is returned normalized; reads then simply find nothing.
func (s *Service) canon(ctx context.Context, name string) string {
	name = NormalizeExerciseName(name)
	if name == "" {
		return name
	}
	if _, err := strconv.Atoi(name); err == nil {
		return name // GetExercise would read a bare number as an id
	}
	if ex, err := s.store.GetExercise(ctx, name); err == nil {
		return ex.Name
	}
	return name
}
