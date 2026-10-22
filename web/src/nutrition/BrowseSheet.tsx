import { useEffect, useRef, useState } from 'react';
import { Button, Ltr, Sheet } from '../components/ui';
import { Icon } from '../components/Icon';
import type { Toast } from '../state/store';
import { longDate } from '../lib/format';
import { newKey, nutritionRepo } from './repository';
import { fmt, LABEL, UNIT } from './logic';
import { NUTRIENTS, type NutritionEntry } from './types';

type ToastFn = (text: string, tone?: Toast['tone'], action?: Toast['action']) => void;

function Details({ e, today, selected, onCopied, toast }: { e: NutritionEntry; today: string; selected: string; onCopied: (d: string) => void; toast: ToastFn }) {
  const [target, setTarget] = useState(selected);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const keys = useRef(new Map<string, string>()); // one key per (entry, date): a retry never copies twice
  async function copy(date: string) {
    if (!date) return;
    setBusy(true);
    setErr(null);
    const k = keys.current.get(date) ?? newKey();
    keys.current.set(date, k);
    try {
      await nutritionRepo.copy(e.id, date, k);
      keys.current.delete(date); // done: copying again on purpose makes a new copy
      toast(date === today ? `"${e.name}" הועתקה להיום` : `"${e.name}" הועתקה ל${longDate(date)}`);
      onCopied(date);
    } catch (x) {
      setErr(x instanceof Error ? x.message : 'ההעתקה נכשלה');
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="nut-detail">
      <dl className="nut-targets">
        {NUTRIENTS.map((k) => <div key={k}><dt>{LABEL[k]}</dt><dd>{e[k] == null ? <span className="muted">חסר</span> : <><Ltr>{fmt(e[k])}</Ltr> <small>{UNIT[k]}</small></>}</dd></div>)}
      </dl>
      {e.copied_from != null && <p className="muted small">הועתקה מארוחה קודמת.</p>}
      <div className="nut-copy">
        <Button size="sm" variant="primary" icon="copy" disabled={busy} onClick={() => void copy(today)}>העתקה להיום</Button>
        <label className="nut-copy-date">
          <span className="sr">תאריך להעתקה</span>
          <input type="date" dir="ltr" max={today} value={target} onChange={(x) => setTarget(x.target.value)} aria-label="תאריך להעתקה" />
        </label>
        <Button size="sm" variant="secondary" disabled={busy || !target || target > today} onClick={() => void copy(target)}>העתקה לתאריך</Button>
      </div>
      {err && <p className="note note-danger" role="alert"><Icon name="alert" size={14} /> {err}</p>}
    </div>
  );
}

/** Search earlier meals by name and date, and copy one to today or to another date. */
export function BrowseSheet({ open, today, selected, onClose, onCopied, toast }: { open: boolean; today: string; selected: string; onClose: () => void; onCopied: (d: string) => void; toast: ToastFn }) {
  const [q, setQ] = useState('');
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [list, setList] = useState<NutritionEntry[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [openId, setOpenId] = useState<number | null>(null);
  useEffect(() => {
    if (!open) return;
    let live = true;
    const t = window.setTimeout(() => {
      nutritionRepo.entries({ q: q.trim() || undefined, from: from || undefined, to: to || undefined, limit: 100 }).then(
        (l) => { if (live) { setList(l); setErr(null); } },
        (e) => live && setErr(e instanceof Error ? e.message : 'החיפוש נכשל'),
      );
    }, 250);
    return () => { live = false; window.clearTimeout(t); };
  }, [open, q, from, to]);

  const groups: { date: string; items: NutritionEntry[] }[] = [];
  for (const e of list ?? []) {
    const g = groups.find((x) => x.date === e.date);
    if (g) g.items.push(e);
    else groups.push({ date: e.date, items: [e] });
  }
  return (
    <Sheet open={open} onClose={onClose} title="ארוחות קודמות" wide>
      <div className="nut-browse">
        <label className="search">
          <Icon name="search" size={18} />
          <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="חיפוש לפי שם" aria-label="חיפוש ארוחה לפי שם" dir="auto" />
        </label>
        <div className="nut-range">
          <label><span>מתאריך</span><input type="date" dir="ltr" max={today} value={from} onChange={(e) => setFrom(e.target.value)} aria-label="מתאריך" /></label>
          <label><span>עד תאריך</span><input type="date" dir="ltr" max={today} value={to} onChange={(e) => setTo(e.target.value)} aria-label="עד תאריך" /></label>
          {(from || to) && <button type="button" className="linkbtn" onClick={() => { setFrom(''); setTo(''); }}>ניקוי תאריכים</button>}
        </div>
        {err && <p className="note note-danger" role="alert"><Icon name="alert" size={14} /> {err}</p>}
        {list && list.length === 0 && <p className="pick-empty">לא נמצאו ארוחות.</p>}
        {groups.map((g) => (
          <section key={g.date} aria-label={longDate(g.date)}>
            <h3 className="hist-month">{longDate(g.date)}{g.date === today && ' · היום'}</h3>
            <ul className="nut-list">
              {g.items.map((e) => (
                <li key={e.id}>
                  <button type="button" className="nut-entry" aria-expanded={openId === e.id} data-entry-id={e.id} onClick={() => setOpenId(openId === e.id ? null : e.id)}>
                    <span className="nut-entry-name">{e.name}</span>
                    <span className="nut-entry-vals"><span><Ltr>{fmt(e.calories_kcal)}</Ltr> <small>קק״ל</small></span><Icon name={openId === e.id ? 'up' : 'down'} size={16} /></span>
                  </button>
                  {openId === e.id && <Details e={e} today={today} selected={selected <= today ? selected : today} onCopied={onCopied} toast={toast} />}
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </Sheet>
  );
}
