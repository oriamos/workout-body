import { useState } from 'react';
import type { Workout, WorkoutExercise } from '../data/types';
import { useStore } from '../state/store';
import { exerciseById, exerciseSessions, isWorking, previousFor, previousSession } from '../lib/stats';
import { href, navigate } from '../lib/router';
import { dayMonth, kg, shortDate } from '../lib/format';
import { Icon } from './Icon';
import { Button, Ltr, Menu } from './ui';
import { SetRow, type Ghost } from './SetRow';

export function prescriptionText(p: NonNullable<WorkoutExercise['planned']>) {
  const reps = p.repMin === p.repMax ? `${p.repMin}` : `${p.repMin}–${p.repMax}`;
  return { main: `${p.sets} × ${reps}`, extra: [p.weight != null ? `יעד ${kg(p.weight)} ק״ג` : null, p.rir != null ? `RIR ${p.rir}` : null, p.rpe != null ? `RPE ${p.rpe}` : null].filter(Boolean) as string[] };
}

export function ExerciseCard({ workout, we, onFocusExercise }: { workout: Workout; we: WorkoutExercise; onFocusExercise?: (id: string) => void }) {
  const { state, actions } = useStore();
  const ex = exerciseById(state.data, we.exerciseId);
  const prev = previousSession(state.data, we.exerciseId, workout.date);
  const [peek, setPeek] = useState(false);
  const [editNote, setEditNote] = useState(false);
  const [noteDraft, setNoteDraft] = useState(we.note);

  const working = we.sets.filter(isWorking);
  const doneN = working.filter((s) => s.done).length;
  const total = Math.max(we.planned?.sets ?? 0, working.length);

  // Ghost values: what an empty field will take when the set is confirmed.
  let lastEntered: Ghost = { weight: null, reps: null };
  let n = 0;
  const rows = we.sets.map((s) => {
    const p = previousFor(prev, s, we.sets);
    const planned = we.planned;
    const ghost: Ghost = {
      weight: planned?.weight ?? p?.weight ?? lastEntered.weight,
      reps: planned && planned.repMin === planned.repMax ? planned.repMin : p?.reps ?? planned?.repMax ?? lastEntered.reps,
    };
    if (s.weight != null) lastEntered = { weight: s.weight, reps: s.reps ?? lastEntered.reps };
    if (s.type === 'working') n++;
    return { s, p, ghost, number: n };
  });

  const history = exerciseSessions(state.data, we.exerciseId).filter((x) => x.date < workout.date).slice(-3).reverse();
  const presc = we.planned ? prescriptionText(we.planned) : null;

  return (
    <section className="ex" aria-labelledby={`${we.id}-h`} data-we={we.id}>
      <header className="ex-head">
        <div className="ex-title">
          <h3 id={`${we.id}-h`}>
            <a href={href({ name: 'exercise', id: ex.id })} onClick={(e) => { e.preventDefault(); navigate({ name: 'exercise', id: ex.id }); }}>
              <Ltr>{ex.name}</Ltr>
            </a>
          </h3>
          <p className="ex-meta">
            {presc ? (
              <>
                <span className="ex-presc" title="מהתוכנית. עריכת סטים כאן לא משנה אותה"><Ltr>{presc.main}</Ltr></span>
                {presc.extra.map((x) => <span key={x}> · {x}</span>)}
              </>
            ) : (
              <span>לא בתוכנית</span>
            )}
          </p>
        </div>
        <span className={`ex-count ${total > 0 && doneN >= total ? 'complete' : ''}`} aria-label={`${doneN} מתוך ${total} סטים הושלמו`}>
          {total > 0 && doneN >= total && <Icon name="check" size={14} strokeWidth={2.6} />}
          <Ltr>{doneN}/{total}</Ltr>
        </span>
        <Menu
          label={`אפשרויות, ${ex.name}`}
          items={[
            { label: we.note ? 'עריכת הערה' : 'הוספת הערה', icon: 'note', onSelect: () => { setNoteDraft(we.note); setEditNote(true); } },
            { label: 'פרטי תרגיל', icon: 'progress', onSelect: () => navigate({ name: 'exercise', id: ex.id }) },
            { label: 'הסר תרגיל', icon: 'trash', danger: true, onSelect: () => actions.removeExercise(we.id) },
          ]}
        />
      </header>

      {editNote ? (
        <div className="note-edit">
          <textarea
            autoFocus
            rows={2}
            value={noteDraft}
            aria-label={`הערה, ${ex.name}`}
            placeholder="הערה לתרגיל (למשל: גובה ספסל, תחושה)"
            onChange={(e) => setNoteDraft(e.target.value)}
            onKeyDown={(e) => e.key === 'Escape' && setEditNote(false)}
          />
          <div className="note-edit-actions">
            <Button size="sm" variant="ghost" onClick={() => setEditNote(false)}>ביטול</Button>
            <Button size="sm" variant="primary" onClick={() => { actions.setNote(we.id, noteDraft.trim()); setEditNote(false); }}>שמור הערה</Button>
          </div>
        </div>
      ) : (
        we.note && (
          <button type="button" className="ex-note" onClick={() => { setNoteDraft(we.note); setEditNote(true); }}>
            <Icon name="note" size={14} /> {we.note}
          </button>
        )
      )}

      <div className="sets">
        <div className="sets-head" aria-hidden="true">
          <span>סט</span>
          <span>קודם</span>
          <span>ק״ג</span>
          <span>חזרות</span>
          <span />
          <span />
        </div>
        {rows.map((r) => (
          <SetRow key={r.s.id} set={r.s} number={r.number} prev={r.p} ghost={r.ghost} exerciseName={ex.name} restSec={ex.restSec} defaultOpen={r.s.id.endsWith('-bench-1') && workout.date === state.data.today} />
        ))}
        {!prev && we.sets.every((s) => s.weight == null && s.reps == null) && (
          <p className="first-time"><Icon name="info" size={14} /> אין היסטוריה לתרגיל הזה. הסט הראשון ייקבע את נקודת ההתחלה.</p>
        )}
      </div>

      <div className="ex-foot">
        <Button size="sm" variant="ghost" icon="plus" onClick={() => actions.addSet(we.id)}>הוסף סט</Button>
        <Button size="sm" variant="ghost" icon="clock" aria-expanded={peek} onClick={() => setPeek((p) => !p)}>
          היסטוריה אחרונה
          <Icon name={peek ? 'up' : 'down'} size={14} />
        </Button>
      </div>

      <div className={`peek ${peek ? 'open' : ''}`} {...(peek ? {} : ({ inert: '' } as object))}>
        <div className="peek-inner">
          {history.length === 0 ? (
            <p className="peek-empty">עדיין לא בוצע תרגיל זה.</p>
          ) : (
            <ul>
              {history.map((h) => (
                <li key={h.date}>
                  <a href={href({ name: 'history', date: h.date })} onClick={(e) => { e.preventDefault(); navigate({ name: 'history', date: h.date }); }} className="peek-date">
                    {shortDate(h.date)} <span className="peek-plan">{h.workout.name}</span>
                  </a>
                  <span className="peek-sets">
                    {h.we.sets.filter((s) => s.done && isWorking(s)).map((s) => (
                      <Ltr key={s.id} className="peek-set">{kg(s.weight)}×{s.reps ?? '—'}</Ltr>
                    ))}
                  </span>
                </li>
              ))}
            </ul>
          )}
          <a className="peek-all" href={href({ name: 'exercise', id: ex.id })} onClick={(e) => { e.preventDefault(); onFocusExercise?.(ex.id); navigate({ name: 'exercise', id: ex.id }); }}>
            כל ההיסטוריה והגרפים <Icon name="back" size={14} />
          </a>
          {history.length > 0 && <span className="sr">{dayMonth(history[0].date)}</span>}
        </div>
      </div>
    </section>
  );
}
