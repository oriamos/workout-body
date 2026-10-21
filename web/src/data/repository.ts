// The only module the UI talks to for data. Two implementations:
// - `createApiRepository` (default): the workout engine's HTTP API through the same-origin proxy.
// - `createMockRepository` (VITE_DATA_SOURCE=mock): the in-memory prototype with its demo controls.
import type { AppData } from './types';
import { buildSeed } from './seed';
import type { ExternalUpdate, Mutation } from './mutations';
import { createApiRepository } from './apiRepository';
import { IS_MOCK } from './source';

export interface WorkoutRepository {
  load(): Promise<AppData>;
  /** Resolve when the change is stored; reject to surface a failed save. */
  apply(m: Mutation): Promise<void>;
  /** Changes made elsewhere (e.g. through the assistant). Returns an unsubscribe function. */
  subscribe(cb: (u: ExternalUpdate) => void): () => void;
  /** Store the user's recording of a set. Resolves with a playable URL. */
  uploadVideo(setId: string, file: File): Promise<{ url: string; name: string }>;
  /** Remove the recording of a set. */
  deleteVideo(setId: string): Promise<void>;
  /** Short informational messages for the user (e.g. "a similar exercise already exists"). */
  subscribeNotices?(cb: (text: string) => void): () => void;
  capabilities?: { videoNeedsDoneSet?: boolean };
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
    async uploadVideo(_setId, file) {
      await wait(300);
      return { url: URL.createObjectURL(file), name: file.name }; // in-memory object URL, gone on reload
    },
    async deleteVideo() {},
  };
  return { repo, controls };
}

const mock = createMockRepository();
export const mockControls = mock.controls;
export const repo: WorkoutRepository = IS_MOCK ? mock.repo : createApiRepository();
