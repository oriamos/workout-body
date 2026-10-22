import { useState } from 'react';
import { Button, Ltr, Sheet } from '../components/ui';
import { Icon } from '../components/Icon';
import type { Toast } from '../state/store';
import { ApiError } from '../data/apiRepository';
import { longDate } from '../lib/format';
import { nutritionRepo } from './repository';
import { fmt, formFrom, LABEL, parseAmount, targetPatch, UNIT, validateNutrients, type FormErrors, type FormValues } from './logic';
import { NUTRIENTS, type NutrientKey, type NutritionTargets } from './types';
import { issuesToErrors, NumberField } from './EntrySheet';

type ToastFn = (text: string, tone?: Toast['tone'], action?: Toast['action']) => void;

export function TargetsSheet({ open, date, today, targets, onClose, onChanged, toast }: { open: boolean; date: string; today: string; targets: NutritionTargets; onClose: () => void; onChanged: () => void; toast: ToastFn }) {
  const [base, setBase] = useState<NutritionTargets>(targets);
  const [values, setValues] = useState<FormValues>(() => formFrom(targets));
  const [errors, setErrors] = useState<FormErrors>({});
  const [msg, setMsg] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const set = (k: NutrientKey, v: string) => { setValues((x) => ({ ...x, [k]: v })); setErrors((x) => ({ ...x, [k]: undefined })); };

  async function save() {
    const { values: nums, errors: errs } = validateNutrients(values);
    setErrors(errs);
    setMsg(null);
    if (Object.values(errs).some(Boolean)) return;
    const patch = targetPatch(base, nums);
    if (!Object.keys(patch).length) { setMsg('לא שינית אף יעד.'); return; }
    setSaving(true);
    try {
      await nutritionRepo.setTargets(date, patch, base.revision);
      toast(date === today ? 'היעדים עודכנו מהיום והלאה' : `היעדים עודכנו מ־${longDate(date)} והלאה`);
      onChanged();
      onClose();
    } catch (e) {
      if (e instanceof ApiError && e.status === 412) {
        try {
          const fresh = await nutritionRepo.targets(date);
          setBase(fresh);
          setMsg(`היעדים שונו במקום אחר. בתוקף עכשיו: ${NUTRIENTS.map((k) => `${LABEL[k]} ${fresh[k] == null ? 'אין יעד' : fmt(fresh[k])}`).join(' · ')}. הערכים שהקלדת נשארו; שמירה נוספת תחליף אותם.`);
          onChanged();
        } catch {
          setMsg(e.message);
        }
      } else {
        setErrors((x) => ({ ...x, ...issuesToErrors(e) }));
        setMsg(e instanceof Error ? e.message : 'השמירה נכשלה');
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <Sheet
      open={open}
      onClose={onClose}
      title="יעדים יומיים"
      footer={<><Button variant="ghost" onClick={onClose}>ביטול</Button><Button variant="primary" disabled={saving} onClick={() => void save()}>{saving ? 'שומר…' : 'שמירת יעדים'}</Button></>}
    >
      <div className="form nut-form">
        <section className="form-block" aria-label="יעדים בתוקף">
          <h3>בתוקף ב{longDate(date)}</h3>
          <dl className="nut-targets">
            {NUTRIENTS.map((k) => (
              <div key={k}><dt>{LABEL[k]}</dt><dd>{base[k] == null ? <span className="muted">אין יעד</span> : <><Ltr>{fmt(base[k])}</Ltr> <small>{UNIT[k]}</small></>}</dd></div>
            ))}
          </dl>
          <p className="muted small nut-effective">{base.effective_from ? <>בתוקף מ־{longDate(base.effective_from)} (<Ltr>{base.effective_from}</Ltr>).</> : 'עוד לא הוגדרו יעדים.'}</p>
        </section>
        <section className="form-block">
          <h3>שינוי מ־{longDate(date)} והלאה</h3>
          <div className="nut-fields">
            {NUTRIENTS.map((k) => <NumberField key={k} k={k} value={values[k]} error={errors[k]} onChange={(v) => set(k, v)} onBlur={() => { const r = parseAmount(values[k], k); if (!r.ok) setErrors((x) => ({ ...x, [k]: r.msg })); }} />)}
          </div>
          <p className="scope-note"><Icon name="info" size={14} /><span>השינוי חל מהיום הנבחר ועד לשינוי הבא. ימים קודמים שומרים על היעדים שהיו בתוקף בהם. שדה ריק מסיר את היעד.</span></p>
        </section>
        {msg && <p className="note note-danger nut-form-msg" role="alert"><Icon name="alert" size={14} /> {msg}</p>}
      </div>
    </Sheet>
  );
}
