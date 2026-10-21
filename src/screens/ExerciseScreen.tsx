import { useMemo, useState } from 'react';
import { useStore } from '../state/store';
import { goBack, href, navigate } from '../lib/router';
import { kg, longDate, num, shortDate, vol } from '../lib/format';
import { e1rm, exerciseSessions, isWorking, setVolume, counts } from '../lib/stats';
import type { SetEntry } from '../data/types';
import { PageHeader } from '../components/Shell';
import { Chip, EmptyState, Ltr, Segmented } from '../components/ui';
import { Icon } from '../components/Icon';
import { LineChart, type Point } from '../components/Charts';
import { MUSCLE } from '../lib/labels';
import { RANGES, rangeStart, type RangeKey } from '../lib/ranges';

const hasData = (s: SetEntry) => s.weight != null || s.reps != null || s.done;
type Metric = 'weight' | 'reps' | 'volume' | 'e1rm';
const METRICS: Record<Metric, { label: string; unit: string; note: string }> = {
  weight: { label: 'משקל מרבי', unit: 'ק״ג', note: 'הסט הכבד ביותר שהושלם באימון' },
  reps: { label: 'חזרות', unit: 'חזרות', note: 'הכי הרבה חזרות בסט אחד שהושלם' },
  volume: { label: 'נפח', unit: 'ק״ג', note: 'משקל × חזרות, סטי עבודה שהושלמו, בלי חימום' },
  e1rm: { label: '1RM משוער', unit: 'ק״ג', note: 'הערכה בלבד' },
};

export function SetPill({ s }: { s: SetEntry }) {
  const warm = s.type === 'warmup';
  return (
    <span className={`setpill ${warm ? 'warm' : ''} ${!s.done && !warm ? 'notdone' : ''}`} title={warm ? 'חימום' : s.done ? 'הושלם' : 'לא הושלם'}>
      {warm && <small>ח</small>}
      <Ltr>{kg(s.weight)}×{s.reps ?? '—'}</Ltr>
      {!s.done && !warm && <small>לא הושלם</small>}
    </span>
  );
}

export function ExerciseScreen({ id }: { id: string }) {
  const { state } = useStore();
  const ex = state.data.exercises.find((e) => e.id === id);
  const [metric, setMetric] = useState<Metric>('weight');
  const [range, setRange] = useState<RangeKey>('8w');
  const sessions = useMemo(() => exerciseSessions(state.data, id), [state.data, id]);
  if (!ex) return <div className="page"><EmptyState icon="alert" title="התרגיל לא נמצא" /></div>;

  const today = state.data.today;
  const start = rangeStart(range, today, sessions[0]?.date ?? today);
  const pts: Point[] = sessions.map((s) => {
    const counted = s.we.sets.filter(counts);
    let value: number | null = null;
    let detail: string | undefined;
    if (metric === 'weight') { value = s.topWeight; detail = s.topWeight != null ? `${s.topWeight}×${s.bestReps}` : undefined; }
    if (metric === 'reps') { value = counted.length ? Math.max(...counted.map((x) => x.reps as number)) : null; }
    if (metric === 'volume') { value = counted.length ? s.volume : null; detail = `${counted.length} סטים`; }
    if (metric === 'e1rm') { value = s.e1rm; }
    return { date: s.date, value, detail };
  });

  // personal bests
  const allCounted = sessions.flatMap((s) => s.we.sets.filter(counts).map((set) => ({ set, date: s.date })));
  const best = (f: (x: { set: SetEntry; date: string }) => number | null) => allCounted.reduce<{ v: number; date: string; set: SetEntry } | null>((b, x) => { const v = f(x); return v != null && (!b || v > b.v) ? { v, date: x.date, set: x.set } : b; }, null);
  const pbWeight = best((x) => x.set.weight);
  const pbE1 = best((x) => e1rm(x.set.weight as number, x.set.reps as number));
  const pbSet = best((x) => setVolume(x.set));
  const pbSession = sessions.reduce<{ v: number; date: string } | null>((b, s) => (s.volume > 0 && (!b || s.volume > b.v) ? { v: s.volume, date: s.date } : b), null);
  const byReps = new Map<number, { w: number; date: string }>();
  for (const x of allCounted) {
    const r = x.set.reps as number;
    if (r <= 12 && (!byReps.get(r) || (x.set.weight as number) > (byReps.get(r) as { w: number }).w)) byReps.set(r, { w: x.set.weight as number, date: x.date });
  }
  const repRecords = [...byReps.entries()].sort((a, b) => a[0] - b[0]);

  const last = sessions[sessions.length - 1];
  const prior = sessions.length > 1 ? sessions[sessions.length - 2] : null;
  const delta = last && prior && last.topWeight != null && prior.topWeight != null ? last.topWeight - prior.topWeight : null;
  const rows = [...sessions].reverse();

  return (
    <div className="page">
      <PageHeader
        title={<Ltr>{ex.name}</Ltr>}
        back={{ label: 'חזרה', onClick: () => goBack({ name: 'progress' }) }}
        sub={<><Chip>{MUSCLE[ex.muscle]}</Chip><Chip>{ex.equipment}</Chip></>}
      />
      {sessions.length === 0 ? (
        <EmptyState icon="progress" title="עוד אין ביצועים לתרגיל הזה">אחרי הסט הראשון יופיעו כאן גרף, שיאים והיסטוריה.</EmptyState>
      ) : (
        <div className="ex-detail">
          <section className="last-perf" aria-label="ביצוע אחרון">
            <div className="last-perf-head">
              <h2>ביצוע אחרון · {longDate(last.date)}</h2>
              <a href={href({ name: 'history', date: last.date })} onClick={(e) => { e.preventDefault(); navigate({ name: 'history', date: last.date }); }}>האימון המלא <Icon name="back" size={14} /></a>
            </div>
            <div className="sets-compact">{last.we.sets.filter(hasData).map((s) => <SetPill key={s.id} s={s} />)}</div>
            <p className="delta">
              {last.topWeight != null ? <>סט עליון <Ltr>{last.topWeight}×{last.bestReps}</Ltr></> : 'אין משקל מתועד'}
              {delta != null && <> · {delta === 0 ? 'ללא שינוי במשקל המרבי לעומת הפעם הקודמת' : <><Ltr>{delta > 0 ? '+' : '−'}{num(Math.abs(delta))}</Ltr> ק״ג לעומת הפעם הקודמת</>}</>}
              {last.volume > 0 && <> · נפח <Ltr>{vol(last.volume)}</Ltr> ק״ג</>}
            </p>
          </section>

          <div className="ex-detail-cols">
            <div className="ex-detail-main">
              <section className="chart-block" aria-label="גרף התקדמות">
                <div className="chart-controls">
                  <Segmented size="sm" label="מדד" value={metric} onChange={setMetric} options={(Object.keys(METRICS) as Metric[]).map((m) => ({ value: m, label: METRICS[m].label }))} />
                  <Segmented size="sm" label="טווח זמן" value={range} onChange={setRange} options={RANGES.map((r) => ({ value: r.key, label: r.label }))} />
                </div>
                <p className="muted small">{METRICS[metric].label}: {METRICS[metric].note}.</p>
                <LineChart points={pts} start={start} end={today} unit={METRICS[metric].unit} label={`${METRICS[metric].label} של ${ex.name}`} estimate={metric === 'e1rm'} zeroBased={metric === 'volume' || metric === 'reps'} />
              </section>

              <section className="prog-block" style={{ marginTop: 22 }} aria-label="שיאים אישיים">
                <h2 className="block-title">שיאים אישיים</h2>
                <dl className="pb-grid">
                  {pbWeight && <div className="pb"><dt>משקל מרבי</dt><dd><Ltr>{kg(pbWeight.v)}</Ltr> <small>ק״ג × {pbWeight.set.reps}</small><span className="when">{shortDate(pbWeight.date)}</span></dd></div>}
                  {pbE1 && <div className="pb"><dt>1RM משוער (הערכה)</dt><dd><Ltr>~{num(Math.round(pbE1.v * 10) / 10)}</Ltr> <small>ק״ג</small><span className="when">{shortDate(pbE1.date)}</span></dd></div>}
                  {pbSet && <div className="pb"><dt>נפח סט מרבי</dt><dd><Ltr>{vol(pbSet.v)}</Ltr> <small>ק״ג</small><span className="when"><Ltr>{pbSet.set.weight}×{pbSet.set.reps}</Ltr> · {shortDate(pbSet.date)}</span></dd></div>}
                  {pbSession && <div className="pb"><dt>נפח אימון מרבי</dt><dd><Ltr>{vol(pbSession.v)}</Ltr> <small>ק״ג</small><span className="when">{shortDate(pbSession.date)}</span></dd></div>}
                </dl>
                {repRecords.length > 0 && (
                  <>
                    <p className="muted small" style={{ marginTop: 10 }}>המשקל הכבד ביותר לכל מספר חזרות</p>
                    <div className="pb-rep">{repRecords.map(([r, v]) => <span key={r} className="setpill" title={shortDate(v.date)}><Ltr>{v.w}×{r}</Ltr></span>)}</div>
                  </>
                )}
              </section>
            </div>

            <section aria-label="היסטוריית אימונים">
              <h2 className="block-title" style={{ marginBottom: 8 }}>סשן אחר סשן <span className="muted" style={{ fontWeight: 400 }}>({sessions.length})</span></h2>
              <div className="sessions">
                {rows.map((s) => {
                  const working = s.we.sets.filter(isWorking);
                  const planned = s.we.planned?.sets;
                  return (
                    <article key={s.date} className="session">
                      <div className="session-head">
                        <b>{longDate(s.date)}</b>
                        <a href={href({ name: 'history', date: s.date })} onClick={(e) => { e.preventDefault(); navigate({ name: 'history', date: s.date }); }}>האימון המלא <Icon name="back" size={14} /></a>
                      </div>
                      <p className="session-meta"><Ltr>{s.workout.name}</Ltr> · {s.doneSets}{planned ? ` מתוך ${planned}` : ''} סטים הושלמו{s.volume > 0 && <> · נפח <Ltr>{vol(s.volume)}</Ltr> ק״ג</>}</p>
                      <div className="session-sets">
                        {s.we.sets.filter(hasData).map((set) => {
                          const num = set.type === 'working' ? working.indexOf(set) + 1 : set.type === 'warmup' ? 'ח' : set.type === 'drop' ? 'י' : 'כ';
                          return (
                            <div key={set.id} className={`srow ${!set.done ? 'dim' : ''}`}>
                              <span className={`badge badge-${set.type}`}>{num}</span>
                              <span className="srow-main">
                                <Ltr>{kg(set.weight)}×{set.reps ?? '—'}</Ltr>
                                {set.weight == null && <Chip tone="warn" className="inline-chip">חסר משקל</Chip>}
                                {!set.done && set.type !== 'warmup' && <Chip className="inline-chip">לא הושלם</Chip>}
                              </span>
                              <span className="srow-opt">{[set.rpe != null ? `RPE ${set.rpe}` : null, set.rir != null ? `RIR ${set.rir}` : null].filter(Boolean).join(' · ')}</span>
                            </div>
                          );
                        })}
                        {planned != null && planned > working.length && <p className="session-meta">{planned - working.length} סטים מתוכננים לא נרשמו</p>}
                      </div>
                    </article>
                  );
                })}
              </div>
            </section>
          </div>
        </div>
      )}
    </div>
  );
}
