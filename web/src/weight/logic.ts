// Pure body-weight helpers: input parsing/validation, the weekly trend (same computation as the
// engine; used by the mock), headline and display text, chart series.
// No DOM, no network: unit-tested by scripts/unit-weight.mjs.
import type { WeighIn, WeightTrend, WeightTrendDir, WeightWeek } from './types';

export const MIN_KG = 20;
export const MAX_KG = 400;
export const MAX_NOTE = 200;
export const DEFAULT_WEEKS = 8;
export const RANGES = [4, 8, 12, 26] as const;
export type RangeWeeks = (typeof RANGES)[number];

// ---- dates (ISO yyyy-mm-dd, calendar arithmetic in UTC so it never depends on the device zone) ----
const toUTC = (d: string) => new Date(`${d}T00:00:00Z`);
const iso = (t: Date) => t.toISOString().slice(0, 10);
export const isISODate = (s: string | undefined | null): s is string => !!s && /^\d{4}-\d{2}-\d{2}$/.test(s) && iso(toUTC(s)) === s;
export function plusDays(d: string, n: number): string {
  const t = toUTC(d);
  t.setUTCDate(t.getUTCDate() + n);
  return iso(t);
}
/** Weeks run Sunday→Saturday by default (the user's week; the engine's default week start).
 *  The real UI never recomputes weeks: it shows week_start/week_end from the API. This is for the mock. */
export const WEEK_START_DAY = 0; // 0 = Sunday … 6 = Saturday
/** First day of the week containing `d`. */
export const weekStartOf = (d: string, startDay = WEEK_START_DAY) => plusDays(d, -((toUTC(d).getUTCDay() - startDay + 7) % 7));

// ---- numbers ----
/** Go's math.Round: half away from zero. */
const roundHalfAway = (v: number) => Math.sign(v) * Math.round(Math.abs(v));
export const round2 = (v: number) => roundHalfAway(v * 100) / 100;

const nf1 = new Intl.NumberFormat('en-US', { minimumFractionDigits: 1, maximumFractionDigits: 2 });
/** A weight for display: always one decimal (81.0), two when recorded (81.25). Missing is a dash. */
export const fmtKg = (v: number | null | undefined) => (v == null ? '—' : nf1.format(v));
/** Signed change with a real minus sign: "+0.4", "−0.35", "0.0". */
export function fmtSigned(v: number | null | undefined, digits = 2): string {
  if (v == null) return '—';
  const f = new Intl.NumberFormat('en-US', { minimumFractionDigits: 1, maximumFractionDigits: digits }).format(Math.abs(v));
  if (v > 0) return `+${f}`;
  if (v < 0) return `−${f}`;
  return f;
}
export const fmtPct = (v: number | null | undefined) => (v == null ? '—' : `${fmtSigned(v)}%`);

// ---- form ----
export type ParseResult = { ok: true; value: number } | { ok: false; msg: string };

/** Body weight in kg: decimals with '.' or ',' (e.g. "81,4"); 20–400. Rounded to 2 decimals like the engine. */
export function parseWeight(raw: string): ParseResult {
  const t = raw.trim().replace(/\s+/g, '').replace(',', '.');
  if (t === '') return { ok: false, msg: 'חסר משקל' };
  if (!/^-?(\d+\.?\d*|\.\d+)$/.test(t)) return { ok: false, msg: 'צריך להיות מספר, למשל 81.4' };
  const n = Number(t);
  if (!Number.isFinite(n)) return { ok: false, msg: 'צריך להיות מספר, למשל 81.4' };
  if (n < MIN_KG || n > MAX_KG) return { ok: false, msg: `משקל בין ${MIN_KG} ל־${MAX_KG} ק״ג` };
  return { ok: true, value: round2(n) };
}

export function validateNote(raw: string): { ok: true; value: string } | { ok: false; msg: string } {
  const v = raw.trim();
  if ([...v].length > MAX_NOTE) return { ok: false, msg: `הערה עד ${MAX_NOTE} תווים` };
  return { ok: true, value: v };
}

export function validateDate(d: string, today: string): { ok: true } | { ok: false; msg: string } {
  if (!isISODate(d)) return { ok: false, msg: 'בחר תאריך' };
  if (d > today) return { ok: false, msg: 'אי אפשר לרשום משקל לתאריך עתידי' };
  return { ok: true };
}

export type WeighForm = { date: string; weight: string; note: string };
export type WeighErrors = Partial<Record<keyof WeighForm, string>>;
export function validateForm(f: WeighForm, today: string): { values: { date: string; weight_kg: number; note: string } | null; errors: WeighErrors } {
  const errors: WeighErrors = {};
  const d = validateDate(f.date, today);
  if (!d.ok) errors.date = d.msg;
  const w = parseWeight(f.weight);
  if (!w.ok) errors.weight = w.msg;
  const n = validateNote(f.note);
  if (!n.ok) errors.note = n.msg;
  if (!d.ok || !w.ok || !n.ok) return { values: null, errors };
  return { values: { date: f.date, weight_kg: w.value, note: n.value }, errors };
}
/** The text to put in the weight field for a saved value. */
export const weightInput = (v: number) => String(v);

// ---- weekly trend (mirror of internal/training/bodyweight.go WeightTrendFor) ----
export function trendOf(change: number): Exclude<WeightTrendDir, ''> {
  if (change >= 0.1) return 'up';
  if (change <= -0.1) return 'down';
  return 'flat';
}

export function computeTrend(all: WeighIn[], to: string, weeks: number, today: string, startDay = WEEK_START_DAY): WeightTrend {
  const lastStart = weekStartOf(to, startDay);
  const first = plusDays(lastStart, -7 * (weeks - 1));
  const prior = plusDays(first, -7);
  const inRange = [...all].filter((e) => e.date >= prior && e.date <= to).sort((a, b) => a.date.localeCompare(b.date));
  const byWeek = new Map<string, number[]>();
  for (const e of inRange) {
    const k = weekStartOf(e.date, startDay);
    byWeek.set(k, [...(byWeek.get(k) ?? []), e.weight_kg]);
  }
  const stat = (start: string): WeightWeek => {
    const vals = byWeek.get(start) ?? [];
    const w: WeightWeek = { week_start: start, week_end: plusDays(start, 6), days_logged: vals.length, average_kg: null, min_kg: null, max_kg: null, change_kg: null, change_pct: null };
    if (vals.length) {
      w.average_kg = round2(vals.reduce((a, b) => a + b, 0) / vals.length);
      w.min_kg = Math.min(...vals);
      w.max_kg = Math.max(...vals);
    }
    return w;
  };
  let prev = stat(prior);
  const out: WeightWeek[] = [];
  let firstAvg: number | null = null;
  let lastAvg: number | null = null;
  for (let wk = first; wk <= lastStart; wk = plusDays(wk, 7)) {
    const w = stat(wk);
    if (w.average_kg != null && prev.average_kg != null) {
      const ch = round2(w.average_kg - prev.average_kg);
      w.change_kg = ch;
      w.change_pct = roundHalfAway((ch / prev.average_kg) * 10000) / 100;
      w.trend = trendOf(ch);
    }
    if (w.average_kg != null) {
      if (firstAvg == null) firstAvg = w.average_kg;
      lastAvg = w.average_kg;
    }
    out.push(w);
    prev = w;
  }
  // The engine compares pointers: two different weeks are needed, even with equal averages.
  const withAvg = out.filter((w) => w.average_kg != null);
  const overall = withAvg.length >= 2 && firstAvg != null && lastAvg != null ? round2(lastAvg - firstAvg) : null;
  const latest = [...all].filter((e) => e.date <= to).sort((a, b) => a.date.localeCompare(b.date)).pop() ?? null;
  return { from: first, to, today, weeks: out, entries: inRange.filter((e) => e.date >= first), latest, overall_change_kg: overall };
}

// ---- display ----
export const TREND_LABEL: Record<Exclude<WeightTrendDir, ''>, string> = { up: 'עלייה', down: 'ירידה', flat: 'ללא שינוי' };
export const NO_COMPARISON = 'אין מספיק נתונים להשוואה';

export function weighCount(n: number): string {
  if (n === 0) return 'אין שקילות';
  return n === 1 ? 'שקילה אחת' : `${n} שקילות`;
}

export interface Headline {
  week: WeightWeek | null;
  /** Average of the current week, or null when it has no weigh-in. */
  average: number | null;
  days: number;
  countText: string;
  comparison: { kg: number; pct: number | null; dir: Exclude<WeightTrendDir, ''>; label: string } | null;
  comparisonText: string;
}

/** The current week (the last one, containing `to`) and its change vs the previous week, as the engine reports it. */
export function headline(t: WeightTrend | null): Headline {
  const week = t?.weeks.length ? t.weeks[t.weeks.length - 1] : null;
  const average = week?.average_kg ?? null;
  const days = week?.days_logged ?? 0;
  let comparison: Headline['comparison'] = null;
  if (week && week.change_kg != null) {
    const dir = week.trend || trendOf(week.change_kg);
    comparison = { kg: week.change_kg, pct: week.change_pct, dir, label: TREND_LABEL[dir] };
  }
  const comparisonText = comparison ? `${comparisonPhrase(comparison)} לעומת השבוע שעבר` : NO_COMPARISON;
  return { week, average, days, countText: weighCount(days), comparison, comparisonText };
}

const nfAbs = new Intl.NumberFormat('en-US', { minimumFractionDigits: 1, maximumFractionDigits: 2 });
/** "ירידה של 0.4 ק״ג (0.49%)" — the word carries the direction, so the numbers are magnitudes.
 *  A flat week keeps the small signed change visible: "ללא שינוי משמעותי (−0.03 ק״ג)". */
export function comparisonPhrase(c: { kg: number; pct: number | null; dir: Exclude<WeightTrendDir, ''> }): string {
  if (c.dir === 'flat') return `ללא שינוי משמעותי (${fmtSigned(c.kg)} ק״ג)`;
  return `${TREND_LABEL[c.dir]} של ${nfAbs.format(Math.abs(c.kg))} ק״ג${c.pct != null ? ` (${nfAbs.format(Math.abs(c.pct))}%)` : ''}`;
}

/** "12–18 באוק׳" style range text helper input: day/month numbers of a week. */
export function weekRange(w: { week_start: string; week_end: string }): string {
  const [, m1, d1] = w.week_start.split('-').map(Number);
  const [, m2, d2] = w.week_end.split('-').map(Number);
  return m1 === m2 ? `${d1}–${d2}/${m2}` : `${d1}/${m1}–${d2}/${m2}`;
}

/** Weeks for the table, newest first. */
export const weeksNewestFirst = (t: WeightTrend | null): WeightWeek[] => (t ? [...t.weeks].reverse() : []);
/** Weigh-ins for the recent list, newest first. */
export const entriesNewestFirst = (list: WeighIn[]): WeighIn[] => [...list].sort((a, b) => b.date.localeCompare(a.date));

export function minMaxText(w: WeightWeek): string {
  if (w.min_kg == null || w.max_kg == null) return '—';
  return w.min_kg === w.max_kg ? fmtKg(w.min_kg) : `${fmtKg(w.min_kg)}–${fmtKg(w.max_kg)}`;
}

// ---- chart ----
export interface SeriesPoint { date: string; value: number | null; detail?: string }

/** Daily weigh-ins as points, and each week's average placed mid-week (4th day, clamped to the range end).
 *  A week without weigh-ins is a gap in the average line, never an interpolated value. */
export function chartSeries(t: WeightTrend): { daily: SeriesPoint[]; weekly: SeriesPoint[] } {
  const daily = t.entries.map((e) => ({ date: e.date, value: e.weight_kg, detail: e.note || undefined }));
  const weekly = t.weeks.map((w) => {
    const mid = plusDays(w.week_start, 3);
    return { date: mid > t.to ? t.to : mid, value: w.average_kg };
  });
  return { daily, weekly };
}

/** The weigh-in of a date among known entries, if any. */
export const entryOn = (list: WeighIn[], date: string) => list.find((e) => e.date === date) ?? null;

export const sameEntry = (a: WeighIn | null | undefined, b: WeighIn | null | undefined) => (a?.date ?? null) === (b?.date ?? null) && (a?.revision ?? 0) === (b?.revision ?? 0);
