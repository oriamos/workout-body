package apisrv

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fullfran/training-mcp/internal/training"
)

// ---- body weight (weigh-ins) -----------------------------------------------------

// weighInInput is PUT /weight/entries/{date}. weight_kg is required; the
// pointer distinguishes "absent" from an explicit (invalid) zero.
type weighInInput struct {
	WeightKG *float64 `json:"weight_kg"`
	Note     *string  `json:"note"`
}

func (s *Server) listWeighIns(w http.ResponseWriter, r *http.Request) error {
	from, to, err := s.dateRange(r)
	if err != nil {
		return err
	}
	list, err := s.svc.ListWeighIns(r.Context(), from, to)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list})
	return nil
}

func (s *Server) getWeighIn(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	wi, err := s.svc.GetWeighIn(r.Context(), date)
	if err != nil {
		return orNotFound(err, "no weigh-in on %s", date)
	}
	setETag(w, wi.Revision)
	writeJSON(w, http.StatusOK, wi)
	return nil
}

// weighInIfMatch reads the optional If-Match of PUT /weight/entries/{date}:
// absent = unconditional upsert, "0" = the date must not have a weigh-in yet,
// "*" = it must have one (any revision), otherwise the expected revision.
func (s *Server) weighInIfMatch(r *http.Request, date string) (*int, error) {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	if v == "" {
		return nil, nil
	}
	if bare := strings.Trim(strings.TrimPrefix(strings.Trim(v, `"`), "W/"), `"`); bare == "0" {
		zero := 0
		return &zero, nil
	}
	rev, _, err := ifMatch(r)
	if err != nil {
		return nil, err
	}
	if rev != nil {
		return rev, nil
	}
	// "*": there must be a current weigh-in; pin its revision.
	cur, err := s.svc.GetWeighIn(r.Context(), date)
	if errors.Is(err, training.ErrNotFound) {
		return nil, &apiError{status: http.StatusPreconditionFailed, Code: codePreconditionFailed,
			Message: "If-Match: * requires an existing weigh-in, but " + date + " has none"}
	}
	if err != nil {
		return nil, err
	}
	return &cur.Revision, nil
}

func (s *Server) putWeighIn(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	var in weighInInput
	if err := decodeBody(w, r, &in); err != nil {
		return err
	}
	if in.WeightKG == nil {
		return invalidField("weight_kg", "is required")
	}
	rev, err := s.weighInIfMatch(r, date)
	if err != nil {
		return err
	}
	note := ""
	if in.Note != nil {
		note = *in.Note
	}
	wi, created, err := s.svc.LogWeighIn(r.Context(), date, *in.WeightKG, note, rev)
	if err != nil {
		return err
	}
	setETag(w, wi.Revision)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		w.Header().Set("Location", Prefix+"/weight/entries/"+wi.Date)
	}
	writeJSON(w, status, wi)
	return nil
}

func (s *Server) deleteWeighIn(w http.ResponseWriter, r *http.Request) error {
	date, err := s.date("date", r.PathValue("date"))
	if err != nil {
		return err
	}
	rev, err := requireIfMatch(r)
	if err != nil {
		return err
	}
	if err := s.svc.DeleteWeighIn(r.Context(), date, rev); err != nil {
		return orNotFound(err, "no weigh-in on %s", date)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) weightTrend(w http.ResponseWriter, r *http.Request) error {
	to, err := s.optDate(r, "to")
	if err != nil {
		return err
	}
	weeks, err := queryInt(r, "weeks")
	if err != nil {
		return err
	}
	if r.URL.Query().Has("weeks") && weeks == 0 {
		return invalidField("weeks", "must be between 1 and 104")
	}
	t, err := s.svc.WeightTrendFor(r.Context(), to, weeks)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, t)
	return nil
}
