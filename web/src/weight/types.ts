// Body-weight shapes, as returned by the engine's /api/v1/weight routes (internal/training/bodyweight.go).
// `null` always means "not enough data" — the UI never fills it in.

export interface WeighIn {
  id: number;
  date: string;
  weight_kg: number;
  /** Omitted by the engine when empty. */
  note?: string;
  revision: number;
  created_at: string;
  updated_at: string;
}

export type WeightTrendDir = 'up' | 'down' | 'flat' | '';

/** One week (Sunday→Saturday by default; week_start/week_end come from the engine). Averages use only the days that have a weigh-in. */
export interface WeightWeek {
  week_start: string;
  week_end: string;
  days_logged: number;
  average_kg: number | null;
  min_kg: number | null;
  max_kg: number | null;
  /** vs the previous week's average; null when either week has no weigh-in. */
  change_kg: number | null;
  change_pct: number | null;
  /** Omitted (or "") without a change. */
  trend?: WeightTrendDir;
}

export interface WeightTrend {
  from: string;
  to: string;
  today: string;
  /** Oldest first; the last one is the week containing `to`. */
  weeks: WeightWeek[];
  /** Weigh-ins in [from, to], oldest first. */
  entries: WeighIn[];
  latest: WeighIn | null;
  overall_change_kg: number | null;
}
