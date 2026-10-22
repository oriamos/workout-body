// The only module the weight screens talk to for data.
// API: /api/v1/weight/... through the same-origin proxy (token added by the proxy).
// Mock (VITE_DATA_SOURCE=mock): deterministic synthetic weigh-ins in memory.
import { IS_MOCK } from '../data/source';
import type { WeighIn, WeightTrend } from './types';
import { createApiWeight } from './apiRepository';
import { createMockWeight } from './mockRepository';

export interface WeightRepository {
  /** Weekly view of the last `weeks` weeks (Sunday→Saturday by default; the engine decides) ending with the week of today. */
  trend(weeks: number): Promise<WeightTrend>;
  /** The weigh-in of a date, or null when there is none. */
  get(date: string): Promise<WeighIn | null>;
  /** Record (or replace) the weigh-in of a date. `expectedRevision`: the revision being replaced, 0 = must not exist yet. */
  put(date: string, weightKg: number, note: string, expectedRevision: number): Promise<{ entry: WeighIn; created: boolean }>;
  remove(date: string, revision: number): Promise<void>;
}

export const weightRepo: WeightRepository = IS_MOCK ? createMockWeight() : createApiWeight();
