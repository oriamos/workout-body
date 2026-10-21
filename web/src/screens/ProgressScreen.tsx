import { useMemo, useState } from 'react';
import { useStore } from '../state/store';
import { addDays, dayMonth, longDate, num, shortDate, vol, weekStart } from '../lib/format';
import { exerciseById, exerciseSessions, findPRs, isWorking, setVolume } from '../lib/stats';
import { href, navigate } from '../lib/router';
import { PageHeader } from '../components/Shell';
import { BarChart, LineChart, type Bar } from '../components/Charts';
import { Ltr, Segmented } from '../components/ui';
import { Icon } from '../components/Icon';
import { MUSCLE } from '../lib/labels';
import { RANGES, rangeStart, type RangeKey } from '../lib/ranges';
import type { MuscleGroup } from '../data/types';

const DEFAULT_PICKS = ['bench', 'squat', 'ohp', 'row'];

export function ProgressScreen() {
  const { state } = useStore();
  const { data } = state;
  const [range, setRange] = useState<RangeKey>('8w');
  const [picks, setPicks] = useState<string[]>(DEFAULT_PICKS);
  const [metric, setMetric] = useState<'weight' | 'e1rm'>('weight');
  const today = data.today;

  const calc = useMemo(() => {
    const first = [...data.workouts].map((w) => w.date).sort()[0] ?? today;
    const start = rangeStart(range, today, first);
    const inRange = data.workouts.filter((w) => w.date >= start && w.date <= today);
    const weeks: string[] = [];
    for (let d = weekStart(start); d <= today; d = addDays(d, 7)) weeks.push(d);
    const perWeek = weeks.map((wk) => {
      const ws = inRange.filter((w) => weekStart(w.date) === wk);
      let sets = 0;
      let volume = 0;
      for (const w of ws) for (const we of w.exercises) for (const s of we.sets) { if (s.done && isWorking(s)) sets++; volume += setVolume(s); }
      return { wk, sessions: ws.length, sets, volume, partial: wk === weekStart(today) };
    });
    const muscles = new Map<MuscleGroup, number>();
    for (const w of inRange) for (const we of w.exercises) {
      const m = exerciseById(data, we.exerciseId).muscle;
      muscles.set(m, (muscles.get(m) ?? 0) + we.sets.filter((s) => s.done && isWorking(s)).length);
    }
    const prs = data.exercises.flatMap((e) => findPRs(data, e.id)).filter((p) => p.date >= start).sort((a, b) => b.date.localeCompare(a.date)).slice(0, 6);
    return { start, inRange, perWeek, muscles: [...muscles.entries()].sort((a, b) => b[1] - a[1]), prs };
  }, [data, range, today]);

  const mk = (key: 'sessions' | 'sets' | 'volume'): Bar[] => calc.perWeek.map((w) => ({ key: w.wk, label: dayMonth(w.wk), value: w[key], partial: w.partial }));
  const full = calc.perWeek.filter((w) => !w.partial);
  const avg = (k: 'sessions' | 'sets' | 'volume') => (full.length ? full.reduce((a, w) => a + w[k], 0) / full.length : null);
  const totalSets = calc.perWeek.reduce((a, w) => a + w.sets, 0);
  const maxMuscle = Math.max(1, ...calc.muscles.map((m) => m[1]));

  return (
    <div className="page">
      <PageHeader title="התקדמות" sub={<>מ־{shortDate(calc.start)} עד היום</>} />
      <div className="progress">
        <div className="prog-top">
          <Segmented label="טווח זמן לכל המסך" value={range} onChange={setRange} options={RANGES.map((r) => ({ value: r.key, label: r.label }))} />
          <p className="muted small">הטווח חל על כל החלקים במסך הזה.</p>
        </div>

        <div className="prog-grid">
          <section className="prog-block" aria-labelledby="freq">
            <h2 id="freq">תדירות אימונים</h2>
            <p className="lead"><b><Ltr>{calc.inRange.length}</Ltr></b> אימונים{avg('sessions') != null && <> · ממוצע <Ltr>{num(Math.round((avg('sessions') as number) * 10) / 10)}</Ltr> בשבוע (שבועות מלאים)</>}</p>
            <BarChart bars={mk('sessions')} label="אימונים בשבוע" unit="אימונים" />
          </section>

          <section className="prog-block" aria-labelledby="sets">
            <h2 id="sets">סטים שהושלמו</h2>
            <p className="lead"><b><Ltr>{totalSets}</Ltr></b> סטי עבודה בטווח{avg('sets') != null && <> · ממוצע <Ltr>{Math.round(avg('sets') as number)}</Ltr> בשבוע</>}</p>
            <BarChart bars={mk('sets')} label="סטים שהושלמו בשבוע" unit="סטים" />
          </section>

          <section className="prog-block" aria-labelledby="vol">
            <h2 id="vol">נפח אימון שהושלם</h2>
            <p className="lead">משקל × חזרות בסטי עבודה שהושלמו (בלי חימום){avg('volume') != null && <>. ממוצע <b><Ltr>{vol(avg('volume') as number)}</Ltr></b> ק״ג בשבוע</>}</p>
            <BarChart bars={mk('volume')} label="נפח בשבוע" unit="ק״ג" format={(n) => (n >= 1000 ? `${num(Math.round(n / 100) / 10)}k` : String(Math.round(n)))} />
          </section>

          <section className="prog-block" aria-labelledby="pbs">
            <h2 id="pbs">שיאים אחרונים</h2>
            {calc.prs.length === 0 ? <p className="muted">אין שיאי משקל חדשים בטווח.</p> : (
              <ul className="pb-list">
                {calc.prs.map((p) => (
                  <li key={p.exerciseId + p.date}>
                    <a className="pb-item" href={href({ name: 'exercise', id: p.exerciseId })} onClick={(e) => { e.preventDefault(); navigate({ name: 'exercise', id: p.exerciseId }); }}>
                      <Icon name="trophy" size={18} />
                      <span><b><Ltr>{exerciseById(data, p.exerciseId).name}</Ltr></b><small>שיא משקל · {longDate(p.date)}</small></span>
                      <span className="pb-val"><Ltr>{p.detail}</Ltr></span>
                    </a>
                  </li>
                ))}
              </ul>
            )}
            <p className="caveat">שיא = המשקל הגבוה ביותר בסט עבודה שהושלם, מעל כל מה שנרשם קודם.</p>
          </section>

          <section className="prog-block wide" aria-labelledby="exp">
            <div className="prog-top">
              <h2 id="exp">התקדמות בתרגילים</h2>
              <Segmented size="sm" label="מדד" value={metric} onChange={setMetric} options={[{ value: 'weight', label: 'משקל מרבי' }, { value: 'e1rm', label: '1RM משוער' }]} />
            </div>
            <div className="pick-chips" role="group" aria-label="בחירת תרגילים">
              {data.exercises.map((e) => (
                <button key={e.id} type="button" className="pick-chip" aria-pressed={picks.includes(e.id)} onClick={() => setPicks((p) => (p.includes(e.id) ? p.filter((x) => x !== e.id) : [...p, e.id]))}>
                  {picks.includes(e.id) && <Icon name="check" size={14} strokeWidth={2.6} />}<Ltr>{e.name}</Ltr>
                </button>
              ))}
            </div>
            {picks.length === 0 && <p className="muted">בחר תרגיל כדי להשוות.</p>}
            <ul className="trend-list">
              {data.exercises.filter((e) => picks.includes(e.id)).map((e) => {
                const ss = exerciseSessions(data, e.id);
                const pts = ss.map((s) => ({ date: s.date, value: metric === 'weight' ? s.topWeight : s.e1rm }));
                const vis = pts.filter((p) => p.date >= calc.start && p.value != null) as { date: string; value: number }[];
                const d = vis.length > 1 ? vis[vis.length - 1].value - vis[0].value : null;
                return (
                  <li key={e.id} className="trend">
                    <div className="trend-head">
                      <a href={href({ name: 'exercise', id: e.id })} onClick={(ev) => { ev.preventDefault(); navigate({ name: 'exercise', id: e.id }); }}><Ltr>{e.name}</Ltr></a>
                      <span className={`trend-delta ${d != null && d > 0 ? 'up' : ''}`}>
                        {d == null ? 'מדידה אחת בלבד' : d === 0 ? 'ללא שינוי' : <><Ltr>{d > 0 ? '+' : '−'}{num(Math.round(Math.abs(d) * 10) / 10)}</Ltr> ק״ג בטווח</>}
                      </span>
                    </div>
                    <LineChart compact points={pts} start={calc.start} end={today} unit="ק״ג" estimate={metric === 'e1rm'} label={`${e.name}, ${metric === 'weight' ? 'משקל מרבי' : '1RM משוער'}`} format={(n) => num(Math.round(n * 10) / 10)} />
                  </li>
                );
              })}
            </ul>
            <p className="caveat">כל גרף בציר משלו, כך שהקו מראה כיוון ולא השוואה בין תרגילים. תרגיל שמבוצע גם ביום כבד וגם ביום קל (כמו סקווט) יראה זיגזג. {metric === 'e1rm' && '1RM משוער הוא הערכה לפי נוסחת Epley, ורק מסטים של עד 10 חזרות.'}</p>
          </section>

          <section className="prog-block wide" aria-labelledby="mus">
            <h2 id="mus">חלוקה לפי קבוצת שריר</h2>
            <div className="dist">
              {calc.muscles.map(([m, n]) => (
                <div key={m} className="dist-row">
                  <span>{MUSCLE[m]}</span>
                  <span className="dist-bar" aria-hidden="true"><span style={{ width: `${(n / maxMuscle) * 100}%` }} /></span>
                  <span className="dist-val"><b><Ltr>{n}</Ltr></b> סטים · <Ltr>{totalSets ? Math.round((n / totalSets) * 100) : 0}%</Ltr></span>
                </div>
              ))}
            </div>
            <p className="caveat">סטי עבודה שהושלמו, לפי קבוצת השריר הראשית של כל תרגיל. תרגילים מורכבים מערבים כמה שרירים, לכן זו הערכה גסה ולא מדידה מדויקת.</p>
          </section>
        </div>
      </div>
    </div>
  );
}
