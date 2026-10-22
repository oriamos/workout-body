import { useSyncExternalStore } from 'react';

export type Tab = 'today' | 'plans' | 'exercises' | 'history' | 'progress';
export type Route =
  | { name: 'today' }
  | { name: 'plans'; id?: string }
  | { name: 'history'; date?: string }
  | { name: 'progress' }
  | { name: 'exercises' }
  | { name: 'exercise'; id: string };

export function parse(hash: string): Route {
  const parts = hash.replace(/^#\/?/, '').split('/').filter(Boolean);
  switch (parts[0]) {
    case 'plans':
      return { name: 'plans', id: parts[1] };
    case 'history':
      return { name: 'history', date: parts[1] };
    case 'progress':
      return { name: 'progress' };
    case 'exercises':
      return { name: 'exercises' };
    case 'exercise':
      return parts[1] ? { name: 'exercise', id: parts[1] } : { name: 'today' };
    default:
      return { name: 'today' };
  }
}

export const href = (r: Route): string => {
  switch (r.name) {
    case 'today': return '#/today';
    case 'plans': return r.id ? `#/plans/${r.id}` : '#/plans';
    case 'history': return r.date ? `#/history/${r.date}` : '#/history';
    case 'progress': return '#/progress';
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

/** Tab to highlight. Exercise detail inherits the tab it was opened from. */
export function useActiveTab(): Tab {
  const r = useRoute();
  useSyncExternalStore(subscribe, getHash);
  if (r.name === 'exercise') {
    const o = (window.history.state as { origin?: Tab } | null)?.origin;
    return o ?? 'progress';
  }
  return r.name;
}

export function navigate(r: Route, opts: { replace?: boolean } = {}) {
  const cur = parse(window.location.hash);
  const origin: Tab = cur.name === 'exercise' ? ((window.history.state as { origin?: Tab } | null)?.origin ?? 'progress') : cur.name;
  const state = { origin: r.name === 'exercise' ? origin : r.name, depth: ((window.history.state as { depth?: number } | null)?.depth ?? 0) + 1 };
  const url = href(r);
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
