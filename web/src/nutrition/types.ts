// Nutrition shapes, as returned by the engine's /api/v1/nutrition routes.
// `null` always means "not provided" — never the same as 0.

export const NUTRIENTS = ['calories_kcal', 'protein_g', 'carbs_g', 'fat_g'] as const;
export type NutrientKey = (typeof NUTRIENTS)[number];
export type Nutrients = Record<NutrientKey, number | null>;

export interface NutritionEntry extends Nutrients {
  id: number;
  date: string;
  name: string;
  copied_from: number | null;
  revision: number;
  created_at: string;
  updated_at: string;
}

export interface NutritionTargets extends Nutrients {
  /** Date of the change in effect; absent/empty when no target was ever set. */
  effective_from?: string;
  /** Precondition for the next change on that date; 0 when none. */
  revision: number;
}

export interface NutrientSummary {
  consumed: number;
  entries_missing_value: number;
  target: number | null;
  remaining: number | null;
  over: number | null;
}

export interface NutritionDay extends Record<NutrientKey, NutrientSummary> {
  date: string;
  today: boolean;
  entry_count: number;
  targets: NutritionTargets;
  entries: NutritionEntry[];
}

export interface EntryInput extends Partial<Nutrients> {
  date?: string;
  name: string;
}
export type EntryPatch = Partial<Nutrients> & { name?: string; date?: string };
export type TargetPatch = Partial<Nutrients>;

export interface EntryFilter { date?: string; from?: string; to?: string; q?: string; limit?: number }
