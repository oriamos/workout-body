import { useEffect, useState } from 'react';
import { useStore, useToday } from '../state/store';
import { PageHeader } from '../components/Shell';
import { SessionSummary, WorkoutView } from '../components/WorkoutView';
import { Button, Chip, EmptyState, Ltr, Skeleton } from '../components/ui';
import { Icon } from '../components/Icon';
import { longDate } from '../lib/format';
import { goBack, navigate } from '../lib/router';
import { exerciseById } from '../lib/stats';
import { useRest } from '../components/RestTimer';

function ago(at: number, now: number) {
  const s = Math.max(0, Math.round((now - at) / 1000));
  if (s < 45) return 'עכשיו';
  const m = Math.round(s / 60);
  return m < 60 ? `לפני ${m} דק׳` : `לפני ${Math.round(m / 60)} שע׳`;
}

export function TodayLoading() {
  return (
    <div className="page" aria-busy="true" aria-label="טוען את אימון היום">
      <div className="page-head"><Skeleton w={190} h={26} /><div style={{ height: 8 }} /><Skeleton w={120} h={14} /></div>
      <div className="today-grid">
        <div className="ex-list">
          {[0, 1].map((i) => (
            <div key={i} className="ex" style={{ gap: 12 }}>
              <Skeleton w={150} h={18} />
              {[0, 1, 2].map((j) => <Skeleton key={j} h={44} className="block" />)}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function LoadError() {
  const { actions } = useStore();
  return (
    <div className="page">
      <EmptyState icon="alert" title="לא הצלחנו לטעון את האימונים" action={<Button variant="primary" icon="refresh" onClick={actions.reload}>נסה שוב</Button>}>
        הנתונים המקומיים לא נפגעו. אפשר לנסות שוב בעוד רגע.
      </EmptyState>
    </div>
  );
}

/** A day's workout (#/workout/YYYY-MM-DD), opened from the week. Logging happens on today's workout only. */
export function WorkoutScreen({ date }: { date?: string }) {
  const { state } = useStore();
  const today = state.data.today;
  const other = date && date !== today ? state.data.workouts.find((w) => w.date === date) : undefined;
  useEffect(() => {
    // A past day's workout lives in History (same editing view, with its own banner).
    if (other) navigate({ name: 'history', date: other.date }, { replace: true });
  }, [other]);
  if (date && date !== today) {
    return (
      <div className="page workout-page">
        <PageHeader title={longDate(date)} back={{ label: 'חזרה לשבוע', onClick: () => goBack({ name: 'today' }) }} />
        <EmptyState icon="today" title={date > today ? 'היום הזה עוד לא הגיע' : 'לא תועד אימון ביום הזה'} action={<Button variant="primary" onClick={() => navigate({ name: 'today' })}>חזרה לשבוע</Button>}>
          אימון מתעדים ביום שבו הוא מתבצע. התוכניות של היום מופיעות במסך השבוע.
        </EmptyState>
      </div>
    );
  }
  return <TodayWorkout />;
}

function TodayWorkout() {
  const { state, actions } = useStore();
  const workout = useToday();
  const rest = useRest();
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 15000);
    return () => window.clearInterval(id);
  }, []);
  const ext = state.lastExternal;

  const header = (
    <PageHeader
      status
      back={{ label: 'חזרה לשבוע', onClick: () => goBack({ name: 'today' }) }}
      title={longDate(state.data.today)}
      sub={
        workout ? (
          <>
            <span className="routine">{workout.planId ? <Ltr>{workout.name}</Ltr> : workout.name}</span>
            {ext && now - ext.at < 600000 && <span className="ext-note"><Icon name="assistant" size={13} /> עודכן {ago(ext.at, now)}</span>}
          </>
        ) : (
          'עוד לא תועד אימון'
        )
      }
      actions={workout ? <button type="button" className="iconbtn" aria-label="הפעל טיימר מנוחה של 90 שניות" title="טיימר מנוחה" onClick={() => rest.start(90)}><Icon name="timer" size={20} /></button> : undefined}
    />
  );

  if (!workout) {
    return (
      <div className="page workout-page">
        {header}
        <div className="today-empty">
          <EmptyState icon="today" title="אין אימון להיום">
            הסט הראשון שתתעד, כאן או בשיחה עם העוזר, יוצר את אימון היום. אין צורך ללחוץ על התחלה.
          </EmptyState>
          <section aria-labelledby="start-h">
            <h2 id="start-h" className="subhead">או התחל מתוכנית</h2>
            <ul className="start-list">
              {state.data.plans.map((p) => (
                <li key={p.id}>
                  <button type="button" onClick={() => actions.startFromPlan(p)}>
                    <span className="start-name"><Ltr>{p.name}</Ltr></span>
                    <span className="start-meta">{p.exercises.length} תרגילים · {p.exercises.reduce((a, e) => a + e.sets, 0)} סטים</span>
                    <Icon name="back" size={16} />
                  </button>
                </li>
              ))}
            </ul>
            <Button variant="ghost" icon="plus" onClick={() => actions.ensureToday()}>אימון חופשי, בלי תוכנית</Button>
          </section>
        </div>
      </div>
    );
  }

  const plan = state.data.plans.find((p) => p.id === workout.planId);
  return (
    <div className="page workout-page">
      {header}
      <div className="today-grid">
        <aside className="today-aside">
          <SessionSummary workout={workout} mode="today" />
          <p className="scope-note">
            <Icon name="info" size={14} />
            <span>
              {plan ? <>התוכנית <Ltr>{plan.name}</Ltr> נשארת כמו שהיא. </> : null}
              שינויים בסטים חלים על האימון של היום בלבד.
            </span>
          </p>
          {plan && (
            <Chip tone="neutral" className="aside-chip">
              <a href="#/plans" onClick={(e) => { e.preventDefault(); navigate({ name: 'plans', id: plan.id }); }}>לתוכנית</a>
            </Chip>
          )}
          <span className="sr">{workout.exercises.map((w) => exerciseById(state.data, w.exerciseId).name).join(', ')}</span>
        </aside>
        <div className="today-main">
          <WorkoutView workout={workout} />
        </div>
      </div>
    </div>
  );
}
