// The only module the nutrition screens talk to for data.
// API: /api/v1/nutrition/... through the same-origin proxy (token added by the proxy).
// Mock (VITE_DATA_SOURCE=mock): deterministic synthetic data in memory.
import { IS_MOCK } from '../data/source';
import type { EntryFilter, EntryInput, EntryPatch, ItemSuggestion, NutritionDay, NutritionEntry, NutritionTargets, TargetPatch } from './types';
import { createApiNutrition } from './apiRepository';
import { createMockNutrition } from './mockRepository';

export interface NutritionRepository {
  day(date: string): Promise<NutritionDay>;
  /** Days in an inclusive range (used to mark days with entries in the calendar). */
  days(from: string, to: string): Promise<NutritionDay[]>;
  entries(f: EntryFilter): Promise<NutritionEntry[]>;
  entry(id: number): Promise<NutritionEntry>;
  /** `key` makes a retried submit safe (same key + same body = no duplicate). */
  add(input: EntryInput, key: string): Promise<NutritionEntry>;
  update(id: number, patch: EntryPatch, revision: number): Promise<NutritionEntry>;
  remove(id: number, revision: number): Promise<void>;
  copy(id: number, date: string, key: string): Promise<NutritionEntry>;
  /** Ingredients used before (latest values of each name), most recent first, optionally matching `q`. */
  suggestItems(q: string, limit?: number): Promise<ItemSuggestion[]>;
  /** Targets in effect on a date. */
  targets(date: string): Promise<NutritionTargets>;
  /** Change targets from `date` forward. `revision` = revision of the targets in effect on that date (0 when none). */
  setTargets(date: string, patch: TargetPatch, revision: number): Promise<NutritionTargets>;
}

export const nutritionRepo: NutritionRepository = IS_MOCK ? createMockNutrition() : createApiNutrition();

export const newKey = () => `web-nutrition:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 10)}`;
