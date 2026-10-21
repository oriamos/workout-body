import { useId, useRef, useState } from 'react';
import type { SetEntry, SetType } from '../data/types';
import { useStore } from '../state/store';
import { Icon } from './Icon';
import { Button, Ltr } from './ui';
import { VideoPlayer } from './Guide';
import { useRest } from './RestTimer';
import { kg } from '../lib/format';
import { prefs } from '../lib/prefs';
import { IS_MOCK } from '../data/source';

export const TYPE_LABEL: Record<SetType, string> = { warmup: 'חימום', working: 'עבודה', drop: 'ירידה', failure: 'כישלון' };
const TYPE_BADGE: Record<SetType, string> = { warmup: 'ח', working: '', drop: 'י', failure: 'כ' };
const RPE = [6, 7, 7.5, 8, 8.5, 9, 9.5, 10];
const RIR = [0, 1, 2, 3, 4];

export interface Ghost { weight: number | null; reps: number | null }

interface Props {
  set: SetEntry;
  number: number; // working-set number
  prev: SetEntry | null;
  ghost: Ghost;
  exerciseName: string;
  restSec: number;
  defaultOpen?: boolean;
}

export function SetRow({ set, number, prev, ghost: ghostIn, exerciseName, restSec, defaultOpen = false }: Props) {
  const { state, actions } = useStore();
  const rest = useRest();
  const [open, setOpen] = useState(defaultOpen);
  const uid = useId();
  const fileRef = useRef<HTMLInputElement>(null);
  // Reps logged from outside with no weight, or a finished set with no weight: don't paper over the gap.
  const gap = set.weight == null && (set.done || (set.reps != null && set.by === 'assistant'));
  const ghost: Ghost = gap ? { ...ghostIn, weight: null } : ghostIn;
  const d = state.drafts[set.id] ?? {};
  const errW = state.errors[`${set.id}:weight`];
  const errR = state.errors[`${set.id}:reps`];
  const conflicts = state.conflicts[set.id] ?? [];
  const fresh = state.fresh[set.id] != null;
  const failed = state.pending[set.id] === 'failed';
  const dirty = d.weight !== undefined || d.reps !== undefined;
  const wVal = d.weight ?? (set.weight == null ? '' : String(set.weight));
  const rVal = d.reps ?? (set.reps == null ? '' : String(set.reps));
  const entered = set.weight != null || set.reps != null || dirty;
  const missingW = gap && d.weight === undefined;
  const hasOptional = set.rpe != null || set.rir != null;
  const label = set.type === 'working' ? String(number) : TYPE_BADGE[set.type];
  const setName = `${set.type === 'working' ? `סט ${number}` : `סט ${TYPE_LABEL[set.type]}`}, ${exerciseName}`;

  const cls = ['set', set.done && 'is-done', !entered && !set.done && 'is-planned', dirty && 'is-dirty', fresh && 'is-fresh', (errW || errR) && 'is-error', missingW && 'is-missing', conflicts.length && 'is-conflict'].filter(Boolean).join(' ');

  const onKey = (field: 'weight' | 'reps') => (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      actions.commitField(set.id, field);
      (e.target as HTMLInputElement).blur();
    }
    if (e.key === 'Escape') {
      actions.revert(set.id, field);
      (e.target as HTMLInputElement).blur();
    }
  };

  const field = (f: 'weight' | 'reps', val: string, err: string | undefined, ph: number | null, width: string) => (
    <input
      className={`cell cell-${f} ${err ? 'invalid' : ''} ${d[f] !== undefined ? 'dirty' : ''}`}
      style={{ width }}
      value={val}
      placeholder={ph == null ? '—' : String(ph)}
      inputMode={f === 'weight' ? 'decimal' : 'numeric'}
      enterKeyHint="done"
      autoComplete="off"
      aria-label={`${f === 'weight' ? 'משקל בקילוגרם' : 'חזרות'}, ${setName}`}
      aria-invalid={!!err}
      aria-describedby={err ? `${uid}-${f}` : undefined}
      onFocus={(e) => e.currentTarget.select()}
      onChange={(e) => actions.draft(set.id, f, e.target.value)}
      onBlur={() => actions.commitField(set.id, f)}
      onKeyDown={onKey(f)}
      dir="ltr"
    />
  );

  return (
    <div className={cls} data-set-id={set.id}>
      <div className="set-row">
        <span className={`badge badge-${set.type}`} title={TYPE_LABEL[set.type]}>
          {label}
          {fresh && <span className="badge-dot" aria-hidden="true" />}
          <span className="sr">{TYPE_LABEL[set.type]}{fresh ? ', עודכן עכשיו' : ''}</span>
        </span>
        <button type="button" className="prev" disabled={!prev} onClick={() => prev && actions.patchSet(set.id, { weight: prev.weight, reps: prev.reps })} aria-label={prev ? `קודם: ${kg(prev.weight)} ק״ג × ${prev.reps}. הקש להעתקה` : 'אין ביצוע קודם'}>
          {prev ? <Ltr>{kg(prev.weight)}<span className="x">×</span>{prev.reps ?? '—'}</Ltr> : <span className="dash">—</span>}
        </button>
        {field('weight', wVal, errW, ghost.weight, '100%')}
        {field('reps', rVal, errR, ghost.reps, '100%')}
        <button
          type="button"
          className="check"
          aria-pressed={set.done}
          aria-label={set.done ? `סט הושלם, ${setName}. הקש לביטול` : `סמן כהושלם, ${setName}`}
          onClick={() => {
            const ok = actions.toggleDone(set.id, ghost);
            if (ok && prefs().autoRest) rest.start(restSec);
          }}
        >
          <Icon name="check" size={20} strokeWidth={2.4} />
        </button>
        <button type="button" className={`more ${open ? 'open' : ''}`} aria-expanded={open} aria-controls={`${uid}-detail`} aria-label={`פרטי סט${hasOptional ? ' (יש RPE/RIR)' : ''}${set.video ? ' (יש סרטון)' : ''}, ${setName}`} onClick={() => setOpen((o) => !o)}>
          <Icon name="down" size={18} />
          {hasOptional && <span className="more-dot" aria-hidden="true" />}
          {set.video && <Icon name="video" size={12} className="more-video" />}
        </button>
      </div>

      {(missingW || errW || errR || failed || conflicts.length > 0 || dirty) && (
        <div className="set-notes">
          {missingW && !errW && (
            <p className="note note-warn"><Icon name="alert" size={14} /> חסר משקל. {set.done ? 'הסט לא ייכלל בנפח.' : 'הוסף כדי להשלים את הסט.'}</p>
          )}
          {errW && <p id={`${uid}-weight`} className="note note-danger" role="alert"><Icon name="alert" size={14} /> {errW}</p>}
          {errR && <p id={`${uid}-reps`} className="note note-danger" role="alert"><Icon name="alert" size={14} /> {errR}</p>}
          {dirty && !errW && !errR && conflicts.length === 0 && <p className="note note-muted"><span className="dot" /> לא נשמר. Enter לשמירה, Esc לביטול</p>}
          {failed && (
            <p className="note note-danger" role="alert">
              <Icon name="alert" size={14} /> השמירה נכשלה, הערך מוצג אך לא נשמר.
              <button type="button" className="linkbtn" onClick={() => actions.retry(set.id)}>נסה שוב</button>
            </p>
          )}
          {conflicts.map((c) => (
            <div key={c.field} className="conflict" role="group" aria-label="התנגשות בין עדכון חיצוני לעריכה שלך">
              <p>
                <Icon name="assistant" size={15} /> <b>{c.field === 'reps' ? 'החזרות' : 'המשקל'}</b> עודכנו ל־<Ltr>{c.theirs ?? '—'}</Ltr> מחוץ לאפליקציה, ואתה הקלדת <Ltr>{c.mine || '—'}</Ltr>. מה לשמור?
              </p>
              <div className="conflict-actions">
                <Button size="sm" variant="secondary" onClick={() => actions.resolve(set.id, c.field, 'mine')}>השאר את שלי (<Ltr>{c.mine || '—'}</Ltr>)</Button>
                <Button size="sm" variant="primary" onClick={() => actions.resolve(set.id, c.field, 'theirs')}>קבל את העדכון (<Ltr>{c.theirs ?? '—'}</Ltr>)</Button>
              </div>
            </div>
          ))}
        </div>
      )}

      <div className={`set-detail ${open ? 'open' : ''}`} id={`${uid}-detail`} {...(open ? {} : ({ inert: "" } as object))}>
        <div className="set-detail-inner">
          <div className="opt">
            <span className="opt-label" id={`${uid}-type`}>סוג סט</span>
            <div className="opt-chips" role="radiogroup" aria-labelledby={`${uid}-type`}>
              {(Object.keys(TYPE_LABEL) as SetType[]).map((t) => (
                <button key={t} type="button" role="radio" aria-checked={set.type === t} className="optchip" onClick={() => actions.patchSet(set.id, { type: t })}>{TYPE_LABEL[t]}</button>
              ))}
            </div>
          </div>
          <div className="opt">
            <span className="opt-label" id={`${uid}-rpe`}>RPE <small>אופציונלי</small></span>
            <div className="opt-chips" role="group" aria-labelledby={`${uid}-rpe`}>
              {RPE.map((v) => (
                <button key={v} type="button" aria-pressed={set.rpe === v} className="optchip" onClick={() => actions.patchSet(set.id, { rpe: set.rpe === v ? null : v })}><Ltr>{v}</Ltr></button>
              ))}
            </div>
          </div>
          <div className="opt">
            <span className="opt-label" id={`${uid}-rir`}>RIR <small>אופציונלי</small></span>
            <div className="opt-chips" role="group" aria-labelledby={`${uid}-rir`}>
              {RIR.map((v) => (
                <button key={v} type="button" aria-pressed={set.rir === v} className="optchip" onClick={() => actions.patchSet(set.id, { rir: set.rir === v ? null : v })}><Ltr>{v}</Ltr></button>
              ))}
            </div>
          </div>
          <div className="opt">
            <span className="opt-label" id={`${uid}-vid`}>סרטון של הסט <small>אופציונלי</small></span>
            <input ref={fileRef} type="file" accept="video/*" hidden onChange={(e) => { actions.attachVideo(set.id, e.target.files?.[0]); e.target.value = ''; }} />
            {set.video ? (
              <div className="setvideo">
                <VideoPlayer src={set.video.url} label={`הסרטון שלי, ${setName}`} />
                <p className="small muted"><Ltr>{set.video.name}</Ltr></p>
                <div className="setvideo-actions">
                  <Button size="sm" variant="secondary" icon="refresh" onClick={() => fileRef.current?.click()}>החלף</Button>
                  <Button size="sm" variant="ghost" icon="trash" onClick={() => actions.removeVideo(set.id)}>הסר סרטון</Button>
                </div>
                <p className="small muted">{IS_MOCK ? 'בפרוטוטיפ הסרטון נשמר בדפדפן בלבד, ויימחק בטעינה מחדש.' : 'הסרטון נשמר בשרת האימונים.'}</p>
              </div>
            ) : (
              <div>
                <Button size="sm" variant="secondary" icon="video" aria-describedby={`${uid}-vid`} onClick={() => fileRef.current?.click()}>צרף סרטון</Button>
              </div>
            )}
          </div>
          <div className="opt-foot">
            <Button size="sm" variant="danger" icon="trash" onClick={() => actions.removeSet(set.id)}>מחק סט</Button>
          </div>
        </div>
      </div>
    </div>
  );
}
