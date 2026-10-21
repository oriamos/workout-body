import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef, type ReactNode } from 'react';
import type { AppData, Plan, SetEntry, Workout, WorkoutExercise } from '../data/types';
import { applyMutation, findSet, type ExternalUpdate, type Mutation } from '../data/mutations';
import { mockControls, repo } from '../data/repository';
import { IS_MOCK } from '../data/source';
import { buildSeed, buildTodayInProgress, TODAY } from '../data/seed';
import { parseNum } from '../lib/format';
import { exerciseById } from '../lib/stats';

type Field = 'weight' | 'reps';
export interface Conflict { setId: string; field: Field; mine: string; theirs: number | null }
export interface Toast { id: number; text: string; tone?: 'info' | 'assistant' | 'error'; action?: { label: string; run: () => void } }

interface State {
  load: 'loading' | 'ready' | 'error';
  data: AppData;
  drafts: Record<string, Partial<Record<Field, string>>>;
  errors: Record<string, string>; // `${setId}:${field}`
  conflicts: Record<string, Conflict[]>;
  fresh: Record<string, number>;
  pending: Record<string, 'saving' | 'failed'>;
  failed: Record<string, Mutation>;
  lastExternal: { at: number; summary: string } | null;
  toasts: Toast[];
}

type Action =
  | { t: 'loading' }
  | { t: 'loaded'; data: AppData }
  | { t: 'loadError' }
  | { t: 'mutate'; m: Mutation; clearDraft?: string }
  | { t: 'external'; u: ExternalUpdate }
  | { t: 'draft'; setId: string; field: Field; value: string }
  | { t: 'clearDraft'; setId: string; field?: Field }
  | { t: 'error'; key: string; msg: string | null }
  | { t: 'resolve'; setId: string; field: Field; choice: 'mine' | 'theirs' }
  | { t: 'pending'; key: string; status: 'saving' | 'failed' | null }
  | { t: 'failed'; key: string; m: Mutation | null }
  | { t: 'toast'; toast: Toast }
  | { t: 'dismissToast'; id: number }
  | { t: 'clearFresh'; id: string }
  | { t: 'resetToday'; workout: Workout | null };

const initial: State = { load: 'loading', data: { today: TODAY, exercises: [], plans: [], workouts: [] }, drafts: {}, errors: {}, conflicts: {}, fresh: {}, pending: {}, failed: {}, lastExternal: null, toasts: [] };

function omit<T extends object>(o: T, k: string): T {
  const { [k]: _, ...rest } = o as Record<string, unknown>;
  return rest as T;
}
let toastId = 1;

function reducer(s: State, a: Action): State {
  switch (a.t) {
    case 'loading': return { ...initial, load: 'loading', toasts: s.toasts };
    case 'loaded': return { ...s, load: 'ready', data: a.data };
    case 'loadError': return { ...s, load: 'error' };
    case 'mutate': {
      const next = { ...s, data: applyMutation(s.data, a.m) };
      if (a.clearDraft) next.drafts = omit(s.drafts, a.clearDraft);
      return next;
    }
    case 'external': {
      const m = a.u.mutation;
      let data = s.data;
      let conflicts = s.conflicts;
      let fresh = { ...s.fresh };
      if (m.t === 'updateSet') {
        const draft = s.drafts[m.setId] ?? {};
        const patch = { ...m.patch, by: 'assistant' as const };
        const found = findSet(s.data, m.setId);
        const list: Conflict[] = [];
        for (const f of ['weight', 'reps'] as Field[]) {
          if (f in m.patch && draft[f] !== undefined && found && String(found.set[f] ?? '') !== draft[f]!.trim()) {
            list.push({ setId: m.setId, field: f, mine: draft[f]!, theirs: (m.patch[f] ?? null) as number | null });
            delete patch[f];
          }
        }
        data = applyMutation(data, { ...m, patch });
        if (list.length) conflicts = { ...conflicts, [m.setId]: [...(conflicts[m.setId] ?? []).filter((c) => !list.some((l) => l.field === c.field)), ...list] };
        fresh[m.setId] = a.u.at;
      } else {
        data = applyMutation(data, m);
        if (m.t === 'addSet') fresh[m.set.id] = a.u.at;
      }
      return { ...s, data, conflicts, fresh, lastExternal: { at: a.u.at, summary: a.u.summary } };
    }
    case 'draft': return { ...s, drafts: { ...s.drafts, [a.setId]: { ...s.drafts[a.setId], [a.field]: a.value } }, errors: omit(s.errors, `${a.setId}:${a.field}`) };
    case 'clearDraft': {
      if (!a.field) return { ...s, drafts: omit(s.drafts, a.setId) };
      const d = { ...s.drafts[a.setId] };
      delete d[a.field];
      return { ...s, drafts: Object.keys(d).length ? { ...s.drafts, [a.setId]: d } : omit(s.drafts, a.setId), errors: omit(s.errors, `${a.setId}:${a.field}`) };
    }
    case 'error': return { ...s, errors: a.msg ? { ...s.errors, [a.key]: a.msg } : omit(s.errors, a.key) };
    case 'resolve': {
      const list = (s.conflicts[a.setId] ?? []).filter((c) => c.field !== a.field);
      const c = (s.conflicts[a.setId] ?? []).find((x) => x.field === a.field);
      const conflicts = list.length ? { ...s.conflicts, [a.setId]: list } : omit(s.conflicts, a.setId);
      if (a.choice === 'theirs' && c) {
        const d = { ...s.drafts[a.setId] };
        delete d[a.field];
        return { ...s, conflicts, data: applyMutation(s.data, { t: 'updateSet', setId: a.setId, patch: { [a.field]: c.theirs, by: 'assistant' } }), drafts: Object.keys(d).length ? { ...s.drafts, [a.setId]: d } : omit(s.drafts, a.setId) };
      }
      return { ...s, conflicts };
    }
    case 'pending': return { ...s, pending: a.status ? { ...s.pending, [a.key]: a.status } : omit(s.pending, a.key) };
    case 'failed': return { ...s, failed: a.m ? { ...s.failed, [a.key]: a.m } : omit(s.failed, a.key) };
    case 'toast': return { ...s, toasts: [...s.toasts.slice(-2), a.toast] };
    case 'dismissToast': return { ...s, toasts: s.toasts.filter((t) => t.id !== a.id) };
    case 'clearFresh': return { ...s, fresh: omit(s.fresh, a.id) };
    case 'resetToday': return { ...s, drafts: {}, errors: {}, conflicts: {}, fresh: {}, data: { ...s.data, workouts: [...s.data.workouts.filter((w) => w.date !== s.data.today), ...(a.workout ? [a.workout] : [])] } };
  }
}

const uid = (p: string) => `${p}-${Math.random().toString(36).slice(2, 8)}`;
export const blankSet = (type: SetEntry['type'] = 'working'): SetEntry => ({ id: uid('s'), type, weight: null, reps: null, rir: null, rpe: null, done: false });

export function validateField(field: Field, raw: string): { ok: true; value: number | null } | { ok: false; msg: string } {
  const n = parseNum(raw);
  if (n === null) return { ok: true, value: null };
  if (Number.isNaN(n)) return { ok: false, msg: field === 'weight' ? 'המשקל חייב להיות מספר' : 'חזרות חייבות להיות מספר שלם' };
  if (field === 'weight') {
    if (n < 0 || n > 500) return { ok: false, msg: 'משקל תקין: 0 עד 500 ק״ג' };
    if (Math.round(n * 4) !== n * 4) return { ok: false, msg: 'המשקל בקפיצות של 0.25 ק״ג' };
  } else {
    if (!Number.isInteger(n)) return { ok: false, msg: 'חזרות חייבות להיות מספר שלם' };
    if (n < 0 || n > 100) return { ok: false, msg: 'חזרות: 0 עד 100' };
  }
  return { ok: true, value: n };
}

export const SCENARIOS = ['empty', 'progress', 'completed'] as const;
export type Scenario = (typeof SCENARIOS)[number];

function useStoreImpl() {
  const [state, dispatch] = useReducer(reducer, initial);
  const ref = useRef(state);
  ref.current = state;
  const keyN = useRef(0);

  const load = useCallback(() => {
    dispatch({ t: 'loading' });
    repo.load().then((data) => dispatch({ t: 'loaded', data }), () => dispatch({ t: 'loadError' }));
  }, []);
  useEffect(load, [load]);

  const toast = useCallback((text: string, tone: Toast['tone'] = 'info', action?: Toast['action']) => {
    const id = toastId++;
    dispatch({ t: 'toast', toast: { id, text, tone, action } });
    window.setTimeout(() => dispatch({ t: 'dismissToast', id }), action ? 6000 : 3800);
  }, []);

  const persist = useCallback((m: Mutation, key: string) => {
    dispatch({ t: 'pending', key, status: 'saving' });
    repo.apply(m).then(
      () => {
        dispatch({ t: 'pending', key, status: null });
        dispatch({ t: 'failed', key, m: null });
      },
      (err: unknown) => {
        dispatch({ t: 'pending', key, status: 'failed' });
        dispatch({ t: 'failed', key, m });
        // The engine explains why (validation, stale revision, ...). The mock's failures are silent by design.
        if (!IS_MOCK && err instanceof Error && err.message) toast(err.message, 'error');
      },
    );
  }, [toast]);

  const mutate = useCallback((m: Mutation, key?: string, clearDraft?: string) => {
    dispatch({ t: 'mutate', m, clearDraft });
    persist(m, key ?? `k${keyN.current++}`);
  }, [persist]);

  useEffect(() => repo.subscribeNotices?.((text) => toast(text, 'info')), [toast]);

  // Incoming changes from elsewhere. Never touches focus or open edits.
  useEffect(() => {
    return repo.subscribe((u) => {
      dispatch({ t: 'external', u });
      toast(u.summary, 'assistant');
      window.setTimeout(() => {
        const m = u.mutation;
        if (m.t === 'updateSet') dispatch({ t: 'clearFresh', id: m.setId });
        if (m.t === 'addSet') dispatch({ t: 'clearFresh', id: m.set.id });
      }, 2600);
    });
  }, [toast]);

  const actions = useMemo(() => {
    const get = () => ref.current;
    const todayWorkout = () => get().data.workouts.find((w) => w.date === get().data.today) ?? null;

    function commitField(setId: string, field: Field): boolean {
      const raw = get().drafts[setId]?.[field];
      if (raw === undefined) return true;
      const r = validateField(field, raw);
      if (!r.ok) {
        dispatch({ t: 'error', key: `${setId}:${field}`, msg: r.msg });
        return false;
      }
      if (get().conflicts[setId]?.some((c) => c.field === field)) return true; // wait for resolution
      mutate({ t: 'updateSet', setId, patch: { [field]: r.value, by: 'me' } }, setId);
      dispatch({ t: 'clearDraft', setId, field });
      // Enter commits and then blurs, which commits again before React re-renders: drop the draft from
      // the ref now so the same edit is not saved twice.
      const d = { ...get().drafts[setId] };
      delete d[field];
      ref.current = { ...get(), drafts: { ...get().drafts, [setId]: d } };
      return true;
    }

    return {
      reload: load,
      toast,
      draft: (setId: string, field: Field, value: string) => dispatch({ t: 'draft', setId, field, value }),
      revert: (setId: string, field: Field) => dispatch({ t: 'clearDraft', setId, field }),
      commitField,
      toggleDone(setId: string, ghost: { weight: number | null; reps: number | null }): boolean {
        const found = findSet(get().data, setId);
        if (!found) return false;
        if (found.set.done) {
          // Where only performed sets are stored (the engine), un-doing a set also drops its video there.
          const dropVideo = !!found.set.video && !!repo.capabilities?.videoNeedsDoneSet;
          mutate({ t: 'updateSet', setId, patch: { done: false, by: 'me', ...(dropVideo ? { video: null } : {}) } }, setId);
          if (dropVideo) toast('הסט סומן כלא בוצע, והסרטון שלו נמחק מהשרת', 'info');
          return false;
        }
        const d = get().drafts[setId] ?? {};
        const vals: Record<Field, number | null> = { weight: found.set.weight, reps: found.set.reps };
        let bad = false;
        for (const f of ['weight', 'reps'] as Field[]) {
          if (d[f] !== undefined) {
            const r = validateField(f, d[f]!);
            if (!r.ok) { dispatch({ t: 'error', key: `${setId}:${f}`, msg: r.msg }); bad = true; continue; }
            vals[f] = r.value;
          }
          if (vals[f] == null) vals[f] = ghost[f];
        }
        if (bad) return false;
        if (vals.reps == null) { dispatch({ t: 'error', key: `${setId}:reps`, msg: 'חסרות חזרות' }); bad = true; }
        if (vals.weight == null) { dispatch({ t: 'error', key: `${setId}:weight`, msg: 'חסר משקל. אפשר להזין 0 לתרגיל בלי משקל' }); bad = true; }
        if (bad) return false;
        mutate({ t: 'updateSet', setId, patch: { weight: vals.weight, reps: vals.reps, done: true, by: 'me' } }, setId);
        dispatch({ t: 'clearDraft', setId });
        return true;
      },
      patchSet: (setId: string, patch: Partial<SetEntry>) => mutate({ t: 'updateSet', setId, patch: { ...patch, by: 'me' } }, setId),
      resolve(setId: string, field: Field, choice: 'mine' | 'theirs') {
        dispatch({ t: 'resolve', setId, field, choice });
        if (choice === 'mine') {
          const raw = get().drafts[setId]?.[field];
          const r = raw === undefined ? null : validateField(field, raw);
          if (r && r.ok) {
            mutate({ t: 'updateSet', setId, patch: { [field]: r.value, by: 'me' } }, setId);
            dispatch({ t: 'clearDraft', setId, field });
          }
        } else mutate({ t: 'updateSet', setId, patch: { [field]: get().conflicts[setId]?.find((c) => c.field === field)?.theirs ?? null, by: 'assistant' } }, setId);
      },
      attachVideo(setId: string, file: File | undefined) {
        if (!file) return;
        if (!file.type.startsWith('video/')) return toast('אפשר לצרף רק קובץ וידאו', 'error');
        if (file.size > 300 * 1024 * 1024) return toast('הסרטון גדול מדי (עד 300MB)', 'error');
        const prevUrl = findSet(get().data, setId)?.set.video?.url;
        const key = `${setId}:video`;
        dispatch({ t: 'pending', key, status: 'saving' });
        repo.uploadVideo(setId, file).then(
          (video) => {
            dispatch({ t: 'pending', key, status: null });
            if (prevUrl?.startsWith('blob:')) URL.revokeObjectURL(prevUrl);
            mutate({ t: 'updateSet', setId, patch: { video, by: 'me' } }, setId);
          },
          (err: unknown) => {
            dispatch({ t: 'pending', key, status: null });
            toast(err instanceof Error && err.message ? err.message : 'העלאת הסרטון נכשלה', 'error');
          },
        );
      },
      removeVideo(setId: string) {
        const prev = findSet(get().data, setId)?.set.video;
        if (!prev) return;
        mutate({ t: 'updateSet', setId, patch: { video: null, by: 'me' } }, setId);
        // Deleting a stored video cannot be undone, so the real delete waits until the undo window closes.
        let undone = false;
        const timer = window.setTimeout(() => {
          if (undone) return;
          repo.deleteVideo(setId).then(
            () => { if (prev.url.startsWith('blob:')) URL.revokeObjectURL(prev.url); },
            (err: unknown) => toast(err instanceof Error && err.message ? err.message : 'מחיקת הסרטון נכשלה', 'error'),
          );
        }, 6500);
        toast('הסרטון הוסר מהסט', 'info', { label: 'ביטול', run: () => { undone = true; window.clearTimeout(timer); mutate({ t: 'updateSet', setId, patch: { video: prev, by: 'me' } }, setId); } });
      },
      addSet(weId: string) {
        const set = blankSet();
        mutate({ t: 'addSet', weId, set }, set.id);
        return set.id;
      },
      removeSet(setId: string) {
        const f = findSet(get().data, setId);
        if (!f) return;
        const { set, index, we } = f;
        mutate({ t: 'removeSet', setId }, setId);
        dispatch({ t: 'clearDraft', setId });
        toast('הסט נמחק', 'info', { label: 'ביטול', run: () => mutate({ t: 'addSet', weId: we.id, set, index }, set.id) });
      },
      setNote: (weId: string, note: string) => mutate({ t: 'setNote', weId, note }),
      removeExercise(weId: string) {
        const w = get().data.workouts.find((x) => x.exercises.some((e) => e.id === weId));
        const we = w?.exercises.find((e) => e.id === weId);
        if (!w || !we) return;
        const index = w.exercises.indexOf(we);
        mutate({ t: 'removeExercise', weId });
        toast(`${exerciseById(get().data, we.exerciseId).name} הוסר`, 'info', { label: 'ביטול', run: () => {
          dispatch({ t: 'mutate', m: { t: 'addExercise', workoutId: w.id, we } });
          void index;
          persist({ t: 'addExercise', workoutId: w.id, we }, `k${keyN.current++}`);
        } });
      },
      /** Add an exercise to a workout. With no workout id, uses (and if needed creates) today's. */
      addExercise(exerciseId: string, workoutId?: string) {
        const wid = workoutId ?? actions2.ensureToday();
        const we: WorkoutExercise = { id: uid('we'), exerciseId, planned: null, note: '', sets: [blankSet()] };
        mutate({ t: 'addExercise', workoutId: wid, we });
      },
      ensureToday(): string {
        const w = todayWorkout();
        if (w) return w.id;
        const nw: Workout = { id: `w-${get().data.today}`, date: get().data.today, planId: null, name: 'אימון חופשי', startedAt: new Date().toTimeString().slice(0, 5), exercises: [] };
        mutate({ t: 'createWorkout', workout: nw });
        return nw.id;
      },
      startFromPlan(plan: Plan) {
        if (todayWorkout()) return;
        const base = buildTodayInProgress(plan, get().data.today);
        base.startedAt = new Date().toTimeString().slice(0, 5);
        mutate({ t: 'createWorkout', workout: base });
      },
      savePlan: (plan: Plan) => mutate({ t: 'savePlan', plan }),
      deletePlan: (planId: string) => mutate({ t: 'deletePlan', planId }),
      addLibraryExercise: (exercise: AppData['exercises'][number]) => mutate({ t: 'addLibraryExercise', exercise }),
      retry(key: string) {
        const m = get().failed[key];
        if (m) persist(m, key);
      },
      retryAll() {
        Object.entries(get().failed).forEach(([k, m]) => persist(m, k));
      },
      // ---- demo-only ----
      setScenario(sc: Scenario) {
        const d = get().data;
        if (sc === 'empty') return dispatch({ t: 'resetToday', workout: null });
        const seed = buildSeed();
        const w = seed.workouts.find((x) => x.date === d.today)!;
        if (sc === 'progress') return dispatch({ t: 'resetToday', workout: w });
        const done: Workout = {
          ...w,
          exercises: w.exercises.map((we) => ({
            ...we,
            sets: we.sets.map((s) => {
              if (s.done) return s;
              const w0 = s.weight ?? we.planned?.weight ?? (we.exerciseId === 'row' ? 55 : we.exerciseId === 'ohp' ? 37.5 : we.exerciseId === 'bench' ? 60 : we.exerciseId === 'facepull' ? 25 : 60);
              return { ...s, weight: w0, reps: s.reps ?? we.planned?.repMax ?? 8, done: true };
            }),
          })),
        };
        dispatch({ t: 'resetToday', workout: done });
      },
      emitExternal(u: ExternalUpdate) { mockControls.emit(u); },
      setDraftDirect: (setId: string, field: Field, value: string) => dispatch({ t: 'draft', setId, field, value }),
    };
  }, [load, mutate, persist, toast]);
  const actions2 = actions;

  return { state, actions, dispatch };
}

type Store = ReturnType<typeof useStoreImpl>;
const Ctx = createContext<Store | null>(null);
export function StoreProvider({ children }: { children: ReactNode }) {
  const s = useStoreImpl();
  return <Ctx.Provider value={s}>{children}</Ctx.Provider>;
}
export function useStore() {
  const s = useContext(Ctx);
  if (!s) throw new Error('StoreProvider missing');
  return s;
}
export function useToday(): Workout | null {
  const { state } = useStore();
  return state.data.workouts.find((w) => w.date === state.data.today) ?? null;
}
