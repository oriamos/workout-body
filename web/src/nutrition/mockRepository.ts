// In-memory NutritionRepository for VITE_DATA_SOURCE=mock. Deterministic synthetic data; same rules
// as the engine (missing stays missing, no future food, targets apply from a date forward, If-Match).
import { ApiError } from '../data/apiRepository';
import { TODAY } from '../data/seed';
import { addDays } from '../lib/format';
import { buildDay, sumItems, targetsAt } from './logic';
import type { NutritionRepository } from './repository';
import { NUTRIENTS, type EntryInput, type ItemSuggestion, type NutrientKey, type Nutrients, type NutritionEntry, type NutritionItem, type NutritionTargets } from './types';

type Vals = [number | null, number | null, number | null, number | null];
type Ing = [string, number | null, ...Vals];
/** Meals as [name, values] (a simple meal) or [name, ingredients]. Some values are deliberately unknown. */
const MEALS: [string, Vals | Ing[]][] = [
  ['יוגורט עם גרנולה', [['יוגורט 3%', 200, 130, 12, 9, 6], ['גרנולה', 40, 180, 4, 26, 6], ['דבש', 10, 30, null, 8, 0]]],
  ['חביתה משתי ביצים ולחם', [['ביצה', 100, 143, 12.6, 0.7, 9.5], ['לחם מלא', 60, 150, 6, 26, 2], ['שמן זית', 5, 44, 0, 0, 5]]],
  ['חזה עוף, אורז וסלט', [['חזה עוף', 150, 248, 46.5, 0, 5.4], ['אורז לבן מבושל', 180, 234, 4.3, 51, 0.5], ['סלט ירקות', 120, 30, 1.2, 6, null]]],
  ['שייק חלבון', [180, 30, 6, 3]],
  ['פסטה ברוטב עגבניות', [['פסטה מבושלת', 200, 316, 11.6, 62, 1.8], ['רוטב עגבניות', 120, 60, 2, 10, null], ['פרמזן', 10, 39, 3.6, 0.3, 2.6]]],
  ['תפוח', [80, null, 21, null]],
  ['סלמון ותפוחי אדמה', [['סלמון', 150, 312, 30, 0, 20], ['תפוחי אדמה אפויים', 200, 186, 5, 42, 0.2], ['ברוקולי', 100, 35, 2.4, 7, 0.4]]],
  ['כריך טונה', [430, 28, null, 14]],
];

const nutrientsOf = (v: Vals): Nutrients => ({ calories_kcal: v[0], protein_g: v[1], carbs_g: v[2], fat_g: v[3] });
function mealParts(def: Vals | Ing[]): { items: NutritionItem[]; incomplete_values: NutrientKey[]; values: Nutrients } {
  if (typeof def[0] !== 'object' || def[0] === null) return { items: [], incomplete_values: [], values: nutrientsOf(def as Vals) };
  const items = (def as Ing[]).map(([name, amount_g, ...v]) => ({ name, amount_g, ...nutrientsOf(v as Vals) }));
  const { sum, incomplete } = sumItems(items);
  return { items, incomplete_values: incomplete, values: sum };
}

export function createMockNutrition(): NutritionRepository {
  const today = TODAY;
  let nextId = 1;
  let nextRev = 3;
  const stamp = (d: string) => `${d}T12:00:00Z`;
  const entries: NutritionEntry[] = [];
  const meal = (date: string, i: number): NutritionEntry => {
    const [name, def] = MEALS[i % MEALS.length];
    const { items, incomplete_values, values } = mealParts(def);
    return { id: nextId++, date, name, ...values, items, incomplete_values, copied_from: null, revision: 1, created_at: stamp(date), updated_at: stamp(date) };
  };
  for (let back = 20; back >= 1; back--) {
    const date = addDays(today, -back);
    const n = 2 + ((back * 7) % 3); // 2..4 meals
    for (let i = 0; i < n; i++) entries.push(meal(date, back * 3 + i));
  }
  entries.push(meal(today, 0), meal(today, 2));
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
  const anyValue = (n: Partial<Nutrients>) => NUTRIENTS.some((k) => n[k] != null);
  const fromItemsError = () => new ApiError(400, 'validation_error', 'ארוחה שמורכבת ממרכיבים מקבלת את הערכים מהם. שנה את המרכיבים, או הסר אותם כדי להזין ערכים ישירות.', [{ field: 'items', message: 'values come from the items' }]);
  const cleanItems = (list: NutritionItem[]): NutritionItem[] => {
    if (list.length > 50) throw new ApiError(400, 'validation_error', 'עד 50 מרכיבים בארוחה', [{ field: 'items', message: 'at most 50' }]);
    return list.map((it, i) => {
      const name = it.name.trim().replace(/\s+/g, ' ');
      if (!name) throw new ApiError(400, 'validation_error', 'חסר שם למרכיב', [{ field: `items[${i}].name`, message: 'is required' }]);
      return { name, amount_g: it.amount_g ?? null, ...values(it) };
    });
  };
  /** Store items on an entry; a non-empty list sets the entry's values to the sums. */
  const setItems = (e: NutritionEntry, list: NutritionItem[]) => {
    e.items = list;
    if (list.length) {
      const { sum, incomplete } = sumItems(list);
      Object.assign(e, sum);
      e.incomplete_values = incomplete;
    } else e.incomplete_values = [];
  };

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
      const list = cleanItems(input.items ?? []);
      if (list.length && anyValue(input)) throw fromItemsError();
      const e: NutritionEntry = { id: nextId++, date, name: input.name, ...values(input), items: [], incomplete_values: [], copied_from: null, revision: 1, created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
      setItems(e, list);
      entries.push(e);
      idem.set(key, { body, id: e.id });
      return clone(e);
    },
    async update(id, patch, revision) {
      await wait();
      const e = find(id);
      const setsValues = NUTRIENTS.some((k) => k in patch);
      if (setsValues && (patch.items ? patch.items.length > 0 : e.items.length > 0)) throw fromItemsError();
      checkRev(e.revision, revision);
      if (patch.date) noFuture(patch.date);
      const list = patch.items ? cleanItems(patch.items) : null;
      if (patch.name !== undefined) e.name = patch.name;
      if (patch.date !== undefined) e.date = patch.date;
      for (const k of NUTRIENTS) if (k in patch) e[k] = patch[k] ?? null;
      if (list) setItems(e, list); // [] keeps the current sums as the meal's own values
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
    async suggestItems(q, limit = 8) {
      await wait(80);
      const needle = q.trim().toLowerCase();
      // Latest use of each ingredient name (newest entry first), with how often it was used.
      const latest = new Map<string, ItemSuggestion>();
      for (const e of [...entries].sort((a, b) => b.date.localeCompare(a.date) || b.id - a.id)) {
        for (const it of e.items) {
          const k = it.name.toLowerCase();
          if (needle && !k.includes(needle)) continue;
          const cur = latest.get(k);
          if (cur) cur.uses++;
          else latest.set(k, { ...clone(it), last_used: e.date, uses: 1 });
        }
      }
      return [...latest.values()].sort((a, b) => b.last_used.localeCompare(a.last_used) || b.uses - a.uses || a.name.localeCompare(b.name)).slice(0, limit);
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
