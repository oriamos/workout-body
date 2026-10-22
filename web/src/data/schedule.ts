// The week view's data: which plans fall on which day, what was done, weekly tallies.
// Same shape and rules as the engine's GET /api/v1/schedule (docs/engine.md#plan-schedules), so the
// API repository can map the server's answer into it and the mock can compute it from its own data.
// Pure: no network, no DOM. Unit-tested by scripts/unit-schedule.mjs.
// The first day of the week is configurable in the engine (WORKOUT_WEEK_START, default Sunday); the UI
// learns it from the server's default week (days[0].weekday) and the mock uses Sunday.
import type { AppData, Plan, PlanSchedule, Weekday, Workout } from './types';
import { addDays, parseDate } from '../lib/format';

export type ScheduleStatus = 'done' | 'started' | 'missed' | 'due' | 'upcoming';

export interface ScheduledPlan { planId: string; name: string; status: ScheduleStatus }
export interface DayWorkoutRef { id: string; planId: string | null; planName: string | null; workingSets: number }
export interface ScheduleDay { date: string; weekday: Weekday; today: boolean; scheduled: ScheduledPlan[]; workout: DayWorkoutRef | null }
export interface WeeklyTarget { weekStart: string; planId: string; name: string; target: number; completed: number; remaining: number; dates: string[] }
export interface WeekSchedule {
  from: string;
  to: string;
  today: string;
  days: ScheduleDay[];
  weeklyTargets: WeeklyTarget[];
  unscheduled: { planId: string; name: string }[];
}

/** Sunday = 0, like Date#getDay. */
const BY_JS_DAY: Weekday[] = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'];
/** Monday first, as the engine stores and lists them in a plan's schedule. */
export const WEEKDAYS: Weekday[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
export const DEFAULT_WEEK_START: Weekday = 'sun';
/** The seven weekdays in display order for a week that starts on `first`. */
export const weekOrder = (first: Weekday = DEFAULT_WEEK_START): Weekday[] => {
  const i = BY_JS_DAY.indexOf(first);
  return [...BY_JS_DAY.slice(i), ...BY_JS_DAY.slice(0, i)];
};
export const WEEKDAY_HE: Record<Weekday, string> = { mon: 'שני', tue: 'שלישי', wed: 'רביעי', thu: 'חמישי', fri: 'שישי', sat: 'שבת', sun: 'ראשון' };
export const WEEKDAY_HE_SHORT: Record<Weekday, string> = { mon: 'ב׳', tue: 'ג׳', wed: 'ד׳', thu: 'ה׳', fri: 'ו׳', sat: 'ש׳', sun: 'א׳' };

export const weekdayOf = (date: string): Weekday => BY_JS_DAY[parseDate(date).getDay()];
/** First day of the week (starting on `first`) that contains `date`. */
export function startOfWeek(date: string, first: Weekday = DEFAULT_WEEK_START): string {
  return addDays(date, -((parseDate(date).getDay() - BY_JS_DAY.indexOf(first) + 7) % 7));
}

/** Completed non-warm-up sets: what makes a workout count as done. */
export const workingSetsDone = (w: Workout) => w.exercises.reduce((a, we) => a + we.sets.filter((s) => s.done && s.type !== 'warmup').length, 0);

export function scheduleText(s: PlanSchedule | null | undefined): string | null {
  if (!s) return null;
  if (s.type === 'weekdays') return weekOrder().filter((d) => s.days.includes(d)).map((d) => WEEKDAY_HE_SHORT[d]).join(' ');
  return s.times_per_week === 1 ? 'פעם בשבוע' : `${s.times_per_week} פעמים בשבוע`;
}

/** The schedule view over [from, to] computed from in-memory data (the mock's source of truth). */
export function computeSchedule(data: Pick<AppData, 'today' | 'plans' | 'workouts'>, from: string, to: string, first: Weekday = DEFAULT_WEEK_START): WeekSchedule {
  const byDate = new Map(data.workouts.map((w) => [w.date, w]));
  const planName = (id: string | null) => (id ? data.plans.find((p) => p.id === id)?.name ?? null : null);
  const fixed = data.plans.filter((p): p is Plan & { schedule: Extract<PlanSchedule, { type: 'weekdays' }> } => p.schedule?.type === 'weekdays');
  const freq = data.plans.filter((p): p is Plan & { schedule: Extract<PlanSchedule, { type: 'times_per_week' }> } => p.schedule?.type === 'times_per_week');

  const days: ScheduleDay[] = [];
  for (let d = from; d <= to; d = addDays(d, 1)) {
    const wd = weekdayOf(d);
    const w = byDate.get(d) ?? null;
    const working = w ? workingSetsDone(w) : 0;
    const scheduled = fixed.filter((p) => p.schedule.days.includes(wd)).map((p): ScheduledPlan => {
      let status: ScheduleStatus;
      if (w && w.planId === p.id) status = working > 0 ? 'done' : 'started';
      else status = d < data.today ? 'missed' : d === data.today ? 'due' : 'upcoming';
      return { planId: p.id, name: p.name, status };
    });
    days.push({
      date: d,
      weekday: wd,
      today: d === data.today,
      scheduled,
      workout: w ? { id: w.id, planId: w.planId, planName: w.planId ? planName(w.planId) ?? w.name : null, workingSets: working } : null,
    });
  }

  const weeklyTargets: WeeklyTarget[] = [];
  for (let ws = startOfWeek(from, first); ws <= to; ws = addDays(ws, 7)) {
    for (const p of freq) {
      const dates: string[] = [];
      for (let i = 0; i < 7; i++) {
        const d = addDays(ws, i);
        const w = byDate.get(d);
        if (w && w.planId === p.id && workingSetsDone(w) > 0) dates.push(d);
      }
      const target = p.schedule.times_per_week;
      weeklyTargets.push({ weekStart: ws, planId: p.id, name: p.name, target, completed: dates.length, remaining: Math.max(0, target - dates.length), dates });
    }
  }

  return {
    from,
    to,
    today: data.today,
    days,
    weeklyTargets,
    unscheduled: data.plans.filter((p) => !p.schedule).map((p) => ({ planId: p.id, name: p.name })),
  };
}

/** Week totals for the header: days trained vs. sessions the schedule asks for. */
export function weekTotals(view: WeekSchedule) {
  const done = view.days.filter((d) => d.workout && d.workout.workingSets > 0).length;
  const slots = view.days.reduce((a, d) => a + d.scheduled.length, 0);
  const freq = view.weeklyTargets.reduce((a, t) => a + t.target, 0);
  return { done, planned: slots + freq, hasSchedule: slots > 0 || view.weeklyTargets.length > 0 };
}
