// Pure nutrition helpers: form parsing/validation, patches, day totals (mock), display text.
// No DOM, no network: unit-tested by scripts/unit-nutrition.mjs.
import { NUTRIENTS, type EntryPatch, type NutrientKey, type NutrientSummary, type Nutrients, type NutritionDay, type NutritionEntry, type NutritionTargets, type TargetPatch } from './types';

export const LABEL: Record<NutrientKey, string> = { calories_kcal: 'קלוריות', protein_g: 'חלבון', carbs_g: 'פחמימות', fat_g: 'שומן' };
export const UNIT: Record<NutrientKey, string> = { calories_kcal: 'קק״ל', protein_g: 'גרם', carbs_g: 'גרם', fat_g: 'גרם' };
export const SHORT_UNIT: Record<NutrientKey, string> = { calories_kcal: 'קק״ל', protein_g: 'ג׳', carbs_g: 'ג׳', fat_g: 'ג׳' };
export const MAX: Record<NutrientKey, number> = { calories_kcal: 20000, protein_g: 2000, carbs_g: 2000, fat_g: 2000 };
export const MAX_NAME = 200;

const nf = new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 });
/** A value for display; missing is a dash, never 0. */
export const fmt = (v: number | null | undefined) => (v == null ? '—' : nf.format(v));

export type FieldKey = 'name' | NutrientKey;
export type FormValues = Record<FieldKey, string>;
export type FormErrors = Partial<Record<FieldKey, string>>;

export const emptyForm = (): FormValues => ({ name: '', calories_kcal: '', protein_g: '', carbs_g: '', fat_g: '' });
export const formFrom = (n: Partial<Nutrients> & { name?: string }): FormValues => ({
  name: n.name ?? '',
  calories_kcal: n.calories_kcal == null ? '' : String(n.calories_kcal),
  protein_g: n.protein_g == null ? '' : String(n.protein_g),
  carbs_g: n.carbs_g == null ? '' : String(n.carbs_g),
  fat_g: n.fat_g == null ? '' : String(n.fat_g),
});

/** '' -> null (not provided). Decimals with '.' or ','. Returns an error text for anything else. */
export function parseAmount(raw: string, key: NutrientKey): { ok: true; value: number | null } | { ok: false; msg: string } {
  const t = raw.trim().replace(',', '.');
  if (t === '') return { ok: true, value: null };
  if (!/^-?(\d+\.?\d*|\.\d+)$/.test(t)) return { ok: false, msg: 'צריך להיות מספר' };
  const n = Number(t);
  if (!Number.isFinite(n)) return { ok: false, msg: 'צריך להיות מספר' };
  if (n < 0) return { ok: false, msg: 'לא יכול להיות שלילי. השאר ריק אם לא ידוע' };
  if (n > MAX[key]) return { ok: false, msg: `עד ${nf.format(MAX[key])} ${UNIT[key]}` };
  return { ok: true, value: n };
}

export function validateNutrients(v: FormValues): { values: Nutrients; errors: FormErrors } {
  const values = { calories_kcal: null, protein_g: null, carbs_g: null, fat_g: null } as Nutrients;
  const errors: FormErrors = {};
  for (const k of NUTRIENTS) {
    const r = parseAmount(v[k], k);
    if (r.ok) values[k] = r.value;
    else errors[k] = r.msg;
  }
  return { values, errors };
}

export function validateEntryForm(v: FormValues): { name: string; values: Nutrients; errors: FormErrors } {
  const { values, errors } = validateNutrients(v);
  const name = v.name.trim().replace(/\s+/g, ' ');
  if (!name) errors.name = 'חסר שם לארוחה';
  else if (name.length > MAX_NAME) errors.name = `עד ${MAX_NAME} תווים`;
  return { name, values, errors };
}

/** Merge-patch of what changed against the saved entry; null clears a value. */
export function entryPatch(base: NutritionEntry, name: string, values: Nutrients): EntryPatch {
  const p: EntryPatch = {};
  if (name !== base.name) p.name = name;
  for (const k of NUTRIENTS) if (values[k] !== base[k]) p[k] = values[k];
  return p;
}

/** Merge-patch for targets from a date forward: only the changed values; null removes a target. */
export function targetPatch(current: NutritionTargets, values: Nutrients): TargetPatch {
  const p: TargetPatch = {};
  for (const k of NUTRIENTS) if (values[k] !== (current[k] ?? null)) p[k] = values[k];
  return p;
}

const r2 = (v: number) => Math.round(v * 100) / 100;
export function summarize(consumed: number, missing: number, target: number | null): NutrientSummary {
  if (target == null) return { consumed: r2(consumed), entries_missing_value: missing, target: null, remaining: null, over: null };
  return { consumed: r2(consumed), entries_missing_value: missing, target, remaining: Math.max(0, r2(target - consumed)), over: Math.max(0, r2(consumed - target)) };
}

/** Same computation as the engine (internal/training/nutrition.go NutritionDays); used by the mock. */
export function buildDay(date: string, today: string, entries: NutritionEntry[], targets: NutritionTargets): NutritionDay {
  const day = { date, today: date === today, entry_count: entries.length, targets, entries } as NutritionDay;
  for (const k of NUTRIENTS) {
    let sum = 0;
    let miss = 0;
    for (const e of entries) {
      const v = e[k];
      if (v == null) miss++;
      else sum += v;
    }
    day[k] = summarize(sum, miss, targets[k] ?? null);
  }
  return day;
}

/** The targets in effect on `date` from a change history (changes apply until the next one). */
export function targetsAt(history: NutritionTargets[], date: string): NutritionTargets {
  const cur = [...history].filter((t) => (t.effective_from ?? '') <= date).sort((a, b) => (a.effective_from ?? '').localeCompare(b.effective_from ?? '')).pop();
  return cur ?? { calories_kcal: null, protein_g: null, carbs_g: null, fat_g: null, revision: 0 };
}

export function missingNote(n: number): string | null {
  if (n <= 0) return null;
  return n === 1 ? 'חסר בארוחה אחת' : `חסר ב־${n} ארוחות`;
}

/** The status line under a nutrient: remaining, over, or no target. */
export function statusText(s: NutrientSummary, key: NutrientKey): { text: string; tone: 'muted' | 'ok' | 'over' } {
  if (s.target == null) return { text: 'אין יעד', tone: 'muted' };
  if ((s.over ?? 0) > 0) return { text: `חריגה של ${fmt(s.over)} ${SHORT_UNIT[key]}`, tone: 'over' };
  if ((s.remaining ?? 0) === 0) return { text: 'היעד הושג', tone: 'ok' };
  return { text: `נותרו ${fmt(s.remaining)} ${SHORT_UNIT[key]}`, tone: 'muted' };
}

/** Consumed for display: when every entry of the day lacks the value, the total is unknown, not 0. */
export function consumedText(s: NutrientSummary, entryCount: number): string {
  return entryCount > 0 && s.entries_missing_value === entryCount ? '—' : fmt(s.consumed);
}

/** Fraction of the target for the progress bar (0..1), or null when there is no target. */
export function progress(s: NutrientSummary): number | null {
  if (s.target == null) return null;
  if (s.target === 0) return s.consumed > 0 ? 1 : 0;
  return Math.min(1, s.consumed / s.target);
}

export const isISODate = (s: string | undefined): s is string => !!s && /^\d{4}-\d{2}-\d{2}$/.test(s);
