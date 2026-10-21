import { addDays, weekStart } from './format';

export type RangeKey = '4w' | '8w' | 'all';
export const RANGES: { key: RangeKey; label: string; weeks: number | null }[] = [
  { key: '4w', label: '4 שבועות', weeks: 4 },
  { key: '8w', label: '8 שבועות', weeks: 8 },
  { key: 'all', label: 'הכול', weeks: null },
];

/** Ranges are whole calendar weeks ending with the current (partial) week, on every screen. */
export function rangeStart(key: RangeKey, today: string, firstRecord: string): string {
  const r = RANGES.find((x) => x.key === key)!;
  if (r.weeks == null) return firstRecord;
  const s = addDays(weekStart(today), -7 * (r.weeks - 1));
  return s < firstRecord ? firstRecord : s;
}
