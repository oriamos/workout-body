import type { AppData, Exercise, Plan, SetEntry, Workout, WorkoutExercise } from './types';

/** Every change to the record, whoever makes it (this UI or the external assistant), is one of these. */
export type Mutation =
  | { t: 'createWorkout'; workout: Workout }
  | { t: 'addExercise'; workoutId: string; we: WorkoutExercise }
  | { t: 'removeExercise'; weId: string }
  | { t: 'setNote'; weId: string; note: string }
  | { t: 'addSet'; weId: string; set: SetEntry; index?: number }
  | { t: 'updateSet'; setId: string; patch: Partial<Omit<SetEntry, 'id'>> }
  | { t: 'removeSet'; setId: string }
  | { t: 'savePlan'; plan: Plan }
  | { t: 'deletePlan'; planId: string }
  | { t: 'addLibraryExercise'; exercise: Exercise };

export interface ExternalUpdate {
  id: string;
  at: number;
  mutation: Mutation;
  /** Human sentence for the quiet confirmation, e.g. "Bench Press · סט 3 · 60 × 6". */
  summary: string;
}

function mapWorkouts(data: AppData, fn: (w: Workout) => Workout): AppData {
  return { ...data, workouts: data.workouts.map(fn) };
}
function mapWe(data: AppData, weId: string, fn: (we: WorkoutExercise) => WorkoutExercise): AppData {
  return mapWorkouts(data, (w) => (w.exercises.some((e) => e.id === weId) ? { ...w, exercises: w.exercises.map((e) => (e.id === weId ? fn(e) : e)) } : w));
}

export function applyMutation(data: AppData, m: Mutation): AppData {
  switch (m.t) {
    case 'createWorkout':
      if (data.workouts.some((w) => w.date === m.workout.date)) return data; // one workout per date
      return { ...data, workouts: [...data.workouts, m.workout] };
    case 'addExercise':
      return mapWorkouts(data, (w) => (w.id === m.workoutId ? { ...w, exercises: [...w.exercises, m.we] } : w));
    case 'removeExercise':
      return mapWorkouts(data, (w) => ({ ...w, exercises: w.exercises.filter((e) => e.id !== m.weId) }));
    case 'setNote':
      return mapWe(data, m.weId, (e) => ({ ...e, note: m.note }));
    case 'addSet':
      return mapWe(data, m.weId, (e) => {
        const sets = e.sets.slice();
        sets.splice(m.index ?? sets.length, 0, m.set);
        return { ...e, sets };
      });
    case 'updateSet':
      return mapWorkouts(data, (w) => ({
        ...w,
        exercises: w.exercises.map((e) => (e.sets.some((s) => s.id === m.setId) ? { ...e, sets: e.sets.map((s) => (s.id === m.setId ? { ...s, ...m.patch } : s)) } : e)),
      }));
    case 'removeSet':
      return mapWorkouts(data, (w) => ({ ...w, exercises: w.exercises.map((e) => (e.sets.some((s) => s.id === m.setId) ? { ...e, sets: e.sets.filter((s) => s.id !== m.setId) } : e)) }));
    case 'savePlan':
      return { ...data, plans: data.plans.some((p) => p.id === m.plan.id) ? data.plans.map((p) => (p.id === m.plan.id ? m.plan : p)) : [...data.plans, m.plan] };
    case 'deletePlan':
      return { ...data, plans: data.plans.filter((p) => p.id !== m.planId) };
    case 'addLibraryExercise':
      return { ...data, exercises: [...data.exercises, m.exercise] };
  }
}

export function findSet(data: AppData, setId: string) {
  for (const w of data.workouts)
    for (const we of w.exercises) {
      const set = we.sets.find((s) => s.id === setId);
      if (set) return { workout: w, we, set, index: we.sets.indexOf(set) };
    }
  return null;
}
