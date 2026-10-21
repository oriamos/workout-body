import { useMemo, useState } from 'react';
import { useStore } from '../state/store';
import { goBack, href, navigate, useRoute } from '../lib/router';
import { addDays, dayMonth, dayName, longDate, monthName, parseDate, toISO } from '../lib/format';
import { exerciseById, workoutTotals } from '../lib/stats';
import { PageHeader } from '../components/Shell';
import { Chip, EmptyState, IconButton, Ltr } from '../components/ui';
import { Icon } from '../components/Icon';
import { SessionSummary, WorkoutView, workoutPRs } from '../components/WorkoutView';

const DOW = ['א', 'ב', 'ג', 'ד', 'ה', 'ו', 'ש'];

function Calendar({ month, onMonth, trained, selected, today }: { month: string; onMonth: (m: string) => void; trained: Set<string>; selected?: string; today: string }) {
  const first = parseDate(month);
  const pad = first.getDay();
  const days = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate();
  const cells: (string | null)[] = [...Array(pad).fill(null), ...Array.from({ length: days }, (_, i) => toISO(new Date(first.getFullYear(), first.getMonth(), i + 1)))];
  const shift = (n: number) => onMonth(toISO(new Date(first.getFullYear(), first.getMonth() + n, 1)));
  return (
    <section className="cal" aria-label="לוח אימונים">
      <div className="cal-head">
        <IconButton icon="right" label="חודש קודם" onClick={() => shift(-1)} />
        <h2 className="cal-title">{monthName(first.getMonth())} {first.getFullYear()}</h2>
        <IconButton icon="left" label="חודש הבא" onClick={() => shift(1)} />
      </div>
      <div className="cal-grid">
        {DOW.map((d, i) => <span key={d} className="cal-dow" aria-hidden="true" title={dayName(addDays('2026-10-18', i))}>{d}</span>)}
        {cells.map((d, i) => {
          if (!d) return <span key={`b${i}`} className="cal-day blank" />;
          const t = trained.has(d);
          const cls = ['cal-day', t && 'trained', d === today && 'today', d === selected && 'sel', d > today && 'future'].filter(Boolean).join(' ');
          const label = `${longDate(d)}${t ? ', יש אימון' : ''}${d === today ? ', היום' : ''}`;
          return t ? (
            <button key={d} type="button" className={cls} aria-label={label} aria-current={d === selected ? 'date' : undefined} onClick={() => navigate({ name: 'history', date: d })}>{parseDate(d).getDate()}</button>
          ) : (
            <span key={d} className={cls} aria-label={label}>{parseDate(d).getDate()}</span>
          );
        })}
      </div>
      <p className="cal-legend"><span><span className="dot" /> יום עם אימון</span><span>מסגרת: היום</span></p>
    </section>
  );
}

export function HistoryScreen() {
  const route = useRoute();
  const { state } = useStore();
  const date = route.name === 'history' ? route.date : undefined;
  const [filter, setFilter] = useState<string>('all');
  const [showFilters, setShowFilters] = useState(false);
  const [monthState, setMonth] = useState<string | null>(null);
  const today = state.data.today;

  const workouts = useMemo(() => [...state.data.workouts].sort((a, b) => b.date.localeCompare(a.date)), [state.data.workouts]);
  const trained = useMemo(() => new Set(workouts.map((w) => w.date)), [workouts]);
  const plansUsed = useMemo(() => [...new Set(workouts.map((w) => w.name))], [workouts]);
  const shown = filter === 'all' ? workouts : workouts.filter((w) => w.name === filter);
  const selected = date ? workouts.find((w) => w.date === date) : undefined;
  const month = monthState ?? (selected ? selected.date.slice(0, 8) + '01' : today.slice(0, 8) + '01');

  const groups: { key: string; label: string; items: typeof shown }[] = [];
  for (const w of shown) {
    const k = w.date.slice(0, 7);
    let g = groups.find((x) => x.key === k);
    if (!g) groups.push((g = { key: k, label: `${monthName(parseDate(w.date).getMonth())} ${w.date.slice(0, 4)}`, items: [] }));
    g.items.push(w);
  }

  return (
    <div className="page">
      <PageHeader
        title={selected ? 'פרטי אימון' : 'היסטוריה'}
        sub={selected ? undefined : `${workouts.length} אימונים רשומים`}
        back={selected ? { label: 'חזרה להיסטוריה', onClick: () => goBack({ name: 'history' }) } : undefined}
      />
      <div className={`hist-grid ${selected || date ? 'has-detail' : ''}`}>
        <div className="hist-list-col">
          <Calendar month={month} onMonth={setMonth} trained={trained} selected={selected?.date} today={today} />
          <div className="filters">
            <button type="button" className="filter-toggle" aria-expanded={showFilters} onClick={() => setShowFilters((s) => !s)}>
              <Icon name="filter" size={16} /> סינון{filter !== 'all' && <>: <Ltr>{filter}</Ltr></>}
            </button>
            {showFilters && (
              <div className="filter-chips" role="group" aria-label="סינון לפי תוכנית">
                {['all', ...plansUsed].map((p) => (
                  <button key={p} type="button" className="pick-chip" aria-pressed={filter === p} onClick={() => setFilter(p)}>{p === 'all' ? 'הכול' : <Ltr>{p}</Ltr>}</button>
                ))}
              </div>
            )}
          </div>
          {shown.length === 0 && <EmptyState title="אין אימונים בסינון הזה" />}
          {groups.map((g) => (
            <div key={g.key}>
              <h2 className="hist-month">{g.label}</h2>
              <ul className="hist-list">
                {g.items.map((w) => {
                  const t = workoutTotals(w);
                  const prs = workoutPRs(state.data, w);
                  const top = w.exercises[0];
                  const topSet = top?.sets.filter((s) => s.done && s.type !== 'warmup' && s.weight != null).sort((a, b) => (b.weight as number) - (a.weight as number))[0];
                  return (
                    <li key={w.id}>
                      <a className={`hist-item ${selected?.id === w.id ? 'active' : ''}`} href={href({ name: 'history', date: w.date })} aria-current={selected?.id === w.id ? 'true' : undefined} onClick={(e) => { e.preventDefault(); navigate({ name: 'history', date: w.date }); }}>
                        <span className="hist-date"><b>{parseDate(w.date).getDate()}</b><span>{dayName(w.date)}</span></span>
                        <span className="hist-main">
                          <span className="hist-name"><Ltr>{w.name}</Ltr>{w.date === today && <Chip tone="accent" className="inline-chip">היום</Chip>}</span>
                          <span className="hist-hl">
                            {prs.length > 0 ? (
                              <><Icon name="trophy" size={14} /> שיא: <Ltr>{exerciseById(state.data, prs[0].exerciseId).name} {prs[0].weight}×{prs[0].reps}</Ltr></>
                            ) : topSet && top ? (
                              <Ltr>{exerciseById(state.data, top.exerciseId).name} {topSet.weight}×{topSet.reps}</Ltr>
                            ) : (
                              <span className="muted">אין סט מושלם עדיין</span>
                            )}
                          </span>
                        </span>
                        <span className="hist-sets"><b><Ltr>{t.done}/{t.planned}</Ltr></b>סטים</span>
                      </a>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>

        <div className="hist-detail-col">
          {selected ? (
            <div key={selected.id}>
              <p className="banner" role="status"><Icon name="edit" size={16} /><span>אתה עורך את האימון מתאריך <b>{longDate(selected.date)}</b>{selected.date !== today && <> ({dayMonth(selected.date)})</>}. שינויים נשמרים באימון הזה בלבד.</span></p>
              <h2 className="sr">{selected.name}</h2>
              <p className="hist-detail-title"><b className="detail-name"><Ltr>{selected.name}</Ltr></b></p>
              <SessionSummary workout={selected} mode="history" />
              <WorkoutView workout={selected} />
            </div>
          ) : date ? (
            <EmptyState icon="history" title="לא נמצא אימון בתאריך הזה" />
          ) : (
            <div className="hist-placeholder"><EmptyState icon="history" title="בחר אימון לצפייה ועריכה">הקש על יום מסומן בלוח או על שורה ברשימה.</EmptyState></div>
          )}
        </div>
      </div>
    </div>
  );
}
