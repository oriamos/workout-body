// Pure mapping between the workout engine's JSON API (/api/v1) and the UI's domain types.
// No network, no DOM: unit-tested by scripts/unit-api-mapping.mjs.
import type { Exercise, HowTo, MuscleGroup, Plan, PlanExercise, Prescription, SetEntry, SetType, Workout, WorkoutExercise } from './types';
import { EXERCISES } from './seed';

// ---------- engine shapes (subset of internal/apisrv/openapi.yaml) ----------
export interface SConfig { timezone: string; today: string; api_version: string; media: { enabled: boolean; max_video_bytes: number } }
export interface SExercise { id: number; name: string; aliases: string[]; muscle_group?: string; set_count: number }
export interface SSet {
  id: number; session_id: number; position: number; exercise_id: number; exercise: string;
  weight_kg: number | null; reps: number | null; rpe: number | null; rir: number | null;
  set_type: string; revision: number; set_number?: number; notes?: string;
}
export interface SProgress {
  exercise: string; muscle_group?: string; superset?: string; notes?: string; target_sets: number; done_sets: number;
  warmup_sets?: number; rep_min?: number; rep_max?: number; target_rpe?: number; target_weight_kg?: number; target_rir?: number; skipped?: boolean;
}
export interface SWorkout { id: number; date: string; sets: SSet[]; progress: SProgress[]; plan_id: number | null; plan_name: string | null; revision: number }
export interface SWorkoutSummary { id: number; date: string; set_count: number; plan_name?: string }
export interface SPlanItem { position: number; exercise: string; target_sets: number; rep_min?: number; rep_max?: number; target_rpe?: number; target_weight_kg?: number; target_rir?: number; superset?: string; notes?: string }
export interface SPlan { id: number; name: string; notes?: string; schedule: unknown | null; items?: SPlanItem[]; revision: number; total_sets: number }
export interface SMedia { id: number; filename: string; content_type: string; size_bytes: number }
export interface SGuide { exercise_id: number; exercise: string; instructions: string; videos: SMedia[] }
export interface SBatchResult { workout: SWorkout; created: boolean; sets: SSet[]; new_exercises: { id: number; name: string; similar?: string[] }[]; replayed: boolean }

export const PLACEHOLDER_DEMO = '/demo-placeholder.mp4';
export const mediaUrl = (id: number) => `/api/v1/media/${id}`;

// ---------- names ----------
export const normName = (s: string) => s.trim().toLowerCase().replace(/\s+/g, ' ');
/** "bench press" -> "Bench Press". Only for display; the engine's canonical name stays lower case. */
export const titleCase = (s: string) => s.replace(/(^|[\s\-(/])(\p{Ll})/gu, (_, p: string, c: string) => p + c.toUpperCase());

// ---------- muscle groups ----------
// The engine uses the upstream (Spanish) taxonomy, see internal/training/model.go MuscleGroups.
const MUSCLE_MAP: Record<string, MuscleGroup> = {
  pecho: 'chest',
  espalda: 'back', trapecio: 'back',
  'hombro anterior': 'shoulders', 'hombro lateral': 'shoulders', 'hombro posterior': 'shoulders',
  cuadriceps: 'quads',
  isquios: 'posterior', gluteo: 'posterior', aductor: 'posterior',
  biceps: 'arms', triceps: 'arms', antebrazo: 'arms',
  gemelo: 'calves',
};
export const mapMuscle = (g: string | undefined | null): MuscleGroup => (g ? MUSCLE_MAP[normName(g)] ?? 'other' : 'other');

// Built-in knowledge about the prototype's exercises (Hebrew how-to, equipment, rest), matched by name.
interface Known { muscle: MuscleGroup; equipment: string; restSec: number; howTo?: HowTo }
const KNOWN = new Map<string, Known>(EXERCISES.map((e) => [normName(e.name), { muscle: e.muscle, equipment: e.equipment, restSec: e.restSec, howTo: e.howTo }]));
export const knownExercise = (name: string, aliases: string[] = []): Known | undefined => [name, ...aliases].map((n) => KNOWN.get(normName(n))).find(Boolean);
/** Prototype seed ids ('bench') to their English names, used by scripts and tests. */
export const SEED_NAME: Record<string, string> = Object.fromEntries(EXERCISES.map((e) => [e.id, e.name]));

export interface ExerciseMeta { muscle?: MuscleGroup; equipment?: string; restSec?: number }

export function toClientExercise(se: SExercise, meta: ExerciseMeta | undefined): Exercise {
  const known = knownExercise(se.name, se.aliases);
  const muscle = meta?.muscle ?? (se.muscle_group ? mapMuscle(se.muscle_group) : known?.muscle ?? 'other');
  return {
    id: String(se.id),
    name: titleCase(se.name),
    muscle,
    equipment: meta?.equipment ?? known?.equipment ?? '',
    restSec: meta?.restSec ?? known?.restSec ?? 120,
    howTo: known?.howTo,
    demoVideo: PLACEHOLDER_DEMO,
  };
}

/** Written guidance from the engine is free text: show it as the summary, never invent steps. */
export function howToFromGuide(instructions: string): HowTo | undefined {
  const t = instructions.trim();
  return t ? { summary: t, steps: [], cues: [], mistakes: [] } : undefined;
}

// ---------- sets ----------
export const toSetType = (t: string): SetType => (t === 'warmup' || t === 'drop' || t === 'failure' ? t : 'working'); // 'backoff' shows as working
export const toSetEntry = (s: SSet): SetEntry => ({ id: String(s.id), type: toSetType(s.set_type), weight: s.weight_kg, reps: s.reps, rir: s.rir, rpe: s.rpe, done: true });

/** Body for one performed set (POST /workouts/{date}/sets). Unknown metrics are omitted, not sent as 0. */
export function setInput(ex: { exercise_id: number } | { exercise: string }, s: Pick<SetEntry, 'weight' | 'reps' | 'rir' | 'rpe' | 'type'>) {
  return {
    ...ex,
    ...(s.weight != null && { weight_kg: s.weight }),
    ...(s.reps != null && { reps: s.reps }),
    ...(s.rir != null && { rir: s.rir }),
    ...(s.rpe != null && { rpe: s.rpe }),
    ...(s.type !== 'working' && { set_type: s.type }),
  };
}

/** Merge-patch for a persisted set from a UI patch. `null` clears. `type` only when it really changes. */
export function setPatchBody(patch: Partial<SetEntry>, origType: string | undefined): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if ('weight' in patch) out.weight_kg = patch.weight ?? null;
  if ('reps' in patch) out.reps = patch.reps ?? null;
  if ('rir' in patch) out.rir = patch.rir ?? null;
  if ('rpe' in patch) out.rpe = patch.rpe ?? null;
  if (patch.type && patch.type !== toSetType(origType ?? 'working')) out.set_type = patch.type;
  return out;
}

// ---------- prescriptions and plans ----------
export function toPrescription(p: { target_sets: number; rep_min?: number; rep_max?: number; target_weight_kg?: number; target_rir?: number; target_rpe?: number; skipped?: boolean }): Prescription | null {
  if (!p.target_sets) return null;
  return {
    sets: p.target_sets,
    repMin: p.rep_min ?? p.rep_max ?? 0,
    repMax: p.rep_max ?? p.rep_min ?? 0,
    weight: p.target_weight_kg ?? null,
    rir: p.target_rir ?? null,
    rpe: p.target_rpe ?? null,
  };
}

/** Engine targets omit 0 and reject explicit null/0 in plan documents, so leave them out. */
export function targetFields(p: { repMin: number; repMax: number; weight: number | null; rir: number | null; rpe: number | null }) {
  return {
    ...(p.repMin > 0 && { rep_min: p.repMin }),
    ...(p.repMax > 0 && { rep_max: p.repMax }),
    ...(p.weight != null && { target_weight_kg: p.weight }),
    ...(p.rir != null && { target_rir: p.rir }),
    ...(p.rpe != null && p.rpe > 0 && { target_rpe: p.rpe }),
  };
}

export function prescriptionInput(exercise: string, p: Prescription, note?: string) {
  return { exercise, target_sets: p.sets, ...targetFields(p), ...(note ? { notes: note } : {}) };
}

export function planDocument(plan: Plan, nameOf: (exerciseId: string) => string, keep: { schedule?: unknown; notes?: string; items?: SPlanItem[] }) {
  const prevItem = (name: string) => keep.items?.find((i) => normName(i.exercise) === normName(name));
  return {
    format: 'workout-plan',
    version: 1,
    name: plan.name,
    ...(keep.notes ? { notes: keep.notes } : {}),
    ...(keep.schedule ? { schedule: keep.schedule } : {}),
    items: plan.exercises.map((pe) => {
      const exercise = nameOf(pe.exerciseId);
      const prev = prevItem(exercise);
      return {
        exercise,
        sets: pe.sets,
        ...targetFields(pe),
        ...(prev?.superset ? { superset: prev.superset } : {}),
        ...(prev?.notes ? { notes: prev.notes } : {}),
      };
    }),
  };
}

export function toClientPlan(sp: SPlan, exerciseIdOf: (name: string) => string): Plan {
  return {
    id: String(sp.id),
    name: sp.name,
    exercises: (sp.items ?? []).map((it, i): PlanExercise => {
      const p = toPrescription(it) ?? { sets: it.target_sets, repMin: 0, repMax: 0, weight: null, rir: null, rpe: null };
      return { id: `${sp.id}:${i}`, exerciseId: exerciseIdOf(it.exercise), sets: p.sets, repMin: p.repMin, repMax: p.repMax, weight: p.weight, rir: p.rir, rpe: p.rpe };
    }),
  };
}

// ---------- workouts ----------
export interface WorkoutLocal {
  /** Exercise names added in the UI but with no set yet (UI-only until the first set is completed). */
  pending?: string[];
  /** Notes for exercises without a prescription, by canonical exercise name (UI-only). */
  notes?: Record<string, string>;
  startedAt?: string;
}

export const draftId = (weId: string, n: number) => `draft:${weId}:${n}`;
export const blankDraft = (id: string): SetEntry => ({ id, type: 'working', weight: null, reps: null, rir: null, rpe: null, done: false });

/**
 * Engine workout -> UI workout. One WorkoutExercise per exercise in progress order (prescriptions
 * first, then performed-only work), every engine set as a done SetEntry, then local draft rows for
 * the planned sets still to do. `exerciseIdOf` maps an engine exercise (id, name) to the UI id.
 */
export function toClientWorkout(sw: SWorkout, exerciseIdOf: (name: string, serverId?: number) => string, local: WorkoutLocal = {}): Workout {
  const wid = String(sw.id);
  const sets = [...sw.sets].sort((a, b) => a.position - b.position);
  const order: { exId: string; name: string; item?: SProgress }[] = [];
  const seen = new Set<string>();
  const push = (exId: string, name: string, item?: SProgress) => {
    if (seen.has(exId)) return;
    seen.add(exId);
    order.push({ exId, name, item });
  };
  for (const item of sw.progress) {
    const s = sets.find((x) => normName(x.exercise) === normName(item.exercise));
    push(exerciseIdOf(item.exercise, s?.exercise_id), item.exercise, item);
  }
  for (const s of sets) push(exerciseIdOf(s.exercise, s.exercise_id), s.exercise);
  for (const name of local.pending ?? []) push(exerciseIdOf(name), name);

  const exercises: WorkoutExercise[] = order.map(({ exId, name, item }) => {
    const weId = `${wid}:${exId}`;
    const mine = sets.filter((s) => exerciseIdOf(s.exercise, s.exercise_id) === exId).map(toSetEntry);
    const planned = item ? toPrescription(item) : null;
    const rows = [...mine];
    const working = mine.filter((s) => s.type !== 'warmup').length;
    if (planned && !item?.skipped) for (let n = working; n < planned.sets; n++) rows.push(blankDraft(draftId(weId, n + 1)));
    if (!rows.length) rows.push(blankDraft(draftId(weId, 1))); // pending exercise: one empty row, like the prototype
    const note = (planned ? item?.notes : local.notes?.[normName(name)] ?? item?.notes) ?? '';
    return { id: weId, exerciseId: exId, planned, note, sets: rows };
  });
  return { id: wid, date: sw.date, planId: sw.plan_id != null ? String(sw.plan_id) : null, name: sw.plan_name || 'אימון חופשי', startedAt: local.startedAt ?? '', exercises };
}

// ---------- change detection ----------
export interface Snapshot { revision: number; sets: Map<number, SSet>; progress: SProgress[] }
export const snapshotOf = (sw: SWorkout): Snapshot => ({ revision: sw.revision, sets: new Map(sw.sets.map((s) => [s.id, s])), progress: sw.progress });
export const emptySnapshot = (): Snapshot => ({ revision: -1, sets: new Map(), progress: [] });

export interface WorkoutDiff {
  added: SSet[];
  changed: { set: SSet; patch: Partial<SetEntry>; exerciseChanged: boolean }[];
  removed: SSet[];
  newPrescriptions: SProgress[];
  notes: { exercise: string; note: string }[];
}

/** What changed on the server between two reads of the same workout. */
export function diffWorkout(old: Snapshot, sw: SWorkout): WorkoutDiff {
  const d: WorkoutDiff = { added: [], changed: [], removed: [], newPrescriptions: [], notes: [] };
  const ids = new Set<number>();
  for (const s of [...sw.sets].sort((a, b) => a.position - b.position)) {
    ids.add(s.id);
    const prev = old.sets.get(s.id);
    if (!prev) { d.added.push(s); continue; }
    const patch: Partial<SetEntry> = {};
    if (prev.weight_kg !== s.weight_kg) patch.weight = s.weight_kg;
    if (prev.reps !== s.reps) patch.reps = s.reps;
    if (prev.rpe !== s.rpe) patch.rpe = s.rpe;
    if (prev.rir !== s.rir) patch.rir = s.rir;
    if (toSetType(prev.set_type) !== toSetType(s.set_type)) patch.type = toSetType(s.set_type);
    const exerciseChanged = prev.exercise_id !== s.exercise_id;
    if (Object.keys(patch).length || exerciseChanged) d.changed.push({ set: s, patch, exerciseChanged });
  }
  for (const [id, s] of old.sets) if (!ids.has(id)) d.removed.push(s);
  for (const item of sw.progress) {
    const prev = old.progress.find((p) => normName(p.exercise) === normName(item.exercise));
    if (item.target_sets > 0 && !(prev && prev.target_sets > 0)) d.newPrescriptions.push(item);
    if (prev && (prev.notes ?? '') !== (item.notes ?? '')) d.notes.push({ exercise: item.exercise, note: item.notes ?? '' });
  }
  return d;
}

/** Working-set number of `s` among the non-warm-up sets of its exercise. */
export function workingNumber(sw: SWorkout, s: SSet): number {
  return sw.sets.filter((x) => x.exercise_id === s.exercise_id && x.set_type !== 'warmup' && x.position <= s.position).length;
}

const kgText = (n: number | null) => (n == null ? '—' : String(Math.round(n * 100) / 100));
const iso = (v: string | number) => `⁦${v}⁩`; // keep names/numbers LTR inside Hebrew sentences
/** Same wording as the prototype's simulated assistant: "Bench Press · סט 3 · 60 × 6". */
export function setSummary(name: string, n: number, s: Pick<SSet, 'weight_kg' | 'reps' | 'set_type'>): string {
  const label = s.set_type === 'warmup' ? 'סט חימום' : `סט ${n}`;
  return `${iso(name)} · ${label} · ${iso(`${kgText(s.weight_kg)} × ${s.reps ?? '—'}`)}`;
}
export function changeSummary(name: string, n: number, patch: Partial<SetEntry>): string {
  const parts: string[] = [];
  if ('weight' in patch) parts.push(`משקל ${iso(kgText(patch.weight ?? null))}`);
  if ('reps' in patch) parts.push(`חזרות ${iso(patch.reps ?? '—')}`);
  if ('rpe' in patch) parts.push(`RPE ${iso(patch.rpe ?? '—')}`);
  if ('rir' in patch) parts.push(`RIR ${iso(patch.rir ?? '—')}`);
  if ('type' in patch) parts.push('סוג הסט');
  return `${iso(name)} · סט ${n} · עודכן: ${parts.join(', ')}`;
}
export { iso };
