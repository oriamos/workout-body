import { useRef, useState } from 'react';
import { Sheet, Button, Chip, Ltr } from './ui';
import { Icon } from './Icon';
import { useStore } from '../state/store';
import { SAMPLE_BROKEN, SAMPLE_INVALID, SAMPLE_VALID, validatePlanJson, type ImportResult } from '../lib/planImport';
import { navigate } from '../lib/router';
import { IS_MOCK } from '../data/source';

/** Power-user flow: pick or drop a file, preview, fix or confirm. Samples exist because the prototype has no real files. */
export function ImportSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { state, actions } = useStore();
  const [result, setResult] = useState<ImportResult | null>(null);
  const [fileName, setFileName] = useState('');
  const [drag, setDrag] = useState(false);
  const [paste, setPaste] = useState(false);
  const [text, setText] = useState('');
  const input = useRef<HTMLInputElement>(null);

  const reset = () => { setResult(null); setFileName(''); setText(''); setPaste(false); };
  const close = () => { reset(); onClose(); };
  const run = (t: string, name: string) => { setFileName(name); setResult(validatePlanJson(t, state.data)); };
  const read = (f: File | undefined) => { if (f) f.text().then((t) => run(t, f.name)); };

  const confirm = () => {
    if (!result?.build) return;
    const { plan, newExercises } = result.build(state.data);
    newExercises.forEach(actions.addLibraryExercise);
    actions.savePlan(plan);
    reset();
    onClose();
    navigate({ name: 'plans', id: plan.id });
    actions.toast(`התוכנית "${plan.name}" יובאה`);
  };

  const ok = result && result.errors.length === 0;
  return (
    <Sheet
      open={open}
      onClose={close}
      wide
      title="ייבוא תוכנית מ־JSON"
      footer={
        result ? (
          <>
            <Button variant="ghost" onClick={reset}>בחירה מחדש</Button>
            <Button variant="primary" disabled={!ok} onClick={confirm}>{ok ? 'ייבא תוכנית' : 'יש לתקן את הקובץ'}</Button>
          </>
        ) : undefined
      }
    >
      {!result ? (
        <div className="import-pick">
          <div
            className={`drop ${drag ? 'drag' : ''}`}
            onDragOver={(e) => { e.preventDefault(); setDrag(true); }}
            onDragLeave={() => setDrag(false)}
            onDrop={(e) => { e.preventDefault(); setDrag(false); read(e.dataTransfer.files[0]); }}
          >
            <Icon name="upload" size={26} />
            <p><b>גרור לכאן קובץ JSON</b></p>
            <Button variant="secondary" onClick={() => input.current?.click()}>בחירת קובץ</Button>
            <input ref={input} type="file" accept=".json,application/json" hidden onChange={(e) => { read(e.target.files?.[0]); e.target.value = ''; }} />
          </div>
          <button type="button" className="linkbtn" aria-expanded={paste} onClick={() => setPaste((p) => !p)}>או הדבק JSON</button>
          {paste && (
            <div className="paste">
              <textarea dir="ltr" rows={6} value={text} onChange={(e) => setText(e.target.value)} aria-label="הדבקת JSON" spellCheck={false} />
              <Button variant="primary" size="sm" disabled={!text.trim()} onClick={() => run(text, 'JSON מודבק')}>תצוגה מקדימה</Button>
            </div>
          )}
          {IS_MOCK && <div className="samples">
            <p className="muted small">בפרוטוטיפ אפשר לנסות דוגמאות במקום קובץ אמיתי:</p>
            <div className="demo-grid">
              <Button size="sm" onClick={() => run(SAMPLE_VALID, 'push-day.json')}>דוגמה תקינה</Button>
              <Button size="sm" onClick={() => run(SAMPLE_INVALID, 'pull-day.json')}>דוגמה עם שגיאות</Button>
              <Button size="sm" onClick={() => run(SAMPLE_BROKEN, 'legs.json')}>קובץ שבור</Button>
            </div>
          </div>}
        </div>
      ) : (
        <div className="import-preview">
          <p className="muted small"><Icon name="file" size={14} /> <Ltr>{fileName}</Ltr></p>
          {result.name && <h3 className="import-name"><Ltr>{result.name}</Ltr> <Chip tone={ok ? 'accent' : 'danger'}>{ok ? 'תקין' : `${result.errors.length} שגיאות`}</Chip></h3>}
          {result.errors.length > 0 && (
            <div className="issues issues-error" role="alert">
              <h4>צריך לתקן לפני הייבוא</h4>
              <ul>{result.errors.map((e, i) => <li key={i}><Icon name="alert" size={15} /> <span>{e.text}</span></li>)}</ul>
            </div>
          )}
          {result.warnings.length > 0 && (
            <div className="issues issues-warn">
              <h4>שים לב</h4>
              <ul>{result.warnings.map((e, i) => <li key={i}><Icon name="info" size={15} /> <span>{e.text}</span></li>)}</ul>
            </div>
          )}
          {result.rows.length > 0 && (
            <table className="preview-table">
              <caption className="sr">תצוגה מקדימה של התרגילים</caption>
              <thead><tr><th scope="col">#</th><th scope="col">תרגיל</th><th scope="col">סטים</th><th scope="col">חזרות</th><th scope="col">יעד</th></tr></thead>
              <tbody>
                {result.rows.map((r, i) => (
                  <tr key={i} className={r.ok ? '' : 'bad'}>
                    <td>{r.ok ? i + 1 : <Icon name="alert" size={14} />}</td>
                    <td><Ltr>{r.name}</Ltr>{!r.known && r.ok && <Chip tone="warn" className="inline-chip">חדש</Chip>}</td>
                    <td><Ltr>{r.sets ?? '—'}</Ltr></td>
                    <td><Ltr>{r.reps}</Ltr></td>
                    <td className="muted">{[r.weight != null ? `${r.weight} ק״ג` : null, r.rir != null ? `RIR ${r.rir}` : null, r.rpe != null ? `RPE ${r.rpe}` : null].filter(Boolean).join(' · ') || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </Sheet>
  );
}
