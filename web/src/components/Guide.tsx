import { useState } from 'react';
import type { Exercise } from '../data/types';
import { useStore } from '../state/store';
import { href, navigate } from '../lib/router';
import { kg, longDate } from '../lib/format';
import { Icon } from './Icon';
import { Chip, Ltr, Segmented, Sheet } from './ui';

export function VideoPlayer({ src, poster, label }: { src: string; poster?: string; label: string }) {
  return <video className="video" src={src} poster={poster} controls playsInline preload="metadata" aria-label={label} />;
}

function HowToText({ ex }: { ex: Exercise }) {
  const h = ex.howTo;
  if (!h) return <p className="muted">עוד אין הסבר לתרגיל הזה.</p>;
  return (
    <div className="howto">
      <p className="howto-summary">{h.summary}</p>
      <h4>ביצוע</h4>
      <ol className="howto-steps">{h.steps.map((s) => <li key={s}>{s}</li>)}</ol>
      <h4>כדאי לזכור</h4>
      <ul className="howto-cues">{h.cues.map((c) => <li key={c}><Icon name="check" size={15} strokeWidth={2.4} /> {c}</li>)}</ul>
      <h4>טעויות נפוצות</h4>
      <ul className="howto-cues bad">{h.mistakes.map((c) => <li key={c}><Icon name="alert" size={15} /> {c}</li>)}</ul>
      <p className="caveat">הסבר כללי ולא תחליף למאמן. אם משהו כואב, עצור.</p>
    </div>
  );
}

/** Every video the user attached to a set of this exercise, newest first. */
export function MyVideos({ exerciseId, onOpenWorkout }: { exerciseId: string; onOpenWorkout?: () => void }) {
  const { state } = useStore();
  const items = state.data.workouts
    .flatMap((w) => w.exercises.filter((we) => we.exerciseId === exerciseId).flatMap((we) => we.sets.filter((s) => s.video).map((s) => ({ w, s, n: we.sets.filter((x) => x.type === 'working').indexOf(s) + 1 }))))
    .sort((a, b) => b.w.date.localeCompare(a.w.date));
  if (!items.length)
    return (
      <div className="video-empty">
        <Icon name="video" size={24} />
        <p><b>עוד אין סרטונים שלך בתרגיל הזה.</b></p>
        <p className="muted small">בכל סט, פתח את פרטי הסט (החץ בקצה השורה) וצרף סרטון. הוא יופיע כאן עם התאריך והסט.</p>
      </div>
    );
  return (
    <ul className="my-videos">
      {items.map(({ w, s, n }) => (
        <li key={s.id}>
          <VideoPlayer src={s.video!.url} label={`הסרטון שלי, ${longDate(w.date)}`} />
          <p className="my-video-meta">
            <a href={href({ name: 'history', date: w.date })} onClick={(e) => { e.preventDefault(); onOpenWorkout?.(); navigate({ name: 'history', date: w.date }); }}>{longDate(w.date)}</a>
            {' · '}{s.type === 'working' ? `סט ${n}` : 'חימום'} · <Ltr>{kg(s.weight)}×{s.reps ?? '—'}</Ltr>
          </p>
        </li>
      ))}
    </ul>
  );
}

type Tab = 'how' | 'demo' | 'mine';

export function GuideTabs({ ex, onOpenWorkout }: { ex: Exercise; onOpenWorkout?: () => void }) {
  const { state } = useStore();
  const [tab, setTab] = useState<Tab>('how');
  const mine = state.data.workouts.reduce((a, w) => a + w.exercises.filter((we) => we.exerciseId === ex.id).reduce((b, we) => b + we.sets.filter((s) => s.video).length, 0), 0);
  return (
    <div className="guide">
      <Segmented size="sm" label="הסבר וסרטונים" value={tab} onChange={setTab} options={[{ value: 'how', label: 'הסבר' }, { value: 'demo', label: 'סרטון הדגמה' }, { value: 'mine', label: `הסרטונים שלי${mine ? ` (${mine})` : ''}` }]} />
      <div className="guide-body">
        {tab === 'how' && <HowToText ex={ex} />}
        {tab === 'demo' && (
          <div className="demo">
            {ex.demoVideo ? <VideoPlayer src={ex.demoVideo} poster="/demo-poster.jpg" label={`סרטון הדגמה: ${ex.name}`} /> : <p className="muted">אין סרטון הדגמה לתרגיל הזה.</p>}
            <p className="muted small"><Chip tone="warn" className="inline-chip">ממלא מקום</Chip> בפרוטוטיפ זה אותו קטע לדוגמה בכל התרגילים. בגרסה אמיתית יגיע כאן סרטון ההדגמה של התרגיל.</p>
          </div>
        )}
        {tab === 'mine' && <MyVideos exerciseId={ex.id} onOpenWorkout={onOpenWorkout} />}
      </div>
    </div>
  );
}

export function GuideSheet({ ex, open, onClose }: { ex: Exercise; open: boolean; onClose: () => void }) {
  return (
    <Sheet open={open} onClose={onClose} title={ex.name} wide>
      <GuideTabs ex={ex} onOpenWorkout={onClose} />
    </Sheet>
  );
}
