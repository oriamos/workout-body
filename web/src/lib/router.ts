import { useSyncExternalStore } from 'react';

export type Tab = 'today' | 'plans' | 'exercises' | 'history' | 'progress' | 'nutrition' | 'weight';
export type Route =
  | { name: 'today'; week?: string } // home: the week view; `week` = first day of a week other than the current one
  | { name: 'workout'; date?: string } // the day's workout logging view (default: today)
  | { name: 'plans'; id?: string }
  | { name: 'history'; date?: string }
  | { name: 'progress' }
  | { name: 'weight' }
  | { name: 'nutrition'; date?: string }
  | { name: 'exercises' }
  | { name: 'exercise'; id: string };

const isDate = (s: string | undefined): s is string => /^\d{4}-\d{2}-\d{2}$/.test(s ?? '');

export function parse(hash: string): Route {
  const parts = hash.replace(/^#\/?/, '').split('/').filter(Boolean);
  switch (parts[0]) {
    case 'plans':
      return { name: 'plans', id: parts[1] };
    case 'history':
      return { name: 'history', date: parts[1] };
    case 'progress':
      return { name: 'progress' };
    case 'weight':
      return { name: 'weight' };
    case 'nutrition':
      return { name: 'nutrition', date: /^\d{4}-\d{2}-\d{2}$/.test(parts[1] ?? '') ? parts[1] : undefined };
    case 'exercises':
      return { name: 'exercises' };
    case 'exercise':
      return parts[1] ? { name: 'exercise', id: parts[1] } : { name: 'today' };
    case 'workout':
      return { name: 'workout', date: isDate(parts[1]) ? parts[1] : undefined };
    default:
      return { name: 'today', week: parts[0] === 'today' && isDate(parts[1]) ? parts[1] : undefined };
  }
}

export const href = (r: Route): string => {
  switch (r.name) {
    case 'today': return r.week ? `#/today/${r.week}` : '#/today';
    case 'workout': return r.date ? `#/workout/${r.date}` : '#/workout';
    case 'plans': return r.id ? `#/plans/${r.id}` : '#/plans';
    case 'history': return r.date ? `#/history/${r.date}` : '#/history';
    case 'progress': return '#/progress';
    case 'weight': return '#/weight';
    case 'nutrition': return r.date ? `#/nutrition/${r.date}` : '#/nutrition';
    case 'exercises': return '#/exercises';
    case 'exercise': return `#/exercise/${r.id}`;
  }
};

const listeners = new Set<() => void>();
let snapshot = window.location.hash;
const emit = () => {
  snapshot = window.location.hash;
  listeners.forEach((l) => l());
};
window.addEventListener('popstate', emit);
window.addEventListener('hashchange', emit);

const subscribe = (cb: () => void) => (listeners.add(cb), () => listeners.delete(cb));
const getHash = () => snapshot;

export function useRoute(): Route {
  return parse(useSyncExternalStore(subscribe, getHash));
}

/** The tab a route belongs to (a day's workout opens from the week, the home tab). */
const tabOf = (r: Route): Tab => (r.name === 'workout' ? 'today' : r.name === 'exercise' ? 'progress' : r.name);

/** Tab to highlight. Exercise detail inherits the tab it was opened from. */
export function useActiveTab(): Tab {
  const r = useRoute();
  useSyncExternalStore(subscribe, getHash);
  if (r.name === 'exercise') {
    const o = (window.history.state as { origin?: Tab } | null)?.origin;
    return o ?? 'progress';
  }
  return tabOf(r);
}

export function navigate(r: Route, opts: { replace?: boolean } = {}) {
  const cur = parse(window.location.hash);
  const origin: Tab = cur.name === 'exercise' ? ((window.history.state as { origin?: Tab } | null)?.origin ?? 'progress') : tabOf(cur);
  const state = { origin: r.name === 'exercise' ? origin : tabOf(r), depth: ((window.history.state as { depth?: number } | null)?.depth ?? 0) + 1 };
  const url = href(r);
  if (url === window.location.hash && !opts.replace) return emit(); // already there: no duplicate history entry
  if (opts.replace) window.history.replaceState({ ...state, depth: state.depth - 1 }, '', url);
  else window.history.pushState(state, '', url);
  emit();
}

/** Go back if the app itself pushed the current entry, otherwise to `fallback`. */
export function goBack(fallback: Route) {
  const d = (window.history.state as { depth?: number } | null)?.depth ?? 0;
  if (d > 0) window.history.back();
  else navigate(fallback, { replace: true });
}
