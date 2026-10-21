import { useSyncExternalStore } from 'react';

const state = { autoRest: true };
const subs = new Set<() => void>();
let snap = { ...state };
export const prefs = () => snap;
export function setPref<K extends keyof typeof state>(k: K, v: (typeof state)[K]) {
  snap = { ...snap, [k]: v };
  subs.forEach((s) => s());
}
export const usePrefs = () => useSyncExternalStore((cb) => (subs.add(cb), () => subs.delete(cb)), prefs);
