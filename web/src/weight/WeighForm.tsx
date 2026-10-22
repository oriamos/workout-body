import { useEffect, useRef, useState, type RefObject } from 'react';
import { Button } from '../components/ui';
import { Icon } from '../components/Icon';
import type { Toast } from '../state/store';
import { ApiError } from '../data/apiRepository';
import { longDate } from '../lib/format';
import { weightRepo } from './repository';
import { entryOn, fmtKg, MAX_NOTE, parseWeight, validateForm, weightInput, type WeighErrors, type WeighForm as FormValues } from './logic';
import type { WeighIn } from './types';

type ToastFn = (text: string, tone?: Toast['tone'], action?: Toast['action']) => void;

export interface WeighFormProps {
  /** Date the form starts on. */
  date: string;
  /** The weigh-in of that date as known when the form opened (null = none). It is the If-Match precondition. */
  base: WeighIn | null;
  today: string;
  /** Allow choosing another (past) date. */
  dateEditable?: boolean;
  /** Weigh-ins already loaded, to look up a chosen date without a request. */
  known: WeighIn[];
  /** Range start of `known`: dates before it are looked up on the server. */
  knownFrom: string;
  idPrefix: string;
  onSaved: (entry: WeighIn, created: boolean) => void | Promise<void>;
  /** Something changed on the server (after a 412): refresh the views. */
  onChanged: () => void;
  onCancel?: () => void;
  onDirty?: (dirty: boolean) => void;
  toast: ToastFn;
  /** Render the actions inside the form (inline) — a Sheet passes its own footer instead. */
  inlineActions?: boolean;
  formRef?: RefObject<HTMLFormElement>;
}

const fromEntry = (date: string, e: WeighIn | null): FormValues => ({ date, weight: e ? weightInput(e.weight_kg) : '', note: e?.note ?? '' });

/** Log or edit the weigh-in of one date. Its values are its own: polling never rewrites what is typed. */
export function useWeighForm(p: WeighFormProps) {
  const [values, setValues] = useState<FormValues>(() => fromEntry(p.date, p.base));
  const [base, setBase] = useState<WeighIn | null>(p.base);
  const [errors, setErrors] = useState<WeighErrors>({});
  const [msg, setMsg] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [looking, setLooking] = useState(false);
  const touched = useRef({ weight: false, note: false });
  const lookupSeq = useRef(0);
  const dirty = touched.current.weight || touched.current.note || values.date !== p.date;
  const { onDirty } = p;
  useEffect(() => { onDirty?.(dirty); }, [dirty, onDirty]);

  const set = (k: keyof FormValues, v: string) => {
    if (k === 'weight' || k === 'note') touched.current[k] = true;
    setValues((x) => ({ ...x, [k]: v }));
    setErrors((x) => ({ ...x, [k]: undefined }));
  };

  /** A new date: find its weigh-in (that becomes the precondition) and prefill fields not typed in. */
  async function changeDate(d: string) {
    set('date', d);
    setMsg(null);
    const seq = ++lookupSeq.current;
    if (!/^\d{4}-\d{2}-\d{2}$/.test(d) || d > p.today) { setBase(null); return; }
    let found: WeighIn | null;
    if (d >= p.knownFrom) found = entryOn(p.known, d);
    else {
      setLooking(true);
      try { found = await weightRepo.get(d); } catch { found = null; } finally { if (seq === lookupSeq.current) setLooking(false); }
    }
    if (seq !== lookupSeq.current) return;
    setBase(found);
    setValues((x) => ({ ...x, weight: touched.current.weight ? x.weight : found ? weightInput(found.weight_kg) : '', note: touched.current.note ? x.note : found?.note ?? '' }));
  }

  /** After a 412: show what the server has now, keep what was typed, and take the fresh revision. */
  async function reloadBase(date: string) {
    let fresh: WeighIn | null = null;
    try { fresh = await weightRepo.get(date); } catch { /* keep the message generic */ }
    setBase(fresh);
    setMsg(fresh
      ? `השקילה של ${longDate(date)} השתנתה במקום אחר. בשרת עכשיו: ${fmtKg(fresh.weight_kg)} ק״ג${fresh.note ? ` (${fresh.note})` : ''}. הערך שהקלדת נשאר בטופס; שמירה נוספת תחליף את מה שבשרת.`
      : `השקילה של ${longDate(date)} נמחקה במקום אחר. הערך שהקלדת נשאר בטופס; שמירה נוספת תרשום אותה מחדש.`);
    p.onChanged();
  }

  async function submit() {
    const { values: v, errors: errs } = validateForm(values, p.today);
    setErrors(errs);
    setMsg(null);
    if (!v) return;
    if (base && base.date === v.date && base.weight_kg === v.weight_kg && (base.note ?? '') === v.note) { p.onCancel?.(); return; }
    setSaving(true);
    try {
      const { entry, created } = await weightRepo.put(v.date, v.weight_kg, v.note, base?.date === v.date ? base.revision : 0);
      p.toast(created ? (v.date === p.today ? `נרשם: ${fmtKg(entry.weight_kg)} ק״ג` : `נרשם ל${longDate(v.date)}: ${fmtKg(entry.weight_kg)} ק״ג`) : `השקילה עודכנה: ${fmtKg(entry.weight_kg)} ק״ג`);
      touched.current = { weight: false, note: false };
      await p.onSaved(entry, created);
    } catch (e) {
      if (e instanceof ApiError && e.status === 412) await reloadBase(v.date);
      else {
        const fieldErr: WeighErrors = {};
        if (e instanceof ApiError) for (const i of e.issues) {
          if (i.field === 'weight_kg') fieldErr.weight = i.message;
          else if (i.field === 'date' || i.field === 'note') fieldErr[i.field] = i.message;
        }
        setErrors((x) => ({ ...x, ...fieldErr }));
        setMsg(e instanceof Error ? e.message : 'השמירה נכשלה');
      }
    } finally {
      setSaving(false);
    }
  }

  return { values, base, errors, msg, saving, looking, set, changeDate, submit, blurWeight: () => { const r = parseWeight(values.weight); if (values.weight.trim() && !r.ok) setErrors((x) => ({ ...x, weight: r.msg })); } };
}

export function WeighFormFields({ f, p }: { f: ReturnType<typeof useWeighForm>; p: WeighFormProps }) {
  const { values, errors, base } = f;
  const id = p.idPrefix;
  const replacing = !!base && base.date === values.date;
  return (
    <form ref={p.formRef} className="form wt-form" onSubmit={(e) => { e.preventDefault(); void f.submit(); }} noValidate>
      <div className="wt-form-row">
        <label className="numfield wt-field wt-weight" htmlFor={`${id}-w`}>
          <span>משקל <small className="muted">(ק״ג)</small></span>
          <input id={`${id}-w`} dir="ltr" inputMode="decimal" name="weight_kg" autoComplete="off" enterKeyHint="done" className={errors.weight ? 'invalid' : ''} value={values.weight} placeholder="81.4" aria-invalid={!!errors.weight} aria-describedby={errors.weight ? `${id}-we` : undefined} onChange={(e) => f.set('weight', e.target.value)} onBlur={f.blurWeight} />
          {errors.weight && <small id={`${id}-we`} className="field-err" role="alert">{errors.weight}</small>}
        </label>
        {p.dateEditable ? (
          <label className="numfield wt-field wt-date" htmlFor={`${id}-d`}>
            <span>תאריך</span>
            <input id={`${id}-d`} type="date" name="date" max={p.today} className={errors.date ? 'invalid' : ''} value={values.date} aria-invalid={!!errors.date} onChange={(e) => void f.changeDate(e.target.value)} />
            {errors.date && <small className="field-err" role="alert">{errors.date}</small>}
          </label>
        ) : null}
      </div>
      <label className="numfield wt-field wt-note" htmlFor={`${id}-n`}>
        <span>הערה <small className="muted">(לא חובה)</small></span>
        <input id={`${id}-n`} dir="auto" name="note" autoComplete="off" maxLength={MAX_NOTE + 20} className={errors.note ? 'invalid' : ''} value={values.note} placeholder="למשל: אחרי ארוחה מלוחה" aria-invalid={!!errors.note} onChange={(e) => f.set('note', e.target.value)} />
        {errors.note && <small className="field-err" role="alert">{errors.note}</small>}
      </label>
      {values.date !== p.today && /^\d{4}-\d{2}-\d{2}$/.test(values.date) && values.date < p.today && (
        <p className="banner wt-date-note" role="status"><Icon name="calendar" size={16} /><span>השקילה תירשם ליום <b>{longDate(values.date)}</b>, לא להיום.</span></p>
      )}
      {f.looking ? <p className="muted small">בודק אם יש שקילה בתאריך הזה…</p> : replacing && (
        <p className="muted small wt-replace-note">כבר רשומה שקילה בתאריך הזה ({fmtKg(base.weight_kg)} ק״ג). שמירה תעדכן אותה.</p>
      )}
      {f.msg && <p className="note note-danger wt-form-msg" role="alert"><Icon name="alert" size={14} /> {f.msg}</p>}
      {p.inlineActions ? (
        <div className="wt-form-actions">
          {p.onCancel && <Button variant="ghost" onClick={p.onCancel}>ביטול</Button>}
          <Button type="submit" variant="primary" icon="check" disabled={f.saving || f.looking}>{f.saving ? 'שומר…' : replacing ? 'עדכון שקילה' : 'שמירת שקילה'}</Button>
        </div>
      ) : <button type="submit" hidden />}
    </form>
  );
}
