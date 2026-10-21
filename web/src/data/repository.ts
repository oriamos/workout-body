// The only module the UI talks to for data. It is a mock: state lives in memory.
// A real integration replaces `createMockRepository` with a client that reads and writes
// the workout engine and pushes remote changes through `subscribe`.
import type { AppData } from './types';
import { buildSeed } from './seed';
import type { ExternalUpdate, Mutation } from './mutations';

export interface WorkoutRepository {
  load(): Promise<AppData>;
  /** Resolve when the change is stored; reject to surface a failed save. */
  apply(m: Mutation): Promise<void>;
  /** Changes made elsewhere (e.g. through the assistant). Returns an unsubscribe function. */
  subscribe(cb: (u: ExternalUpdate) => void): () => void;
}

/** Demo-only switches. Not part of the real interface. */
export interface MockControls {
  failNextSave: boolean;
  failNextLoad: boolean;
  slowLoad: boolean;
  emit(u: ExternalUpdate): void;
}

export function createMockRepository(): { repo: WorkoutRepository; controls: MockControls } {
  const listeners = new Set<(u: ExternalUpdate) => void>();
  const controls: MockControls = {
    failNextSave: false,
    failNextLoad: false,
    slowLoad: false,
    emit: (u) => listeners.forEach((l) => l(u)),
  };
  const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
  const repo: WorkoutRepository = {
    async load() {
      await wait(controls.slowLoad ? 1800 : 450);
      if (controls.failNextLoad) {
        controls.failNextLoad = false;
        throw new Error('load failed');
      }
      return buildSeed();
    },
    async apply() {
      await wait(420);
      if (controls.failNextSave) {
        controls.failNextSave = false;
        throw new Error('save failed');
      }
    },
    subscribe(cb) {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
  };
  return { repo, controls };
}

export const { repo, controls: mockControls } = createMockRepository();
