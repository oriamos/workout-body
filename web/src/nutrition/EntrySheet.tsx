import { useRef, useState } from 'react';
import { Button, Ltr, Sheet } from '../components/ui';
import { Icon } from '../components/Icon';
import type { Toast } from '../state/store';
import { ApiError } from '../data/apiRepository';
import { longDate } from '../lib/format';
import { newKey, nutritionRepo } from './repository';
import { emptyForm, entryPatch, fmt, formFrom, LABEL, MAX, parseAmount, SHORT_UNIT, UNIT, validateEntryForm, type FieldKey, type FormErrors, type FormValues } from './logic';
import { NUTRIENTS, type EntryInput, type NutrientKey, type NutritionEntry } from './types';

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

export function EntrySheet({ open, entry, date, today, onClose, onChanged, toast }: { open: boolean; entry: NutritionEntry | null; date: string; today: string; onClose: () => void; onChanged: () => void; toast: ToastFn }) {
  const [values, setValues] = useState<FormValues>(() => (entry ? formFrom(entry) : emptyForm()));
  const [errors, setErrors] = useState<FormErrors>({});
  const [base, setBase] = useState<NutritionEntry | null>(entry);
  const [msg, setMsg] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const attempt = useRef<{ body: string; key: string } | null>(null);
  const editing = !!entry;
  const entryDate = base?.date ?? date;

  const set = (k: FieldKey, v: string) => { setValues((x) => ({ ...x, [k]: v })); setErrors((x) => ({ ...x, [k]: undefined })); };
  const blurCheck = (k: NutrientKey) => { const r = parseAmount(values[k], k); if (!r.ok) setErrors((x) => ({ ...x, [k]: r.msg })); };

  /** After a 412 or 404: show the server's current values, keep what the user typed. */
  async function reloadBase(id: number) {
    try {
      const fresh = await nutritionRepo.entry(id);
      setBase(fresh);
      setMsg(`הארוחה שונתה במקום אחר. בשרת עכשיו: ${fresh.name} · ${NUTRIENTS.map((k) => `${LABEL[k]} ${fmt(fresh[k])}`).join(' · ')}. הערכים שהקלדת נשארו בטופס; שמירה נוספת תחליף את מה שבשרת.`);
    } catch {
      setMsg('הארוחה נמחקה במקום אחר.');
      setBase(null);
    }
    onChanged();
  }

  async function submit() {
    const { name, values: nums, errors: errs } = validateEntryForm(values);
    setErrors(errs);
    setMsg(null);
    if (Object.values(errs).some(Boolean)) return;
    setSaving(true);
    try {
      if (editing) {
        if (!base) return;
        const patch = entryPatch(base, name, nums);
        if (!Object.keys(patch).length) { onClose(); return; }
        await nutritionRepo.update(base.id, patch, base.revision);
        toast('הארוחה עודכנה');
      } else {
        const input: EntryInput = { date, name };
        for (const k of NUTRIENTS) if (nums[k] != null) input[k] = nums[k]; // empty = not provided, never 0
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
        <div className="nut-fields">
          {NUTRIENTS.map((k) => <NumberField key={k} k={k} value={values[k]} error={errors[k]} onChange={(v) => set(k, v)} onBlur={() => blurCheck(k)} />)}
        </div>
        <p className="muted small">אפשר להשאיר שדה ריק אם הערך לא ידוע. הוא יסומן כחסר ולא ייספר כ־0. עד <Ltr>{fmt(MAX.calories_kcal)}</Ltr> {SHORT_UNIT.calories_kcal} ו־<Ltr>{fmt(MAX.protein_g)}</Ltr> גרם לכל ערך.</p>
        {msg && <p className="note note-danger nut-form-msg" role="alert"><Icon name="alert" size={14} /> {msg}</p>}
        <button type="submit" hidden />
      </form>
    </Sheet>
  );
}
