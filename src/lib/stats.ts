import type { AppData, Exercise, SetEntry, Workout, WorkoutExercise } from '../data/types';
import { addDays, weekStart } from './format';

export const isWorking = (s: SetEntry) => s.type !== 'warmup';
/** A set counts toward volume/PRs only when it is done and has both numbers. */
export const counts = (s: SetEntry) => s.done && isWorking(s) && s.weight != null && s.reps != null;

export function setVolume(s: SetEntry) {
  return counts(s) ? (s.weight as number) * (s.reps as number) : 0;
}
/** Epley estimate. Only meaningful for low-rep sets, so higher reps return null. */
export function e1rm(weight: number, reps: number): number | null {
  if (reps < 1 || reps > 10) return null;
  return reps === 1 ? weight : weight * (1 + reps / 30);
}

export interface SessionStats {
  date: string;
  workout: Workout;
  we: WorkoutExercise;
  topWeight: number | null;
  bestReps: number | null; // reps at top weight
  volume: number;
  e1rm: number | null;
  doneSets: number;
}

export function exerciseStats(we: WorkoutExercise, w: Workout): SessionStats {
  const sets = we.sets.filter(counts);
  const top = sets.reduce<SetEntry | null>((b, s) => (!b || (s.weight as number) > (b.weight as number) || ((s.weight as number) === (b.weight as number) && (s.reps as number) > (b.reps as number)) ? s : b), null);
  let best: number | null = null;
  for (const s of sets) {
    const v = e1rm(s.weight as number, s.reps as number);
    if (v != null && (best == null || v > best)) best = v;
  }
  return {
    date: w.date,
    workout: w,
    we,
    topWeight: top ? top.weight : null,
    bestReps: top ? top.reps : null,
    volume: sets.reduce((a, s) => a + setVolume(s), 0),
    e1rm: best,
    doneSets: we.sets.filter((s) => s.done && isWorking(s)).length,
  };
}

/** Sessions for one exercise, oldest first. */
export function exerciseSessions(data: AppData, exerciseId: string): SessionStats[] {
  const out: SessionStats[] = [];
  for (const w of [...data.workouts].sort((a, b) => a.date.localeCompare(b.date))) {
    for (const we of w.exercises) if (we.exerciseId === exerciseId && we.sets.some((s) => s.done || s.reps != null || s.weight != null)) out.push(exerciseStats(we, w));
  }
  return out;
}

/** Most recent earlier session of an exercise, used for the "previous" column. */
export function previousSession(data: AppData, exerciseId: string, beforeDate: string): SessionStats | null {
  const s = exerciseSessions(data, exerciseId).filter((x) => x.date < beforeDate && x.doneSets + x.we.sets.filter((q) => q.done && !isWorking(q)).length > 0);
  return s.length ? s[s.length - 1] : null;
}

/** Previous value for set at `index`, matched within the same warm-up/working group. */
export function previousFor(prev: SessionStats | null, set: SetEntry, sets: SetEntry[]): SetEntry | null {
  if (!prev) return null;
  const group = (x: SetEntry) => (isWorking(x) ? 'w' : 'u');
  const idx = sets.filter((x) => group(x) === group(set)).indexOf(set);
  const cand = prev.we.sets.filter((x) => group(x) === group(set) && x.done);
  return cand[idx] ?? null;
}

export function workoutTotals(w: Workout) {
  let planned = 0;
  let done = 0;
  let volume = 0;
  let extraDone = 0;
  for (const we of w.exercises) {
    const working = we.sets.filter(isWorking);
    const doneN = working.filter((s) => s.done).length;
    if (we.planned) planned += Math.max(we.planned.sets, working.length);
    else extraDone += doneN, (planned += working.length);
    done += doneN;
    volume += we.sets.reduce((a, s) => a + setVolume(s), 0);
  }
  return { planned, done, volume, extraDone };
}

export interface PR {
  exerciseId: string;
  date: string;
  kind: 'weight' | 'e1rm' | 'volume';
  value: number;
  detail: string;
}

/** Personal bests in chronological order of when they were first reached. */
export function findPRs(data: AppData, exerciseId: string): PR[] {
  const out: PR[] = [];
  let bestW = 0;
  let bestE = 0;
  for (const s of exerciseSessions(data, exerciseId)) {
    if (s.topWeight != null && s.topWeight > bestW) {
      if (bestW > 0) out.push({ exerciseId, date: s.date, kind: 'weight', value: s.topWeight, detail: `${s.topWeight} × ${s.bestReps}` });
      bestW = s.topWeight;
    }
    if (s.e1rm != null && s.e1rm > bestE) {
      bestE = s.e1rm;
    }
  }
  return out;
}

export function weekKeys(from: string, to: string): string[] {
  const out: string[] = [];
  for (let d = weekStart(from); d <= to; d = addDays(d, 7)) out.push(d);
  return out;
}

export function exerciseById(data: AppData, id: string): Exercise {
  return data.exercises.find((e) => e.id === id) as Exercise;
}
