import { useState } from 'react';
import { Sheet, Segmented, Button } from './ui';
import { SCENARIOS, useStore, type Scenario } from '../state/store';
import { mockControls } from '../data/repository';
import { isWorking } from '../lib/stats';
import { navigate } from '../lib/router';
import { kg } from '../lib/format';
import type { ExternalUpdate } from '../data/mutations';
import type { SetEntry } from '../data/types';

const LABEL: Record<Scenario, string> = { empty: 'ללא אימון', progress: 'באמצע', completed: 'הושלם' };

/** Prototype controls. Everything here is simulated; none of it is part of the product UI. */
export function DemoSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { state, actions } = useStore();
  const [scenario, setScenario] = useState<Scenario>('progress');
  const [failNext, setFailNext] = useState(false);
  const today = state.data.workouts.find((w) => w.date === state.data.today);
  const nameOf = (id: string) => state.data.exercises.find((e) => e.id === id)?.name ?? id;
  const seq = () => Date.now();
  const iso = (v: string | number) => `\u2066${v}\u2069`; // keep names/numbers LTR inside Hebrew sentences

  function find(exId: string) {
    return today?.exercises.find((e) => e.exerciseId === exId);
  }
  function fire(u: ExternalUpdate | null, note: string) {
    onClose();
    navigate({ name: 'today' });
    if (!u) return actions.toast(note);
    window.setTimeout(() => actions.emitExternal(u), 350);
  }
  function nextNewSet() {
    if (!today) return fire(null, 'אין אימון להיום. בחר "באמצע" למעלה.');
    const bench = find('bench');
    const squat = find('squat');
    const fp = find('facepull');
    const target = (we: typeof bench, label: string, w: number, r: number): ExternalUpdate | null => {
      const s = we?.sets.filter(isWorking).find((x) => !x.done && x.reps == null && x.weight == null);
      if (!we || !s) return null;
      const n = we.sets.filter(isWorking).indexOf(s) + 1;
      return { id: `u${seq()}`, at: Date.now(), mutation: { t: 'updateSet', setId: s.id, patch: { weight: w, reps: r, done: true } }, summary: `${iso(label)} · סט ${n} · ${iso(`${kg(w)} × ${r}`)}` };
    };
    const u = target(bench, nameOf('bench'), 60, 6) ?? target(squat, nameOf('squat'), 130, 4);
    if (u) return fire(u, '');
    if (fp) {
      const set: SetEntry = { id: `fp-${seq()}`, type: 'working', weight: 25, reps: 15, rir: null, rpe: null, done: true, by: 'assistant' };
      const n = fp.sets.filter(isWorking).length + 1;
      return fire({ id: `u${seq()}`, at: Date.now(), mutation: { t: 'addSet', weId: fp.id, set }, summary: `${iso(nameOf('facepull'))} · סט ${n} · ${iso('25 × 15')}` }, '');
    }
    fire(null, 'אין סטים פנויים לעדכון בהדגמה הזו.');
  }
  function correction() {
    const b = find('bench');
    const s = b?.sets.filter(isWorking)[1];
    if (!b || !s) {
      fire(null, 'אין סט 2 ב־Bench Press. בחר "באמצע" למעלה.');
      return null;
    }
    const reps = s.reps === 8 ? 7 : 8;
    return { u: { id: `u${seq()}`, at: Date.now(), mutation: { t: 'updateSet', setId: s.id, patch: { reps } } as const, summary: `${iso(nameOf('bench'))} · סט 2 · חזרות עודכנו ל־${iso(reps)}` } satisfies ExternalUpdate, s };
  }

  return (
    <Sheet open={open} onClose={onClose} title="כלי הדגמה" wide>
      <div className="form">
        <p className="muted small demo-note">פרוטוטיפ בלבד. אין חיבור לשרת או לעוזר: העדכונים כאן מדומים כדי להראות איך הממשק מגיב.</p>

        <section className="form-block">
          <h3>מצב אימון היום</h3>
          <Segmented label="מצב אימון היום" value={scenario} onChange={(v) => { setScenario(v); actions.setScenario(v); navigate({ name: 'today' }); }} options={SCENARIOS.map((s) => ({ value: s, label: LABEL[s] }))} />
        </section>

        <section className="form-block">
          <h3>עדכון חיצוני</h3>
          <div className="demo-grid">
            <Button onClick={nextNewSet}>רישום סט חדש</Button>
            <Button onClick={() => { const c = correction(); if (c) fire(c.u, ''); }}>תיקון חזרות (סט 2 בנצ׳)</Button>
            <Button
              onClick={() => {
                const c = correction();
                if (!c) return;
                onClose();
                navigate({ name: 'today' });
                actions.setDraftDirect(c.s.id, 'reps', '6');
                window.setTimeout(() => actions.emitExternal(c.u), 1200);
              }}
            >
              התנגשות עם עריכה שלי
            </Button>
          </div>
          <p className="muted small">"התנגשות" מקלידה 6 בסט 2 של Bench Press בלי לשמור, ואז מגיע מבחוץ עדכון לחזרות של אותו סט.</p>
        </section>

        <section className="form-block">
          <h3>שמירה וטעינה</h3>
          <label className="check-row">
            <input type="checkbox" checked={failNext} onChange={(e) => { setFailNext(e.target.checked); mockControls.failNextSave = e.target.checked; }} />
            <span>השמירה הבאה תיכשל</span>
          </label>
          <div className="demo-grid">
            <Button onClick={() => { mockControls.slowLoad = true; onClose(); navigate({ name: 'today' }); actions.reload(); window.setTimeout(() => (mockControls.slowLoad = false), 100); }}>טעינה איטית</Button>
            <Button onClick={() => { mockControls.failNextLoad = true; onClose(); navigate({ name: 'today' }); actions.reload(); }}>כשל טעינה</Button>
          </div>
        </section>

        <section className="form-block">
          <h3>תוכניות</h3>
          <p className="muted small">דוגמאות ה־JSON (תקין ושגוי) זמינות בחלון הייבוא במסך התוכניות.</p>
          <Button onClick={() => { onClose(); navigate({ name: 'plans' }); }}>למסך התוכניות</Button>
        </section>
      </div>
    </Sheet>
  );
}
