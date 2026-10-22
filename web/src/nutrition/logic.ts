// Pure nutrition helpers: form parsing/validation, ingredient sums and scaling, patches, day totals
// (mock), display text. No DOM, no network: unit-tested by scripts/unit-nutrition.mjs.
import { NUTRIENTS, type EntryPatch, type NutrientKey, type NutrientSummary, type Nutrients, type NutritionDay, type NutritionEntry, type NutritionItem, type NutritionTargets, type TargetPatch } from './types';

export const LABEL: Record<NutrientKey, string> = { calories_kcal: 'קלוריות', protein_g: 'חלבון', carbs_g: 'פחמימות', fat_g: 'שומן' };
export const UNIT: Record<NutrientKey, string> = { calories_kcal: 'קק״ל', protein_g: 'גרם', carbs_g: 'גרם', fat_g: 'גרם' };
export const SHORT_UNIT: Record<NutrientKey, string> = { calories_kcal: 'קק״ל', protein_g: 'ג׳', carbs_g: 'ג׳', fat_g: 'ג׳' };
export const MAX: Record<NutrientKey, number> = { calories_kcal: 20000, protein_g: 2000, carbs_g: 2000, fat_g: 2000 };
export const MAX_NAME = 200;
export const MAX_ITEMS = 50;
export const MAX_AMOUNT_G = 5000;

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

type Parsed = { ok: true; value: number | null } | { ok: false; msg: string };
function parseNumber(raw: string): Parsed {
  const t = raw.trim().replace(',', '.');
  if (t === '') return { ok: true, value: null };
  if (!/^-?(\d+\.?\d*|\.\d+)$/.test(t)) return { ok: false, msg: 'צריך להיות מספר' };
  const n = Number(t);
  if (!Number.isFinite(n)) return { ok: false, msg: 'צריך להיות מספר' };
  return { ok: true, value: n };
}

/** '' -> null (not provided). Decimals with '.' or ','. Returns an error text for anything else. */
export function parseAmount(raw: string, key: NutrientKey): Parsed {
  const r = parseNumber(raw);
  if (!r.ok || r.value == null) return r;
  if (r.value < 0) return { ok: false, msg: 'לא יכול להיות שלילי. השאר ריק אם לא ידוע' };
  if (r.value > MAX[key]) return { ok: false, msg: `עד ${nf.format(MAX[key])} ${UNIT[key]}` };
  return r;
}

/** An ingredient's amount in grams: optional, more than 0, at most 5,000. */
export function parseGrams(raw: string): Parsed {
  const r = parseNumber(raw);
  if (!r.ok || r.value == null) return r;
  if (r.value <= 0) return { ok: false, msg: 'צריך להיות יותר מ־0. השאר ריק אם לא ידוע' };
  if (r.value > MAX_AMOUNT_G) return { ok: false, msg: `עד ${nf.format(MAX_AMOUNT_G)} גרם` };
  return r;
}

export const normalizeName = (s: string) => s.trim().replace(/\s+/g, ' ');
const noValues = (): Nutrients => ({ calories_kcal: null, protein_g: null, carbs_g: null, fat_g: null });
const str = (v: number | null | undefined) => (v == null ? '' : String(v));

export function validateNutrients(v: FormValues): { values: Nutrients; errors: FormErrors } {
  const values = noValues();
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
  const name = normalizeName(v.name);
  if (!name) errors.name = 'חסר שם לארוחה';
  else if (name.length > MAX_NAME) errors.name = `עד ${MAX_NAME} תווים`;
  return { name, values, errors };
}

/** Merge-patch of a simple (no ingredients) meal against the saved entry; null clears a value.
 *  When the saved entry has ingredients, `items: []` drops them (the sums stay as its own values). */
export function entryPatch(base: NutritionEntry, name: string, values: Nutrients): EntryPatch {
  const p: EntryPatch = {};
  if (name !== base.name) p.name = name;
  if (base.items?.length) p.items = [];
  for (const k of NUTRIENTS) if (values[k] !== base[k]) p[k] = values[k];
  return p;
}

/** Merge-patch of a meal made of ingredients: the whole list when it changed, never meal-level values. */
export function itemsPatch(base: NutritionEntry, name: string, items: NutritionItem[]): EntryPatch {
  const p: EntryPatch = {};
  if (name !== base.name) p.name = name;
  if (!itemsEqual(base.items ?? [], items)) p.items = items;
  return p;
}

const itemSig = (it: NutritionItem) => JSON.stringify([it.name, it.amount_g ?? null, ...NUTRIENTS.map((k) => it[k] ?? null)]);
export const itemsEqual = (a: NutritionItem[], b: NutritionItem[]) => a.length === b.length && a.every((x, i) => itemSig(x) === itemSig(b[i]));

// ---- ingredients ----

/** Same rule as the engine's SumItems: each value is the sum of the items that provide it, null when
 *  none does (never 0); `incomplete` = values only some items provide; `missing` = items lacking each. */
export function sumItems(items: Partial<Nutrients>[]): { sum: Nutrients; incomplete: NutrientKey[]; missing: Record<NutrientKey, number> } {
  const sum = noValues();
  const incomplete: NutrientKey[] = [];
  const missing = { calories_kcal: 0, protein_g: 0, carbs_g: 0, fat_g: 0 };
  for (const k of NUTRIENTS) {
    let total = 0;
    let have = 0;
    for (const it of items) {
      const v = it[k];
      if (v != null) { total += v; have++; }
    }
    missing[k] = items.length - have;
    if (have > 0) {
      sum[k] = r2(total);
      if (have < items.length) incomplete.push(k);
    }
  }
  return { sum, incomplete, missing };
}

/** "Partial" note for a meal total that some ingredients lack. */
export function partialNote(missingItems: number): string | null {
  if (missingItems <= 0) return null;
  return missingItems === 1 ? 'חלקי: חסר במרכיב אחד' : `חלקי: חסר ב־${missingItems} מרכיבים`;
}

/** Rounding for scaled values: whole kcal, grams to one decimal. */
export const roundNutrient = (v: number, k: NutrientKey) => (k === 'calories_kcal' ? Math.round(v) : Math.round(v * 10) / 10);

/** Values known for `from` grams, scaled to `to` grams. Unchanged when either amount is unknown or equal. */
export function scaleValues(base: Nutrients, from: number | null, to: number | null): { values: Nutrients; scaled: boolean } {
  if (from == null || to == null || from <= 0 || to <= 0 || from === to) return { values: { ...base }, scaled: false };
  const values = noValues();
  for (const k of NUTRIENTS) { const v = base[k]; values[k] = v == null ? null : roundNutrient((v * to) / from, k); }
  return { values, scaled: true };
}

export type ItemField = 'name' | 'amount_g' | NutrientKey;
export type ItemErrors = Partial<Record<ItemField, string>>;
/** One ingredient row of the form. `base` = the picked suggestion (values for its amount), used to rescale. */
export interface ItemRow extends Record<ItemField, string> {
  id: number;
  base: { amount_g: number | null; values: Nutrients } | null;
  scaled: boolean;
}

let rowSeq = 0;
export const emptyRow = (): ItemRow => ({ id: ++rowSeq, name: '', amount_g: '', calories_kcal: '', protein_g: '', carbs_g: '', fat_g: '', base: null, scaled: false });
export const rowFrom = (it: Partial<NutritionItem>): ItemRow => ({
  id: ++rowSeq, name: it.name ?? '', amount_g: str(it.amount_g), calories_kcal: str(it.calories_kcal), protein_g: str(it.protein_g), carbs_g: str(it.carbs_g), fat_g: str(it.fat_g), base: null, scaled: false,
});
const ROW_FIELDS: ItemField[] = ['name', 'amount_g', ...NUTRIENTS];
export const isBlankRow = (r: ItemRow) => ROW_FIELDS.every((f) => r[f].trim() === '');
const withValues = (r: ItemRow, v: Nutrients): ItemRow => ({ ...r, calories_kcal: str(v.calories_kcal), protein_g: str(v.protein_g), carbs_g: str(v.carbs_g), fat_g: str(v.fat_g) });

/** Picking a suggestion fills the row; a different amount already typed scales its values. */
export function pickSuggestion(row: ItemRow, s: NutritionItem): ItemRow {
  const base = { amount_g: s.amount_g ?? null, values: { calories_kcal: s.calories_kcal ?? null, protein_g: s.protein_g ?? null, carbs_g: s.carbs_g ?? null, fat_g: s.fat_g ?? null } };
  const typed = parseGrams(row.amount_g);
  const amount = typed.ok ? typed.value : null;
  if (amount == null) return withValues({ ...row, name: s.name, amount_g: str(base.amount_g), base, scaled: false }, base.values);
  const { values, scaled } = scaleValues(base.values, base.amount_g, amount);
  return withValues({ ...row, name: s.name, base, scaled }, values);
}

/** The amount typed on a row: rescales from the picked suggestion when there is one. */
export function setRowAmount(row: ItemRow, raw: string): ItemRow {
  if (!row.base || row.base.amount_g == null) return { ...row, amount_g: raw };
  const r = parseGrams(raw);
  if (!r.ok) return { ...row, amount_g: raw };
  const { values, scaled } = scaleValues(row.base.values, row.base.amount_g, r.value);
  return withValues({ ...row, amount_g: raw, scaled }, values);
}

/** Any other field typed by hand: the row stops following the suggestion. */
export const setRowField = (row: ItemRow, k: Exclude<ItemField, 'amount_g'>, raw: string): ItemRow => ({ ...row, [k]: raw, base: null, scaled: false });

/** Values of a row for the live totals; anything not (yet) a valid number counts as not provided. */
export function rowValues(r: ItemRow): Nutrients {
  const v = noValues();
  for (const k of NUTRIENTS) { const p = parseAmount(r[k], k); v[k] = p.ok ? p.value : null; }
  return v;
}

/** Live meal totals of the form rows (blank rows ignored). */
export function liveTotals(rows: ItemRow[]) {
  const filled = rows.filter((r) => !isBlankRow(r));
  return { count: filled.length, ...sumItems(filled.map(rowValues)) };
}

/** Validate the ingredient rows; blank rows are ignored. Errors are keyed by row id. */
export function validateItems(rows: ItemRow[]): { items: NutritionItem[]; errors: Record<number, ItemErrors>; listError: string | null } {
  const items: NutritionItem[] = [];
  const errors: Record<number, ItemErrors> = {};
  for (const r of rows) {
    if (isBlankRow(r)) continue;
    const e: ItemErrors = {};
    const name = normalizeName(r.name);
    if (!name) e.name = 'חסר שם למרכיב';
    else if (name.length > MAX_NAME) e.name = `עד ${MAX_NAME} תווים`;
    const a = parseGrams(r.amount_g);
    if (!a.ok) e.amount_g = a.msg;
    const it: NutritionItem = { name, amount_g: a.ok ? a.value : null, ...noValues() };
    for (const k of NUTRIENTS) {
      const p = parseAmount(r[k], k);
      if (p.ok) it[k] = p.value;
      else e[k] = p.msg;
    }
    if (Object.keys(e).length) errors[r.id] = e;
    items.push(it);
  }
  let listError: string | null = null;
  if (items.length === 0) listError = 'הוסף לפחות מרכיב אחד, או סמן "ארוחה פשוטה בלי מרכיבים".';
  else if (items.length > MAX_ITEMS) listError = `עד ${MAX_ITEMS} מרכיבים בארוחה`;
  return { items, errors, listError };
}

export function itemCountText(n: number): string {
  return n === 1 ? 'מרכיב אחד' : `${n} מרכיבים`;
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
      if (v != null) sum += v;
      // A meal whose value only some ingredients provide counts (its known part) and is flagged too.
      if (v == null || e.incomplete_values?.includes(k)) miss++;
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

/** Consumed for display: when every entry of the day lacks the value, the total is unknown, not 0.
 *  (entries_missing_value also counts partial meals, so look at the entries themselves.) */
export function consumedText(s: NutrientSummary, entries: Partial<Nutrients>[], key: NutrientKey): string {
  return entries.length > 0 && entries.every((e) => e[key] == null) ? '—' : fmt(s.consumed);
}

/** Fraction of the target for the progress bar (0..1), or null when there is no target. */
export function progress(s: NutrientSummary): number | null {
  if (s.target == null) return null;
  if (s.target === 0) return s.consumed > 0 ? 1 : 0;
  return Math.min(1, s.consumed / s.target);
}

export const isISODate = (s: string | undefined): s is string => !!s && /^\d{4}-\d{2}-\d{2}$/.test(s);
