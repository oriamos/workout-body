package training

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Nutrition tracking: one entry per portion actually consumed, plus
// effective-dated daily targets. Natural-language interpretation and any
// estimation happen in the external assistant; the engine only validates,
// stores and summarizes explicit values.

// Limits, documented in docs/nutrition.md.
const (
	MaxCaloriesKcal   = 20000
	MaxMacroGrams     = 2000
	maxNutritionName  = 200
	MaxNutritionList  = 500
	maxNutritionRange = 366
)

// ErrNutritionDisabled means the store does not support nutrition.
var ErrNutritionDisabled = errors.New("nutrition tracking is not available on this store")

// Nutrients are the four tracked values for a whole portion (or a day).
// nil means "not provided" — different from an explicit 0.
type Nutrients struct {
	CaloriesKcal *float64 `json:"calories_kcal"`
	ProteinG     *float64 `json:"protein_g"`
	CarbsG       *float64 `json:"carbs_g"`
	FatG         *float64 `json:"fat_g"`
}

// NutritionEntry is one portion consumed on a local date.
type NutritionEntry struct {
	ID   int64  `json:"id"`
	Date string `json:"date"`
	Name string `json:"name"`
	Nutrients
	// CopiedFrom is the entry this one was copied from, if any. The copy is
	// independent: editing either never changes the other.
	CopiedFrom *int64 `json:"copied_from"`
	Revision   int    `json:"revision"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// NutritionInput creates an entry. Date empty means today in the configured
// timezone.
type NutritionInput struct {
	Date string
	Name string
	Nutrients
	IdempotencyKey string
}

// NutritionPatch changes only what is given. Clear* turns a value back into
// "not provided"; a nil pointer alone means "unchanged".
type NutritionPatch struct {
	Name *string
	Date *string
	Nutrients
	ClearCalories, ClearProtein, ClearCarbs, ClearFat bool
	// ExpectedRevision, when non-nil, must match the entry's revision.
	ExpectedRevision *int
}

// NutritionFilter selects entries by inclusive local date range and an
// optional case-insensitive substring of the name.
type NutritionFilter struct {
	From, To string
	Query    string
	Limit    int
}

// NutritionTargets is the target set in effect on a date. EffectiveFrom is the
// date of the change that applies (empty when no target was ever set);
// Revision identifies that change (unique across all changes, never reused;
// 0 when none) and is the precondition for the next change.
type NutritionTargets struct {
	EffectiveFrom string `json:"effective_from,omitempty"`
	Nutrients
	Revision int `json:"revision"`
}

// TargetPatch changes targets from a date forward. Omitted values are kept
// from the targets in effect on that date; Clear* removes a target.
type TargetPatch struct {
	Nutrients
	ClearCalories, ClearProtein, ClearCarbs, ClearFat bool
	// ExpectedRevision is the revision of the targets in effect on the date
	// (as returned by a read; 0 when none). Nil skips the check.
	ExpectedRevision *int
}

// NutrientSummary compares one nutrient's consumption with its target.
type NutrientSummary struct {
	// Consumed sums the entries that provided this value.
	Consumed float64 `json:"consumed"`
	// MissingEntries counts entries without this value, so a total that
	// may be incomplete says so instead of pretending.
	MissingEntries int      `json:"entries_missing_value"`
	Target         *float64 `json:"target"`
	// Remaining is target - consumed when that is positive (else 0); Over is
	// consumed - target when positive (else 0). Both are null without a target.
	Remaining *float64 `json:"remaining"`
	Over      *float64 `json:"over"`
}

// NutritionDay is a day's entries, totals and targets, computed on read from
// the stored entries (no counters that could drift).
type NutritionDay struct {
	Date       string           `json:"date"`
	Today      bool             `json:"today"`
	EntryCount int              `json:"entry_count"`
	Calories   NutrientSummary  `json:"calories_kcal"`
	Protein    NutrientSummary  `json:"protein_g"`
	Carbs      NutrientSummary  `json:"carbs_g"`
	Fat        NutrientSummary  `json:"fat_g"`
	Targets    NutritionTargets `json:"targets"`
	Entries    []NutritionEntry `json:"entries"`
}

// NutritionStore persists nutrition data. It is a separate port so a store
// without nutrition support still satisfies Store.
type NutritionStore interface {
	// AddNutrition inserts an entry, honouring the idempotency key in the same
	// transaction. replayed is true when an earlier identical request is
	// returned instead.
	AddNutrition(ctx context.Context, e NutritionEntry, key string) (NutritionEntry, bool, error)
	GetNutrition(ctx context.Context, id int64) (NutritionEntry, error)
	ListNutrition(ctx context.Context, f NutritionFilter) ([]NutritionEntry, error)
	UpdateNutrition(ctx context.Context, id int64, p NutritionPatch, now string) (NutritionEntry, error)
	DeleteNutrition(ctx context.Context, id int64, expectedRevision *int) error
	CopyNutrition(ctx context.Context, id int64, date, now, key string) (NutritionEntry, bool, error)
	TargetsAt(ctx context.Context, date string) (NutritionTargets, error)
	TargetHistory(ctx context.Context) ([]NutritionTargets, error)
	SetTargets(ctx context.Context, from string, p TargetPatch, now string) (NutritionTargets, error)
	DeleteTargetChange(ctx context.Context, from string, expectedRevision *int) error
}

func (s *Service) nutrition() (NutritionStore, error) {
	if s.nut == nil {
		return nil, ErrNutritionDisabled
	}
	return s.nut, nil
}

func (s *Service) nowStamp() string { return s.clock().UTC().Format(time.RFC3339) }

func nutrientIssues(n Nutrients, path string) []Issue {
	f := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}
	var out []Issue
	check := func(v *float64, name string, max float64, unit string) {
		if v == nil {
			return
		}
		switch {
		case math.IsNaN(*v) || math.IsInf(*v, 0):
			out = append(out, Issue{Field: f(name), Message: "must be a finite number"})
		case *v < 0:
			out = append(out, Issue{Field: f(name), Message: "must not be negative; omit it if unknown"})
		case *v > max:
			out = append(out, Issue{Field: f(name), Message: fmt.Sprintf("must be at most %g %s for one portion", max, unit)})
		}
	}
	check(n.CaloriesKcal, "calories_kcal", MaxCaloriesKcal, "kcal")
	check(n.ProteinG, "protein_g", MaxMacroGrams, "g")
	check(n.CarbsG, "carbs_g", MaxMacroGrams, "g")
	check(n.FatG, "fat_g", MaxMacroGrams, "g")
	return out
}

func normalizeNutritionName(v string) string { return strings.Join(strings.Fields(v), " ") }

func nameIssue(name string) []Issue {
	switch {
	case name == "":
		return []Issue{{Field: "name", Message: "is required"}}
	case tooLong(name, maxNutritionName):
		return []Issue{{Field: "name", Message: fmt.Sprintf("must be at most %d characters", maxNutritionName)}}
	}
	return nil
}

// consumptionDate resolves a date for food that was eaten: today by default,
// any past date explicitly, never the future.
func (s *Service) consumptionDate(date, field string) (string, error) {
	d, err := s.ResolveDate(date)
	if err != nil {
		return "", invalid(field, "%q is not a valid calendar date; use YYYY-MM-DD", date)
	}
	if d > s.Today() {
		return "", invalid(field, "%s is in the future; food can be logged for today or earlier (today is %s)", d, s.Today())
	}
	return d, nil
}

// AddNutritionEntry stores one consumed portion. Only the name is required;
// each nutrient is optional and stored exactly as given (a missing value is
// never filled with 0, and calories are never derived from macros).
func (s *Service) AddNutritionEntry(ctx context.Context, in NutritionInput) (NutritionEntry, bool, error) {
	ns, err := s.nutrition()
	if err != nil {
		return NutritionEntry{}, false, err
	}
	in.Name = normalizeNutritionName(in.Name)
	iss := append(nameIssue(in.Name), nutrientIssues(in.Nutrients, "")...)
	date, dErr := s.consumptionDate(in.Date, "date")
	if dErr != nil {
		iss = append(iss, IssuesOf(dErr)...)
	}
	if len(in.IdempotencyKey) > MaxKeyLen {
		iss = append(iss, Issue{Field: "idempotency_key", Message: fmt.Sprintf("must be at most %d characters", MaxKeyLen)})
	}
	if err := issues(iss); err != nil {
		return NutritionEntry{}, false, err
	}
	now := s.nowStamp()
	return ns.AddNutrition(ctx, NutritionEntry{Date: date, Name: in.Name, Nutrients: in.Nutrients, CreatedAt: now, UpdatedAt: now}, in.IdempotencyKey)
}

// GetNutritionEntry returns one entry by id.
func (s *Service) GetNutritionEntry(ctx context.Context, id int64) (NutritionEntry, error) {
	ns, err := s.nutrition()
	if err != nil {
		return NutritionEntry{}, err
	}
	if id <= 0 {
		return NutritionEntry{}, invalid("entry_id", "must be a positive id")
	}
	return ns.GetNutrition(ctx, id)
}

// ListNutritionEntries lists entries newest date first (chronological within
// a day) for an inclusive date range, optionally filtered by name.
func (s *Service) ListNutritionEntries(ctx context.Context, f NutritionFilter) ([]NutritionEntry, error) {
	ns, err := s.nutrition()
	if err != nil {
		return nil, err
	}
	f.Query = strings.TrimSpace(f.Query)
	if err := checkDates(f.From, f.To); err != nil {
		return nil, err
	}
	if f.Limit < 0 || f.Limit > MaxNutritionList {
		return nil, invalid("limit", "must be between 0 and %d (0 uses the default of 100)", MaxNutritionList)
	}
	if f.Limit == 0 {
		f.Limit = 100
	}
	if tooLong(f.Query, maxNutritionName) {
		return nil, invalid("q", "must be at most %d characters", maxNutritionName)
	}
	out, err := ns.ListNutrition(ctx, f)
	if err == nil && out == nil {
		out = []NutritionEntry{}
	}
	return out, err
}

// UpdateNutritionEntry changes only the given fields of an entry.
func (s *Service) UpdateNutritionEntry(ctx context.Context, id int64, p NutritionPatch) (NutritionEntry, error) {
	ns, err := s.nutrition()
	if err != nil {
		return NutritionEntry{}, err
	}
	var iss []Issue
	if id <= 0 {
		iss = append(iss, Issue{Field: "entry_id", Message: "must be a positive id"})
	}
	empty := p.Name == nil && p.Date == nil && p.CaloriesKcal == nil && p.ProteinG == nil && p.CarbsG == nil && p.FatG == nil &&
		!p.ClearCalories && !p.ClearProtein && !p.ClearCarbs && !p.ClearFat
	if empty {
		iss = append(iss, Issue{Message: "provide at least one field to change"})
	}
	if p.Name != nil {
		v := normalizeNutritionName(*p.Name)
		p.Name = &v
		iss = append(iss, nameIssue(v)...)
	}
	if p.Date != nil {
		d, err := s.consumptionDate(*p.Date, "date")
		if err != nil {
			iss = append(iss, IssuesOf(err)...)
		} else {
			p.Date = &d
		}
	}
	iss = append(iss, nutrientIssues(p.Nutrients, "")...)
	for _, c := range []struct {
		set   bool
		clear bool
		name  string
	}{{p.CaloriesKcal != nil, p.ClearCalories, "calories_kcal"}, {p.ProteinG != nil, p.ClearProtein, "protein_g"},
		{p.CarbsG != nil, p.ClearCarbs, "carbs_g"}, {p.FatG != nil, p.ClearFat, "fat_g"}} {
		if c.set && c.clear {
			iss = append(iss, Issue{Field: c.name, Message: "cannot both set a value and clear it"})
		}
	}
	if err := issues(iss); err != nil {
		return NutritionEntry{}, err
	}
	return ns.UpdateNutrition(ctx, id, p, s.nowStamp())
}

// DeleteNutritionEntry removes an entry. Copies of it are kept.
func (s *Service) DeleteNutritionEntry(ctx context.Context, id int64, expectedRevision *int) error {
	ns, err := s.nutrition()
	if err != nil {
		return err
	}
	if id <= 0 {
		return invalid("entry_id", "must be a positive id")
	}
	return ns.DeleteNutrition(ctx, id, expectedRevision)
}

// CopyNutritionEntry creates a new, independent entry with the same name and
// values on date (today when empty).
func (s *Service) CopyNutritionEntry(ctx context.Context, id int64, date, key string) (NutritionEntry, bool, error) {
	ns, err := s.nutrition()
	if err != nil {
		return NutritionEntry{}, false, err
	}
	var iss []Issue
	if id <= 0 {
		iss = append(iss, Issue{Field: "entry_id", Message: "must be a positive id"})
	}
	d, dErr := s.consumptionDate(date, "date")
	if dErr != nil {
		iss = append(iss, IssuesOf(dErr)...)
	}
	if len(key) > MaxKeyLen {
		iss = append(iss, Issue{Field: "idempotency_key", Message: fmt.Sprintf("must be at most %d characters", MaxKeyLen)})
	}
	if err := issues(iss); err != nil {
		return NutritionEntry{}, false, err
	}
	return ns.CopyNutrition(ctx, id, d, s.nowStamp(), key)
}

// NutritionTargetsOn returns the targets in effect on date (today when empty).
func (s *Service) NutritionTargetsOn(ctx context.Context, date string) (NutritionTargets, error) {
	ns, err := s.nutrition()
	if err != nil {
		return NutritionTargets{}, err
	}
	d, err := s.ResolveDate(date)
	if err != nil {
		return NutritionTargets{}, err
	}
	return ns.TargetsAt(ctx, d)
}

// NutritionTargetHistory lists every target change, oldest first.
func (s *Service) NutritionTargetHistory(ctx context.Context) ([]NutritionTargets, error) {
	ns, err := s.nutrition()
	if err != nil {
		return nil, err
	}
	out, err := ns.TargetHistory(ctx)
	if err == nil && out == nil {
		out = []NutritionTargets{}
	}
	return out, err
}

// SetNutritionTargets changes targets from date (today when empty) forward,
// until the next change. Omitted values keep what was in effect on that date;
// earlier dates are never affected. Targets may be set for future dates.
func (s *Service) SetNutritionTargets(ctx context.Context, from string, p TargetPatch) (NutritionTargets, error) {
	ns, err := s.nutrition()
	if err != nil {
		return NutritionTargets{}, err
	}
	d, err := s.ResolveDate(from)
	if err != nil {
		return NutritionTargets{}, invalid("effective_from", "%q is not a valid calendar date; use YYYY-MM-DD", from)
	}
	iss := nutrientIssues(p.Nutrients, "")
	empty := p.CaloriesKcal == nil && p.ProteinG == nil && p.CarbsG == nil && p.FatG == nil &&
		!p.ClearCalories && !p.ClearProtein && !p.ClearCarbs && !p.ClearFat
	if empty {
		iss = append(iss, Issue{Message: "provide at least one target to set or clear"})
	}
	if p.ExpectedRevision != nil && *p.ExpectedRevision < 0 {
		iss = append(iss, Issue{Field: "expected_revision", Message: "must not be negative"})
	}
	if err := issues(iss); err != nil {
		return NutritionTargets{}, err
	}
	return ns.SetTargets(ctx, d, p, s.nowStamp())
}

// DeleteNutritionTargetChange removes the change made on a date, so the
// previous change applies again from there.
func (s *Service) DeleteNutritionTargetChange(ctx context.Context, from string, expectedRevision *int) error {
	ns, err := s.nutrition()
	if err != nil {
		return err
	}
	if !validDate(from) {
		return invalid("effective_from", "%q is not a valid calendar date; use YYYY-MM-DD", from)
	}
	return ns.DeleteTargetChange(ctx, from, expectedRevision)
}

func round2n(v float64) float64 { return math.Round(v*100) / 100 }

func summarize(consumed float64, missing int, target *float64) NutrientSummary {
	out := NutrientSummary{Consumed: round2n(consumed), MissingEntries: missing, Target: target}
	if target != nil {
		rem, over := round2n(*target-consumed), round2n(consumed-*target)
		if rem < 0 {
			rem = 0
		}
		if over < 0 {
			over = 0
		}
		out.Remaining, out.Over = &rem, &over
	}
	return out
}

// NutritionDayOn returns a day's entries, totals, targets, remaining and
// over-target amounts (today when date is empty). It never creates anything
// and has nothing to do with workouts.
func (s *Service) NutritionDayOn(ctx context.Context, date string) (NutritionDay, error) {
	days, err := s.NutritionDays(ctx, date, date)
	if err != nil {
		return NutritionDay{}, err
	}
	return days[0], nil
}

// NutritionDays returns one summary per date in an inclusive range (at most a
// year), including days without entries.
func (s *Service) NutritionDays(ctx context.Context, from, to string) ([]NutritionDay, error) {
	ns, err := s.nutrition()
	if err != nil {
		return nil, err
	}
	if from, err = s.ResolveDate(from); err != nil {
		return nil, invalid("from", "%q is not a valid calendar date; use YYYY-MM-DD", from)
	}
	if to == "" {
		to = from
	} else if to, err = s.ResolveDate(to); err != nil {
		return nil, invalid("to", "%q is not a valid calendar date; use YYYY-MM-DD", to)
	}
	if err := checkDates(from, to); err != nil {
		return nil, err
	}
	start, _ := time.Parse("2006-01-02", from)
	end, _ := time.Parse("2006-01-02", to)
	if int(end.Sub(start).Hours()/24)+1 > maxNutritionRange {
		return nil, invalid("to", "the range may cover at most %d days", maxNutritionRange)
	}
	entries, err := ns.ListNutrition(ctx, NutritionFilter{From: from, To: to, Limit: -1})
	if err != nil {
		return nil, err
	}
	byDate := map[string][]NutritionEntry{}
	for _, e := range entries {
		byDate[e.Date] = append(byDate[e.Date], e)
	}
	today := s.Today()
	var out []NutritionDay
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		day := NutritionDay{Date: date, Today: date == today, Entries: byDate[date]}
		if day.Entries == nil {
			day.Entries = []NutritionEntry{}
		}
		// ListNutrition returns newest date first but chronological within a
		// day already; keep that order.
		t, err := ns.TargetsAt(ctx, date)
		if err != nil {
			return nil, err
		}
		day.Targets = t
		var sum [4]float64
		var miss [4]int
		for _, e := range day.Entries {
			for i, v := range []*float64{e.CaloriesKcal, e.ProteinG, e.CarbsG, e.FatG} {
				if v == nil {
					miss[i]++
				} else {
					sum[i] += *v
				}
			}
		}
		day.EntryCount = len(day.Entries)
		day.Calories = summarize(sum[0], miss[0], t.CaloriesKcal)
		day.Protein = summarize(sum[1], miss[1], t.ProteinG)
		day.Carbs = summarize(sum[2], miss[2], t.CarbsG)
		day.Fat = summarize(sum[3], miss[3], t.FatG)
		out = append(out, day)
	}
	return out, nil
}
