// Home: this week at a glance. Seven days from the configured first day of the week (Sunday by default),
// what was done and what is planned on each, and today's workout one tap away.
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { useStore } from '../state/store';
import { repo } from '../data/repository';
import { computeSchedule, DEFAULT_WEEK_START, startOfWeek, weekTotals, WEEKDAY_HE, type ScheduleDay, type ScheduleStatus, type WeekSchedule, type WeeklyTarget } from '../data/schedule';
import type { Plan, Weekday } from '../data/types';
import { addDays, monthName, parseDate, vol } from '../lib/format';
import { workoutTotals } from '../lib/stats';
import { href, navigate } from '../lib/router';
import { PageHeader } from '../components/Shell';
import { Button, Chip, IconButton, Ltr, Skeleton } from '../components/ui';
import { Icon } from '../components/Icon';
import { WeighInCard } from '../weight/WeighInCard';
import './week.css';

const STATUS: Record<ScheduleStatus, { label: string; tone: 'neutral' | 'accent' | 'warn' | 'assistant' | 'danger' }> = {
  done: { label: 'בוצע', tone: 'accent' },
  started: { label: 'התחיל', tone: 'assistant' },
  missed: { label: 'פוספס', tone: 'danger' },
  due: { label: 'היום', tone: 'warn' },
  upcoming: { label: 'מתוכנן', tone: 'neutral' },
};

/** "18–24 באוקטובר", "27 בספטמבר – 3 באוקטובר" (year added when it is not this year's). */
export function rangeLabel(from: string, to: string, today: string): string {
  const a = parseDate(from);
  const b = parseDate(to);
  const year = (d: Date) => (String(d.getFullYear()) !== today.slice(0, 4) ? ` ${d.getFullYear()}` : '');
  // Isolates keep "18–24" in reading order inside the RTL heading.
  if (a.getMonth() === b.getMonth() && a.getFullYear() === b.getFullYear()) return `\u2066${a.getDate()}–${b.getDate()}\u2069 ב${monthName(a.getMonth())}${year(b)}`;
  return `${a.getDate()} ב${monthName(a.getMonth())}${a.getFullYear() !== b.getFullYear() ? year(a) : ''} – ${b.getDate()} ב${monthName(b.getMonth())}${year(b)}`;
}

// The server's first day of the week, learnt from its default week (days[0].weekday).
let serverWeekStart: Weekday | null = null;

/**
 * The week view. Against the engine it is GET /schedule (the default week when `week` is not given),
 * re-read whenever a workout in that week changes in the store (own edits, or the assistant's picked
 * up by polling). In the mock it is computed from the in-memory data.
 */
function useWeekView(week: string | undefined): { view: WeekSchedule | null; failed: boolean; retry: () => void } {
  const { state } = useStore();
  const data = state.data;
  const remote = !!repo.schedule;
  const key = week ?? 'current';
  const [got, setGot] = useState<{ key: string; view: WeekSchedule } | null>(null);
  const [failed, setFailed] = useState(false);
  const [nonce, setNonce] = useState(0);

  const local = useMemo(() => {
    if (remote) return null;
    const from = week ?? startOfWeek(data.today, DEFAULT_WEEK_START);
    return computeSchedule(data, from, addDays(from, 6), DEFAULT_WEEK_START);
  }, [remote, week, data]);

  // What the server's answer depends on: the week's workouts (plan + completed working sets) and the plans' schedules.
  const known = got?.key === key ? got.view : null;
  const sig = useMemo(() => {
    if (!remote) return '';
    const from = known?.from ?? week ?? addDays(data.today, -6);
    const to = known?.to ?? addDays(from, known ? 6 : 12);
    const ws = data.workouts.filter((w) => w.date >= from && w.date <= to).map((w) => `${w.date}:${w.planId}:${w.exercises.reduce((a, we) => a + we.sets.filter((s) => s.done && s.type !== 'warmup').length, 0)}`);
    return `${ws.sort().join(',')}|${data.plans.map((p) => `${p.id}:${JSON.stringify(p.schedule ?? null)}`).join(',')}`;
  }, [remote, data, known, week]);

  useEffect(() => {
    if (!repo.schedule) return;
    let live = true;
    const t = window.setTimeout(() => {
      repo.schedule!(week ? { from: week, to: addDays(week, 6) } : undefined).then(
        (v) => {
          if (!live) return;
          if (!week && v.days[0]) serverWeekStart = v.days[0].weekday;
          setGot({ key, view: v });
          setFailed(false);
        },
        () => live && setFailed(true),
      );
    }, got?.key === key ? 500 : 0); // first read of a week right away, refreshes debounced
    return () => { live = false; window.clearTimeout(t); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, sig, nonce]);

  if (!remote) return { view: local, failed: false, retry: () => {} };
  let view = known;
  if (!view && failed) {
    // No answer from the server: show what the loaded data says rather than nothing.
    const first = serverWeekStart ?? DEFAULT_WEEK_START;
    const from = week ?? startOfWeek(data.today, first);
    view = computeSchedule(data, from, addDays(from, 6), first);
  }
  return { view, failed, retry: () => setNonce((n) => n + 1) };
}

export function WeekLoading() {
  return (
    <div className="page week" aria-busy="true" aria-label="טוען את השבוע">
      <div className="page-head"><Skeleton w={170} h={26} /><div style={{ height: 8 }} /><Skeleton w={130} h={14} /></div>
      <div className="week-days">
        {Array.from({ length: 7 }, (_, i) => <Skeleton key={i} h={64} className="block" />)}
      </div>
    </div>
  );
}

function FrequencyLine({ t }: { t: WeeklyTarget }) {
  return (
    <li className="freq">
      <Icon name="target" size={15} />
      <span>
        <Ltr>{t.name}</Ltr>: <Ltr>{t.completed}</Ltr> מתוך <Ltr>{t.target}</Ltr> השבוע
        {t.remaining > 0 ? <>, נותר <Ltr>{t.remaining}</Ltr></> : <span className="freq-done"> · הושלם</span>}
      </span>
    </li>
  );
}

function WeekSummary({ view, isCurrent }: { view: WeekSchedule; isCurrent: boolean }) {
  const t = weekTotals(view);
  const targets = view.weeklyTargets.filter((x) => x.weekStart === view.from);
  if (!t.hasSchedule) {
    return (
      <section className="glass-panel week-summary week-empty" aria-labelledby="week-empty-h">
        <h2 id="week-empty-h" className="week-empty-title"><Icon name="calendar" size={18} /> עוד אין לוח זמנים קבוע</h2>
        <p>קבע לתוכנית ימים קבועים בשבוע (למשל שני וחמישי) או מספר פעמים בשבוע, וכאן יופיע מה מתוכנן לכל יום ומה כבר בוצע.</p>
        <p className="muted small">{t.done === 0 ? 'אין אימונים בשבוע הזה עדיין.' : `${t.done === 1 ? 'אימון אחד' : `${t.done} אימונים`} בשבוע הזה.`}</p>
        <Button variant="primary" icon="plans" onClick={() => navigate({ name: 'plans' })}>לתוכניות</Button>
      </section>
    );
  }
  const pct = t.planned ? Math.min(1, t.done / t.planned) : 0;
  return (
    <section className="glass-panel week-summary" aria-label="סיכום השבוע">
      <p className="week-count">
        <b className="summary-num"><Ltr>{t.done}</Ltr></b>
        <span className="summary-of">מתוך <Ltr>{t.planned}</Ltr> אימונים מתוכננים {isCurrent ? 'השבוע' : 'בשבוע הזה'}</span>
      </p>
      <div className="meter" role="progressbar" aria-label="אימונים שבוצעו" aria-valuemin={0} aria-valuemax={t.planned} aria-valuenow={t.done}><span style={{ transform: `scaleX(${pct})` }} /></div>
      {targets.length > 0 && <ul className="freq-list">{targets.map((x) => <FrequencyLine key={x.planId} t={x} />)}</ul>}
    </section>
  );
}

function TodayActions({ day, view }: { day: ScheduleDay; view: WeekSchedule }) {
  const { state, actions } = useStore();
  const today = state.data.today;
  const hasWorkout = !!day.workout || state.data.workouts.some((w) => w.date === today);
  const open = () => navigate({ name: 'workout', date: today });
  if (hasWorkout) return <div className="day-actions"><Button variant="primary" icon="today" onClick={open}>פתח את האימון של היום</Button></div>;

  const byId = (id: string) => state.data.plans.find((p) => p.id === id);
  const due = day.scheduled.filter((s) => s.status === 'due').map((s) => byId(s.planId)).filter((p): p is Plan => !!p);
  const freq = view.weeklyTargets.filter((t) => t.weekStart === view.from && t.remaining > 0).map((t) => byId(t.planId)).filter((p): p is Plan => !!p);
  const options = due.length ? due : freq;
  const start = (p: Plan) => { actions.startFromPlan(p); navigate({ name: 'workout', date: today }); };

  if (options.length === 0) return <div className="day-actions"><Button variant="primary" icon="today" onClick={open}>פתח את האימון של היום</Button></div>;
  if (options.length === 1) {
    return (
      <div className="day-actions">
        <Button variant="primary" icon="play" onClick={() => start(options[0])}>התחל אימון · <Ltr>{options[0].name}</Ltr></Button>
        <Button variant="ghost" size="sm" onClick={open}>אימון אחר</Button>
      </div>
    );
  }
  return (
    <div className="day-actions day-pick" role="group" aria-labelledby="pick-h">
      <span id="pick-h" className="day-pick-title">איזה אימון היום?</span>
      {options.map((p, i) => <Button key={p.id} variant={i === 0 ? 'primary' : 'secondary'} icon="play" onClick={() => start(p)}>התחל · <Ltr>{p.name}</Ltr></Button>)}
      <Button variant="ghost" size="sm" onClick={open}>אימון אחר</Button>
    </div>
  );
}

function DayCard({ day, view }: { day: ScheduleDay; view: WeekSchedule }) {
  const { state } = useStore();
  const w = state.data.workouts.find((x) => x.date === day.date);
  const totals = w ? workoutTotals(w) : null;
  const sets = totals ? totals.done : day.workout?.workingSets ?? 0;
  const past = day.date < view.today;
  const future = day.date > view.today;
  const followed = day.workout?.planId ? day.scheduled.some((s) => s.planId === day.workout!.planId) : false;
  const workoutName = day.workout ? (day.workout.planName ?? w?.name ?? 'אימון חופשי') : null;
  const d = parseDate(day.date);
  const label = `יום ${WEEKDAY_HE[day.weekday]}, ${d.getDate()} ב${monthName(d.getMonth())}${day.today ? ', היום' : ''}`;

  const planLine = (s: ScheduleDay['scheduled'][number], link: boolean) => (
    <span key={s.planId} className="day-plan" data-plan={s.name} data-status={s.status}>
      {link ? (
        <a href={href({ name: 'plans', id: s.planId })} onClick={(e) => { e.preventDefault(); navigate({ name: 'plans', id: s.planId }); }}><Ltr>{s.name}</Ltr></a>
      ) : (
        <Ltr className="day-plan-name">{s.name}</Ltr>
      )}
      <Chip tone={STATUS[s.status].tone}>{STATUS[s.status].label}</Chip>
    </span>
  );

  let body: ReactNode;
  if (day.workout) {
    const to = day.today ? { name: 'workout' as const, date: day.date } : { name: 'history' as const, date: day.date };
    body = (
      <a className="day-link" href={href(to)} onClick={(e) => { e.preventDefault(); navigate(to); }} aria-label={`${label}: ${workoutName}, ${sets === 1 ? 'סט אחד' : `${sets} סטים`}${day.today ? '' : '. פתח את האימון'}`}>
        {day.scheduled.map((s) => planLine(s, false))}
        {!followed && <span className="day-plan day-workout-name"><Ltr className="day-plan-name">{workoutName}</Ltr>{day.scheduled.length > 0 && <span className="muted small">במקום</span>}</span>}
        <span className="day-stats">
          {sets === 1 ? 'סט אחד' : <><Ltr>{sets}</Ltr> סטים</>}{totals && totals.volume > 0 && <> · <Ltr>{vol(totals.volume)}</Ltr> ק״ג נפח</>}
          {!day.today && <Icon name="left" size={16} className="day-go" />}
        </span>
      </a>
    );
  } else if (day.scheduled.length) {
    body = <div className="day-plans">{day.scheduled.map((s) => planLine(s, future || day.today))}</div>;
  } else {
    body = <span className="day-rest">{future || day.today ? 'אין אימון מתוכנן' : 'מנוחה'}</span>;
  }

  return (
    <li className={`day ${day.today ? 'is-today' : ''} ${past ? 'is-past' : ''} ${future ? 'is-future' : ''}`} data-date={day.date} data-weekday={day.weekday} aria-current={day.today ? 'date' : undefined}>
      <div className="day-date">
        <span className="sr">{label}</span>
        <span className="day-name" aria-hidden="true">{day.today ? 'היום' : WEEKDAY_HE[day.weekday]}</span>
        <b aria-hidden="true">{d.getDate()}</b>
      </div>
      <div className="day-body">
        {body}
        {day.today && <TodayActions day={day} view={view} />}
      </div>
    </li>
  );
}

export function WeekScreen({ week }: { week?: string }) {
  const { state } = useStore();
  const { view, failed, retry } = useWeekView(week);
  const today = state.data.today;
  const isCurrent = !!view?.days.some((d) => d.today);
  const go = (w?: string) => navigate({ name: 'today', week: w }, { replace: true });

  const title = view ? rangeLabel(view.from, view.to, today) : 'השבוע';
  return (
    <div className="page week">
      <PageHeader title={title} sub={!view || isCurrent ? undefined : view.from > today ? 'שבוע עתידי' : 'שבוע קודם'} status />
      <nav className="week-nav" aria-label="ניווט בין שבועות">
        <IconButton icon="right" label="שבוע קודם" disabled={!view} onClick={() => view && go(addDays(view.from, -7))} />
        {isCurrent || !view ? <span className="week-nav-label">{view ? 'השבוע' : ''}</span> : <Button size="sm" variant="secondary" onClick={() => go(undefined)}>השבוע</Button>}
        <IconButton icon="left" label="שבוע הבא" disabled={!view} onClick={() => view && go(addDays(view.from, 7))} />
      </nav>
      {failed && (
        <p className="week-warn" role="status"><Icon name="alert" size={15} /> לא הצלחנו לטעון את לוח הזמנים מהשרת. מוצג מה שידוע מהאימונים שנטענו. <button type="button" className="linkbtn" onClick={retry}>נסה שוב</button></p>
      )}
      {!view ? (
        <div className="week-days" aria-busy="true">{Array.from({ length: 7 }, (_, i) => <Skeleton key={i} h={64} className="block" />)}</div>
      ) : (
        <div className="week-grid">
          <div className="week-side">
            <WeekSummary view={view} isCurrent={isCurrent} />
          </div>
          <ol className="week-days" aria-label={`ימי השבוע, ${title}`}>
            {view.days.map((d) => <DayCard key={d.date} day={d} view={view} />)}
          </ol>
          {isCurrent && <div className="week-extra"><WeighInCard today={today} /></div>}
        </div>
      )}
    </div>
  );
}
