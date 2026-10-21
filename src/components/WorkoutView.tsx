import { useMemo, useState } from 'react';
import type { Workout } from '../data/types';
import { useStore } from '../state/store';
import { exerciseById, exerciseSessions, exerciseStats, isWorking, workoutTotals } from '../lib/stats';
import { vol } from '../lib/format';
import { Icon } from './Icon';
import { Button, Chip, Ltr } from './ui';
import { ExerciseCard } from './ExerciseCard';
import { AddExerciseSheet } from './AddExerciseSheet';

export function workoutPRs(data: Parameters<typeof exerciseSessions>[0], w: Workout) {
  const out: { exerciseId: string; weight: number; reps: number | null }[] = [];
  for (const we of w.exercises) {
    const s = exerciseStats(we, w);
    if (s.topWeight == null) continue;
    const earlier = exerciseSessions(data, we.exerciseId).filter((x) => x.date < w.date && x.topWeight != null);
    if (earlier.length && s.topWeight > Math.max(...earlier.map((x) => x.topWeight as number))) out.push({ exerciseId: we.exerciseId, weight: s.topWeight, reps: s.bestReps });
  }
  return out;
}

export function SessionSummary({ workout, mode }: { workout: Workout; mode: 'today' | 'history' }) {
  const { state } = useStore();
  const t = workoutTotals(workout);
  const pct = t.planned ? Math.min(1, t.done / t.planned) : 0;
  const finished = t.planned > 0 && t.done >= t.planned;
  const exDone = workout.exercises.filter((we) => {
    const w = we.sets.filter(isWorking);
    return w.length > 0 && w.every((s) => s.done) && (!we.planned || w.length >= we.planned.sets);
  }).length;
  const prs = useMemo(() => workoutPRs(state.data, workout), [state.data, workout]);
  const skipped = workout.exercises.reduce((a, we) => a + (we.planned ? Math.max(0, we.planned.sets - we.sets.filter(isWorking).length) : 0), 0);
  const notDone = workout.exercises.reduce((a, we) => a + we.sets.filter((s) => isWorking(s) && !s.done && (s.reps != null || s.weight != null)).length, 0);

  return (
    <div className="summary" aria-label="סיכום אימון">
      <div className="summary-main">
        <div className="summary-sets">
          <span className="summary-num"><Ltr>{t.done}</Ltr></span>
          <span className="summary-of">מתוך <Ltr>{t.planned}</Ltr> סטים</span>
          {finished && <Chip tone="accent" icon="check">הושלם</Chip>}
          {!finished && t.done > 0 && mode === 'today' && <Chip>בתהליך</Chip>}
        </div>
        <div className="meter" role="progressbar" aria-valuemin={0} aria-valuemax={t.planned} aria-valuenow={t.done} aria-label="סטים שהושלמו מתוך המתוכננים">
          <span style={{ transform: `scaleX(${pct})` }} />
        </div>
      </div>
      <dl className="summary-stats">
        <div><dt>נפח שהושלם</dt><dd><Ltr>{vol(t.volume)}</Ltr> <small>ק״ג</small></dd></div>
        <div><dt>תרגילים</dt><dd><Ltr>{exDone}/{workout.exercises.length}</Ltr> <small>הושלמו</small></dd></div>
        <div><dt>{mode === 'today' ? 'התחלה' : 'שעה'}</dt><dd><Ltr>{workout.startedAt}</Ltr></dd></div>
      </dl>
      {(prs.length > 0 || skipped > 0 || notDone > 0) && (
        <ul className="summary-notes">
          {prs.map((p) => (
            <li key={p.exerciseId} className="hl-pr"><Icon name="trophy" size={14} /> שיא משקל: <Ltr>{exerciseById(state.data, p.exerciseId).name}</Ltr> <Ltr>{p.weight}×{p.reps}</Ltr></li>
          ))}
          {notDone > 0 && <li><Icon name="alert" size={14} /> <Ltr>{notDone}</Ltr> {notDone === 1 ? 'סט נרשם בלי להיות מסומן כהושלם' : 'סטים נרשמו בלי להיות מסומנים כהושלמו'}</li>}
          {skipped > 0 && mode === 'history' && <li><Icon name="info" size={14} /> <Ltr>{skipped}</Ltr> {skipped === 1 ? 'סט מתוכנן לא בוצע' : 'סטים מתוכננים לא בוצעו'}</li>}
        </ul>
      )}
    </div>
  );
}

export function WorkoutView({ workout }: { workout: Workout }) {
  const { actions, state } = useStore();
  const [adding, setAdding] = useState(false);
  const planned = workout.exercises.filter((we) => we.planned);
  const extra = workout.exercises.filter((we) => !we.planned);
  const isToday = workout.date === state.data.today;
  return (
    <div className="wv">
      <div className="ex-list">
        {planned.map((we) => <ExerciseCard key={we.id} workout={workout} we={we} />)}
      </div>
      {extra.length > 0 && (
        <>
          <h2 className="subhead">נוספו לאימון, לא בתוכנית</h2>
          <div className="ex-list">
            {extra.map((we) => <ExerciseCard key={we.id} workout={workout} we={we} />)}
          </div>
        </>
      )}
      <div className="add-ex">
        <Button variant="secondary" icon="plus" onClick={() => setAdding(true)}>הוסף תרגיל</Button>
        <p className="muted small">{isToday ? 'התרגיל יתווסף לאימון של היום בלבד.' : 'התרגיל יתווסף לאימון הזה בלבד.'}</p>
      </div>
      <AddExerciseSheet open={adding} onClose={() => setAdding(false)} onPick={(id) => actions.addExercise(id, workout.id)} exclude={workout.exercises.map((w) => w.exerciseId)} />
    </div>
  );
}
