import { useSyncExternalStore } from 'react';

export type Theme = 'light' | 'dark';
const stored = (() => { try { return localStorage.getItem('theme'); } catch { return null; } })();
const state: { autoRest: boolean; theme: Theme } = { autoRest: true, theme: stored === 'dark' ? 'dark' : 'light' };
const subs = new Set<() => void>();
let snap = { ...state };
export const prefs = () => snap;
export function setPref<K extends keyof typeof state>(k: K, v: (typeof state)[K]) {
  snap = { ...snap, [k]: v };
  if (k === 'theme') { try { localStorage.setItem('theme', String(v)); } catch { /* private mode */ } }
  subs.forEach((s) => s());
}
export const usePrefs = () => useSyncExternalStore((cb) => (subs.add(cb), () => subs.delete(cb)), prefs);
