// NutritionRepository over the engine's HTTP API (/api/v1/nutrition/...).
import { ApiError } from '../data/apiRepository';
import type { NutritionRepository } from './repository';
import type { EntryFilter, ItemSuggestion, NutritionDay, NutritionEntry, NutritionTargets } from './types';

const BASE = '/api/v1/nutrition';

async function req<T>(method: string, path: string, opts: { json?: unknown; headers?: Record<string, string> } = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers };
  if (opts.json !== undefined) headers['Content-Type'] ??= 'application/json';
  let r: Response;
  try {
    r = await fetch(BASE + path, { method, headers, body: opts.json === undefined ? undefined : JSON.stringify(opts.json), credentials: 'same-origin' });
  } catch {
    throw new ApiError(0, 'network', 'אין חיבור לשרת. בדוק את החיבור ונסה שוב.');
  }
  if (r.status === 204) return null as T;
  const text = await r.text();
  let data: unknown = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* not JSON */ }
  if (!r.ok) {
    if (r.status === 401) throw new ApiError(401, 'unauthorized', 'השרת דחה את ההרשאה. ודא ש־WORKOUT_API_TOKEN מוגדר בפרוקסי.');
    if (r.status === 412) throw new ApiError(412, 'precondition_failed', 'הנתון השתנה במקום אחר (למשל דרך העוזר). הערכים העדכניים נטענו, והשינוי שלך לא נשמר.');
    if (r.status === 404 && !data) throw new ApiError(404, 'not_found', 'מעקב התזונה לא זמין בשרת הזה.');
    const e = (data as { error?: { code: string; message: string; issues?: { field: string; message: string }[] } } | null)?.error;
    if (e) throw new ApiError(r.status, e.code, e.message, e.issues ?? []);
    throw new ApiError(r.status, 'http', `שגיאת שרת (${r.status})`);
  }
  return data as T;
}

const qs = (o: Record<string, string | number | undefined>) => {
  const p = Object.entries(o).filter(([, v]) => v !== undefined && v !== '').map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`);
  return p.length ? `?${p.join('&')}` : '';
};
const ifMatch = (rev: number) => ({ 'If-Match': `"${rev}"` });
const merge = { 'Content-Type': 'application/merge-patch+json' };

/** Older servers have no ingredients: treat a missing list as none. */
const norm = (e: NutritionEntry): NutritionEntry => ({ ...e, items: e.items ?? [], incomplete_values: e.incomplete_values ?? [] });
const normDay = (d: NutritionDay): NutritionDay => ({ ...d, entries: d.entries.map(norm) });

export function createApiNutrition(): NutritionRepository {
  return {
    day: (date) => req<NutritionDay>('GET', `/days/${encodeURIComponent(date)}`).then(normDay),
    days: (from, to) => req<{ days: NutritionDay[] }>('GET', `/days${qs({ from, to })}`).then((r) => r.days.map(normDay)),
    entries: (f: EntryFilter) => req<{ entries: NutritionEntry[] }>('GET', `/entries${qs({ date: f.date, from: f.from, to: f.to, q: f.q, limit: f.limit })}`).then((r) => r.entries.map(norm)),
    entry: (id) => req<NutritionEntry>('GET', `/entries/${id}`).then(norm),
    add: (input, key) => req<{ entry: NutritionEntry }>('POST', '/entries', { json: input, headers: { 'Idempotency-Key': key } }).then((r) => norm(r.entry)),
    update: (id, patch, revision) => req<NutritionEntry>('PATCH', `/entries/${id}`, { json: patch, headers: { ...ifMatch(revision), ...merge } }).then(norm),
    remove: (id, revision) => req<void>('DELETE', `/entries/${id}`, { headers: ifMatch(revision) }),
    copy: (id, date, key) => req<{ entry: NutritionEntry }>('POST', `/entries/${id}/copy`, { json: { date }, headers: { 'Idempotency-Key': key } }).then((r) => norm(r.entry)),
    suggestItems: (q, limit = 8) => req<{ items: ItemSuggestion[] }>('GET', `/items${qs({ q: q.trim(), limit })}`).then((r) => r.items ?? []),
    targets: (date) => req<{ targets: NutritionTargets }>('GET', `/targets${qs({ date })}`).then((r) => r.targets),
    setTargets: (date, patch, revision) => req<NutritionTargets>('PUT', `/targets/${encodeURIComponent(date)}`, { json: patch, headers: { ...ifMatch(revision), ...merge } }),
  };
}
