// Deterministic synthetic history: ~8 weeks, upper/lower split, plus today's session.
// Everything the UI shows (totals, PRs, charts) is computed from these records.
import type { AppData, Exercise, Plan, PlanExercise, PlanSchedule, SetEntry, Workout, WorkoutExercise, Prescription } from './types';
import { addDays } from '../lib/format';
import { HOWTO } from './howto';

export const TODAY = '2026-10-21'; // fixed so the prototype is reproducible

const BASE_EXERCISES: Exercise[] = [
  { id: 'bench', name: 'Bench Press', muscle: 'chest', equipment: 'מוט', restSec: 150 },
  { id: 'squat', name: 'Back Squat', muscle: 'quads', equipment: 'מוט', restSec: 180 },
  { id: 'row', name: 'Barbell Row', muscle: 'back', equipment: 'מוט', restSec: 120 },
  { id: 'ohp', name: 'Overhead Press', muscle: 'shoulders', equipment: 'מוט', restSec: 120 },
  { id: 'rdl', name: 'Romanian Deadlift', muscle: 'posterior', equipment: 'מוט', restSec: 150 },
  { id: 'pulldown', name: 'Lat Pulldown', muscle: 'back', equipment: 'כבל', restSec: 90 },
  { id: 'incline', name: 'Incline Dumbbell Press', muscle: 'chest', equipment: 'משקולות יד', restSec: 120 },
  { id: 'cablerow', name: 'Seated Cable Row', muscle: 'back', equipment: 'כבל', restSec: 90 },
  { id: 'legpress', name: 'Leg Press', muscle: 'quads', equipment: 'מכונה', restSec: 120 },
  { id: 'legcurl', name: 'Seated Leg Curl', muscle: 'posterior', equipment: 'מכונה', restSec: 75 },
  { id: 'lateral', name: 'Lateral Raise', muscle: 'shoulders', equipment: 'משקולות יד', restSec: 60 },
  { id: 'pushdown', name: 'Triceps Pushdown', muscle: 'arms', equipment: 'כבל', restSec: 75 },
  { id: 'curl', name: 'Dumbbell Curl', muscle: 'arms', equipment: 'משקולות יד', restSec: 60 },
  { id: 'calf', name: 'Standing Calf Raise', muscle: 'calves', equipment: 'מכונה', restSec: 60 },
  { id: 'facepull', name: 'Face Pull', muscle: 'shoulders', equipment: 'כבל', restSec: 60 },
];

export const EXERCISES: Exercise[] = BASE_EXERCISES.map((e) => ({ ...e, howTo: HOWTO[e.id], demoVideo: '/demo-placeholder.mp4' }));

interface Cfg {
  ex: string;
  sets: number;
  min: number;
  max: number;
  base: number[]; // top working weight per week
  warm?: boolean;
  rpeLast?: boolean;
  target?: number | null;
}

const PLAN_CFG: Record<string, { name: string; items: Cfg[] }> = {
  'upper-a': {
    name: 'Upper A',
    items: [
      { ex: 'bench', sets: 4, min: 6, max: 8, base: [50, 52.5, 52.5, 55, 55, 57.5, 60, 60], warm: true, rpeLast: true },
      { ex: 'row', sets: 3, min: 8, max: 10, base: [50, 52.5, 52.5, 55, 55, 57.5, 57.5, 60] },
      { ex: 'ohp', sets: 3, min: 8, max: 10, base: [30, 30, 32.5, 32.5, 35, 35, 35, 37.5] },
      { ex: 'pulldown', sets: 3, min: 10, max: 12, base: [55, 57.5, 57.5, 60, 60, 62.5, 62.5, 65] },
      { ex: 'pushdown', sets: 3, min: 10, max: 12, base: [25, 27.5, 27.5, 30, 30, 30, 32.5, 32.5] },
      { ex: 'curl', sets: 2, min: 10, max: 12, base: [10, 12, 12, 12, 14, 14, 14, 14] },
    ],
  },
  'lower-a': {
    name: 'Lower A',
    items: [
      { ex: 'squat', sets: 4, min: 4, max: 6, base: [100, 105, 107.5, 110, 115, 120, 122.5, 125], warm: true, rpeLast: true },
      { ex: 'rdl', sets: 3, min: 8, max: 8, base: [80, 82.5, 85, 87.5, 90, 92.5, 95, 97.5] },
      { ex: 'legpress', sets: 3, min: 10, max: 12, base: [160, 170, 170, 180, 190, 190, 200, 200] },
      { ex: 'legcurl', sets: 3, min: 10, max: 12, base: [40, 40, 45, 45, 45, 50, 50, 50] },
      { ex: 'calf', sets: 4, min: 12, max: 15, base: [60, 60, 70, 70, 70, 80, 80, 80] },
    ],
  },
  'upper-b': {
    name: 'Upper B',
    items: [
      { ex: 'incline', sets: 4, min: 8, max: 10, base: [20, 22, 22, 24, 24, 26, 26, 28] },
      { ex: 'cablerow', sets: 3, min: 10, max: 10, base: [50, 52.5, 55, 55, 57.5, 60, 60, 62.5] },
      { ex: 'pulldown', sets: 3, min: 8, max: 10, base: [60, 60, 62.5, 65, 65, 67.5, 70, 70] },
      { ex: 'lateral', sets: 3, min: 12, max: 15, base: [8, 8, 10, 10, 10, 12, 12, 12] },
      { ex: 'facepull', sets: 3, min: 15, max: 15, base: [20, 22.5, 22.5, 25, 25, 27.5, 27.5, 30] },
      { ex: 'curl', sets: 2, min: 12, max: 12, base: [10, 10, 12, 12, 12, 14, 14, 14] },
    ],
  },
  'lower-b': {
    name: 'Lower B',
    items: [
      { ex: 'squat', sets: 3, min: 8, max: 8, base: [80, 82.5, 85, 87.5, 90, 92.5, 95, 97.5], warm: true },
      { ex: 'rdl', sets: 3, min: 6, max: 8, base: [90, 92.5, 95, 97.5, 100, 100, 105, 105] },
      { ex: 'legpress', sets: 3, min: 12, max: 12, base: [150, 160, 160, 170, 170, 180, 180, 190] },
      { ex: 'legcurl', sets: 3, min: 10, max: 12, base: [40, 45, 45, 45, 50, 50, 50, 55] },
      { ex: 'calf', sets: 3, min: 12, max: 15, base: [60, 70, 70, 70, 80, 80, 80, 90] },
    ],
  },
};

const FULL_BODY: Plan = {
  id: 'full-body',
  name: 'Full Body',
  exercises: [
    { id: 'fb1', exerciseId: 'bench', sets: 3, repMin: 6, repMax: 8, weight: null, rir: null, rpe: null },
    { id: 'fb2', exerciseId: 'squat', sets: 3, repMin: 4, repMax: 4, weight: 130, rir: 2, rpe: null },
    { id: 'fb3', exerciseId: 'row', sets: 3, repMin: 8, repMax: 10, weight: null, rir: null, rpe: null },
    { id: 'fb4', exerciseId: 'ohp', sets: 2, repMin: 8, repMax: 10, weight: null, rir: null, rpe: null },
  ],
};

function rng(seed: number) {
  return () => {
    seed |= 0;
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const round25 = (n: number) => Math.round(n / 2.5) * 2.5;
const blank = (id: string, type: SetEntry['type'] = 'working'): SetEntry => ({ id, type, weight: null, reps: null, rir: null, rpe: null, done: false });

/** The fixed weekly schedule the synthetic history follows (see buildHistory's slots), plus one frequency plan. */
const SCHEDULES: Record<string, PlanSchedule> = {
  'upper-a': { type: 'weekdays', days: ['mon'] },
  'lower-a': { type: 'weekdays', days: ['tue'] },
  'upper-b': { type: 'weekdays', days: ['thu'] },
  'lower-b': { type: 'weekdays', days: ['fri'] },
  'full-body': { type: 'times_per_week', times_per_week: 1 },
};

function buildPlans(): Plan[] {
  const plans: Plan[] = Object.entries(PLAN_CFG).map(([id, p]) => ({
    id,
    name: p.name,
    schedule: SCHEDULES[id],
    exercises: p.items.map((c, i): PlanExercise => ({
      id: `${id}-${i}`,
      exerciseId: c.ex,
      sets: c.sets,
      repMin: c.min,
      repMax: c.max,
      weight: null,
      rir: c.ex === 'pulldown' ? 2 : null,
      rpe: c.rpeLast ? 8 : null,
    })),
  }));
  plans.push({ ...FULL_BODY, schedule: SCHEDULES['full-body'] });
  return plans;
}

const MISSED = new Set(['2026-09-15', '2026-09-24', '2026-10-02']); // days the routine was skipped

function buildHistory(): Workout[] {
  const r = rng(42);
  const out: Workout[] = [];
  const monday0 = '2026-08-31';
  const slots: [number, string][] = [[0, 'upper-a'], [1, 'lower-a'], [3, 'upper-b'], [4, 'lower-b']];
  for (let w = 0; w < 8; w++) {
    for (const [off, planId] of slots) {
      const date = addDays(monday0, w * 7 + off);
      if (date >= TODAY || MISSED.has(date)) continue;
      const cfg = PLAN_CFG[planId];
      const exercises: WorkoutExercise[] = cfg.items.map((c, ei) => {
        const top = c.base[w];
        const jumped = w > 0 && top > c.base[w - 1];
        const sets: SetEntry[] = [];
        const wid = `${date}-${ei}`;
        if (c.warm) {
          sets.push({ ...blank(`${wid}-w0`, 'warmup'), weight: round25(top * 0.5), reps: 8, done: true });
          sets.push({ ...blank(`${wid}-w1`, 'warmup'), weight: round25(top * 0.75), reps: c.min <= 5 ? 3 : 5, done: true });
        }
        for (let i = 0; i < c.sets; i++) {
          let reps = c.max - Math.floor(i * 0.8 + r() * 1.2) - (jumped ? 1 : 0);
          reps = Math.max(c.min - 1, Math.min(c.max, reps));
          if (c.min === c.max) reps = c.max - (i === c.sets - 1 && r() > 0.6 ? 1 : 0);
          const s: SetEntry = { ...blank(`${wid}-${i}`), weight: top, reps, done: true };
          if (c.rpeLast && i === c.sets - 1 && r() > 0.35) s.rpe = [8, 8.5, 9, 9.5][Math.floor(r() * 4)];
          if (c.ex === 'pulldown' && i === c.sets - 1) s.rir = r() > 0.5 ? 1 : 2;
          sets.push(s);
        }
        return { id: wid, exerciseId: c.ex, planned: { sets: c.sets, repMin: c.min, repMax: c.max, weight: null, rir: null, rpe: null }, note: '', sets };
      });

      // hand-placed irregularities: an unfinished set, a skipped set, a session with no weights written down
      const find = (exId: string) => exercises.find((e) => e.exerciseId === exId);
      if (date === '2026-09-21') {
        const s = find('ohp')!.sets.filter((x) => x.type === 'working');
        const last = s[s.length - 1];
        last.reps = 5;
        last.done = false;
      }
      if (date === '2026-10-08') find('lateral')!.sets.pop();
      if (date === '2026-10-13') find('squat')!.sets.pop();
      if (date === '2026-09-17') find('lateral')!.sets.forEach((s) => (s.weight = null));
      if (date === '2026-10-19') find('bench')!.note = 'כתף ימין מרגישה טוב יותר';
      const hh = 17 + Math.floor(r() * 3);
      const mm = ['00', '15', '30', '45'][Math.floor(r() * 4)];
      out.push({ id: `w-${date}`, date, planId, name: cfg.name, startedAt: `${hh}:${mm}`, exercises });
    }
  }
  return out;
}

function prescription(p: PlanExercise): Prescription {
  return { sets: p.sets, repMin: p.repMin, repMax: p.repMax, weight: p.weight, rir: p.rir, rpe: p.rpe };
}

/** Today's session in progress, per the brief's example. */
export function buildTodayInProgress(plan: Plan, date = TODAY): Workout {
  const exercises: WorkoutExercise[] = plan.exercises.map((pe) => {
    const sets: SetEntry[] = Array.from({ length: pe.sets }, (_, i) => blank(`${date}-${pe.exerciseId}-${i}`));
    return { id: `${date}-${pe.exerciseId}`, exerciseId: pe.exerciseId, planned: prescription(pe), note: '', sets };
  });
  return { id: `w-${date}`, date, planId: plan.id, name: plan.name, startedAt: '18:04', exercises };
}

function todayInProgress(plan: Plan): Workout {
  const w = buildTodayInProgress(plan);
  const bench = w.exercises.find((e) => e.exerciseId === 'bench')!;
  bench.sets = [
    { ...blank(`${TODAY}-bench-w0`, 'warmup'), weight: 40, reps: 8, done: true },
    { ...blank(`${TODAY}-bench-0`), weight: 60, reps: 8, done: true },
    { ...blank(`${TODAY}-bench-1`), weight: 60, reps: 7, rpe: 8, done: true },
    blank(`${TODAY}-bench-2`),
  ];
  bench.note = 'עצירה קצרה בחזה בכל חזרה';
  // Performed but not part of the routine; reps came in by voice, weight still missing.
  w.exercises.push({
    id: `${TODAY}-facepull`,
    exerciseId: 'facepull',
    planned: null,
    note: '',
    sets: [{ ...blank(`${TODAY}-facepull-0`), weight: null, reps: 15, done: false, by: 'assistant' }],
  });
  return w;
}

export function buildSeed(): AppData {
  const plans = buildPlans();
  const fb = plans.find((p) => p.id === 'full-body') as Plan;
  return { today: TODAY, exercises: EXERCISES, plans, workouts: [...buildHistory(), todayInProgress(fb)] };
}
