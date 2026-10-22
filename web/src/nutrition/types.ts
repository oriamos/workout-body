// Nutrition shapes, as returned by the engine's /api/v1/nutrition routes.
// `null` always means "not provided" — never the same as 0.

export const NUTRIENTS = ['calories_kcal', 'protein_g', 'carbs_g', 'fat_g'] as const;
export type NutrientKey = (typeof NUTRIENTS)[number];
export type Nutrients = Record<NutrientKey, number | null>;

/** One ingredient of a meal: its name, the optional amount used, and the values for that amount. */
export interface NutritionItem extends Nutrients {
  name: string;
  amount_g: number | null;
}

/** An ingredient used before (latest values), from GET /nutrition/items. */
export interface ItemSuggestion extends NutritionItem {
  last_used: string;
  uses: number;
}

export interface NutritionEntry extends Nutrients {
  id: number;
  date: string;
  name: string;
  /** Ingredients, in order; [] for a meal logged as one portion. With items, the values above are their sums. */
  items: NutritionItem[];
  /** Values only some items provide: the meal's total for them is partial. */
  incomplete_values: NutrientKey[];
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

/** With `items`, the meal-level values must not be sent (they are computed as the items' sums). */
export interface EntryInput extends Partial<Nutrients> {
  date?: string;
  name: string;
  items?: NutritionItem[];
}
/** `items` replaces the whole list; `items: []` removes them and keeps the sums as editable values. */
export type EntryPatch = Partial<Nutrients> & { name?: string; date?: string; items?: NutritionItem[] };
export type TargetPatch = Partial<Nutrients>;

export interface EntryFilter { date?: string; from?: string; to?: string; q?: string; limit?: number }
