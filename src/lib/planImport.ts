// Prototype import format (illustrative, not a contract):
// { "name": "Push Day", "exercises": [{ "name": "Bench Press", "sets": 4, "reps": [6, 8] | 8 | {"min":6,"max":8}, "weight": 60, "rir": 2, "rpe": 8 }] }
import type { AppData, Exercise, Plan, PlanExercise } from '../data/types';

export interface ImportIssue { row: number | null; text: string }
export interface ImportRow { name: string; sets: number | null; reps: string; weight: number | null; rir: number | null; rpe: number | null; known: boolean; ok: boolean }
export interface ImportResult {
  errors: ImportIssue[];
  warnings: ImportIssue[];
  name: string;
  rows: ImportRow[];
  build?: (data: AppData) => { plan: Plan; newExercises: Exercise[] };
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const show = (v: unknown) => (typeof v === 'string' ? `"${v}"` : JSON.stringify(v));

export function validatePlanJson(text: string, data: AppData): ImportResult {
  const res: ImportResult = { errors: [], warnings: [], name: '', rows: [] };
  let json: unknown;
  try {
    json = JSON.parse(text);
  } catch (e) {
    res.errors.push({ row: null, text: `הקובץ אינו JSON תקין (${(e as Error).message}). בדוק פסיקים, מרכאות וסוגריים.` });
    return res;
  }
  if (!isObj(json)) {
    res.errors.push({ row: null, text: 'הקובץ צריך להכיל אובייקט עם השדות name ו־exercises.' });
    return res;
  }
  const name = typeof json.name === 'string' ? json.name.trim() : '';
  res.name = name;
  if (!name || name.length > 40) res.errors.push({ row: null, text: 'שם התוכנית (name) חסר או ארוך מ־40 תווים.' });
  if (data.plans.some((p) => p.name.toLowerCase() === name.toLowerCase()) && name) res.warnings.push({ row: null, text: `כבר קיימת תוכנית בשם "${name}". היבוא ייצור תוכנית נוספת באותו שם.` });
  const list = json.exercises;
  if (!Array.isArray(list) || list.length < 1 || list.length > 20) {
    res.errors.push({ row: null, text: 'exercises חייב להיות מערך של 1 עד 20 תרגילים.' });
    return res;
  }
  const library = new Map(data.exercises.map((e) => [e.name.toLowerCase(), e]));
  const parsed: { ex: PlanExercise; name: string; known: Exercise | undefined }[] = [];
  list.forEach((raw, i) => {
    const n = i + 1;
    const rowErrors: string[] = [];
    if (!isObj(raw)) {
      res.errors.push({ row: n, text: `תרגיל ${n}: צריך להיות אובייקט.` });
      res.rows.push({ name: '—', sets: null, reps: '—', weight: null, rir: null, rpe: null, known: false, ok: false });
      return;
    }
    const exName = typeof raw.name === 'string' ? raw.name.trim() : '';
    if (!exName) rowErrors.push('שם התרגיל (name) חסר.');
    const sets = raw.sets;
    if (!Number.isInteger(sets) || (sets as number) < 1 || (sets as number) > 20) rowErrors.push(`sets חייב להיות מספר שלם בין 1 ל־20 (התקבל: ${show(sets)}).`);
    let min = NaN;
    let max = NaN;
    const r = raw.reps;
    if (typeof r === 'number') min = max = r;
    else if (Array.isArray(r) && r.length === 2) [min, max] = r as number[];
    else if (isObj(r)) [min, max] = [r.min as number, r.max as number];
    if (![min, max].every((x) => Number.isInteger(x) && x >= 1 && x <= 100)) rowErrors.push(`reps חייב להיות מספר, או טווח [מינימום, מקסימום] של מספרים שלמים בין 1 ל־100 (התקבל: ${show(r)}).`);
    else if (min > max) rowErrors.push(`בטווח החזרות המינימום (${min}) גדול מהמקסימום (${max}).`);
    const weight = raw.weight === undefined ? null : raw.weight;
    if (weight !== null && (typeof weight !== 'number' || weight < 0 || weight > 500)) rowErrors.push(`weight חייב להיות בין 0 ל־500 ק״ג (התקבל: ${show(weight)}).`);
    const rir = raw.rir === undefined ? null : raw.rir;
    if (rir !== null && (!Number.isInteger(rir) || (rir as number) < 0 || (rir as number) > 5)) rowErrors.push(`rir חייב להיות מספר שלם בין 0 ל־5 (התקבל: ${show(rir)}).`);
    const rpe = raw.rpe === undefined ? null : raw.rpe;
    if (rpe !== null && (typeof rpe !== 'number' || rpe < 5 || rpe > 10 || (rpe * 2) % 1 !== 0)) rowErrors.push(`rpe חייב להיות בין 5 ל־10 בקפיצות של 0.5 (התקבל: ${show(rpe)}).`);
    const known = library.get(exName.toLowerCase());
    if (exName && !known) res.warnings.push({ row: n, text: `תרגיל ${n}: "${exName}" לא נמצא בספריית התרגילים, ויתווסף כתרגיל חדש (קבוצת שריר: אחר).` });
    rowErrors.forEach((t) => res.errors.push({ row: n, text: `תרגיל ${n}${exName ? ` (${exName})` : ''}: ${t}` }));
    res.rows.push({ name: exName || '—', sets: Number.isInteger(sets) ? (sets as number) : null, reps: Number.isNaN(min) ? '—' : min === max ? `${min}` : `${min}–${max}`, weight: typeof weight === 'number' ? weight : null, rir: typeof rir === 'number' ? rir : null, rpe: typeof rpe === 'number' ? rpe : null, known: !!known, ok: rowErrors.length === 0 });
    if (!rowErrors.length) parsed.push({ ex: { id: '', exerciseId: known?.id ?? '', sets: sets as number, repMin: min, repMax: max, weight: weight as number | null, rir: rir as number | null, rpe: rpe as number | null }, name: exName, known });
  });
  if (!res.errors.length) {
    res.build = (d) => {
      const id = `imp-${Math.random().toString(36).slice(2, 7)}`;
      const created = new Map<string, Exercise>();
      const exercises = parsed.map((p, i) => {
        let exId = p.known?.id;
        if (!exId) {
          const key = p.name.toLowerCase();
          if (!created.has(key)) created.set(key, { id: `x-${key.replace(/[^a-z0-9]+/g, '-')}-${i}`, name: p.name, muscle: 'other', equipment: '—', restSec: 90 });
          exId = (created.get(key) as Exercise).id;
        }
        return { ...p.ex, id: `${id}-${i}`, exerciseId: exId };
      });
      void d;
      return { plan: { id, name: res.name, exercises, importedAt: new Date().toISOString() }, newExercises: [...created.values()] };
    };
  }
  return res;
}

export function exportPlan(plan: Plan, data: AppData): string {
  const ex = (id: string) => data.exercises.find((e) => e.id === id)?.name ?? id;
  return JSON.stringify(
    {
      name: plan.name,
      exercises: plan.exercises.map((e) => ({
        name: ex(e.exerciseId),
        sets: e.sets,
        reps: e.repMin === e.repMax ? e.repMin : [e.repMin, e.repMax],
        ...(e.weight != null && { weight: e.weight }),
        ...(e.rir != null && { rir: e.rir }),
        ...(e.rpe != null && { rpe: e.rpe }),
      })),
    },
    null,
    2,
  );
}

export const SAMPLE_VALID = JSON.stringify({ name: 'Push Day', exercises: [{ name: 'Bench Press', sets: 4, reps: [6, 8], weight: 60, rpe: 8 }, { name: 'Incline Dumbbell Press', sets: 3, reps: [8, 10], weight: 24 }, { name: 'Overhead Press', sets: 3, reps: [8, 10] }, { name: 'Lateral Raise', sets: 3, reps: [12, 15], rir: 2 }, { name: 'Triceps Pushdown', sets: 3, reps: [10, 12] }, { name: 'Cable Crossover', sets: 2, reps: 15 }] }, null, 2);
export const SAMPLE_INVALID = JSON.stringify({ name: 'Pull Day', exercises: [{ name: 'Barbell Row', sets: 4, reps: [8, 10] }, { name: 'Lat Pulldown', sets: 'three', reps: [10, 12] }, { name: 'Seated Cable Row', sets: 3, reps: [12, 8] }, { name: 'Dumbbell Curl', sets: 3, reps: 12, weight: -5 }, { name: '', sets: 2, reps: 15 }] }, null, 2);
export const SAMPLE_BROKEN = '{ "name": "Legs", "exercises": [ { "name": "Back Squat", "sets": 3, "reps": [5, 5], }, ] ';
