// UI-only data that the workout engine does not store. It lives in this browser's localStorage,
// so it is per device and can disappear (private mode, cleared site data). Every access is guarded.
import type { ExerciseMeta } from './apiMapping';
import { normName } from './apiMapping';

const KEYS = {
  meta: 'wb.exerciseMeta.v1', // { [exerciseId]: { muscle?, equipment?, restSec? } }
  pending: 'wb.pendingExercises.v1', // { [date]: canonical exercise names added without a set yet }
  notes: 'wb.localNotes.v1', // { [date]: { [canonical name]: note } } for exercises without a prescription
  startedAt: 'wb.startedAt.v1', // { [date]: 'HH:MM' } the engine does not record a start time
};

function read<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}
function write(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* storage unavailable: UI-only metadata is best effort */
  }
}

export const exerciseMeta = (): Record<string, ExerciseMeta> => read(KEYS.meta, {});
export function setExerciseMeta(id: string, meta: ExerciseMeta) {
  write(KEYS.meta, { ...exerciseMeta(), [id]: { ...exerciseMeta()[id], ...meta } });
}

export const pendingFor = (date: string): string[] => read<Record<string, string[]>>(KEYS.pending, {})[date] ?? [];
export function setPending(date: string, name: string, on: boolean) {
  const all = read<Record<string, string[]>>(KEYS.pending, {});
  const n = normName(name);
  const list = (all[date] ?? []).filter((x) => x !== n);
  if (on) list.push(n);
  if (list.length) all[date] = list;
  else delete all[date];
  write(KEYS.pending, all);
}

export const notesFor = (date: string): Record<string, string> => read<Record<string, Record<string, string>>>(KEYS.notes, {})[date] ?? {};
export function setLocalNote(date: string, name: string, note: string) {
  const all = read<Record<string, Record<string, string>>>(KEYS.notes, {});
  const day = { ...all[date] };
  if (note) day[normName(name)] = note;
  else delete day[normName(name)];
  if (Object.keys(day).length) all[date] = day;
  else delete all[date];
  write(KEYS.notes, all);
}

export const startedAtFor = (date: string): string | undefined => read<Record<string, string>>(KEYS.startedAt, {})[date];
export function setStartedAt(date: string, hhmm: string) {
  if (hhmm) write(KEYS.startedAt, { ...read<Record<string, string>>(KEYS.startedAt, {}), [date]: hhmm });
}
