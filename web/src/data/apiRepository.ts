// WorkoutRepository over the workout engine's JSON API (/api/v1, see internal/apisrv/openapi.yaml).
// The browser only ever calls same-origin `/api/v1/...`; the dev/preview proxy (vite.config.ts) adds
// the bearer token, which never reaches client code.
//
// Model: the engine stores PERFORMED sets only. Planned-but-not-done rows, exercises added without a
// set, and notes on exercises without a prescription are local to this UI (drafts / localStorage).
// Ids: objects loaded from the server use String(serverId) as their UI id; objects created in the UI
// keep their client id and are mapped to the server id once the server answers.
import type { AppData, Exercise, Plan, SetEntry, Workout, WorkoutExercise } from './types';
import { applyMutation, findSet, type ExternalUpdate, type Mutation } from './mutations';
import type { WorkoutRepository } from './repository';
import {
  blankDraft, changeSummary, diffWorkout, draftId, emptySnapshot, howToFromGuide, iso, mediaUrl, normName, planDocument, prescriptionInput,
  setInput, setPatchBody, setSummary, snapshotOf, titleCase, toClientExercise, toClientPlan, toClientWorkout, toPrescription, toSetEntry,
  workingNumber,
  type SBatchResult, type SConfig, type SExercise, type SGuide, type SMedia, type SPlan, type SPlanItem, type Snapshot, type SSet, type SWorkout,
  type SWorkoutSummary,
} from './apiMapping';
import { exerciseMeta, notesFor, pendingFor, setLocalNote, setPending, setStartedAt, startedAtFor } from './localMeta';
import { addDays } from '../lib/format';

export class ApiError extends Error {
  readonly isApiError = true;
  constructor(public status: number, public code: string, message: string, public issues: { field: string; message: string }[] = []) {
    super(message);
  }
}

const POLL_MS = 5000;
const STALE_MSG = 'הנתון השתנה במקום אחר (למשל דרך העוזר). הערך העדכני מוצג, והשינוי שלך לא נשמר.';

interface Res<T> { data: T; status: number; etag: number | null }
interface PlanInfo { id: number; revision: number; schedule: unknown | null; notes?: string; items?: SPlanItem[] }

async function pool<T, R>(items: T[], n: number, fn: (t: T) => Promise<R>): Promise<R[]> {
  const out: R[] = new Array(items.length);
  let i = 0;
  await Promise.all(Array.from({ length: Math.min(n, items.length) }, async () => {
    while (i < items.length) {
      const k = i++;
      out[k] = await fn(items[k]);
    }
  }));
  return out;
}

export function createApiRepository(base = '/api/v1'): WorkoutRepository {
  // ---------- transport ----------
  async function req<T>(method: string, path: string, opts: { json?: unknown; body?: BodyInit; headers?: Record<string, string> } = {}): Promise<Res<T>> {
    const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers };
    let body = opts.body;
    if (opts.json !== undefined) {
      body = JSON.stringify(opts.json);
      headers['Content-Type'] ??= 'application/json';
    }
    let r: Response;
    try {
      r = await fetch(base + path, { method, headers, body, credentials: 'same-origin' });
    } catch {
      throw new ApiError(0, 'network', 'אין חיבור לשרת האימונים. בדוק את החיבור ונסה שוב.');
    }
    const tag = r.headers.get('ETag');
    const etag = tag ? Number(tag.replace(/^W\//, '').replace(/"/g, '')) : null;
    if (r.status === 204) return { data: null as T, status: 204, etag };
    const text = await r.text();
    let data: unknown = null;
    try { data = text ? JSON.parse(text) : null; } catch { /* non-JSON body */ }
    if (!r.ok) {
      if (r.status === 401) throw new ApiError(401, 'unauthorized', 'השרת דחה את ההרשאה. ודא ש־WORKOUT_API_TOKEN מוגדר בפרוקסי.');
      const e = (data as { error?: { code: string; message: string; issues?: { field: string; message: string }[] } } | null)?.error;
      if (e) throw new ApiError(r.status, e.code, e.message, e.issues ?? []);
      if (r.status === 502 || r.status === 504) throw new ApiError(r.status, 'proxy', 'שרת האימונים לא זמין כרגע.');
      throw new ApiError(r.status, 'http', `שגיאת שרת (${r.status})`);
    }
    return { data: data as T, status: r.status, etag };
  }
  const dateUrl = (d: string) => `/workouts/${encodeURIComponent(d)}`;
  const ifMatch = (rev: number) => ({ 'If-Match': `"${rev}"` });

  // ---------- state ----------
  let config: SConfig = { timezone: '', today: '', api_version: 'v1', media: { enabled: false, max_video_bytes: 0 } };
  let mirror: AppData = { today: '', exercises: [], plans: [], workouts: [] }; // what the UI shows, kept in step with every mutation
  let loaded = false;
  const exS = new Map<string, number>(); // UI exercise id -> engine id
  const exC = new Map<number, string>(); // engine exercise id -> UI id
  const exCanon = new Map<string, string>(); // UI exercise id -> engine canonical name
  const byName = new Map<string, number>(); // normalized name/alias -> engine id
  const setS = new Map<string, number>(); // UI set id -> engine set id
  const setC = new Map<number, string>();
  const setRev = new Map<number, number>();
  const setType = new Map<number, string>(); // engine set_type as stored (e.g. 'backoff')
  const remoteVer = new Map<number, number>(); // bumps whenever a change made elsewhere is applied to a set
  const setMedia = new Map<number, number>(); // engine set id -> media id of its video
  const plans = new Map<string, PlanInfo>(); // UI plan id -> engine plan
  const snaps = new Map<string, Snapshot>(); // date -> last known server state of that workout
  const idemKeys = new WeakMap<Mutation, string>();
  const listeners = new Set<(u: ExternalUpdate) => void>();
  const noticeListeners = new Set<(text: string) => void>();
  const notice = (text: string) => noticeListeners.forEach((l) => l(text));

  function reset() {
    for (const m of [exS, exC, exCanon, byName, setS, setC, setRev, setType, remoteVer, setMedia, plans, snaps] as Map<unknown, unknown>[]) m.clear();
  }

  // ---------- write queue: one write at a time, in the order the UI made them ----------
  let chain: Promise<unknown> = Promise.resolve();
  let busy = 0;
  let writeSeq = 0;
  function enqueue<T>(fn: () => Promise<T>): Promise<T> {
    busy++;
    writeSeq++;
    const p = chain.then(fn);
    chain = p.then(() => undefined, () => undefined).then(() => { busy--; writeSeq++; });
    return p;
  }

  // ---------- exercises ----------
  function registerExercise(se: SExercise, clientId = String(se.id)) {
    exS.set(clientId, se.id);
    if (!exC.has(se.id)) exC.set(se.id, clientId);
    exCanon.set(clientId, se.name);
    byName.set(normName(se.name), se.id);
    for (const a of se.aliases ?? []) byName.set(normName(a), se.id);
  }
  const canonOf = (exId: string) => exCanon.get(exId) ?? normName(mirror.exercises.find((e) => e.id === exId)?.name ?? exId);
  const exRef = (exId: string): { exercise_id: number } | { exercise: string } => (exS.has(exId) ? { exercise_id: exS.get(exId)! } : { exercise: canonOf(exId) });
  const nameOf = (exId: string) => mirror.exercises.find((e) => e.id === exId)?.name ?? titleCase(canonOf(exId));
  /** UI exercise id for an engine (name, id); unknown ones get a placeholder so the UI never breaks. */
  function exerciseIdOf(name: string, serverId?: number, extra?: Exercise[]): string {
    const sid = serverId ?? byName.get(normName(name));
    if (sid != null && exC.has(sid)) return exC.get(sid)!;
    if (sid != null) {
      registerExercise({ id: sid, name: normName(name), aliases: [], set_count: 0 });
      extra?.push(toClientExercise({ id: sid, name: normName(name), aliases: [], set_count: 0 }, exerciseMeta()[String(sid)]));
      return String(sid);
    }
    const id = `name:${normName(name)}`;
    if (!exCanon.has(id)) {
      exCanon.set(id, normName(name));
      extra?.push({ ...toClientExercise({ id: 0, name: normName(name), aliases: [], set_count: 0 }, undefined), id });
    }
    return id;
  }
  /** Make sure an exercise first seen in a server read exists in the UI library. */
  function ensureExercise(serverId: number, name: string): string {
    if (exC.has(serverId) && mirror.exercises.some((e) => e.id === exC.get(serverId))) return exC.get(serverId)!;
    const extra: Exercise[] = [];
    const id = exerciseIdOf(name, serverId, extra);
    for (const ex of extra) if (!mirror.exercises.some((e) => e.id === ex.id)) emit({ t: 'addLibraryExercise', exercise: ex }, `תרגיל חדש בספרייה: ${iso(ex.name)}`);
    return id;
  }

  // ---------- sets ----------
  function registerSet(clientId: string, s: SSet) {
    setS.set(clientId, s.id);
    setC.set(s.id, clientId);
    setRev.set(s.id, s.revision);
    setType.set(s.id, s.set_type);
  }
  function unregisterSet(sid: number) {
    const cid = setC.get(sid);
    if (cid) setS.delete(cid);
    setC.delete(sid);
    setRev.delete(sid);
    setType.delete(sid);
    setMedia.delete(sid);
  }
  function idemKey(m: Mutation, clientId: string) {
    // A retry of the same mutation reuses its key (no duplicate); a new mutation on the same row
    // (e.g. undo after delete) gets a fresh one, so it is never answered with a deleted set.
    if (!idemKeys.has(m)) idemKeys.set(m, `web:${clientId}:${Math.random().toString(36).slice(2, 10)}`.slice(0, 128));
    return idemKeys.get(m)!;
  }

  // ---------- external updates ----------
  function emit(mutation: Mutation, summary: string) {
    mirror = applyMutation(mirror, mutation);
    const u: ExternalUpdate = { id: `x${Date.now()}-${Math.random().toString(36).slice(2, 7)}`, at: Date.now(), mutation, summary };
    listeners.forEach((l) => l(u));
  }

  function localFor(date: string) {
    return { pending: pendingFor(date), notes: notesFor(date), startedAt: startedAtFor(date) };
  }

  /**
   * Fold a fresh server read of a workout into the UI: compare with the last known server state and
   * emit ExternalUpdates for what changed elsewhere. Local drafts and pending edits are left alone
   * (only untouched empty planned rows may be filled by a set logged elsewhere, like the prototype).
   */
  function absorb(date: string, sw: SWorkout) {
    const old = snaps.get(date) ?? emptySnapshot();
    for (const s of sw.sets) if (!exC.has(s.exercise_id)) ensureExercise(s.exercise_id, s.exercise);
    for (const s of sw.sets) byName.set(normName(s.exercise), s.exercise_id);
    let wk = mirror.workouts.find((w) => w.date === date);
    if (!wk) {
      const extra: Exercise[] = [];
      const w = toClientWorkout(sw, (n, sid) => exerciseIdOf(n, sid, extra), localFor(date));
      for (const ex of extra) if (!mirror.exercises.some((e) => e.id === ex.id)) emit({ t: 'addLibraryExercise', exercise: ex }, `תרגיל חדש בספרייה: ${iso(ex.name)}`);
      for (const s of sw.sets) registerSet(String(s.id), s);
      snaps.set(date, snapshotOf(sw));
      const last = [...sw.sets].sort((x, y) => y.position - x.position)[0];
      emit({ t: 'createWorkout', workout: w }, last ? setSummary(titleCase(last.exercise), workingNumber(sw, last), last) : `אימון היום נפתח · ${w.planId ? iso(w.name) : w.name}`);
      return;
    }
    const d = diffWorkout(old, sw);
    const findWe = (exId: string) => mirror.workouts.find((w) => w.date === date)?.exercises.find((e) => e.exerciseId === exId);
    const progressItem = (exId: string) => sw.progress.find((p) => exerciseIdOf(p.exercise) === exId);

    for (const s of d.added) {
      if (setC.has(s.id)) continue; // written by this client
      const exId = ensureExercise(s.exercise_id, s.exercise);
      const entry: SetEntry = { ...toSetEntry(s), by: 'assistant' };
      const summary = setSummary(nameOf(exId), workingNumber(sw, s), s);
      const we = findWe(exId);
      wk = mirror.workouts.find((w) => w.date === date)!;
      if (!we) {
        const weId = `${wk.id}:${exId}`;
        registerSet(entry.id, s);
        const item = progressItem(exId);
        emit({ t: 'addExercise', workoutId: wk.id, we: { id: weId, exerciseId: exId, planned: item ? toPrescription(item) : null, note: item?.notes ?? '', sets: [entry] } }, summary);
        continue;
      }
      const blank = s.set_type !== 'warmup' ? we.sets.find((x) => !x.done && !setS.has(x.id) && x.weight == null && x.reps == null && x.rir == null && x.rpe == null && !x.video) : undefined;
      if (blank) {
        registerSet(blank.id, s);
        emit({ t: 'updateSet', setId: blank.id, patch: { type: entry.type, weight: s.weight_kg, reps: s.reps, rir: s.rir, rpe: s.rpe, done: true, by: 'assistant' } }, summary);
      } else {
        registerSet(entry.id, s);
        const firstOpen = we.sets.findIndex((x) => !x.done);
        emit({ t: 'addSet', weId: we.id, set: entry, index: firstOpen < 0 ? undefined : firstOpen }, summary);
      }
    }
    for (const c of d.changed) {
      const cid = setC.get(c.set.id);
      setRev.set(c.set.id, c.set.revision);
      setType.set(c.set.id, c.set.set_type);
      remoteVer.set(c.set.id, (remoteVer.get(c.set.id) ?? 0) + 1);
      if (!cid || !findSet(mirror, cid)) continue;
      const exId = ensureExercise(c.set.exercise_id, c.set.exercise);
      if (c.exerciseChanged) {
        const found = findSet(mirror, cid)!;
        emit({ t: 'removeSet', setId: cid }, `${iso(nameOf(found.we.exerciseId))} · סט הועבר לתרגיל אחר`);
        const we = findWe(exId);
        const entry: SetEntry = { ...toSetEntry(c.set), id: cid, by: 'assistant' };
        wk = mirror.workouts.find((w) => w.date === date)!;
        if (we) emit({ t: 'addSet', weId: we.id, set: entry }, setSummary(nameOf(exId), workingNumber(sw, c.set), c.set));
        else emit({ t: 'addExercise', workoutId: wk.id, we: { id: `${wk.id}:${exId}`, exerciseId: exId, planned: null, note: '', sets: [entry] } }, setSummary(nameOf(exId), workingNumber(sw, c.set), c.set));
        continue;
      }
      emit({ t: 'updateSet', setId: cid, patch: { ...c.patch, by: 'assistant' } }, changeSummary(nameOf(exId), workingNumber(sw, c.set), c.patch));
    }
    for (const s of d.removed) {
      const cid = setC.get(s.id);
      unregisterSet(s.id);
      if (cid && findSet(mirror, cid)) emit({ t: 'removeSet', setId: cid }, `${iso(nameOf(exC.get(s.exercise_id) ?? ''))} · סט נמחק`);
    }
    for (const item of d.newPrescriptions) {
      const exId = exerciseIdOf(item.exercise);
      if (findWe(exId)) continue;
      wk = mirror.workouts.find((w) => w.date === date)!;
      const weId = `${wk.id}:${exId}`;
      const planned = toPrescription(item)!;
      const rows = Array.from({ length: item.skipped ? 0 : planned.sets }, (_, i) => blankDraft(draftId(weId, i + 1)));
      if (!rows.length) rows.push(blankDraft(draftId(weId, 1)));
      emit({ t: 'addExercise', workoutId: wk.id, we: { id: weId, exerciseId: exId, planned, note: item.notes ?? '', sets: rows } }, `${iso(nameOf(exId))} נוסף לאימון`);
    }
    for (const n of d.notes) {
      const we = findWe(exerciseIdOf(n.exercise));
      if (we && we.note !== n.note) emit({ t: 'setNote', weId: we.id, note: n.note }, `${iso(nameOf(we.exerciseId))} · ההערה עודכנה`);
    }
    snaps.set(date, snapshotOf(sw));
  }

  async function refetch(date: string) {
    try {
      const r = await req<SWorkout>('GET', dateUrl(date));
      absorb(date, r.data);
    } catch {
      /* best effort: the save is reported as failed anyway */
    }
  }
  /** Re-read after a 412 so the fresh value is visible, then fail the save. */
  async function stale(date: string, e: unknown): Promise<never> {
    if (e instanceof ApiError && e.status === 412) {
      await refetch(date);
      throw new ApiError(412, e.code, STALE_MSG);
    }
    throw e;
  }
  const guardRemote = (sid: number, seen: number | undefined) => {
    if ((remoteVer.get(sid) ?? 0) !== (seen ?? 0)) throw new ApiError(412, 'precondition_failed', STALE_MSG);
  };

  // ---------- helpers over the mirror ----------
  function weLocation(data: AppData, weId: string) {
    for (const w of data.workouts) {
      const we = w.exercises.find((e) => e.id === weId);
      if (we) return { workout: w, we };
    }
    return null;
  }
  function prescribed(date: string, exId: string) {
    return snaps.get(date)?.progress.find((p) => p.target_sets > 0 && exerciseIdOf(p.exercise) === exId);
  }
  function isDuplicate(data: AppData, m: Mutation) {
    if (m.t === 'addSet') return !!findSet(data, m.set.id);
    if (m.t === 'addExercise') return !!weLocation(data, m.we.id);
    if (m.t === 'addLibraryExercise') return data.exercises.some((e) => e.id === m.exercise.id);
    return false;
  }

  async function postSets(date: string, body: object, key?: string): Promise<SBatchResult> {
    const r = await req<SBatchResult>('POST', `${dateUrl(date)}/sets`, { json: body, headers: key ? { 'Idempotency-Key': key } : {} });
    for (const ne of r.data.new_exercises ?? []) if (ne.similar?.length) notice(`נוצר תרגיל חדש "${titleCase(ne.name)}". שמות דומים שכבר קיימים: ${ne.similar.map(titleCase).join(', ')}`);
    return r.data;
  }
  /** Map sets this client just created, then fold the returned workout in. */
  function ownSets(date: string, res: SBatchResult, clientIds: { id: string; exId: string }[]) {
    res.sets.forEach((s, i) => {
      const c = clientIds[i];
      if (!c) return;
      registerSet(c.id, s);
      if (!exS.has(c.exId)) registerExercise({ id: s.exercise_id, name: s.exercise, aliases: [], set_count: 0 }, c.exId);
      snaps.get(date)?.sets.set(s.id, s);
    });
    absorb(date, res.workout);
  }

  // ---------- mutations ----------
  function prepare(m: Mutation, before: AppData, after: AppData): () => Promise<void> {
    const local = () => Promise.resolve();
    switch (m.t) {
      case 'createWorkout': {
        const { workout } = m;
        const date = workout.date;
        if (before.workouts.some((w) => w.date === date && w.id !== workout.id)) return local; // one workout per date
        setStartedAt(date, workout.startedAt);
        const planned = workout.exercises.filter((we) => we.planned);
        return async () => {
          if (snaps.has(date)) return; // the server already has it (a retry after success, or loaded)
          const info = workout.planId ? plans.get(workout.planId) : undefined;
          let sw: SWorkout;
          if (info) sw = (await req<SWorkout>('POST', `${dateUrl(date)}/plan`, { json: { plan_id: info.id } })).data;
          else if (planned.length) sw = (await postSets(date, { prescriptions: planned.map((we) => prescriptionInput(canonOf(we.exerciseId), we.planned!)) })).workout;
          else sw = (await req<SWorkout>('PUT', dateUrl(date))).data;
          // The workout may already have had sets (e.g. logged through the assistant): show them.
          if (!snaps.has(date)) snaps.set(date, { ...emptySnapshot(), progress: sw.progress.filter((p) => workout.exercises.some((we) => we.exerciseId === exerciseIdOf(p.exercise))) });
          absorb(date, sw);
        };
      }

      case 'addExercise': {
        const loc = weLocation(after, m.we.id);
        if (!loc) return local;
        const date = loc.workout.date;
        const exId = m.we.exerciseId;
        const done = m.we.sets.filter((s) => s.done);
        if (!m.we.planned && !done.length) {
          setPending(date, canonOf(exId), true); // UI-only until its first set is completed
          if (m.we.note) setLocalNote(date, canonOf(exId), m.we.note);
          return local;
        }
        return async () => {
          const body: Record<string, unknown> = {};
          if (m.we.planned) body.prescriptions = [prescriptionInput(canonOf(exId), m.we.planned, m.we.note)];
          if (done.length) body.sets = done.map((s) => setInput(exRef(exId), s));
          const res = await postSets(date, body, done.length ? idemKey(m, m.we.id) : undefined);
          ownSets(date, res, done.map((s) => ({ id: s.id, exId })));
          if (!m.we.planned) {
            setPending(date, canonOf(exId), false);
            if (m.we.note) setLocalNote(date, canonOf(exId), m.we.note);
          }
        };
      }

      case 'removeExercise': {
        const loc = weLocation(before, m.weId);
        if (!loc) return local;
        const date = loc.workout.date;
        const exId = loc.we.exerciseId;
        setPending(date, canonOf(exId), false);
        setLocalNote(date, canonOf(exId), '');
        const rows = loc.we.sets.map((s) => ({ id: s.id, seen: setS.has(s.id) ? remoteVer.get(setS.get(s.id)!) : undefined }));
        return async () => {
          try {
            for (const row of rows) {
              const sid = setS.get(row.id);
              if (!sid) continue;
              guardRemote(sid, row.seen);
              const r = await req<{ workout: SWorkout | null }>('DELETE', `/sets/${sid}`, { headers: ifMatch(setRev.get(sid)!) });
              snaps.get(date)?.sets.delete(sid);
              unregisterSet(sid);
              if (r.data.workout) absorb(date, r.data.workout);
            }
            const item = prescribed(date, exId);
            if (item) {
              const fresh = (await req<SWorkout>('GET', dateUrl(date))).data;
              absorb(date, fresh);
              const r = await req<SWorkout>('DELETE', `${dateUrl(date)}/prescriptions/${encodeURIComponent(item.exercise)}`, { headers: ifMatch(fresh.revision) });
              snaps.set(date, snapshotOf(r.data));
            }
          } catch (e) {
            return stale(date, e);
          }
        };
      }

      case 'setNote': {
        const loc = weLocation(after, m.weId);
        if (!loc) return local;
        const date = loc.workout.date;
        const exId = loc.we.exerciseId;
        const item = prescribed(date, exId);
        if (!item) {
          // The engine keeps notes on prescriptions only. An exercise that is not prescribed in this
          // workout keeps its note in this browser (localStorage), not on the server.
          setLocalNote(date, canonOf(exId), m.note);
          return local;
        }
        const seenNote = item.notes ?? '';
        return async () => {
          try {
            const fresh = (await req<SWorkout>('GET', dateUrl(date))).data;
            const now = fresh.progress.find((p) => normName(p.exercise) === normName(item.exercise));
            if ((now?.notes ?? '') !== seenNote) {
              absorb(date, fresh);
              throw new ApiError(412, 'precondition_failed', STALE_MSG);
            }
            const r = await req<SWorkout>('PATCH', `${dateUrl(date)}/prescriptions/${encodeURIComponent(item.exercise)}`, {
              json: { notes: m.note || null }, headers: { ...ifMatch(fresh.revision), 'Content-Type': 'application/merge-patch+json' },
            });
            absorb(date, fresh);
            snaps.set(date, snapshotOf(r.data));
          } catch (e) {
            return stale(date, e);
          }
        };
      }

      case 'addSet': {
        if (!m.set.done) return local; // an empty row stays local until it is completed
        const loc = weLocation(after, m.weId);
        if (!loc) return local;
        const date = loc.workout.date;
        const exId = loc.we.exerciseId;
        const set = m.set;
        return async () => {
          const res = await postSets(date, { sets: [setInput(exRef(exId), set)] }, idemKey(m, set.id));
          ownSets(date, res, [{ id: set.id, exId }]);
          setPending(date, canonOf(exId), false);
        };
      }

      case 'updateSet': {
        const f = findSet(after, m.setId);
        if (!f) return local;
        const date = f.workout.date;
        const exId = f.we.exerciseId;
        const values = f.set;
        const sidNow = setS.get(m.setId);
        const seen = sidNow != null ? remoteVer.get(sidNow) : undefined;
        return async () => {
          const sid = setS.get(m.setId); // resolved when the job runs: an earlier queued save may have mapped it
          try {
            if (sid == null) {
              if (m.patch.done !== true) return; // draft row: stays local
              const res = await postSets(date, { sets: [setInput(exRef(exId), values)] }, idemKey(m, m.setId));
              ownSets(date, res, [{ id: m.setId, exId }]);
              setPending(date, canonOf(exId), false);
              return;
            }
            guardRemote(sid, sid === sidNow ? seen : undefined);
            if (m.patch.done === false) {
              // Engine sets are performed sets: "not done" removes it there; the row stays here as a draft.
              const r = await req<{ workout: SWorkout | null }>('DELETE', `/sets/${sid}`, { headers: ifMatch(setRev.get(sid)!) });
              snaps.get(date)?.sets.delete(sid);
              unregisterSet(sid);
              if (r.data.workout) absorb(date, r.data.workout);
              const we = weLocation(mirror, f.we.id)?.we;
              if (we && !we.planned && !we.sets.some((s) => setS.has(s.id))) setPending(date, canonOf(exId), true);
              return;
            }
            const body = setPatchBody(m.patch, setType.get(sid));
            if (!Object.keys(body).length) return;
            const r = await req<SSet>('PATCH', `/sets/${sid}`, { json: body, headers: { ...ifMatch(setRev.get(sid)!), 'Content-Type': 'application/merge-patch+json' } });
            setRev.set(sid, r.data.revision);
            setType.set(sid, r.data.set_type);
            const snap = snaps.get(date);
            if (snap?.sets.has(sid)) snap.sets.set(sid, { ...snap.sets.get(sid)!, ...r.data });
          } catch (e) {
            return stale(date, e);
          }
        };
      }

      case 'removeSet': {
        const f = findSet(before, m.setId);
        if (!f) return local;
        const date = f.workout.date;
        const sidNow = setS.get(m.setId);
        const seen = sidNow != null ? remoteVer.get(sidNow) : undefined;
        return async () => {
          const sid = setS.get(m.setId);
          if (sid == null) return;
          try {
            guardRemote(sid, sid === sidNow ? seen : undefined);
            const r = await req<{ workout: SWorkout | null }>('DELETE', `/sets/${sid}`, { headers: ifMatch(setRev.get(sid)!) });
            snaps.get(date)?.sets.delete(sid);
            unregisterSet(sid);
            if (r.data.workout) absorb(date, r.data.workout);
          } catch (e) {
            return stale(date, e);
          }
        };
      }

      case 'savePlan': {
        const plan = m.plan;
        return async () => {
          const info = plans.get(plan.id);
          if (!plan.exercises.length) {
            if (!info) return; // a new, empty plan stays local until its first exercise
            throw new ApiError(400, 'validation_error', 'תוכנית חייבת לכלול לפחות תרגיל אחד.');
          }
          const doc = planDocument(plan, canonOf, info ?? {});
          if (!info) {
            const r = await req<{ plan: SPlan; new_exercises: { id: number; name: string; similar?: string[] }[] }>('POST', '/plans', { json: doc });
            plans.set(plan.id, { id: r.data.plan.id, revision: r.data.plan.revision, schedule: r.data.plan.schedule, notes: r.data.plan.notes, items: r.data.plan.items });
            for (const ne of r.data.new_exercises ?? []) if (!byName.has(normName(ne.name))) byName.set(normName(ne.name), ne.id);
            return;
          }
          try {
            const r = await req<SPlan>('PUT', `/plans/${info.id}`, { json: doc, headers: ifMatch(info.revision) });
            plans.set(plan.id, { ...info, revision: r.data.revision, schedule: r.data.schedule, notes: r.data.notes, items: r.data.items });
          } catch (e) {
            if (e instanceof ApiError && e.status === 412) {
              try {
                const fresh = await req<SPlan>('GET', `/plans/${info.id}`);
                plans.set(plan.id, { ...info, revision: fresh.data.revision, schedule: fresh.data.schedule, notes: fresh.data.notes, items: fresh.data.items });
                const extra: Exercise[] = [];
                const p = { ...toClientPlan(fresh.data, (n) => exerciseIdOf(n, undefined, extra)), id: plan.id };
                for (const ex of extra) emit({ t: 'addLibraryExercise', exercise: ex }, `תרגיל חדש בספרייה: ${iso(ex.name)}`);
                emit({ t: 'savePlan', plan: p }, `התוכנית ${iso(p.name)} עודכנה במקום אחר`);
              } catch { /* keep the original error */ }
              throw new ApiError(412, e.code, STALE_MSG);
            }
            throw e;
          }
        };
      }

      case 'deletePlan': {
        return async () => {
          const info = plans.get(m.planId);
          if (!info) return;
          await req('DELETE', `/plans/${info.id}`, { headers: ifMatch(info.revision) });
          plans.delete(m.planId);
        };
      }

      case 'addLibraryExercise': {
        const ex = m.exercise;
        return async () => {
          if (exS.has(ex.id)) return;
          const r = await req<{ exercise: SExercise; created: boolean; similar: string[] }>('POST', '/exercises', { json: { name: ex.name } });
          registerExercise(r.data.exercise, ex.id);
          if (!r.data.created) notice(`"${ex.name}" כבר קיים בספרייה בשם "${titleCase(r.data.exercise.name)}"`);
          else if (r.data.similar?.length) notice(`נוסף "${ex.name}". שמות דומים שכבר קיימים: ${r.data.similar.map(titleCase).join(', ')}`);
        };
      }
    }
  }

  // ---------- polling ----------
  let polling = false;
  async function poll() {
    if (!loaded || polling || busy || document.visibilityState !== 'visible') return;
    polling = true;
    const seq = writeSeq;
    const today = config.today;
    try {
      const r = await req<SWorkout>('GET', dateUrl(today));
      if (busy || seq !== writeSeq) return; // a save started meanwhile; the next poll will catch up
      if (snaps.get(today)?.revision === r.data.revision && mirror.workouts.some((w) => w.date === today)) return;
      absorb(today, r.data);
    } catch {
      /* 404 = no workout yet; network errors: try again next tick */
    } finally {
      polling = false;
    }
  }

  // ---------- load ----------
  async function load(): Promise<AppData> {
    loaded = false;
    reset();
    const [cfg, exs, planList] = await Promise.all([
      req<SConfig>('GET', '/config'),
      req<{ exercises: SExercise[] }>('GET', '/exercises'),
      req<{ plans: SPlan[] }>('GET', '/plans'),
    ]);
    config = cfg.data;
    const today = config.today;
    const meta = exerciseMeta();
    const exercises: Exercise[] = exs.data.exercises.map((se) => {
      registerExercise(se);
      return toClientExercise(se, meta[String(se.id)]);
    });
    const [planDetails, summaries] = await Promise.all([
      pool(planList.data.plans, 6, (p) => req<SPlan>('GET', `/plans/${p.id}`)),
      req<{ workouts: SWorkoutSummary[] }>('GET', `/workouts?from=${addDays(today, -70)}&to=${today}&limit=100`),
    ]);
    const serverWorkouts = (await pool(summaries.data.workouts, 6, (w) => req<SWorkout>('GET', dateUrl(w.date)))).map((r) => r.data);
    const extra: Exercise[] = [];
    const exId = (n: string, sid?: number) => exerciseIdOf(n, sid, extra);
    const plansOut: Plan[] = planDetails.map((r) => {
      const sp = r.data;
      plans.set(String(sp.id), { id: sp.id, revision: sp.revision, schedule: sp.schedule, notes: sp.notes, items: sp.items });
      return toClientPlan(sp, exId);
    });
    const workouts: Workout[] = serverWorkouts.map((sw) => {
      for (const s of sw.sets) registerSet(String(s.id), s);
      snaps.set(sw.date, snapshotOf(sw));
      return toClientWorkout(sw, exId, localFor(sw.date));
    });
    exercises.push(...extra);

    // How-to text and demo videos from the engine's exercise guides.
    const guides = await pool(exercises.filter((e) => exS.has(e.id)), 6, (e) => req<SGuide>('GET', `/exercises/${exS.get(e.id)}/guide`).then((r) => r.data, () => null));
    guides.forEach((g) => {
      if (!g) return;
      const ex = exercises.find((e) => e.id === exC.get(g.exercise_id));
      if (!ex) return;
      if (!ex.howTo) ex.howTo = howToFromGuide(g.instructions);
      if (g.videos[0]) ex.demoVideo = mediaUrl(g.videos[0].id);
    });

    // The user's own set videos: only for today's workout, to keep the request count small.
    const tw = workouts.find((w) => w.date === today);
    if (tw && config.media.enabled) {
      const ids = tw.exercises.flatMap((we) => we.sets.filter((s) => setS.has(s.id)).map((s) => s.id));
      const vids = await pool(ids, 6, (id) => req<{ videos: SMedia[] }>('GET', `/sets/${setS.get(id)}/videos`).then((r) => r.data.videos, () => []));
      ids.forEach((id, i) => {
        const v = vids[i][0];
        if (!v) return;
        setMedia.set(setS.get(id)!, v.id);
        for (const we of tw.exercises) for (const s of we.sets) if (s.id === id) s.video = { url: mediaUrl(v.id), name: v.filename };
      });
    }
    mirror = { today, exercises, plans: plansOut, workouts };
    loaded = true;
    return mirror;
  }

  // ---------- the interface ----------
  return {
    load,
    apply(m) {
      const before = mirror;
      if (!isDuplicate(before, m)) mirror = applyMutation(mirror, m);
      if (m.t === 'updateSet' && 'video' in m.patch && Object.keys(m.patch).every((k) => k === 'video' || k === 'by')) return Promise.resolve(); // videos go through uploadVideo/deleteVideo
      const job = prepare(m, before, mirror);
      return enqueue(job);
    },
    subscribe(cb) {
      listeners.add(cb);
      const tick = () => void poll();
      const id = window.setInterval(tick, POLL_MS);
      const onVis = () => document.visibilityState === 'visible' && tick();
      document.addEventListener('visibilitychange', onVis);
      window.addEventListener('focus', tick);
      return () => {
        listeners.delete(cb);
        window.clearInterval(id);
        document.removeEventListener('visibilitychange', onVis);
        window.removeEventListener('focus', tick);
      };
    },
    subscribeNotices(cb) {
      noticeListeners.add(cb);
      return () => noticeListeners.delete(cb);
    },
    uploadVideo(setId, file) {
      if (!config.media.enabled) return Promise.reject(new ApiError(501, 'media_disabled', 'שמירת סרטונים לא זמינה בשרת הזה.'));
      if (config.media.max_video_bytes && file.size > config.media.max_video_bytes) {
        return Promise.reject(new ApiError(413, 'payload_too_large', `הסרטון גדול מדי (עד ${Math.floor(config.media.max_video_bytes / 1048576)}MB)`));
      }
      return enqueue(async () => {
        const sid = setS.get(setId);
        if (sid == null) throw new ApiError(400, 'draft_set', 'סמן את הסט כהושלם לפני צירוף סרטון. רק סט שבוצע נשמר בשרת.');
        const r = await req<SMedia>('POST', `/sets/${sid}/videos?filename=${encodeURIComponent(file.name.slice(0, 120))}`, { body: file, headers: { 'Content-Type': file.type || 'application/octet-stream' } });
        const prev = setMedia.get(sid);
        setMedia.set(sid, r.data.id);
        if (prev) await req('DELETE', `/media/${prev}`).catch(() => undefined); // "replace" keeps one video per set
        return { url: mediaUrl(r.data.id), name: r.data.filename };
      });
    },
    deleteVideo(setId) {
      return enqueue(async () => {
        const sid = setS.get(setId);
        const mid = sid != null ? setMedia.get(sid) : undefined;
        if (mid == null) return;
        try {
          await req('DELETE', `/media/${mid}`);
        } catch (e) {
          if (!(e instanceof ApiError && e.status === 404)) throw e;
        }
        if (sid != null && setMedia.get(sid) === mid) setMedia.delete(sid);
      });
    },
    capabilities: { videoNeedsDoneSet: true },
  };
}

export type { WorkoutExercise };
