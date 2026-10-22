package apisrv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

// ---- nutrition entries --------------------------------------------------------

// nutritionEntryInput is POST /nutrition/entries. A nutrient that is absent or
// null is "not provided" and is stored as such, never as 0.
type nutritionEntryInput struct {
	Date         string   `json:"date"`
	Name         string   `json:"name"`
	CaloriesKcal *float64 `json:"calories_kcal"`
	ProteinG     *float64 `json:"protein_g"`
	CarbsG       *float64 `json:"carbs_g"`
	FatG         *float64 `json:"fat_g"`
}

type entryResult struct {
	Entry    training.NutritionEntry `json:"entry"`
	Replayed bool                    `json:"replayed"`
}

func (s *Server) writeEntryResult(w http.ResponseWriter, e training.NutritionEntry, replayed bool) {
	setETag(w, e.Revision)
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	} else {
		w.Header().Set("Location", Prefix+"/nutrition/entries/"+strconv.FormatInt(e.ID, 10))
	}
	writeJSON(w, status, entryResult{Entry: e, Replayed: replayed})
}

func (s *Server) listNutritionEntries(w http.ResponseWriter, r *http.Request) error {
	date, err := s.optDate(r, "date")
	if err != nil {
		return err
	}
	from, to, err := s.dateRange(r)
	if err != nil {
		return err
	}
	if date != "" {
		if from != "" || to != "" {
			return invalidField("date", "use either date or from/to, not both")
		}
		from, to = date, date
	}
	limit, err := queryInt(r, "limit")
	if err != nil {
		return err
	}
	list, err := s.svc.ListNutritionEntries(r.Context(), training.NutritionFilter{
		From: from, To: to, Query: r.URL.Query().Get("q"), Limit: limit,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list})
	return nil
}

func (s *Server) addNutritionEntry(w http.ResponseWriter, r *http.Request) error {
	var in nutritionEntryInput
	if err := decodeBody(w, r, &in); err != nil {
		return err
	}
	date := strings.TrimSpace(in.Date)
	if date == "today" {
		date = s.svc.Today()
	}
	e, replayed, err := s.svc.AddNutritionEntry(r.Context(), training.NutritionInput{
		Date: date, Name: in.Name,
		Nutrients:      training.Nutrients{CaloriesKcal: in.CaloriesKcal, ProteinG: in.ProteinG, CarbsG: in.CarbsG, FatG: in.FatG},
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	})
	if err != nil {
		return err
	}
	s.writeEntryResult(w, e, replayed)
	return nil
}

func (s *Server) getNutritionEntry(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	e, err := s.svc.GetNutritionEntry(r.Context(), id)
	if err != nil {
		return orNotFound(err, "nutrition entry %d does not exist", id)
	}
	setETag(w, e.Revision)
	writeJSON(w, http.StatusOK, e)
	return nil
}

var nutrientKeys = []string{"calories_kcal", "protein_g", "carbs_g", "fat_g"}

// nutrientPatch reads the four nutrient members of a merge-patch: a number
// sets the value, null asks for it to be cleared, absent leaves it alone.
func nutrientPatch(raw map[string]json.RawMessage, iss *[]training.Issue) (n training.Nutrients, clear [4]bool) {
	dst := []**float64{&n.CaloriesKcal, &n.ProteinG, &n.CarbsG, &n.FatG}
	for i, k := range nutrientKeys {
		v, ok := raw[k]
		if !ok {
			continue
		}
		if isNull(v) {
			clear[i] = true
			continue
		}
		var f float64
		if member(raw, k, &f, iss) {
			*dst[i] = &f
		}
	}
	return n, clear
}

var nutritionPatchKeys = map[string]bool{"name": true, "date": true, "calories_kcal": true, "protein_g": true, "carbs_g": true, "fat_g": true}

func (s *Server) patchNutritionEntry(w http.ResponseWriter, r *http.Request) error {
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
	raw, err := patchObject(data, nutritionPatchKeys)
	if err != nil {
		return err
	}
	var iss []training.Issue
	p := training.NutritionPatch{ExpectedRevision: rev}
	for _, k := range []string{"name", "date"} {
		v, ok := raw[k]
		if !ok {
			continue
		}
		if isNull(v) {
			iss = append(iss, training.Issue{Field: k, Message: "cannot be null"})
			continue
		}
		var str string
		if member(raw, k, &str, &iss) {
			if k == "name" {
				p.Name = &str
			} else {
				if strings.TrimSpace(str) == "today" {
					str = s.svc.Today()
				}
				p.Date = &str
			}
		}
	}
	var clear [4]bool
	p.Nutrients, clear = nutrientPatch(raw, &iss)
	p.ClearCalories, p.ClearProtein, p.ClearCarbs, p.ClearFat = clear[0], clear[1], clear[2], clear[3]
	if len(iss) > 0 {
		sortIssues(iss)
		return invalidIssues(iss)
	}
	e, err := s.svc.UpdateNutritionEntry(r.Context(), id, p)
	if err != nil {
		return orNotFound(err, "nutrition entry %d does not exist", id)
	}
	setETag(w, e.Revision)
	writeJSON(w, http.StatusOK, e)
	return nil
}

func (s *Server) deleteNutritionEntry(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return err
	}
	if err := s.svc.DeleteNutritionEntry(r.Context(), id, rev); err != nil {
		return orNotFound(err, "nutrition entry %d does not exist", id)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) copyNutritionEntry(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Date string `json:"date"`
	}
	if err := decodeOptionalBody(w, r, &body); err != nil {
		return err
	}
	date := strings.TrimSpace(body.Date)
	if date == "today" {
		date = s.svc.Today()
	}
	e, replayed, err := s.svc.CopyNutritionEntry(r.Context(), id, date, strings.TrimSpace(r.Header.Get("Idempotency-Key")))
	if err != nil {
		return orNotFound(err, "nutrition entry %d does not exist", id)
	}
	s.writeEntryResult(w, e, replayed)
	return nil
}

// decodeOptionalBody is decodeBody for endpoints whose body may be omitted
// entirely; an empty body leaves dst untouched.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return &apiError{status: http.StatusRequestEntityTooLarge, Code: codePayloadTooLarge, Message: "request body is too large"}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := requireJSON(r); err != nil {
		return err
	}
	return decodeStrict(data, dst)
}

// ---- nutrition days -------------------------------------------------------------

func (s *Server) nutritionDay(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	d, err := s.svc.NutritionDayOn(r.Context(), date)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, d)
	return nil
}

func (s *Server) nutritionDays(w http.ResponseWriter, r *http.Request) error {
	from, to, err := s.dateRange(r)
	if err != nil {
		return err
	}
	days, err := s.svc.NutritionDays(r.Context(), from, to)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days})
	return nil
}

// ---- nutrition targets ------------------------------------------------------------

func (s *Server) getNutritionTargets(w http.ResponseWriter, r *http.Request) error {
	date, err := s.optDate(r, "date")
	if err != nil {
		return err
	}
	t, err := s.svc.NutritionTargetsOn(r.Context(), date)
	if err != nil {
		return err
	}
	hist, err := s.svc.NutritionTargetHistory(r.Context())
	if err != nil {
		return err
	}
	setETag(w, t.Revision)
	writeJSON(w, http.StatusOK, map[string]any{"targets": t, "history": hist})
	return nil
}

// targetsIfMatch is requireIfMatch where "0" is valid: it is the ETag of
// "no targets set yet", the precondition for the first change.
func targetsIfMatch(r *http.Request) (*int, error) {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	bare := strings.Trim(strings.TrimPrefix(strings.Trim(v, `"`), "W/"), `"`)
	if v != "" && bare == "0" {
		zero := 0
		return &zero, nil
	}
	return requireIfMatch(r)
}

var targetKeys = map[string]bool{"calories_kcal": true, "protein_g": true, "carbs_g": true, "fat_g": true}

func (s *Server) putNutritionTargets(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	rev, err := targetsIfMatch(r)
	if err != nil {
		return err
	}
	data, err := readBody(w, r, "application/merge-patch+json")
	if err != nil {
		return err
	}
	raw, err := patchObject(data, targetKeys)
	if err != nil {
		return err
	}
	var iss []training.Issue
	p := training.TargetPatch{ExpectedRevision: rev}
	var clear [4]bool
	p.Nutrients, clear = nutrientPatch(raw, &iss)
	p.ClearCalories, p.ClearProtein, p.ClearCarbs, p.ClearFat = clear[0], clear[1], clear[2], clear[3]
	if len(iss) > 0 {
		sortIssues(iss)
		return invalidIssues(iss)
	}
	t, err := s.svc.SetNutritionTargets(r.Context(), date, p)
	if err != nil {
		return err
	}
	setETag(w, t.Revision)
	writeJSON(w, http.StatusOK, t)
	return nil
}

func (s *Server) deleteNutritionTargets(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return err
	}
	if err := s.svc.DeleteNutritionTargetChange(r.Context(), date, rev); err != nil {
		return orNotFound(err, "no target change is effective from %s", date)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
