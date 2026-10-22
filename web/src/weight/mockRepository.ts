// In-memory WeightRepository for VITE_DATA_SOURCE=mock. Deterministic synthetic weigh-ins (about ten
// weeks, with skipped days and one whole week without any) and the engine's rules: one per date,
// 20–400 kg, no future dates, If-Match on replace/delete. The weekly view uses the same computation.
import { ApiError } from '../data/apiRepository';
import { TODAY } from '../data/seed';
import { computeTrend, MAX_KG, MAX_NOTE, MIN_KG, plusDays, round2 } from './logic';
import type { WeightRepository } from './repository';
import type { WeighIn } from './types';

const NOTES: Record<number, string> = { 4: 'אחרי ארוחה מלוחה', 11: 'בוקר, לפני קפה', 23: 'אחרי טיסה' };

export function createMockWeight(): WeightRepository {
  const today = TODAY;
  let nextId = 1;
  const entries: WeighIn[] = [];
  for (let back = 70; back >= 1; back--) {
    // Gaps: roughly two skipped days a week, plus a full Sunday–Saturday week off (days 32–38 back).
    if ((back * 5) % 7 < 2 || (back >= 32 && back <= 38)) continue;
    const date = plusDays(today, -back);
    // A slow drift down with day-to-day noise (water, salt, food) — all deterministic.
    const noise = (((back * 37) % 11) - 5) * 0.12;
    const weight_kg = round2(84.2 - (70 - back) * 0.035 + noise);
    const stamp = `${date}T06:30:00Z`;
    entries.push({ id: nextId++, date, weight_kg, ...(NOTES[back] && { note: NOTES[back] }), revision: 1, created_at: stamp, updated_at: stamp });
  }

  const wait = (ms = 200) => new Promise((r) => setTimeout(r, ms));
  const clone = <T,>(v: T): T => JSON.parse(JSON.stringify(v));
  const find = (date: string) => entries.find((e) => e.date === date) ?? null;
  const stale = () => new ApiError(412, 'precondition_failed', 'השקילה השתנתה במקום אחר (למשל דרך העוזר).');

  return {
    async trend(weeks) {
      await wait();
      return clone(computeTrend(entries, today, weeks, today));
    },
    async get(date) {
      await wait(100);
      return clone(find(date));
    },
    async put(date, weight_kg, note, expectedRevision) {
      await wait();
      const issues = [];
      if (date > today) issues.push({ field: 'date', message: 'אי אפשר לרשום משקל לתאריך עתידי' });
      if (!(weight_kg >= MIN_KG && weight_kg <= MAX_KG)) issues.push({ field: 'weight_kg', message: `משקל בין ${MIN_KG} ל־${MAX_KG} ק״ג` });
      if ([...note.trim()].length > MAX_NOTE) issues.push({ field: 'note', message: `הערה עד ${MAX_NOTE} תווים` });
      if (issues.length) throw new ApiError(400, 'validation_error', issues.map((i) => i.message).join(' · '), issues);
      const cur = find(date);
      if ((cur?.revision ?? 0) !== expectedRevision) throw stale();
      const now = new Date().toISOString();
      if (cur) {
        Object.assign(cur, { weight_kg: round2(weight_kg), note: note.trim() || undefined, revision: cur.revision + 1, updated_at: now });
        return { entry: clone(cur), created: false };
      }
      const e: WeighIn = { id: nextId++, date, weight_kg: round2(weight_kg), ...(note.trim() && { note: note.trim() }), revision: 1, created_at: now, updated_at: now };
      entries.push(e);
      return { entry: clone(e), created: true };
    },
    async remove(date, revision) {
      await wait();
      const cur = find(date);
      if (!cur) throw new ApiError(404, 'not_found', 'השקילה כבר נמחקה.');
      if (cur.revision !== revision) throw stale();
      entries.splice(entries.indexOf(cur), 1);
    },
  };
}
