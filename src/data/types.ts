// Domain types for the prototype. Shapes are illustrative, not a production API contract.
// `null` always means "not recorded". It is never the same as 0.

export type MuscleGroup = 'chest' | 'back' | 'shoulders' | 'quads' | 'posterior' | 'arms' | 'calves' | 'other';

export interface HowTo {
  summary: string;
  steps: string[];
  cues: string[];
  mistakes: string[];
}

export interface Exercise {
  id: string;
  name: string; // English display name
  muscle: MuscleGroup;
  equipment: string;
  restSec: number;
  howTo?: HowTo;
  /** Reference demo clip. The prototype ships one placeholder clip for every exercise. */
  demoVideo?: string;
}

export type SetType = 'warmup' | 'working' | 'drop' | 'failure';

export interface SetEntry {
  id: string;
  type: SetType;
  weight: number | null; // kg
  reps: number | null;
  rir: number | null;
  rpe: number | null;
  done: boolean;
  /** The user's own recording of this set. Prototype: an in-memory object URL, gone on reload. */
  video?: { url: string; name: string } | null;
  /** Who last changed it. Drives the subtle "updated" marker, never shown as plumbing. */
  by?: 'me' | 'assistant';
}

export interface Prescription {
  sets: number;
  repMin: number;
  repMax: number;
  weight: number | null; // optional target
  rir: number | null;
  rpe: number | null;
}

export interface WorkoutExercise {
  id: string;
  exerciseId: string;
  /** Copied from the routine when the workout was created. Editing a set never edits this or the routine. */
  planned: Prescription | null;
  note: string;
  sets: SetEntry[];
}

export interface Workout {
  id: string;
  date: string; // local calendar date YYYY-MM-DD, unique per workout
  planId: string | null;
  name: string;
  startedAt: string; // HH:MM
  exercises: WorkoutExercise[];
}

export interface PlanExercise {
  id: string;
  exerciseId: string;
  sets: number;
  repMin: number;
  repMax: number;
  weight: number | null;
  rir: number | null;
  rpe: number | null;
}

export interface Plan {
  id: string;
  name: string;
  exercises: PlanExercise[];
  importedAt?: string;
}

export interface AppData {
  today: string;
  exercises: Exercise[];
  plans: Plan[];
  workouts: Workout[];
}
