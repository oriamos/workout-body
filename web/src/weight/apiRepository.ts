// WeightRepository over the engine's HTTP API (/api/v1/weight/...).
import { ApiError } from '../data/apiRepository';
import type { WeightRepository } from './repository';
import type { WeighIn, WeightTrend } from './types';

const BASE = '/api/v1/weight';
export const STALE_MSG = 'השקילה השתנתה במקום אחר (למשל דרך העוזר).';

const FIELD_MSG: Record<string, string> = {
  weight_kg: 'משקל בין 20 ל־400 ק״ג',
  date: 'אי אפשר לרשום משקל לתאריך עתידי',
  note: 'הערה עד 200 תווים',
};

async function req<T>(method: string, path: string, opts: { json?: unknown; headers?: Record<string, string> } = {}): Promise<{ data: T; status: number }> {
  const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers };
  if (opts.json !== undefined) headers['Content-Type'] = 'application/json';
  let r: Response;
  try {
    r = await fetch(BASE + path, { method, headers, body: opts.json === undefined ? undefined : JSON.stringify(opts.json), credentials: 'same-origin' });
  } catch {
    throw new ApiError(0, 'network', 'אין חיבור לשרת. בדוק את החיבור ונסה שוב.');
  }
  if (r.status === 204) return { data: null as T, status: 204 };
  const text = await r.text();
  let data: unknown = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* not JSON */ }
  if (!r.ok) {
    if (r.status === 401) throw new ApiError(401, 'unauthorized', 'השרת דחה את ההרשאה. ודא ש־WORKOUT_API_TOKEN מוגדר בפרוקסי.');
    if (r.status === 412) throw new ApiError(412, 'precondition_failed', STALE_MSG);
    if (r.status === 501 || (r.status === 404 && !data)) throw new ApiError(r.status, 'weight_disabled', 'מעקב המשקל לא זמין בשרת הזה.');
    const e = (data as { error?: { code: string; message: string; issues?: { field: string; message: string }[] } } | null)?.error;
    if (e) {
      // The engine's texts are English; show ours for the fields we know.
      const issues = (e.issues ?? []).map((i) => ({ field: i.field, message: FIELD_MSG[i.field] ?? i.message }));
      throw new ApiError(r.status, e.code, issues.length ? issues.map((i) => i.message).join(' · ') : e.message, issues);
    }
    throw new ApiError(r.status, 'http', `שגיאת שרת (${r.status})`);
  }
  return { data: data as T, status: r.status };
}

const enc = encodeURIComponent;

export function createApiWeight(): WeightRepository {
  return {
    trend: (weeks) => req<WeightTrend>('GET', `/trend?weeks=${weeks}`).then((r) => r.data),
    // The range listing answers 200 with [] when there is none, so "no weigh-in" is not an HTTP error.
    get: (date) => req<{ entries: WeighIn[] }>('GET', `/entries?from=${enc(date)}&to=${enc(date)}`).then((r) => r.data.entries.find((e) => e.date === date) ?? null),
    put: (date, weight_kg, note, expectedRevision) =>
      req<WeighIn>('PUT', `/entries/${enc(date)}`, { json: { weight_kg, note }, headers: { 'If-Match': `"${expectedRevision}"` } }).then((r) => ({ entry: r.data, created: r.status === 201 })),
    remove: (date, revision) => req<void>('DELETE', `/entries/${enc(date)}`, { headers: { 'If-Match': `"${revision}"` } }).then(() => undefined),
  };
}
