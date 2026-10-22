// In-memory NutritionRepository for VITE_DATA_SOURCE=mock. Deterministic synthetic data; same rules
// as the engine (missing stays missing, no future food, targets apply from a date forward, If-Match).
import { ApiError } from '../data/apiRepository';
import { TODAY } from '../data/seed';
import { addDays } from '../lib/format';
import { buildDay, targetsAt } from './logic';
import type { NutritionRepository } from './repository';
import { NUTRIENTS, type EntryInput, type Nutrients, type NutritionEntry, type NutritionTargets } from './types';

const MEALS: [string, number | null, number | null, number | null, number | null][] = [
  ['יוגורט עם גרנולה', 320, 18, 42, 8],
  ['חביתה משתי ביצים ולחם', 410, 22, 30, 21],
  ['חזה עוף, אורז וסלט', 620, 48, 70, 12],
  ['שייק חלבון', 180, 30, 6, 3],
  ['פסטה ברוטב עגבניות', 540, 17, 92, null],
  ['תפוח', 80, null, 21, null],
  ['סלמון ותפוחי אדמה', 650, 40, 45, 30],
  ['כריך טונה', 430, 28, null, 14],
];

export function createMockNutrition(): NutritionRepository {
  const today = TODAY;
  let nextId = 1;
  let nextRev = 3;
  const stamp = (d: string) => `${d}T12:00:00Z`;
  const entries: NutritionEntry[] = [];
  for (let back = 20; back >= 1; back--) {
    const date = addDays(today, -back);
    const n = 2 + ((back * 7) % 3); // 2..4 meals
    for (let i = 0; i < n; i++) {
      const [name, calories_kcal, protein_g, carbs_g, fat_g] = MEALS[(back * 3 + i) % MEALS.length];
      entries.push({ id: nextId++, date, name, calories_kcal, protein_g, carbs_g, fat_g, copied_from: null, revision: 1, created_at: stamp(date), updated_at: stamp(date) });
    }
  }
  const [b, l] = [MEALS[0], MEALS[2]];
  for (const [name, calories_kcal, protein_g, carbs_g, fat_g] of [b, l]) entries.push({ id: nextId++, date: today, name, calories_kcal, protein_g, carbs_g, fat_g, copied_from: null, revision: 1, created_at: stamp(today), updated_at: stamp(today) });
  const history: NutritionTargets[] = [
    { effective_from: addDays(today, -30), calories_kcal: 2200, protein_g: 150, carbs_g: null, fat_g: 70, revision: 1 },
    { effective_from: addDays(today, -7), calories_kcal: 2400, protein_g: 160, carbs_g: 260, fat_g: 75, revision: 2 },
  ];
  const idem = new Map<string, { body: string; id: number }>();

  const wait = (ms = 250) => new Promise((r) => setTimeout(r, ms));
  const clone = <T,>(v: T): T => JSON.parse(JSON.stringify(v));
  const sorted = (list: NutritionEntry[]) => [...list].sort((a, b) => b.date.localeCompare(a.date) || a.id - b.id);
  const find = (id: number) => {
    const e = entries.find((x) => x.id === id);
    if (!e) throw new ApiError(404, 'not_found', `ארוחה ${id} לא נמצאה`);
    return e;
  };
  const noFuture = (d: string) => {
    if (d > today) throw new ApiError(400, 'validation_error', 'אי אפשר לרשום אוכל לתאריך עתידי');
  };
  const checkRev = (have: number, want: number) => {
    if (have !== want) throw new ApiError(412, 'precondition_failed', 'הנתון השתנה במקום אחר. הערכים העדכניים נטענו, והשינוי שלך לא נשמר.');
  };
  const values = (n: Partial<Nutrients>): Nutrients => ({ calories_kcal: n.calories_kcal ?? null, protein_g: n.protein_g ?? null, carbs_g: n.carbs_g ?? null, fat_g: n.fat_g ?? null });

  return {
    async day(date) {
      await wait();
      return clone(buildDay(date, today, sorted(entries.filter((e) => e.date === date)), targetsAt(history, date)));
    },
    async days(from, to) {
      await wait(120);
      const out = [];
      for (let d = from; d <= to; d = addDays(d, 1)) out.push(buildDay(d, today, sorted(entries.filter((e) => e.date === d)), targetsAt(history, d)));
      return clone(out);
    },
    async entries(f) {
      await wait();
      const q = f.q?.trim().toLowerCase();
      return clone(sorted(entries.filter((e) => (!f.date || e.date === f.date) && (!f.from || e.date >= f.from) && (!f.to || e.date <= f.to) && (!q || e.name.toLowerCase().includes(q)))).slice(0, f.limit ?? 100));
    },
    async entry(id) {
      await wait(100);
      return clone(find(id));
    },
    async add(input: EntryInput, key) {
      await wait();
      const body = JSON.stringify(input);
      const prev = idem.get(key);
      if (prev) {
        if (prev.body !== body) throw new ApiError(409, 'conflict', 'מפתח השליחה כבר שימש לבקשה אחרת');
        return clone(find(prev.id));
      }
      const date = input.date ?? today;
      noFuture(date);
      const e: NutritionEntry = { id: nextId++, date, name: input.name, ...values(input), copied_from: null, revision: 1, created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
      entries.push(e);
      idem.set(key, { body, id: e.id });
      return clone(e);
    },
    async update(id, patch, revision) {
      await wait();
      const e = find(id);
      checkRev(e.revision, revision);
      if (patch.date) noFuture(patch.date);
      if (patch.name !== undefined) e.name = patch.name;
      if (patch.date !== undefined) e.date = patch.date;
      for (const k of NUTRIENTS) if (k in patch) e[k] = patch[k] ?? null;
      e.revision++;
      e.updated_at = new Date().toISOString();
      return clone(e);
    },
    async remove(id, revision) {
      await wait();
      const e = find(id);
      checkRev(e.revision, revision);
      entries.splice(entries.indexOf(e), 1);
    },
    async copy(id, date, key) {
      await wait();
      const prev = idem.get(key);
      if (prev) return clone(find(prev.id));
      noFuture(date);
      const src = find(id);
      const e: NutritionEntry = { ...clone(src), id: nextId++, date, copied_from: src.id, revision: 1, created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
      entries.push(e);
      idem.set(key, { body: String(id), id: e.id });
      return clone(e);
    },
    async targets(date) {
      await wait(100);
      return clone(targetsAt(history, date));
    },
    async setTargets(date, patch, revision) {
      await wait();
      const cur = targetsAt(history, date);
      checkRev(cur.revision, revision);
      const next: NutritionTargets = { ...values(cur), effective_from: date, revision: nextRev++ };
      for (const k of NUTRIENTS) if (k in patch) next[k] = patch[k] ?? null;
      const i = history.findIndex((t) => t.effective_from === date);
      if (i >= 0) history[i] = next;
      else history.push(next);
      return clone(next);
    },
  };
}
