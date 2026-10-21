package apisrv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

// ---- wire types -------------------------------------------------------------

// workoutJSON renders a workout with sets and progress always present (empty
// arrays rather than absent) and plan fields explicitly nullable.
type workoutJSON struct {
	training.Session
	Sets     []training.Set          `json:"sets"`
	Progress []training.PlanProgress `json:"progress"`
	PlanID   *int64                  `json:"plan_id"`
	PlanName *string                 `json:"plan_name"`
}

func toWorkout(w training.Session) workoutJSON {
	out := workoutJSON{Session: w, Sets: w.Sets, Progress: w.Progress}
	if out.Sets == nil {
		out.Sets = []training.Set{}
	}
	if out.Progress == nil {
		out.Progress = []training.PlanProgress{}
	}
	if w.PlanID > 0 {
		id, name := w.PlanID, w.PlanName
		out.PlanID, out.PlanName = &id, &name
	}
	return out
}

type batchResultJSON struct {
	training.BatchResult
	Workout      workoutJSON            `json:"workout"`
	NewExercises []training.NewExercise `json:"new_exercises"`
}

func toBatchResult(r training.BatchResult) batchResultJSON {
	out := batchResultJSON{BatchResult: r, Workout: toWorkout(r.Workout), NewExercises: r.NewExercises}
	if out.NewExercises == nil {
		out.NewExercises = []training.NewExercise{}
	}
	if out.Sets == nil {
		out.BatchResult.Sets = []training.Set{}
	}
	if out.Prescriptions == nil {
		out.BatchResult.Prescriptions = []training.PlanItem{}
	}
	return out
}

type importResultJSON struct {
	training.PlanImportResult
	NewExercises []training.NewExercise `json:"new_exercises"`
}

func toImportResult(r training.PlanImportResult) importResultJSON {
	out := importResultJSON{PlanImportResult: r, NewExercises: r.NewExercises}
	if out.NewExercises == nil {
		out.NewExercises = []training.NewExercise{}
	}
	return out
}

// setInput is one performed set in POST /workouts/{date}/sets. A metric that
// is absent or null is "not recorded".
type setInput struct {
	Exercise   string   `json:"exercise"`
	ExerciseID int64    `json:"exercise_id"`
	WeightKG   *float64 `json:"weight_kg"`
	Reps       *int     `json:"reps"`
	RPE        *float64 `json:"rpe"`
	RIR        *float64 `json:"rir"`
	SetType    string   `json:"set_type"`
	Notes      string   `json:"notes"`
	Technique  string   `json:"technique"`
	Superset   string   `json:"superset"`
}

// prescriptionInput is one planned exercise in POST /workouts/{date}/sets.
type prescriptionInput struct {
	Exercise       string   `json:"exercise"`
	TargetSets     int      `json:"target_sets"`
	RepMin         int      `json:"rep_min"`
	RepMax         int      `json:"rep_max"`
	TargetRPE      float64  `json:"target_rpe"`
	TargetWeightKG *float64 `json:"target_weight_kg"`
	TargetRIR      *float64 `json:"target_rir"`
	Superset       string   `json:"superset"`
	Notes          string   `json:"notes"`
}

type logSetsBody struct {
	Sets          []setInput          `json:"sets"`
	Prescriptions []prescriptionInput `json:"prescriptions"`
	PlanID        int64               `json:"plan_id"`
}

// ---- meta -------------------------------------------------------------------

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, map[string]any{
		"timezone":    s.svc.Location().String(),
		"today":       s.svc.Today(),
		"api_version": APIVersion,
		"media": map[string]any{
			"enabled":         s.svc.MediaEnabled(),
			"max_video_bytes": s.svc.MaxVideoBytes(),
		},
	})
	return nil
}

func (s *Server) getOpenAPI(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(openAPISpec)
	return nil
}

// ---- workouts -----------------------------------------------------------------

func (s *Server) listWorkouts(w http.ResponseWriter, r *http.Request) error {
	from, err := s.optDate(r, "from")
	if err != nil {
		return err
	}
	to, err := s.optDate(r, "to")
	if err != nil {
		return err
	}
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	list, err := s.svc.ListSessions(r.Context(), training.ListFilter{From: from, To: to, Limit: limit})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"workouts": list})
	return nil
}

func (s *Server) getWorkout(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	wk, err := s.svc.GetWorkout(r.Context(), date)
	if err != nil {
		return orNotFound(err, "no workout on %s", date)
	}
	setETag(w, wk.Revision)
	writeJSON(w, http.StatusOK, toWorkout(wk))
	return nil
}

// ensureWorkout is get-or-create for a date: 201 when this call created the
// workout, 200 when it already existed. Idempotent, so no precondition.
func (s *Server) ensureWorkout(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	wk, created, err := s.svc.EnsureWorkout(r.Context(), date)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	setETag(w, wk.Revision)
	writeJSON(w, status, toWorkout(wk))
	return nil
}

func (s *Server) logSets(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	rev, _, err := ifMatch(r)
	if err != nil {
		return err
	}
	var body logSetsBody
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}
	in := training.BatchInput{
		Date:             date,
		PlanID:           body.PlanID,
		IdempotencyKey:   strings.TrimSpace(r.Header.Get("Idempotency-Key")),
		ExpectedRevision: rev,
	}
	for _, v := range body.Sets {
		in.Sets = append(in.Sets, training.AddSetInput{
			ExerciseID: v.ExerciseID, Exercise: v.Exercise, WeightKG: v.WeightKG, Reps: v.Reps, RPE: v.RPE, RIR: v.RIR,
			SetType: v.SetType, Notes: v.Notes, Technique: v.Technique, Superset: v.Superset,
		})
	}
	for _, p := range body.Prescriptions {
		in.Prescriptions = append(in.Prescriptions, training.PlanItem{
			Exercise: p.Exercise, TargetSets: p.TargetSets, RepMin: p.RepMin, RepMax: p.RepMax, TargetRPE: p.TargetRPE,
			TargetWeightKG: p.TargetWeightKG, TargetRIR: p.TargetRIR, Superset: p.Superset, Notes: p.Notes,
		})
	}
	res, err := s.svc.LogSets(r.Context(), in)
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	setETag(w, res.Workout.Revision)
	writeJSON(w, status, toBatchResult(res))
	return nil
}

func (s *Server) followPlan(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	var body struct {
		PlanID int64 `json:"plan_id"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}
	if body.PlanID <= 0 {
		return invalidField("plan_id", "is required (a positive plan id)")
	}
	if _, err := s.svc.StartPlannedSession(r.Context(), date, body.PlanID); err != nil {
		return orNotFound(err, "plan %d does not exist", body.PlanID)
	}
	return s.renderWorkout(w, r, date, http.StatusOK)
}

func (s *Server) renderWorkout(w http.ResponseWriter, r *http.Request, date string, status int) error {
	wk, err := s.svc.GetWorkout(r.Context(), date)
	if err != nil {
		return orNotFound(err, "no workout on %s", date)
	}
	setETag(w, wk.Revision)
	writeJSON(w, status, toWorkout(wk))
	return nil
}

// prescriptionTarget resolves the workout a prescription edit targets and the
// required If-Match revision, which the engine checks inside its transaction.
func (s *Server) prescriptionTarget(r *http.Request) (training.Session, string, *int, error) {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return training.Session{}, "", nil, err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return training.Session{}, "", nil, err
	}
	wk, err := s.svc.GetWorkout(r.Context(), date)
	if err != nil {
		return wk, date, nil, orNotFound(err, "no workout on %s", date)
	}
	return wk, date, rev, nil
}

var prescriptionPatchKeys = map[string]bool{"target_sets": true, "rep_min": true, "rep_max": true, "target_rpe": true,
	"target_weight_kg": true, "target_rir": true, "skipped": true, "notes": true}

func (s *Server) patchPrescription(w http.ResponseWriter, r *http.Request) error {
	exercise := r.PathValue("exercise")
	wk, date, rev, err := s.prescriptionTarget(r)
	if err != nil {
		return err
	}
	data, err := readBody(w, r, "application/merge-patch+json")
	if err != nil {
		return err
	}
	raw, err := patchObject(data, prescriptionPatchKeys)
	if err != nil {
		return err
	}
	p := training.SessionItemPatch{ExpectedRevision: rev}
	var iss []training.Issue
	for k, v := range raw {
		if !isNull(v) {
			continue
		}
		switch k {
		case "target_weight_kg":
			p.ClearTargetWeight = true
		case "target_rir":
			p.ClearTargetRIR = true
		case "notes":
			empty := ""
			p.Notes = &empty
		default:
			iss = append(iss, training.Issue{Field: k, Message: "cannot be null"})
		}
	}
	notNull := func(k string) bool { v, ok := raw[k]; return ok && !isNull(v) }
	var ts, rmin, rmax int
	var rpe, tw, tr float64
	var sk bool
	if notNull("target_sets") && member(raw, "target_sets", &ts, &iss) {
		p.TargetSets = &ts
	}
	if notNull("rep_min") && member(raw, "rep_min", &rmin, &iss) {
		p.RepMin = &rmin
	}
	if notNull("rep_max") && member(raw, "rep_max", &rmax, &iss) {
		p.RepMax = &rmax
	}
	if notNull("target_rpe") && member(raw, "target_rpe", &rpe, &iss) {
		p.TargetRPE = &rpe
	}
	if notNull("target_weight_kg") && member(raw, "target_weight_kg", &tw, &iss) {
		p.TargetWeightKG = &tw
	}
	if notNull("target_rir") && member(raw, "target_rir", &tr, &iss) {
		p.TargetRIR = &tr
	}
	if notNull("skipped") && member(raw, "skipped", &sk, &iss) {
		p.Skipped = &sk
	}
	var note string
	if notNull("notes") && member(raw, "notes", &note, &iss) {
		p.Notes = &note
	}
	if len(iss) > 0 {
		sortIssues(iss)
		return invalidIssues(iss)
	}
	if err := s.svc.AdjustSessionItem(r.Context(), wk.ID, exercise, p); err != nil {
		return orNotFound(err, "workout %s has no prescription for %q", date, exercise)
	}
	return s.renderWorkout(w, r, date, http.StatusOK)
}

func (s *Server) deletePrescription(w http.ResponseWriter, r *http.Request) error {
	exercise := r.PathValue("exercise")
	wk, date, rev, err := s.prescriptionTarget(r)
	if err != nil {
		return err
	}
	if err := s.svc.RemoveSessionItemChecked(r.Context(), wk.ID, exercise, rev); err != nil {
		return orNotFound(err, "workout %s has no prescription for %q", date, exercise)
	}
	return s.renderWorkout(w, r, date, http.StatusOK)
}

// ---- sets ---------------------------------------------------------------------

func (s *Server) getSet(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	set, err := s.svc.GetSet(r.Context(), id)
	if err != nil {
		return orNotFound(err, "set %d does not exist", id)
	}
	setETag(w, set.Revision)
	writeJSON(w, http.StatusOK, set)
	return nil
}

var setPatchKeys = map[string]bool{"exercise": true, "exercise_id": true, "weight_kg": true, "reps": true, "rpe": true,
	"rir": true, "set_type": true, "notes": true, "technique": true}

// nullable metrics: JSON null clears them back to "not recorded".
var setClearable = map[string]bool{"weight_kg": true, "reps": true, "rpe": true, "rir": true}

func (s *Server) patchSet(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return err
	}
	data, err := readBody(w, r, "application/merge-patch+json")
	if err != nil {
		return err
	}
	raw, err := patchObject(data, setPatchKeys)
	if err != nil {
		return err
	}
	p := training.SetPatch{ExpectedRevision: rev}
	var iss []training.Issue
	for k, v := range raw {
		if !isNull(v) {
			continue
		}
		switch k {
		case "weight_kg":
			p.ClearWeight = true
		case "reps":
			p.ClearReps = true
		case "rpe":
			p.ClearRPE = true
		case "rir":
			p.ClearRIR = true
		default:
			iss = append(iss, training.Issue{Field: k, Message: "cannot be null"})
		}
	}
	notNull := func(k string) bool { v, ok := raw[k]; return ok && !isNull(v) }
	if notNull("exercise") {
		var v string
		if member(raw, "exercise", &v, &iss) {
			p.Exercise = &v
		}
	}
	if notNull("exercise_id") {
		var v int64
		if member(raw, "exercise_id", &v, &iss) {
			if v <= 0 {
				iss = append(iss, training.Issue{Field: "exercise_id", Message: "must be a positive id"})
			}
			p.ExerciseID = &v
		}
	}
	if notNull("weight_kg") {
		var v float64
		if member(raw, "weight_kg", &v, &iss) {
			p.WeightKG = &v
		}
	}
	if notNull("reps") {
		var v int
		if member(raw, "reps", &v, &iss) {
			p.Reps = &v
		}
	}
	if notNull("rpe") {
		var v float64
		if member(raw, "rpe", &v, &iss) {
			p.RPE = &v
		}
	}
	if notNull("rir") {
		var v float64
		if member(raw, "rir", &v, &iss) {
			p.RIR = &v
		}
	}
	for _, k := range []string{"set_type", "notes", "technique"} {
		if !notNull(k) {
			continue
		}
		var v string
		if member(raw, k, &v, &iss) {
			switch k {
			case "set_type":
				p.SetType = &v
			case "notes":
				p.Notes = &v
			case "technique":
				p.Technique = &v
			}
		}
	}
	if len(iss) > 0 {
		sortIssues(iss)
		return invalidIssues(iss)
	}
	set, _, err := s.svc.UpdateSet(r.Context(), id, p)
	if err != nil {
		return orNotFound(err, "set %d does not exist", id)
	}
	setETag(w, set.Revision)
	writeJSON(w, http.StatusOK, set)
	return nil
}

func (s *Server) deleteSet(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return err
	}
	sessionID, _, _, err := s.svc.DeleteSet(r.Context(), id, rev)
	if err != nil {
		return orNotFound(err, "set %d does not exist", id)
	}
	out := map[string]any{"deleted_set_id": id, "workout": nil}
	if sess, err := s.svc.GetSession(r.Context(), sessionID); err == nil {
		if wk, err := s.svc.GetWorkout(r.Context(), sess.Date); err == nil {
			out["workout"] = toWorkout(wk)
		}
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// ---- plans ----------------------------------------------------------------------

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) error {
	plans, err := s.svc.ListPlans(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
	return nil
}

func (s *Server) writeImport(w http.ResponseWriter, res training.PlanImportResult) {
	status := http.StatusOK
	if !res.DryRun {
		setETag(w, res.Plan.Revision)
		if res.Created {
			status = http.StatusCreated
			w.Header().Set("Location", Prefix+"/plans/"+strconv.FormatInt(res.Plan.ID, 10))
		}
	}
	writeJSON(w, status, toImportResult(res))
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) error {
	data, err := readBody(w, r)
	if err != nil {
		return err
	}
	res, err := s.svc.ImportPlan(r.Context(), data, training.OnConflictError, false)
	if err != nil {
		return err
	}
	s.writeImport(w, res)
	return nil
}

func (s *Server) importPlan(w http.ResponseWriter, r *http.Request) error {
	dryRun, err := queryBool(r, "dry_run")
	if err != nil {
		return err
	}
	onConflict := r.URL.Query().Get("on_conflict")
	data, err := readBody(w, r)
	if err != nil {
		return err
	}
	res, err := s.svc.ImportPlan(r.Context(), data, onConflict, dryRun)
	if err != nil {
		return err
	}
	s.writeImport(w, res)
	return nil
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.svc.GetPlan(r.Context(), id)
	if err != nil {
		return orNotFound(err, "plan %d does not exist", id)
	}
	setETag(w, p.Revision)
	writeJSON(w, http.StatusOK, p)
	return nil
}

func (s *Server) replacePlan(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return err
	}
	data, err := readBody(w, r)
	if err != nil {
		return err
	}
	p, err := s.svc.ReplacePlan(r.Context(), id, data, rev)
	if err != nil {
		return orNotFound(err, "plan %d does not exist", id)
	}
	setETag(w, p.Revision)
	writeJSON(w, http.StatusOK, p)
	return nil
}

func (s *Server) deletePlan(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return err
	}
	if err := s.svc.DeletePlanChecked(r.Context(), id, rev); err != nil {
		return orNotFound(err, "plan %d does not exist", id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) exportPlan(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.svc.GetPlan(r.Context(), id)
	if err != nil {
		return orNotFound(err, "plan %d does not exist", id)
	}
	setETag(w, p.Revision)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, slug(p.Name, id)))
	writeJSON(w, http.StatusOK, training.PlanToDocument(p))
	return nil
}

// slug makes an ASCII file name from a plan name.
func slug(name string, id int64) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 60 {
		out = strings.TrimRight(out[:60], "-")
	}
	if out == "" {
		out = "plan-" + strconv.FormatInt(id, 10)
	}
	return out
}

// ---- exercises ------------------------------------------------------------------

func (s *Server) listExercises(w http.ResponseWriter, r *http.Request) error {
	list, err := s.svc.Exercises(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"exercises": list})
	return nil
}

// createExercise adds an exercise (201) or returns the one the name or an alias
// already resolves to (200). Near-miss names are reported, never merged.
func (s *Server) createExercise(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}
	ex, created, similar, err := s.svc.CreateExercise(r.Context(), body.Name)
	if err != nil {
		return err
	}
	if similar == nil {
		similar = []string{}
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		w.Header().Set("Location", fmt.Sprintf("%s/exercises/%d", Prefix, ex.ID))
	}
	writeJSON(w, status, map[string]any{"exercise": ex, "created": created, "similar": similar})
	return nil
}

func (s *Server) exercise(r *http.Request) (training.Exercise, error) {
	ref := r.PathValue("ref")
	ex, err := s.svc.GetExercise(r.Context(), ref)
	if err != nil {
		return ex, orNotFound(err, "exercise %q does not exist", ref)
	}
	return ex, nil
}

func (s *Server) getExercise(w http.ResponseWriter, r *http.Request) error {
	ex, err := s.exercise(r)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ex)
	return nil
}

func (s *Server) renderExercise(w http.ResponseWriter, r *http.Request, id int64) error {
	ex, err := s.svc.GetExercise(r.Context(), strconv.FormatInt(id, 10))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ex)
	return nil
}

func (s *Server) renameExercise(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Name *string `json:"name"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}
	if body.Name == nil {
		return invalidField("name", "is required")
	}
	ex, err := s.exercise(r)
	if err != nil {
		return err
	}
	if err := s.svc.RenameExercise(r.Context(), ex.ID, *body.Name); err != nil {
		return err
	}
	return s.renderExercise(w, r, ex.ID)
}

func (s *Server) addAlias(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Alias *string `json:"alias"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}
	if body.Alias == nil {
		return invalidField("alias", "is required")
	}
	ex, err := s.exercise(r)
	if err != nil {
		return err
	}
	if err := s.svc.AddExerciseAlias(r.Context(), ex.ID, *body.Alias); err != nil {
		return err
	}
	return s.renderExercise(w, r, ex.ID)
}

func (s *Server) removeAlias(w http.ResponseWriter, r *http.Request) error {
	ex, err := s.exercise(r)
	if err != nil {
		return err
	}
	alias := r.PathValue("alias")
	norm := training.NormalizeExerciseName(alias)
	found := false
	for _, a := range ex.Aliases {
		if a == norm {
			found = true
		}
	}
	if !found {
		return &apiError{status: http.StatusNotFound, Code: codeNotFound, Message: fmt.Sprintf("%q is not an alias of exercise %q", alias, ex.Name)}
	}
	if err := s.svc.RemoveExerciseAlias(r.Context(), alias); err != nil {
		return orNotFound(err, "alias %q does not exist", alias)
	}
	return s.renderExercise(w, r, ex.ID)
}

func (s *Server) exerciseHistory(w http.ResponseWriter, r *http.Request) error {
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	ex, err := s.exercise(r)
	if err != nil {
		return err
	}
	h, err := s.svc.ExerciseHistory(r.Context(), ex.Name, limit)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, h)
	return nil
}

func (s *Server) exerciseStats(w http.ResponseWriter, r *http.Request) error {
	from, to, err := s.dateRange(r)
	if err != nil {
		return err
	}
	ex, err := s.exercise(r)
	if err != nil {
		return err
	}
	st, err := s.svc.ExerciseStats(r.Context(), strconv.FormatInt(ex.ID, 10), from, to)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, st)
	return nil
}

func (s *Server) statsSummary(w http.ResponseWriter, r *http.Request) error {
	from, to, err := s.dateRange(r)
	if err != nil {
		return err
	}
	sum, err := s.svc.TrainingSummary(r.Context(), from, to)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, sum)
	return nil
}

func (s *Server) dateRange(r *http.Request) (string, string, error) {
	from, err := s.optDate(r, "from")
	if err != nil {
		return "", "", err
	}
	to, err := s.optDate(r, "to")
	return from, to, err
}

// ---- schedules ------------------------------------------------------------------

var scheduleKeys = map[string]bool{"type": true, "days": true, "times_per_week": true}

// parseSchedule strictly decodes the "schedule" member: null clears, an object
// must contain only known keys. Issue fields are prefixed with "schedule.".
func parseSchedule(raw json.RawMessage) (*training.Schedule, error) {
	if isNull(raw) {
		return nil, nil
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(raw, &inner); err != nil || inner == nil {
		return nil, invalidField("schedule", "must be an object or null")
	}
	var iss, typed []training.Issue
	for k := range inner {
		if !scheduleKeys[k] {
			iss = append(iss, training.Issue{Field: "schedule." + k, Message: "unknown field"})
		}
	}
	sc := &training.Schedule{}
	member(inner, "type", &sc.Type, &typed)
	member(inner, "days", &sc.Days, &typed)
	member(inner, "times_per_week", &sc.TimesPerWeek, &typed)
	for _, i := range typed {
		iss = append(iss, training.Issue{Field: "schedule." + i.Field, Message: i.Message})
	}
	if len(iss) > 0 {
		sortIssues(iss)
		return nil, invalidIssues(iss)
	}
	return sc, nil
}

func (s *Server) putPlanSchedule(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return err
	}
	data, err := readBody(w, r)
	if err != nil {
		return err
	}
	var body map[string]json.RawMessage
	if err := decodeStrict(data, &body); err != nil {
		return err
	}
	if body == nil {
		return badRequest("request body must be a JSON object")
	}
	var iss []training.Issue
	for k := range body {
		if k != "schedule" {
			iss = append(iss, training.Issue{Field: k, Message: "unknown field"})
		}
	}
	if len(iss) > 0 {
		sortIssues(iss)
		return invalidIssues(iss)
	}
	raw, ok := body["schedule"]
	if !ok {
		return invalidField("schedule", "is required (an object, or null to clear)")
	}
	sc, err := parseSchedule(raw)
	if err != nil {
		return err
	}
	p, err := s.svc.SetPlanSchedule(r.Context(), id, sc, rev)
	if err != nil {
		return orNotFound(err, "plan %d does not exist", id)
	}
	setETag(w, p.Revision)
	writeJSON(w, http.StatusOK, p)
	return nil
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) error {
	from, to, err := s.dateRange(r)
	if err != nil {
		return err
	}
	v, err := s.svc.GetSchedule(r.Context(), from, to)
	if err != nil {
		return err
	}
	out := scheduleViewJSON{ScheduleView: v, Unscheduled: []planRef{}}
	for _, p := range v.Unscheduled {
		out.Unscheduled = append(out.Unscheduled, planRef{PlanID: p.PlanID, Name: p.Name})
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// scheduleViewJSON renders unscheduled plans without the (meaningless, empty)
// status the engine's ScheduledPlan carries.
type scheduleViewJSON struct {
	training.ScheduleView
	Unscheduled []planRef `json:"unscheduled_plans"`
}

type planRef struct {
	PlanID int64  `json:"plan_id"`
	Name   string `json:"name"`
}
