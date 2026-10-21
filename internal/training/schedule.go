package training

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Schedule types.
const (
	ScheduleWeekdays     = "weekdays"
	ScheduleTimesPerWeek = "times_per_week"
)

// weekdayOrder is the canonical order, Monday first (the training week).
var weekdayOrder = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var weekdayNames = map[string]string{
	"mon": "mon", "monday": "mon", "tue": "tue", "tues": "tue", "tuesday": "tue",
	"wed": "wed", "wednesday": "wed", "thu": "thu", "thur": "thu", "thurs": "thu", "thursday": "thu",
	"fri": "fri", "friday": "fri", "sat": "sat", "saturday": "sat", "sun": "sun", "sunday": "sun",
}

// Schedule says when a plan is meant to be done. A plan has at most one.
//
//	{"type":"weekdays","days":["mon","thu"]}      fixed days of the week
//	{"type":"times_per_week","times_per_week":3}  a weekly frequency, any days
//
// Days are in the configured timezone, and the week starts on Monday. A schedule
// is information about the plan: it never creates or changes a workout.
type Schedule struct {
	Type         string   `json:"type"`
	Days         []string `json:"days,omitempty"`
	TimesPerWeek int      `json:"times_per_week,omitempty"`
}

// normalizeSchedule validates s and returns its canonical form (lower-case
// three-letter days, de-duplicated, Monday first). path prefixes issue fields.
func normalizeSchedule(s *Schedule, path string) (*Schedule, []Issue) {
	if s == nil {
		return nil, nil
	}
	f := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}
	var iss []Issue
	out := &Schedule{Type: strings.ToLower(strings.TrimSpace(s.Type))}
	switch out.Type {
	case ScheduleWeekdays:
		if s.TimesPerWeek != 0 {
			iss = append(iss, Issue{Field: f("times_per_week"), Message: "is not allowed with type weekdays; list the days instead"})
		}
		seen := map[string]bool{}
		for i, d := range s.Days {
			short, ok := weekdayNames[strings.ToLower(strings.TrimSpace(d))]
			if !ok {
				iss = append(iss, Issue{Field: fmt.Sprintf("%s[%d]", f("days"), i), Message: fmt.Sprintf("%q is not a weekday; use mon, tue, wed, thu, fri, sat or sun", d)})
				continue
			}
			if seen[short] {
				iss = append(iss, Issue{Field: fmt.Sprintf("%s[%d]", f("days"), i), Message: fmt.Sprintf("%q is listed twice", short)})
				continue
			}
			seen[short] = true
		}
		if len(s.Days) == 0 {
			iss = append(iss, Issue{Field: f("days"), Message: "list at least one weekday"})
		}
		for _, d := range weekdayOrder {
			if seen[d] {
				out.Days = append(out.Days, d)
			}
		}
	case ScheduleTimesPerWeek:
		if len(s.Days) != 0 {
			iss = append(iss, Issue{Field: f("days"), Message: "is not allowed with type times_per_week; the days are up to you"})
		}
		if s.TimesPerWeek < 1 || s.TimesPerWeek > 7 {
			iss = append(iss, Issue{Field: f("times_per_week"), Message: fmt.Sprintf("must be between 1 and 7, got %d", s.TimesPerWeek)})
		}
		out.TimesPerWeek = s.TimesPerWeek
	default:
		iss = append(iss, Issue{Field: f("type"), Message: fmt.Sprintf("%q is not valid; use %q or %q", s.Type, ScheduleWeekdays, ScheduleTimesPerWeek)})
	}
	if len(iss) > 0 {
		return nil, iss
	}
	return out, nil
}

// weekdayOf returns the three-letter weekday of a YYYY-MM-DD date.
func weekdayOf(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	return weekdayOrder[(int(t.Weekday())+6)%7]
}

// ---- schedule view ---------------------------------------------------------

// WorkoutDay is a stored workout reduced to what the schedule view needs.
type WorkoutDay struct {
	ID          int64
	Date        string
	PlanID      int64
	PlanName    string
	Revision    int
	WorkingSets int
	WarmupSets  int
}

// Statuses of a plan on a scheduled day.
const (
	StatusDone     = "done"     // a workout following this plan has completed working sets
	StatusStarted  = "started"  // a workout following this plan exists but has none yet
	StatusMissed   = "missed"   // the day has passed without it
	StatusDue      = "due"      // today, not done yet
	StatusUpcoming = "upcoming" // in the future
)

// ScheduledPlan is a plan scheduled for a given date.
type ScheduledPlan struct {
	PlanID int64  `json:"plan_id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// WorkoutRef points at the workout that exists on a date, whatever plan it follows.
type WorkoutRef struct {
	ID          int64  `json:"id"`
	PlanID      *int64 `json:"plan_id"`
	PlanName    string `json:"plan_name,omitempty"`
	WorkingSets int    `json:"working_sets"`
	Revision    int    `json:"revision"`
}

// ScheduleDay is one date of the view.
type ScheduleDay struct {
	Date      string          `json:"date"`
	Weekday   string          `json:"weekday"`
	Today     bool            `json:"today"`
	Scheduled []ScheduledPlan `json:"scheduled"`
	Workout   *WorkoutRef     `json:"workout"`
}

// WeeklyTarget tracks a times_per_week plan for one Monday-start week.
type WeeklyTarget struct {
	WeekStart string   `json:"week_start"`
	PlanID    int64    `json:"plan_id"`
	Name      string   `json:"name"`
	Target    int      `json:"target"`
	Completed int      `json:"completed"`
	Remaining int      `json:"remaining"`
	Dates     []string `json:"dates"`
}

// ScheduleView answers "what is on the plan for these days?".
type ScheduleView struct {
	From     string         `json:"from"`
	To       string         `json:"to"`
	Today    string         `json:"today"`
	Timezone string         `json:"timezone"`
	Days     []ScheduleDay  `json:"days"`
	Weekly   []WeeklyTarget `json:"weekly_targets"`
	// Unscheduled lists plans that have no schedule, so a UI can still offer them.
	Unscheduled []ScheduledPlan `json:"unscheduled_plans"`
}

const maxScheduleDays = 62

// SetPlanSchedule sets (or, with a nil schedule, clears) a plan's schedule,
// optionally guarded by the plan revision the caller read.
func (s *Service) SetPlanSchedule(ctx context.Context, planID int64, sched *Schedule, expectedRevision *int) (Plan, error) {
	if planID <= 0 {
		return Plan{}, invalid("plan_id", "must be a positive id")
	}
	norm, iss := normalizeSchedule(sched, "schedule")
	if err := issues(iss); err != nil {
		return Plan{}, err
	}
	if err := s.store.SetPlanSchedule(ctx, planID, norm, expectedRevision); err != nil {
		return Plan{}, err
	}
	return s.store.GetPlan(ctx, planID)
}

// GetSchedule lays plans' schedules over a date range (default: the current
// Monday-to-Sunday week in the configured timezone). Fixed-day plans appear on
// their days with a status; weekly-frequency plans get a per-week tally.
// It reads only: it never creates a workout.
func (s *Service) GetSchedule(ctx context.Context, from, to string) (ScheduleView, error) {
	today := s.Today()
	if from == "" && to == "" {
		from = weekStart(today)
	}
	if from == "" {
		return ScheduleView{}, invalid("from", "is required when to is given")
	}
	if !validDate(from) {
		return ScheduleView{}, invalid("from", "%q is not a valid date; use YYYY-MM-DD", from)
	}
	if to == "" {
		t, _ := time.Parse("2006-01-02", from)
		to = t.AddDate(0, 0, 6).Format("2006-01-02")
	}
	if err := s.checkRange(from, to); err != nil {
		return ScheduleView{}, err
	}
	start, _ := time.Parse("2006-01-02", from)
	end, _ := time.Parse("2006-01-02", to)
	if int(end.Sub(start).Hours()/24)+1 > maxScheduleDays {
		return ScheduleView{}, invalid("to", "the range may cover at most %d days", maxScheduleDays)
	}

	plans, err := s.store.ListPlans(ctx)
	if err != nil {
		return ScheduleView{}, err
	}
	// Weekly tallies need whole weeks, so read workouts for the full weeks
	// that overlap the range.
	wkFrom := weekStart(from)
	wkTo := weekStart(to)
	wkEnd, _ := time.Parse("2006-01-02", wkTo)
	days, err := s.store.WorkoutsInRange(ctx, wkFrom, wkEnd.AddDate(0, 0, 6).Format("2006-01-02"))
	if err != nil {
		return ScheduleView{}, err
	}
	byDate := map[string]WorkoutDay{}
	for _, d := range days {
		byDate[d.Date] = d
	}

	view := ScheduleView{From: from, To: to, Today: today, Timezone: s.Location().String(),
		Days: []ScheduleDay{}, Weekly: []WeeklyTarget{}, Unscheduled: []ScheduledPlan{}}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		day := ScheduleDay{Date: date, Weekday: weekdayOf(date), Today: date == today, Scheduled: []ScheduledPlan{}}
		w, hasWorkout := byDate[date]
		if hasWorkout {
			ref := &WorkoutRef{ID: w.ID, PlanName: w.PlanName, WorkingSets: w.WorkingSets, Revision: w.Revision}
			if w.PlanID > 0 {
				id := w.PlanID
				ref.PlanID = &id
			}
			day.Workout = ref
		}
		for _, p := range plans {
			if p.Schedule == nil || p.Schedule.Type != ScheduleWeekdays {
				continue
			}
			match := false
			for _, wd := range p.Schedule.Days {
				if wd == day.Weekday {
					match = true
				}
			}
			if !match {
				continue
			}
			st := StatusUpcoming
			switch {
			case hasWorkout && w.PlanID == p.ID && w.WorkingSets > 0:
				st = StatusDone
			case hasWorkout && w.PlanID == p.ID:
				st = StatusStarted
			case date < today:
				st = StatusMissed
			case date == today:
				st = StatusDue
			}
			day.Scheduled = append(day.Scheduled, ScheduledPlan{PlanID: p.ID, Name: p.Name, Status: st})
		}
		view.Days = append(view.Days, day)
	}
	for _, p := range plans {
		if p.Schedule == nil {
			view.Unscheduled = append(view.Unscheduled, ScheduledPlan{PlanID: p.ID, Name: p.Name})
			continue
		}
		if p.Schedule.Type != ScheduleTimesPerWeek {
			continue
		}
		for wkStart, _ := time.Parse("2006-01-02", wkFrom); !wkStart.After(wkEnd); wkStart = wkStart.AddDate(0, 0, 7) {
			t := WeeklyTarget{WeekStart: wkStart.Format("2006-01-02"), PlanID: p.ID, Name: p.Name, Target: p.Schedule.TimesPerWeek, Dates: []string{}}
			for i := 0; i < 7; i++ {
				date := wkStart.AddDate(0, 0, i).Format("2006-01-02")
				if w, ok := byDate[date]; ok && w.PlanID == p.ID && w.WorkingSets > 0 {
					t.Completed++
					t.Dates = append(t.Dates, date)
				}
			}
			if t.Remaining = t.Target - t.Completed; t.Remaining < 0 {
				t.Remaining = 0
			}
			view.Weekly = append(view.Weekly, t)
		}
	}
	sort.SliceStable(view.Weekly, func(i, j int) bool {
		if view.Weekly[i].WeekStart != view.Weekly[j].WeekStart {
			return view.Weekly[i].WeekStart < view.Weekly[j].WeekStart
		}
		return view.Weekly[i].Name < view.Weekly[j].Name
	})
	return view, nil
}
