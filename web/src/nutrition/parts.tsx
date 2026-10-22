// Small display pieces shared by the day view and the browse sheet.
import { Ltr } from '../components/ui';
import { fmt, LABEL, SHORT_UNIT } from './logic';
import type { NutrientKey, NutritionItem } from './types';

/** The four values in the compact list format (missing = "—"). */
export function Vals({ v, partial = [] }: { v: Pick<NutritionItem, 'calories_kcal' | 'protein_g' | 'carbs_g' | 'fat_g'>; partial?: string[] }) {
  const mark = (k: string) => (partial.includes(k) ? <sup className="nut-partial-mark" aria-hidden="true">*</sup> : null);
  return (
    <span className="nut-entry-vals">
      <span><Ltr>{fmt(v.calories_kcal)}</Ltr>{mark('calories_kcal')} <small>{SHORT_UNIT.calories_kcal}</small></span>
      <span><small>ח׳</small> <Ltr>{fmt(v.protein_g)}</Ltr>{mark('protein_g')}</span>
      <span><small>פ׳</small> <Ltr>{fmt(v.carbs_g)}</Ltr>{mark('carbs_g')}</span>
      <span><small>ש׳</small> <Ltr>{fmt(v.fat_g)}</Ltr>{mark('fat_g')}</span>
    </span>
  );
}

/** "Partial total" badge: which values only some ingredients provide. */
export function PartialBadge({ keys }: { keys: string[] }) {
  if (!keys.length) return null;
  const which = keys.map((k) => LABEL[k as NutrientKey] ?? k).join(', ');
  return <span className="chip chip-warn nut-partial" title={`סכום חלקי: ${which} חסר בחלק מהמרכיבים`}>סכום חלקי · {which}</span>;
}

/** Ingredients of a meal: name, grams, values. */
export function ItemList({ items }: { items: NutritionItem[] }) {
  return (
    <ul className="nut-items" aria-label="מרכיבים">
      {items.map((it, i) => (
        <li key={i} className="nut-item">
          <span className="nut-item-name">{it.name}{it.amount_g != null && <small className="muted"> · <Ltr>{fmt(it.amount_g)}</Ltr> ג׳</small>}</span>
          <Vals v={it} />
        </li>
      ))}
    </ul>
  );
}

