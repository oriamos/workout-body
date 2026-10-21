const HE_DAYS = ['ראשון', 'שני', 'שלישי', 'רביעי', 'חמישי', 'שישי', 'שבת'];
const HE_MONTHS = ['ינואר', 'פברואר', 'מרץ', 'אפריל', 'מאי', 'יוני', 'יולי', 'אוגוסט', 'ספטמבר', 'אוקטובר', 'נובמבר', 'דצמבר'];

export function parseDate(d: string): Date {
  const [y, m, day] = d.split('-').map(Number);
  return new Date(y, m - 1, day);
}
export function toISO(dt: Date): string {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${dt.getFullYear()}-${p(dt.getMonth() + 1)}-${p(dt.getDate())}`;
}
export function addDays(d: string, n: number): string {
  const dt = parseDate(d);
  dt.setDate(dt.getDate() + n);
  return toISO(dt);
}
export function diffDays(a: string, b: string): number {
  return Math.round((parseDate(a).getTime() - parseDate(b).getTime()) / 86400000);
}
export const dayName = (d: string) => HE_DAYS[parseDate(d).getDay()];
export const monthName = (m: number) => HE_MONTHS[m];
export function longDate(d: string) {
  const dt = parseDate(d);
  return `יום ${HE_DAYS[dt.getDay()]}, ${dt.getDate()} ב${HE_MONTHS[dt.getMonth()]}`;
}
export function shortDate(d: string) {
  const dt = parseDate(d);
  return `${dt.getDate()} ב${HE_MONTHS[dt.getMonth()].slice(0, 3)}׳`.replace('׳׳', '׳');
}
export function dayMonth(d: string) {
  const dt = parseDate(d);
  return `${dt.getDate()}/${dt.getMonth() + 1}`;
}
export function weekdayShort(d: string) {
  return `${HE_DAYS[parseDate(d).getDay()]}`;
}
/** Sunday-based week start (Israel). */
export function weekStart(d: string): string {
  return addDays(d, -parseDate(d).getDay());
}

const nf = new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 });
export const num = (n: number) => nf.format(n);
export const kg = (n: number | null) => (n == null ? '—' : nf.format(n));
export const vol = (n: number) => nf.format(Math.round(n));

export function plural(n: number, one: string, many: string) {
  return n === 1 ? one : `${n} ${many}`;
}
export function parseNum(s: string): number | null {
  const t = s.trim().replace(',', '.');
  if (t === '') return null;
  if (!/^-?\d+(\.\d+)?$/.test(t)) return NaN;
  return Number(t);
}
