import { useMemo, useState } from 'react';
import { useStore } from '../state/store';
import { href, navigate } from '../lib/router';
import { kg, num, parseDate, shortDate } from '../lib/format';
import { exerciseSessions, counts, type SessionStats } from '../lib/stats';
import type { Exercise, MuscleGroup } from '../data/types';
import { PageHeader } from '../components/Shell';
import { Chip, EmptyState, Ltr, Segmented } from '../components/ui';
import { Icon } from '../components/Icon';
import { MUSCLE } from '../lib/labels';

type Sort = 'recent' | 'name' | 'count';

interface Row {
  ex: Exercise;
  sessions: SessionStats[];
  last: SessionStats | null;
  prev: SessionStats | null;
  best: number | null;
}

function daysAgo(date: string, today: string): string {
  const d = Math.round((parseDate(today).getTime() - parseDate(date).getTime()) / 86_400_000);
  if (d <= 0) return 'היום';
  if (d === 1) return 'אתמול';
  if (d < 7) return `לפני ${d} ימים`;
  if (d < 14) return 'לפני שבוע';
  if (d < 60) return `לפני ${Math.round(d / 7)} שבועות`;
  return `לפני ${Math.round(d / 30)} חודשים`;
}

/** Every exercise in the library, with what happened the last time it was done. */
export function ExercisesScreen() {
  const { state } = useStore();
  const { data } = state;
  const [q, setQ] = useState('');
  const [muscle, setMuscle] = useState<MuscleGroup | 'all'>('all');
  const [sort, setSort] = useState<Sort>('recent');

  const rows = useMemo<Row[]>(() => data.exercises.map((ex) => {
    // Only sessions where something was actually done count as "performed".
    const sessions = exerciseSessions(data, ex.id).filter((s) => s.we.sets.some((x) => x.done));
    const last = sessions.length ? sessions[sessions.length - 1] : null;
    const prev = sessions.length > 1 ? sessions[sessions.length - 2] : null;
    const weights = sessions.flatMap((s) => s.we.sets.filter(counts).map((x) => x.weight as number));
    return { ex, sessions, last, prev, best: weights.length ? Math.max(...weights) : null };
  }), [data]);

  const muscles = useMemo(() => [...new Set(data.exercises.map((e) => e.muscle))], [data.exercises]);
  const needle = q.trim().toLowerCase();
  const shown = rows
    .filter((r) => (muscle === 'all' || r.ex.muscle === muscle) && (!needle || r.ex.name.toLowerCase().includes(needle)))
    .sort((a, b) => {
      if (sort === 'name') return a.ex.name.localeCompare(b.ex.name);
      if (sort === 'count') return b.sessions.length - a.sessions.length || a.ex.name.localeCompare(b.ex.name);
      // recent first; never-performed exercises at the end
      return (b.last?.date ?? '').localeCompare(a.last?.date ?? '') || a.ex.name.localeCompare(b.ex.name);
    });
  const performed = shown.filter((r) => r.last);
  const untouched = shown.filter((r) => !r.last);

  const open = (id: string) => navigate({ name: 'exercise', id });

  return (
    <div className="page">
      <PageHeader title="תרגילים" sub={<span className="muted">{data.exercises.length} בספרייה · לחיצה על תרגיל מציגה את כל האימונים שלו</span>} />
      <div className="exlib-controls">
        <label className="search">
          <Icon name="search" size={18} />
          <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="חיפוש תרגיל" aria-label="חיפוש תרגיל" dir="auto" />
        </label>
        <div className="pick-chips" role="group" aria-label="סינון לפי קבוצת שריר">
          <button type="button" className="pick-chip" aria-pressed={muscle === 'all'} onClick={() => setMuscle('all')}>הכול</button>
          {muscles.map((m) => (
            <button key={m} type="button" className="pick-chip" aria-pressed={muscle === m} onClick={() => setMuscle(m)}>{MUSCLE[m]}</button>
          ))}
        </div>
        <Segmented size="sm" label="מיון" value={sort} onChange={setSort} options={[
          { value: 'recent', label: 'אחרונים' }, { value: 'count', label: 'הכי הרבה אימונים' }, { value: 'name', label: 'א–ת' },
        ]} />
      </div>

      {shown.length === 0 ? (
        <EmptyState icon="search" title={data.exercises.length ? 'לא נמצא תרגיל' : 'עוד אין תרגילים'}>
          {data.exercises.length ? 'נסה שם אחר או בטל את הסינון.' : 'תרגילים מתווספים כשמוסיפים אותם לאימון או לתוכנית.'}
        </EmptyState>
      ) : (
        <ul className="exlib glass-panel" aria-label="רשימת תרגילים">
          {[...performed, ...untouched].map((r) => {
            const delta = r.last?.topWeight != null && r.prev?.topWeight != null ? r.last.topWeight - r.prev.topWeight : null;
            const doneSets = r.last ? r.last.we.sets.filter((s) => s.done) : [];
            return (
              <li key={r.ex.id}>
                <a className="exlib-row" href={href({ name: 'exercise', id: r.ex.id })} onClick={(e) => { e.preventDefault(); open(r.ex.id); }}>
                  <span className="exlib-main">
                    <span className="exlib-name"><Ltr>{r.ex.name}</Ltr></span>
                    <span className="exlib-meta">
                      <Chip className="inline-chip">{MUSCLE[r.ex.muscle]}</Chip>
                      {r.last ? (
                        <>
                          <span>{daysAgo(r.last.date, data.today)} · {shortDate(r.last.date)}</span>
                          <span>{r.sessions.length} אימונים</span>
                        </>
                      ) : <span className="muted">עוד לא בוצע</span>}
                    </span>
                    {r.last && (
                      <span className="exlib-sets" aria-label="הסטים בפעם האחרונה">
                        {doneSets.slice(0, 6).map((s) => (
                          <span key={s.id} className={`setpill ${s.type === 'warmup' ? 'warm' : ''}`}>{s.type === 'warmup' && <small>ח</small>}<Ltr>{kg(s.weight)}×{s.reps ?? '—'}</Ltr></span>
                        ))}
                        {doneSets.length > 6 && <span className="muted small">+{doneSets.length - 6}</span>}
                      </span>
                    )}
                  </span>
                  {r.last && (
                    <span className="exlib-side">
                      <span className="exlib-top" title="הסט הכבד ביותר באימון האחרון">
                        {r.last.topWeight != null ? <><Ltr>{num(r.last.topWeight)}</Ltr><small>ק״ג</small></> : <small>אין משקל</small>}
                      </span>
                      {delta != null && (
                        <span className={`exlib-delta ${delta > 0 ? 'up' : delta < 0 ? 'down' : ''}`}>
                          {delta === 0 ? 'ללא שינוי' : <><Ltr>{delta > 0 ? '+' : '−'}{num(Math.abs(delta))}</Ltr> ק״ג</>}
                        </span>
                      )}
                      {r.best != null && r.best !== r.last.topWeight && <span className="exlib-best">שיא <Ltr>{num(r.best)}</Ltr></span>}
                    </span>
                  )}
                  <Icon name="left" size={18} />
                </a>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
