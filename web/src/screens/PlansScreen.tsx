import { useState } from 'react';
import type { Plan, PlanExercise, PlanSchedule, Weekday } from '../data/types';
import { scheduleText, weekOrder, WEEKDAY_HE } from '../data/schedule';
import { useStore, useToday } from '../state/store';
import { goBack, navigate, useRoute, href } from '../lib/router';
import { exerciseById } from '../lib/stats';
import { exportPlan } from '../lib/planImport';
import { kg } from '../lib/format';
import { PageHeader } from '../components/Shell';
import { Button, Chip, EmptyState, IconButton, Ltr, Menu, Segmented } from '../components/ui';
import { Icon } from '../components/Icon';
import { ImportSheet } from '../components/ImportSheet';
import { AddExerciseSheet } from '../components/AddExerciseSheet';

const setsOf = (p: Plan) => p.exercises.reduce((a, e) => a + e.sets, 0);
const repText = (e: PlanExercise) => (e.repMin === e.repMax ? `${e.repMin}` : `${e.repMin}–${e.repMax}`);

function PlanList({ activeId }: { activeId?: string }) {
  const { state } = useStore();
  const today = useToday();
  return (
    <ul className="plan-list">
      {state.data.plans.map((p) => {
        const names = p.exercises.map((e) => exerciseById(state.data, e.exerciseId).name);
        return (
          <li key={p.id}>
            <a href={href({ name: 'plans', id: p.id })} className={`plan-item ${activeId === p.id ? 'active' : ''}`} aria-current={activeId === p.id ? 'true' : undefined} onClick={(e) => { e.preventDefault(); navigate({ name: 'plans', id: p.id }); }}>
              <span className="plan-top">
                <b className="plan-name"><Ltr>{p.name}</Ltr></b>
                {today?.planId === p.id && <Chip tone="accent">אימון היום</Chip>}
                {p.importedAt && <Chip tone="neutral">יובא</Chip>}
                {scheduleText(p.schedule) && <Chip tone="neutral" icon="calendar" className="plan-sched">{scheduleText(p.schedule)}</Chip>}
                <span className="plan-count">{p.exercises.length} תרגילים · {setsOf(p)} סטים</span>
              </span>
              <span className="plan-names"><Ltr>{names.slice(0, 3).join(' · ')}</Ltr>{names.length > 3 && <span className="plan-more">ועוד {names.length - 3}</span>}</span>
            </a>
          </li>
        );
      })}
    </ul>
  );
}

function Stepper({ value, onChange, min, max, label }: { value: number; onChange: (n: number) => void; min: number; max: number; label: string }) {
  return (
    <div className="stepper" role="group" aria-label={label}>
      <IconButton icon="minus" label={`הפחת ${label}`} disabled={value <= min} onClick={() => onChange(value - 1)} size={18} />
      <output><Ltr>{value}</Ltr></output>
      <IconButton icon="plus" label={`הוסף ${label}`} disabled={value >= max} onClick={() => onChange(value + 1)} size={18} />
    </div>
  );
}

function NumField({ label, value, onCommit, placeholder, step = 1, max, error }: { label: string; value: number | null; onCommit: (n: number | null) => boolean | void; placeholder?: string; step?: number; max: number; error?: string }) {
  const [draft, setDraft] = useState<string | null>(null);
  const [bad, setBad] = useState<string | null>(null);
  const commit = () => {
    if (draft == null) return;
    const t = draft.trim();
    if (t === '') { onCommit(null); setDraft(null); setBad(null); return; }
    const n = Number(t);
    if (!Number.isFinite(n) || n < 0 || n > max || Math.round(n / step) * step !== n) { setBad(`ערך תקין: 0 עד ${max}${step < 1 ? ', בקפיצות של ' + step : ''}`); return; }
    onCommit(n);
    setDraft(null);
    setBad(null);
  };
  const err = bad ?? error;
  return (
    <label className="numfield">
      <span>{label}</span>
      <input dir="ltr" inputMode="decimal" className={err ? 'invalid' : ''} value={draft ?? (value == null ? '' : String(value))} placeholder={placeholder ?? '—'} aria-invalid={!!err} onFocus={(e) => e.currentTarget.select()} onChange={(e) => { setDraft(e.target.value); setBad(null); }} onBlur={commit} onKeyDown={(e) => { if (e.key === 'Enter') { commit(); (e.target as HTMLInputElement).blur(); } if (e.key === 'Escape') { setDraft(null); setBad(null); (e.target as HTMLInputElement).blur(); } }} />
      {err && <small className="field-err" role="alert">{err}</small>}
    </label>
  );
}

function PlanExerciseRow({ plan, index, pe }: { plan: Plan; index: number; pe: PlanExercise }) {
  const { state, actions } = useStore();
  const [open, setOpen] = useState(false);
  const ex = exerciseById(state.data, pe.exerciseId);
  const n = plan.exercises.length;
  const patch = (p: Partial<PlanExercise>) => actions.savePlan({ ...plan, exercises: plan.exercises.map((e) => (e.id === pe.id ? { ...e, ...p } : e)) });
  const move = (d: -1 | 1) => {
    const list = plan.exercises.slice();
    const [it] = list.splice(index, 1);
    list.splice(index + d, 0, it);
    actions.savePlan({ ...plan, exercises: list });
  };
  const extra = [pe.weight != null ? `יעד ${kg(pe.weight)} ק״ג` : null, pe.rir != null ? `RIR ${pe.rir}` : null, pe.rpe != null ? `RPE ${pe.rpe}` : null].filter(Boolean);
  return (
    <li className={`pex ${open ? 'open' : ''}`}>
      <div className="pex-row">
        <span className="pex-reorder">
          <IconButton icon="arrowUp" label={`הזז למעלה: ${ex.name}`} disabled={index === 0} onClick={() => move(-1)} size={16} />
          <IconButton icon="arrowDown" label={`הזז למטה: ${ex.name}`} disabled={index === n - 1} onClick={() => move(1)} size={16} />
        </span>
        <button type="button" className="pex-main" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
          <span className="pex-name"><Ltr>{ex.name}</Ltr></span>
          <span className="pex-sum"><Ltr>{pe.sets} × {repText(pe)}</Ltr>{extra.map((x) => ` · ${x}`)}</span>
        </button>
        <IconButton icon={open ? 'up' : 'edit'} label={open ? 'סגור עריכה' : `ערוך: ${ex.name}`} onClick={() => setOpen((o) => !o)} size={18} />
      </div>
      {open && (
        <div className="pex-edit">
          <div className="pex-fields">
            <div className="numfield"><span>סטים</span><Stepper label="סטים" value={pe.sets} min={1} max={20} onChange={(v) => patch({ sets: v })} /></div>
            <NumField label="חזרות מינ׳" max={100} value={pe.repMin} placeholder="" onCommit={(v) => { if (v == null || v < 1) return false; patch({ repMin: v, repMax: Math.max(v, pe.repMax) }); }} />
            <NumField label="חזרות מקס׳" max={100} value={pe.repMax} placeholder="" error={pe.repMax < pe.repMin ? 'המקסימום קטן מהמינימום' : undefined} onCommit={(v) => { if (v == null || v < 1) return false; patch({ repMax: v, repMin: Math.min(v, pe.repMin) }); }} />
            <NumField label="משקל יעד (ק״ג)" max={500} step={0.25} value={pe.weight} onCommit={(v) => patch({ weight: v })} />
          </div>
          <div className="pex-opt">
            <span className="opt-label">RIR <small>אופציונלי</small></span>
            <div className="opt-chips" role="group" aria-label="RIR יעד">
              {[0, 1, 2, 3, 4].map((v) => <button key={v} type="button" className="optchip" aria-pressed={pe.rir === v} onClick={() => patch({ rir: pe.rir === v ? null : v })}><Ltr>{v}</Ltr></button>)}
            </div>
          </div>
          <div className="pex-opt">
            <span className="opt-label">RPE <small>אופציונלי</small></span>
            <div className="opt-chips" role="group" aria-label="RPE יעד">
              {[6, 7, 7.5, 8, 8.5, 9, 9.5, 10].map((v) => <button key={v} type="button" className="optchip" aria-pressed={pe.rpe === v} onClick={() => patch({ rpe: pe.rpe === v ? null : v })}><Ltr>{v}</Ltr></button>)}
            </div>
          </div>
          <div className="pex-foot">
            <Button size="sm" variant="danger" icon="trash" onClick={() => actions.savePlan({ ...plan, exercises: plan.exercises.filter((e) => e.id !== pe.id) })}>הסר מהתוכנית</Button>
          </div>
        </div>
      )}
    </li>
  );
}

type SchedMode = 'none' | 'weekdays' | 'times';

/** When the plan is meant to be done: fixed weekdays, a weekly frequency, or nothing. Drives the week view. */
function ScheduleEditor({ plan }: { plan: Plan }) {
  const { actions } = useStore();
  const s = plan.schedule ?? null;
  const [mode, setMode] = useState<SchedMode>(s?.type === 'weekdays' ? 'weekdays' : s?.type === 'times_per_week' ? 'times' : 'none');
  const save = (schedule: PlanSchedule | null) => actions.savePlan({ ...plan, schedule });
  const days = s?.type === 'weekdays' ? s.days : [];
  const times = s?.type === 'times_per_week' ? s.times_per_week : 2;
  const pick = (m: SchedMode) => {
    setMode(m);
    if (m === 'none') save(null);
    if (m === 'times') save({ type: 'times_per_week', times_per_week: times });
    // weekdays: saved once a day is chosen
  };
  const toggle = (d: Weekday) => {
    const next = days.includes(d) ? days.filter((x) => x !== d) : [...days, d];
    // Stored Monday first, like the engine.
    save(next.length ? { type: 'weekdays', days: (['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as Weekday[]).filter((x) => next.includes(x)) } : null);
  };
  return (
    <section className="sched" aria-labelledby={`sched-h-${plan.id}`}>
      <h3 id={`sched-h-${plan.id}`} className="sched-title"><Icon name="calendar" size={16} /> לוח זמנים</h3>
      <Segmented size="sm" label="סוג לוח זמנים" value={mode} onChange={pick} options={[{ value: 'none', label: 'ללא' }, { value: 'weekdays', label: 'ימים קבועים' }, { value: 'times', label: 'פעמים בשבוע' }]} />
      {mode === 'weekdays' && (
        <div className="opt-chips" role="group" aria-label="ימי האימון">
          {weekOrder().map((d) => <button key={d} type="button" className="optchip" aria-pressed={days.includes(d)} onClick={() => toggle(d)}>{WEEKDAY_HE[d]}</button>)}
        </div>
      )}
      {mode === 'weekdays' && !days.length && <p className="muted small">בחר יום אחד או יותר.</p>}
      {mode === 'times' && (
        <div className="sched-times"><Stepper label="פעמים בשבוע" value={times} min={1} max={7} onChange={(v) => save({ type: 'times_per_week', times_per_week: v })} /><span className="muted small">פעמים בשבוע, בכל יום שתבחר</span></div>
      )}
      <p className="muted small">לוח הזמנים מופיע במסך השבוע. הוא לא יוצר אימונים ולא משנה אימונים שכבר תועדו.</p>
    </section>
  );
}

function PlanDetail({ plan }: { plan: Plan }) {
  const { state, actions } = useStore();
  const today = useToday();
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState<string | null>(null);

  const download = () => {
    const blob = new Blob([exportPlan(plan, state.data)], { type: 'application/json' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `${plan.name.replace(/\s+/g, '-').toLowerCase()}.json`;
    a.click();
    URL.revokeObjectURL(a.href);
    actions.toast('הקובץ הורד');
  };
  const commitName = () => {
    if (name != null && name.trim() && name.trim() !== plan.name) actions.savePlan({ ...plan, name: name.trim() });
    setName(null);
  };
  return (
    <section className="plan-detail" aria-label={`תוכנית ${plan.name}`}>
      <div className="plan-detail-head">
        <label className="sr" htmlFor="plan-name">שם התוכנית</label>
        <input id="plan-name" className="plan-title-input" dir="auto" value={name ?? plan.name} onChange={(e) => setName(e.target.value)} onBlur={commitName} onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()} />
        <Menu label="אפשרויות תוכנית" items={[
          { label: 'ייצוא JSON', icon: 'download', onSelect: download },
          { label: 'מחק תוכנית', icon: 'trash', danger: true, onSelect: () => { actions.deletePlan(plan.id); navigate({ name: 'plans' }, { replace: true }); actions.toast(`"${plan.name}" נמחקה. אימונים שבוצעו לא נפגעו.`); } },
        ]} />
      </div>
      <p className="template-note"><Icon name="info" size={15} /> <span><b>עריכת תבנית.</b> שינויים כאן חלים על אימונים עתידיים בלבד. אימונים שבוצעו, וגם אימון היום אם כבר התחיל, לא משתנים.</span></p>

      <ScheduleEditor plan={plan} />

      <ol className="pex-list">
        {plan.exercises.map((pe, i) => <PlanExerciseRow key={pe.id} plan={plan} index={i} pe={pe} />)}
      </ol>
      {plan.exercises.length === 0 && <EmptyState title="התוכנית ריקה">הוסף תרגיל כדי להתחיל.</EmptyState>}
      <div className="plan-actions">
        <Button icon="plus" onClick={() => setAdding(true)}>הוסף תרגיל</Button>
        {today ? (
          <p className="muted small">כבר יש אימון להיום ({today.name}), לכן אי אפשר להתחיל נוסף באותו תאריך.</p>
        ) : (
          <Button variant="primary" onClick={() => { actions.startFromPlan(plan); navigate({ name: 'workout', date: state.data.today }); }}>התחל אימון מהתוכנית</Button>
        )}
      </div>
      <p className="plan-summary muted small">{plan.exercises.length} תרגילים · {setsOf(plan)} סטים מתוכננים</p>
      <AddExerciseSheet open={adding} onClose={() => setAdding(false)} exclude={plan.exercises.map((e) => e.exerciseId)} onPick={(exerciseId) => actions.savePlan({ ...plan, exercises: [...plan.exercises, { id: `${plan.id}-${Date.now()}`, exerciseId, sets: 3, repMin: 8, repMax: 12, weight: null, rir: null, rpe: null }] })} />
    </section>
  );
}

export function PlansScreen() {
  const route = useRoute();
  const { state, actions } = useStore();
  const [importing, setImporting] = useState(false);
  const id = route.name === 'plans' ? route.id : undefined;
  const plan = state.data.plans.find((p) => p.id === id);
  const detailOnMobile = !!plan;

  return (
    <div className={`page plans ${detailOnMobile ? 'has-detail' : ''}`}>
      <PageHeader
        title="תוכניות"
        sub="תבניות לאימונים. הן לא משתנות כשאתה עורך סט באימון."
        back={plan ? { label: 'חזרה לרשימת התוכניות', onClick: () => goBack({ name: 'plans' }) } : undefined}
      />
      <div className="plans-grid">
        <div className="plans-list-col">
          <div className="plans-toolbar">
            <Button variant="primary" icon="plus" onClick={() => { const p: Plan = { id: `p-${Date.now()}`, name: 'תוכנית חדשה', exercises: [] }; actions.savePlan(p); navigate({ name: 'plans', id: p.id }); }}>תוכנית חדשה</Button>
            <Button variant="ghost" icon="upload" onClick={() => setImporting(true)}>ייבוא JSON</Button>
          </div>
          <PlanList activeId={id} />
        </div>
        <div className="plans-detail-col">
          {plan ? <PlanDetail key={plan.id} plan={plan} /> : <div className="plans-placeholder"><EmptyState icon="plans" title="בחר תוכנית לעריכה">הרשימה כוללת את כל התבניות. אפשר גם להתחיל אימון ישירות מתוכנית.</EmptyState></div>}
        </div>
      </div>
      <ImportSheet open={importing} onClose={() => setImporting(false)} />
    </div>
  );
}
