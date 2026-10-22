import { useEffect, useId, useRef, useState } from 'react';
import { Button, Ltr, Sheet } from '../components/ui';
import { Icon } from '../components/Icon';
import type { Toast } from '../state/store';
import { ApiError } from '../data/apiRepository';
import { longDate } from '../lib/format';
import { newKey, nutritionRepo } from './repository';
import {
  emptyForm, emptyRow, entryPatch, fmt, formFrom, isBlankRow, itemCountText, itemsPatch, LABEL, liveTotals, MAX, MAX_ITEMS, parseAmount, parseGrams, partialNote, pickSuggestion, rowFrom,
  setRowAmount, setRowField, SHORT_UNIT, UNIT, validateEntryForm, validateItems, type FieldKey, type FormErrors, type FormValues, type ItemErrors, type ItemField, type ItemRow,
} from './logic';
import { NUTRIENTS, type EntryInput, type ItemSuggestion, type NutrientKey, type NutritionEntry } from './types';

type ToastFn = (text: string, tone?: Toast['tone'], action?: Toast['action']) => void;

/** Map the engine's validation issues (field names match ours) onto the form. */
export function issuesToErrors(e: unknown): FormErrors {
  const out: FormErrors = {};
  if (e instanceof ApiError) for (const i of e.issues) if (['name', ...NUTRIENTS].includes(i.field)) out[i.field as FieldKey] = i.message;
  return out;
}

export function NumberField({ k, value, error, onChange, onBlur, label }: { k: NutrientKey; value: string; error?: string; onChange: (v: string) => void; onBlur?: () => void; label?: string }) {
  return (
    <label className="numfield nut-field">
      <span>{label ?? LABEL[k]} <small className="muted">({UNIT[k]})</small></span>
      <input dir="ltr" inputMode="decimal" name={k} autoComplete="off" className={error ? 'invalid' : ''} value={value} placeholder="—" aria-invalid={!!error} onChange={(e) => onChange(e.target.value)} onBlur={onBlur} />
      {error && <small className="field-err" role="alert">{error}</small>}
    </label>
  );
}

const SHORT_LABEL: Record<NutrientKey, string> = { calories_kcal: 'קק״ל', protein_g: 'חלבון', carbs_g: 'פחמ׳', fat_g: 'שומן' };

/** Map engine issues on `items[i].field` onto the form rows (i counts the non-blank rows sent). */
function itemIssuesToErrors(e: unknown, rows: ItemRow[]): Record<number, ItemErrors> {
  const out: Record<number, ItemErrors> = {};
  if (!(e instanceof ApiError)) return out;
  const sent = rows.filter((r) => !isBlankRow(r));
  for (const i of e.issues) {
    const m = /^items\[(\d+)\]\.(\w+)$/.exec(i.field);
    const row = m && sent[Number(m[1])];
    if (row && ['name', 'amount_g', ...NUTRIENTS].includes(m[2])) out[row.id] = { ...out[row.id], [m[2]]: i.message };
  }
  return out;
}

/** Ingredient name with suggestions of ingredients used before (GET /nutrition/items). */
function NameCombo({ value, error, onChange, onPick, index }: { value: string; error?: string; onChange: (v: string) => void; onPick: (s: ItemSuggestion) => void; index: number }) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [list, setList] = useState<ItemSuggestion[]>([]);
  const [active, setActive] = useState(-1);
  const seq = useRef(0);
  useEffect(() => {
    if (!open) return;
    const n = ++seq.current;
    const t = window.setTimeout(() => {
      nutritionRepo.suggestItems(value, 6).then((l) => { if (n === seq.current) { setList(l); setActive(-1); } }, () => { if (n === seq.current) setList([]); });
    }, 150);
    return () => window.clearTimeout(t);
  }, [open, value]);
  const shown = open && list.length > 0;
  const pick = (s: ItemSuggestion) => { onPick(s); setOpen(false); };
  return (
    <div className="nut-combo">
      <input
        dir="auto" name="item-name" autoComplete="off" maxLength={250} role="combobox" aria-expanded={shown} aria-controls={`${id}-list`} aria-autocomplete="list"
        aria-activedescendant={shown && active >= 0 ? `${id}-${active}` : undefined} aria-label={`שם מרכיב ${index + 1}`} aria-invalid={!!error}
        className={error ? 'invalid' : ''} value={value} placeholder="למשל: חזה עוף"
        onChange={(e) => { onChange(e.target.value); setOpen(true); }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={(e) => {
          if (!shown) { if (e.key === 'ArrowDown') setOpen(true); return; }
          if (e.key === 'ArrowDown') { e.preventDefault(); setActive((a) => (a + 1) % list.length); }
          else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((a) => (a <= 0 ? list.length - 1 : a - 1)); }
          else if (e.key === 'Enter' && active >= 0) { e.preventDefault(); pick(list[active]); }
          else if (e.key === 'Escape') { e.preventDefault(); setOpen(false); }
        }}
      />
      {shown && (
        <ul className="nut-suggest" role="listbox" id={`${id}-list`} aria-label="מרכיבים קודמים">
          {list.map((s, i) => (
            <li key={s.name} id={`${id}-${i}`} role="option" aria-selected={i === active} className={i === active ? 'active' : ''} onMouseDown={(e) => { e.preventDefault(); pick(s); }}>
              <span className="nut-suggest-name">{s.name}</span>
              <span className="nut-suggest-meta">
                {s.amount_g != null && <><Ltr>{fmt(s.amount_g)}</Ltr> ג׳ · </>}
                <Ltr>{fmt(s.calories_kcal)}</Ltr> {SHORT_UNIT.calories_kcal} · ח׳ <Ltr>{fmt(s.protein_g)}</Ltr>
              </span>
            </li>
          ))}
        </ul>
      )}
      {error && <small className="field-err" role="alert">{error}</small>}
    </div>
  );
}

function MiniField({ label, name, value, error, onChange, onBlur }: { label: string; name: string; value: string; error?: string; onChange: (v: string) => void; onBlur?: () => void }) {
  return (
    <label className="nut-mini">
      <span>{label}</span>
      <input dir="ltr" inputMode="decimal" name={name} autoComplete="off" className={error ? 'invalid' : ''} value={value} placeholder="—" aria-invalid={!!error} title={error} onChange={(e) => onChange(e.target.value)} onBlur={onBlur} />
    </label>
  );
}

function ItemRowEditor({ row, index, errors, canRemove, onChange, onRemove, onError }: { row: ItemRow; index: number; errors: ItemErrors; canRemove: boolean; onChange: (r: ItemRow) => void; onRemove: () => void; onError: (f: ItemField, msg?: string) => void }) {
  const errList = (['amount_g', ...NUTRIENTS] as ItemField[]).filter((f) => errors[f]);
  const check = (f: ItemField) => { const r = f === 'amount_g' ? parseGrams(row.amount_g) : parseAmount(row[f], f as NutrientKey); onError(f, r.ok ? undefined : r.msg); };
  return (
    <li className="nut-item-row" data-row={index}>
      <div className="nut-item-row-head">
        <NameCombo value={row.name} error={errors.name} index={index} onChange={(v) => { onChange(setRowField(row, 'name', v)); onError('name'); }} onPick={(s) => { onChange(pickSuggestion(row, s)); onError('name'); }} />
        {canRemove && <button type="button" className="nut-item-remove" aria-label={`הסרת מרכיב ${index + 1}`} title="הסרת מרכיב" onClick={onRemove}><Icon name="x" size={16} /></button>}
      </div>
      <div className="nut-item-fields">
        <MiniField label="גרם" name="item-amount_g" value={row.amount_g} error={errors.amount_g} onChange={(v) => { onChange(setRowAmount(row, v)); onError('amount_g'); }} onBlur={() => check('amount_g')} />
        {NUTRIENTS.map((k) => <MiniField key={k} label={SHORT_LABEL[k]} name={`item-${k}`} value={row[k]} error={errors[k]} onChange={(v) => { onChange(setRowField(row, k, v)); onError(k); }} onBlur={() => check(k)} />)}
      </div>
      {row.scaled && row.base?.amount_g != null && <small className="nut-scaled" role="status"><Icon name="info" size={12} /> הותאם לכמות (הערכים נשמרו ל־<Ltr>{fmt(row.base.amount_g)}</Ltr> ג׳)</small>}
      {errList.map((f) => <small key={f} className="field-err" role="alert">{f === 'amount_g' ? 'גרם' : LABEL[f as NutrientKey]}: {errors[f]}</small>)}
    </li>
  );
}

/** Live meal totals from the rows: sum of what is provided, "—" when no row provides it, partial note. */
function Totals({ rows }: { rows: ItemRow[] }) {
  const t = liveTotals(rows);
  return (
    <section className="nut-totals" aria-label="סה״כ לארוחה" aria-live="polite">
      <h3 className="nut-totals-title">סה״כ לארוחה <span className="muted">· {itemCountText(t.count)}</span></h3>
      <dl>
        {NUTRIENTS.map((k) => {
          const note = t.sum[k] != null ? partialNote(t.missing[k]) : null;
          return (
            <div key={k} className="nut-total" data-nutrient={k}>
              <dt>{LABEL[k]}</dt>
              <dd><Ltr className="nut-total-val">{fmt(t.sum[k])}</Ltr> <small>{UNIT[k]}</small></dd>
              {note && <dd className="nut-total-partial">{note}</dd>}
            </div>
          );
        })}
      </dl>
    </section>
  );
}

type Mode = 'items' | 'plain';

export function EntrySheet({ open, entry, date, today, onClose, onChanged, toast }: { open: boolean; entry: NutritionEntry | null; date: string; today: string; onClose: () => void; onChanged: () => void; toast: ToastFn }) {
  const [values, setValues] = useState<FormValues>(() => (entry ? formFrom(entry) : emptyForm()));
  const [errors, setErrors] = useState<FormErrors>({});
  const [mode, setMode] = useState<Mode>(() => (entry && !entry.items.length ? 'plain' : 'items'));
  const [rows, setRows] = useState<ItemRow[]>(() => (entry?.items.length ? entry.items.map(rowFrom) : [emptyRow()]));
  const [rowErrors, setRowErrors] = useState<Record<number, ItemErrors>>({});
  const [listError, setListError] = useState<string | null>(null);
  const [base, setBase] = useState<NutritionEntry | null>(entry);
  const [msg, setMsg] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const attempt = useRef<{ body: string; key: string } | null>(null);
  const editing = !!entry;
  const entryDate = base?.date ?? date;
  const hadItems = !!base?.items.length;

  const plainTouched = useRef(false);
  const set = (k: FieldKey, v: string) => { if (k !== 'name') plainTouched.current = true; setValues((x) => ({ ...x, [k]: v })); setErrors((x) => ({ ...x, [k]: undefined })); };
  const blurCheck = (k: NutrientKey) => { const r = parseAmount(values[k], k); if (!r.ok) setErrors((x) => ({ ...x, [k]: r.msg })); };
  const setRow = (r: ItemRow) => setRows((list) => list.map((x) => (x.id === r.id ? r : x)));
  const setRowError = (id: number, f: ItemField, m?: string) => setRowErrors((x) => ({ ...x, [id]: { ...x[id], [f]: m } }));
  const addRow = () => { setRows((l) => [...l, emptyRow()]); setListError(null); };
  const removeRow = (id: number) => setRows((l) => (l.length > 1 ? l.filter((x) => x.id !== id) : [emptyRow()]));

  function toPlain() {
    // Start the simple form from the current sums, so dropping the ingredients keeps the totals.
    const t = liveTotals(rows);
    if (t.count > 0 && !plainTouched.current) setValues((x) => ({ ...x, ...Object.fromEntries(NUTRIENTS.map((k) => [k, t.sum[k] == null ? '' : String(t.sum[k])])) }));
    setMode('plain');
  }
  function toItems() {
    // Breaking a simple meal down: start with one ingredient holding its current values.
    if (rows.every(isBlankRow) && NUTRIENTS.some((k) => values[k].trim() !== '')) setRows([rowFrom({ name: values.name, calories_kcal: num(values.calories_kcal), protein_g: num(values.protein_g), carbs_g: num(values.carbs_g), fat_g: num(values.fat_g) })]);
    setMode('items');
  }
  const num = (v: string) => { const r = parseAmount(v, 'calories_kcal'); return r.ok ? r.value : null; };

  /** After a 412 or 404: show the server's current values, keep what the user typed. */
  async function reloadBase(id: number) {
    try {
      const fresh = await nutritionRepo.entry(id);
      setBase(fresh);
      const parts = fresh.items.length ? ` · ${itemCountText(fresh.items.length)}` : '';
      setMsg(`הארוחה שונתה במקום אחר. בשרת עכשיו: ${fresh.name}${parts} · ${NUTRIENTS.map((k) => `${LABEL[k]} ${fmt(fresh[k])}`).join(' · ')}. הערכים שהקלדת נשארו בטופס; שמירה נוספת תחליף את מה שבשרת.`);
    } catch {
      setMsg('הארוחה נמחקה במקום אחר.');
      setBase(null);
    }
    onChanged();
  }

  async function submit() {
    const { name, values: nums, errors: errs } = validateEntryForm(values);
    const plain = mode === 'plain';
    const it = plain ? null : validateItems(rows);
    const formErrs: FormErrors = plain ? errs : { name: errs.name };
    setErrors(formErrs);
    setRowErrors(it?.errors ?? {});
    setListError(it?.listError ?? null);
    setMsg(null);
    if (Object.values(formErrs).some(Boolean) || (it && (it.listError || Object.keys(it.errors).length))) return;
    setSaving(true);
    try {
      if (editing) {
        if (!base) return;
        const patch = it ? itemsPatch(base, name, it.items) : entryPatch(base, name, nums);
        if (!Object.keys(patch).length) { onClose(); return; }
        await nutritionRepo.update(base.id, patch, base.revision);
        toast('הארוחה עודכנה');
      } else {
        const input: EntryInput = { date, name };
        if (it) input.items = it.items; // the meal's values are the items' sums: never sent
        else for (const k of NUTRIENTS) if (nums[k] != null) input[k] = nums[k]; // empty = not provided, never 0
        const body = JSON.stringify(input);
        // One key per submit attempt; a retry of the same values reuses it, so it can never duplicate.
        if (!attempt.current || attempt.current.body !== body) attempt.current = { body, key: newKey() };
        await nutritionRepo.add(input, attempt.current.key);
        toast(date === today ? 'הארוחה נוספה' : `הארוחה נוספה ל${longDate(date)}`);
      }
      onChanged();
      onClose();
    } catch (e) {
      if (e instanceof ApiError && (e.status === 412 || (e.status === 404 && editing)) && base) await reloadBase(base.id);
      else {
        setErrors((x) => ({ ...x, ...issuesToErrors(e) }));
        if (!plain) setRowErrors((x) => ({ ...x, ...itemIssuesToErrors(e, rows) }));
        setMsg(e instanceof Error ? e.message : 'השמירה נכשלה');
      }
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    if (!base) return;
    setSaving(true);
    try {
      await nutritionRepo.remove(base.id, base.revision);
      toast(`"${base.name}" נמחקה`);
      onChanged();
      onClose();
    } catch (e) {
      setConfirmDelete(false);
      if (e instanceof ApiError && (e.status === 412 || e.status === 404)) await reloadBase(base.id);
      else setMsg(e instanceof Error ? e.message : 'המחיקה נכשלה');
    } finally {
      setSaving(false);
    }
  }

  return (
    <Sheet
      open={open}
      onClose={onClose}
      wide
      title={editing ? 'עריכת ארוחה' : 'הוספת ארוחה'}
      footer={
        <>
          {editing && base && (confirmDelete ? (
            <Button variant="danger" icon="trash" className="nut-del" disabled={saving} onClick={() => void remove()}>כן, למחוק</Button>
          ) : (
            <Button variant="ghost" icon="trash" className="nut-del" disabled={saving} onClick={() => setConfirmDelete(true)}>מחיקה</Button>
          ))}
          <Button variant="ghost" onClick={onClose}>ביטול</Button>
          <Button variant="primary" disabled={saving || (editing && !base)} onClick={() => void submit()}>{saving ? 'שומר…' : editing ? 'שמירה' : 'הוספה'}</Button>
        </>
      }
    >
      <form className="form nut-form" onSubmit={(e) => { e.preventDefault(); void submit(); }} noValidate>
        {entryDate !== today && (
          <p className="banner nut-date-note" role="status"><Icon name="calendar" size={16} /><span>{editing ? 'הארוחה רשומה ליום' : 'הארוחה תירשם ליום'} <b>{longDate(entryDate)}</b>, לא להיום.</span></p>
        )}
        {confirmDelete && <p className="note note-warn" role="alert"><Icon name="alert" size={14} /> למחוק את הארוחה? לחץ שוב על "כן, למחוק" לאישור.</p>}
        <label className="numfield nut-field nut-name">
          <span>שם הארוחה</span>
          <input dir="auto" name="name" autoComplete="off" maxLength={250} className={errors.name ? 'invalid' : ''} value={values.name} placeholder="למשל: חביתה ולחם" aria-invalid={!!errors.name} onChange={(e) => set('name', e.target.value)} />
          {errors.name && <small className="field-err" role="alert">{errors.name}</small>}
        </label>
        <label className="nut-mode">
          <input type="checkbox" name="plain-mode" checked={mode === 'plain'} onChange={(e) => (e.target.checked ? toPlain() : toItems())} />
          <span>ארוחה פשוטה בלי מרכיבים</span>
        </label>
        {mode === 'items' ? (
          <>
            <ol className="nut-item-rows" aria-label="מרכיבים">
              {rows.map((r, i) => (
                <ItemRowEditor key={r.id} row={r} index={i} errors={rowErrors[r.id] ?? {}} canRemove={rows.length > 1 || !isBlankRow(r)} onChange={setRow} onRemove={() => removeRow(r.id)} onError={(f, m) => setRowError(r.id, f, m)} />
              ))}
            </ol>
            <div className="nut-item-actions">
              <Button size="sm" variant="secondary" icon="plus" disabled={rows.length >= MAX_ITEMS} onClick={addRow}>הוספת מרכיב</Button>
              <span className="muted small">ערך ריק = לא ידוע (לא נספר כ־0).</span>
            </div>
            {listError && <p className="note note-warn" role="alert"><Icon name="alert" size={14} /> {listError}</p>}
            <Totals rows={rows} />
          </>
        ) : (
          <>
            {hadItems && <p className="note note-warn nut-convert-note" role="status"><Icon name="info" size={14} /> המרכיבים יוסרו מהארוחה. הסכומים נשארים כערכי הארוחה וניתן לערוך אותם.</p>}
            <div className="nut-fields">
              {NUTRIENTS.map((k) => <NumberField key={k} k={k} value={values[k]} error={errors[k]} onChange={(v) => set(k, v)} onBlur={() => blurCheck(k)} />)}
            </div>
            <p className="muted small">אפשר להשאיר שדה ריק אם הערך לא ידוע. הוא יסומן כחסר ולא ייספר כ־0. עד <Ltr>{fmt(MAX.calories_kcal)}</Ltr> {SHORT_UNIT.calories_kcal} ו־<Ltr>{fmt(MAX.protein_g)}</Ltr> גרם לכל ערך.</p>
            {editing && !hadItems && <Button size="sm" variant="secondary" icon="list" className="nut-split" onClick={toItems}>פירוק למרכיבים</Button>}
          </>
        )}
        {msg && <p className="note note-danger nut-form-msg" role="alert"><Icon name="alert" size={14} /> {msg}</p>}
        <button type="submit" hidden />
      </form>
    </Sheet>
  );
}
