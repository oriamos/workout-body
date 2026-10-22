package training

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Body-weight tracking: one weigh-in per local date, weekly averages and the
// week-over-week change. Weeks follow the configured week start (Service.WeekStartDay).

const (
	MinWeighInKg       = 20
	MaxWeighInKg       = 400
	maxWeighInNote     = 200
	maxWeightTrendWeek = 104
)

// ErrWeightDisabled means the store does not support weigh-ins.
var ErrWeightDisabled = errors.New("weight tracking is not available on this store")

// WeighIn is the body weight recorded for one local date.
type WeighIn struct {
	ID        int64   `json:"id"`
	Date      string  `json:"date"`
	WeightKG  float64 `json:"weight_kg"`
	Note      string  `json:"note,omitempty"`
	Revision  int     `json:"revision"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// WeightWeek summarizes one week (from the configured week start). Averages use only the days
// that have a weigh-in; a week without any has null statistics.
type WeightWeek struct {
	WeekStart  string   `json:"week_start"`
	WeekEnd    string   `json:"week_end"`
	DaysLogged int      `json:"days_logged"`
	AverageKG  *float64 `json:"average_kg"`
	MinKG      *float64 `json:"min_kg"`
	MaxKG      *float64 `json:"max_kg"`
	// ChangeKG and ChangePct compare this week's average with the previous
	// week's; null when either week has no weigh-in (nothing is interpolated).
	ChangeKG  *float64 `json:"change_kg"`
	ChangePct *float64 `json:"change_pct"`
	// Trend is "up", "down" or "flat" (|change| < 0.1 kg), or "" without a change.
	Trend string `json:"trend,omitempty"`
}

// WeightTrend is the weekly view ending at a date.
type WeightTrend struct {
	From  string       `json:"from"`
	To    string       `json:"to"`
	Today string       `json:"today"`
	Weeks []WeightWeek `json:"weeks"` // oldest first
	// Entries are the weigh-ins in the range, oldest first.
	Entries []WeighIn `json:"entries"`
	// Latest is the most recent weigh-in on or before To, if any.
	Latest *WeighIn `json:"latest"`
	// OverallChangeKG compares the last week with a weigh-in against the
	// first week with one in the range; null with fewer than two such weeks.
	OverallChangeKG *float64 `json:"overall_change_kg"`
}

// WeightStore persists weigh-ins; a separate port like NutritionStore.
type WeightStore interface {
	// UpsertWeighIn stores the weigh-in for a date (created reports a new row).
	UpsertWeighIn(ctx context.Context, w WeighIn, expectedRevision *int) (WeighIn, bool, error)
	GetWeighIn(ctx context.Context, date string) (WeighIn, error)
	DeleteWeighIn(ctx context.Context, date string, expectedRevision *int) error
	ListWeighIns(ctx context.Context, from, to string) ([]WeighIn, error)
	LatestWeighIn(ctx context.Context, onOrBefore string) (*WeighIn, error)
}

func (s *Service) weights() (WeightStore, error) {
	if s.wt == nil {
		return nil, ErrWeightDisabled
	}
	return s.wt, nil
}

// LogWeighIn records the body weight for a date (today when empty). Logging a
// date that already has a weigh-in replaces it, so a retry never duplicates.
// expectedRevision, when set, must match the existing weigh-in (0 = must not
// exist yet).
func (s *Service) LogWeighIn(ctx context.Context, date string, weightKG float64, note string, expectedRevision *int) (WeighIn, bool, error) {
	ws, err := s.weights()
	if err != nil {
		return WeighIn{}, false, err
	}
	var iss []Issue
	d, dErr := s.consumptionDate(date, "date")
	if dErr != nil {
		iss = append(iss, Issue{Field: "date", Message: strings.Replace(IssuesOf(dErr)[0].Message, "food can be logged", "weight can be logged", 1)})
	}
	switch {
	case math.IsNaN(weightKG) || math.IsInf(weightKG, 0):
		iss = append(iss, Issue{Field: "weight_kg", Message: "must be a finite number"})
	case weightKG < MinWeighInKg || weightKG > MaxWeighInKg:
		iss = append(iss, Issue{Field: "weight_kg", Message: fmt.Sprintf("must be between %d and %d kg", MinWeighInKg, MaxWeighInKg)})
	}
	note = strings.TrimSpace(note)
	if tooLong(note, maxWeighInNote) {
		iss = append(iss, Issue{Field: "note", Message: fmt.Sprintf("must be at most %d characters", maxWeighInNote)})
	}
	if expectedRevision != nil && *expectedRevision < 0 {
		iss = append(iss, Issue{Field: "expected_revision", Message: "must not be negative"})
	}
	if err := issues(iss); err != nil {
		return WeighIn{}, false, err
	}
	now := s.nowStamp()
	return ws.UpsertWeighIn(ctx, WeighIn{Date: d, WeightKG: round2n(weightKG), Note: note, CreatedAt: now, UpdatedAt: now}, expectedRevision)
}

// GetWeighIn returns the weigh-in of a date (today when empty).
func (s *Service) GetWeighIn(ctx context.Context, date string) (WeighIn, error) {
	ws, err := s.weights()
	if err != nil {
		return WeighIn{}, err
	}
	d, err := s.ResolveDate(date)
	if err != nil {
		return WeighIn{}, err
	}
	return ws.GetWeighIn(ctx, d)
}

// DeleteWeighIn removes the weigh-in of a date.
func (s *Service) DeleteWeighIn(ctx context.Context, date string, expectedRevision *int) error {
	ws, err := s.weights()
	if err != nil {
		return err
	}
	d, err := s.ResolveDate(date)
	if err != nil {
		return err
	}
	return ws.DeleteWeighIn(ctx, d, expectedRevision)
}

// ListWeighIns lists weigh-ins in an inclusive date range, oldest first.
func (s *Service) ListWeighIns(ctx context.Context, from, to string) ([]WeighIn, error) {
	ws, err := s.weights()
	if err != nil {
		return nil, err
	}
	if err := checkDates(from, to); err != nil {
		return nil, err
	}
	out, err := ws.ListWeighIns(ctx, from, to)
	if err == nil && out == nil {
		out = []WeighIn{}
	}
	return out, err
}

func trendOf(change float64) string {
	switch {
	case change >= 0.1:
		return "up"
	case change <= -0.1:
		return "down"
	default:
		return "flat"
	}
}

// WeightTrendFor returns the last `weeks` weeks (configured week start) ending with the
// week containing `to` (today when empty), with averages and week-over-week
// changes. weeks defaults to 8 (max 104).
func (s *Service) WeightTrendFor(ctx context.Context, to string, weeks int) (WeightTrend, error) {
	ws, err := s.weights()
	if err != nil {
		return WeightTrend{}, err
	}
	if weeks == 0 {
		weeks = 8
	}
	if weeks < 1 || weeks > maxWeightTrendWeek {
		return WeightTrend{}, invalid("weeks", "must be between 1 and %d", maxWeightTrendWeek)
	}
	end, err := s.ResolveDate(to)
	if err != nil {
		return WeightTrend{}, invalid("to", "%q is not a valid calendar date; use YYYY-MM-DD", to)
	}
	lastStart, _ := time.Parse("2006-01-02", s.weekStart(end))
	first := lastStart.AddDate(0, 0, -7*(weeks-1))
	// One extra week before the range so the first week has a comparison.
	prior := first.AddDate(0, 0, -7)
	entries, err := ws.ListWeighIns(ctx, prior.Format("2006-01-02"), end)
	if err != nil {
		return WeightTrend{}, err
	}
	out := WeightTrend{From: first.Format("2006-01-02"), To: end, Today: s.Today(), Weeks: []WeightWeek{}, Entries: []WeighIn{}}
	byWeek := map[string][]float64{}
	for _, e := range entries {
		byWeek[s.weekStart(e.Date)] = append(byWeek[s.weekStart(e.Date)], e.WeightKG)
		if e.Date >= out.From {
			out.Entries = append(out.Entries, e)
		}
	}
	stat := func(start time.Time) WeightWeek {
		k := start.Format("2006-01-02")
		w := WeightWeek{WeekStart: k, WeekEnd: start.AddDate(0, 0, 6).Format("2006-01-02")}
		vals := byWeek[k]
		w.DaysLogged = len(vals)
		if len(vals) > 0 {
			sum, lo, hi := 0.0, vals[0], vals[0]
			for _, v := range vals {
				sum += v
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
			avg := round2n(sum / float64(len(vals)))
			w.AverageKG, w.MinKG, w.MaxKG = &avg, &lo, &hi
		}
		return w
	}
	prev := stat(prior)
	var firstAvg, lastAvg *float64
	for wk := first; !wk.After(lastStart); wk = wk.AddDate(0, 0, 7) {
		w := stat(wk)
		if w.AverageKG != nil && prev.AverageKG != nil {
			ch := round2n(*w.AverageKG - *prev.AverageKG)
			pct := math.Round(ch / *prev.AverageKG * 10000) / 100
			w.ChangeKG, w.ChangePct, w.Trend = &ch, &pct, trendOf(ch)
		}
		if w.AverageKG != nil {
			if firstAvg == nil {
				firstAvg = w.AverageKG
			}
			lastAvg = w.AverageKG
		}
		out.Weeks = append(out.Weeks, w)
		prev = w
	}
	if firstAvg != nil && lastAvg != nil && firstAvg != lastAvg {
		ch := round2n(*lastAvg - *firstAvg)
		out.OverallChangeKG = &ch
	}
	if out.Latest, err = ws.LatestWeighIn(ctx, end); err != nil {
		return out, err
	}
	return out, nil
}
