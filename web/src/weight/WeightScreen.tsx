import { useCallback, useEffect, useRef, useState } from 'react';
import { useStore } from '../state/store';
import { PageHeader } from '../components/Shell';
import { Button, EmptyState, IconButton, Ltr, Segmented, Sheet, Skeleton } from '../components/ui';
import { Icon, type IconName } from '../components/Icon';
import { LineChart } from '../components/Charts';
import { ApiError } from '../data/apiRepository';
import { longDate, shortDate } from '../lib/format';
import { weightRepo } from './repository';
import { chartSeries, entriesNewestFirst, entryOn, fmtKg, fmtSigned, headline, minMaxText, plusDays, RANGES, TREND_LABEL, weekRange, weeksNewestFirst, type RangeWeeks } from './logic';
import type { WeighIn, WeightTrend } from './types';
import { useWeighForm, WeighFormFields, type WeighFormProps } from './WeighForm';
import './weight.css';

const POLL_MS = 5000;
const RECENT = 10;
const RANGE_KEY = 'wb.weight.weeks';
const DIR_ICON: Record<'up' | 'down' | 'flat', IconName> = { up: 'arrowUp', down: 'arrowDown', flat: 'minus' };

function readRange(): RangeWeeks {
  try {
    const v = Number(localStorage.getItem(RANGE_KEY));
    return (RANGES as readonly number[]).includes(v) ? (v as RangeWeeks) : 8;
  } catch { return 8; }
}

/** The inline form on the page (log today / a past date). */
function InlineForm(p: WeighFormProps) {
  const f = useWeighForm(p);
  return <WeighFormFields f={f} p={p} />;
}

function EditSheet({ entry, onClose, ...p }: Omit<WeighFormProps, 'date' | 'base' | 'idPrefix'> & { entry: WeighIn; onClose: () => void }) {
  const props: WeighFormProps = { ...p, date: entry.date, base: entry, idPrefix: 'wt-edit', onCancel: onClose };
  const f = useWeighForm(props);
  return (
    <Sheet
      open
      onClose={onClose}
      title={`עריכת שקילה · ${shortDate(entry.date)}`}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>ביטול</Button>
          <Button variant="primary" disabled={f.saving} onClick={() => void f.submit()}>{f.saving ? 'שומר…' : 'שמירה'}</Button>
        </>
      }
    >
      <p className="muted wt-sheet-date">{longDate(entry.date)}</p>
      <WeighFormFields f={f} p={props} />
    </Sheet>
  );
}

type FormState = { key: number; date: string; base: WeighIn | null; explicit: boolean };

export function WeightScreen() {
  const { state, actions } = useStore();
  const today = state.data.today;
  const [weeks, setWeeks] = useState<RangeWeeks>(readRange);
  const [trend, setTrend] = useState<WeightTrend | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [form, setForm] = useState<FormState | null>(null);
  const [editing, setEditing] = useState<WeighIn | null>(null);
  const [confirm, setConfirm] = useState<WeighIn | null>(null);
  const [rowMsg, setRowMsg] = useState<{ date: string; text: string } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const last = useRef('');
  const formDirty = useRef(false);
  const formRef = useRef<HTMLFormElement>(null);
  const keyN = useRef(0);
  const current = useRef(weeks);
  current.current = weeks;

  const refresh = useCallback(async () => {
    const w = weeks;
    try {
      const fresh = await weightRepo.trend(w);
      if (current.current !== w) return;
      const json = JSON.stringify(fresh);
      if (json !== last.current) { last.current = json; setTrend(fresh); }
      setLoadErr(null);
    } catch (e) {
      if (current.current === w) setLoadErr(e instanceof ApiError || e instanceof Error ? e.message : 'הטעינה נכשלה');
    }
  }, [weeks]);

  useEffect(() => {
    void refresh();
    // Weigh-ins logged elsewhere (e.g. through the assistant) appear by polling while the tab is visible.
    // Open forms keep their own values and precondition, so a refresh never touches what is typed.
    const tick = () => { if (document.visibilityState === 'visible') void refresh(); };
    const id = window.setInterval(tick, POLL_MS);
    document.addEventListener('visibilitychange', tick);
    window.addEventListener('focus', tick);
    return () => { window.clearInterval(id); document.removeEventListener('visibilitychange', tick); window.removeEventListener('focus', tick); };
  }, [refresh]);

  const todayEntry = trend ? entryOn(trend.entries, today) : null;

  // The quick-log form: shown by default while today has no weigh-in. A default form nobody has
  // typed in (or focused) gives way to today's weigh-in when one arrives from elsewhere.
  useEffect(() => {
    if (!trend) return;
    if (!form && !todayEntry) {
      formDirty.current = false;
      setForm({ key: ++keyN.current, date: today, base: null, explicit: false });
    } else if (form && !form.explicit && todayEntry && !formDirty.current && !formRef.current?.contains(document.activeElement)) {
      setForm(null);
    }
  }, [trend, todayEntry, form, today]);

  const openForm = (date: string, base: WeighIn | null) => { formDirty.current = false; setForm({ key: ++keyN.current, date, base, explicit: true }); };
  const setRange = (w: RangeWeeks) => {
    setWeeks(w);
    try { localStorage.setItem(RANGE_KEY, String(w)); } catch { /* per-viewer convenience only */ }
  };
  const onDirty = useCallback((d: boolean) => { formDirty.current = d; }, []);

  async function remove(e: WeighIn) {
    setDeleting(true);
    try {
      await weightRepo.remove(e.date, e.revision);
      actions.toast(`השקילה של ${shortDate(e.date)} נמחקה`);
      setConfirm(null);
      setRowMsg(null);
    } catch (err) {
      setConfirm(null);
      if (err instanceof ApiError && (err.status === 412 || err.status === 404)) {
        const fresh = await weightRepo.get(e.date).catch(() => null);
        setRowMsg({ date: e.date, text: fresh ? `השקילה השתנתה במקום אחר ולא נמחקה. בשרת עכשיו: ${fmtKg(fresh.weight_kg)} ק״ג.` : 'השקילה כבר נמחקה במקום אחר.' });
      } else setRowMsg({ date: e.date, text: err instanceof Error ? err.message : 'המחיקה נכשלה' });
    } finally {
      setDeleting(false);
      void refresh();
    }
  }

  const header = <PageHeader title="משקל" sub="שקילה יומית ומגמה שבועית" />;

  if (!trend) {
    return (
      <div className="page weight">
        {header}
        {loadErr ? (
          <EmptyState icon="alert" title="לא הצלחנו לטעון את השקילות" action={<Button variant="primary" icon="refresh" onClick={() => void refresh()}>נסה שוב</Button>}>{loadErr}</EmptyState>
        ) : (
          <div className="wt-grid" aria-busy="true">
            <div className="glass-panel"><Skeleton w={140} h={18} /><div style={{ height: 12 }} /><Skeleton h={44} className="block" /></div>
            <div className="glass-panel"><Skeleton w={180} h={30} /><div style={{ height: 10 }} /><Skeleton w={220} h={14} /></div>
            <div className="glass-panel"><Skeleton h={190} className="block" /></div>
          </div>
        )}
      </div>
    );
  }

  const hl = headline(trend);
  const { daily, weekly } = chartSeries(trend);
  const recent = entriesNewestFirst(trend.entries);
  const shown = showAll ? recent : recent.slice(0, RECENT);
  const formProps = { today, known: trend.entries, knownFrom: trend.from, onChanged: () => void refresh(), toast: actions.toast };

  const quick = (
    <section className="glass-panel wt-quick" aria-labelledby="wt-quick-h">
      <h2 id="wt-quick-h" className="block-title">{form && form.date !== today && form.explicit ? 'רישום שקילה' : 'שקילה היום'}</h2>
      {form ? (
        <InlineForm
          key={form.key}
          {...formProps}
          date={form.date}
          base={form.base}
          dateEditable
          idPrefix="wt-quick"
          inlineActions
          formRef={formRef}
          onDirty={onDirty}
          onCancel={form.explicit ? () => setForm(null) : undefined}
          onSaved={async () => { await refresh(); formDirty.current = false; setForm(null); }}
        />
      ) : todayEntry ? (
        <div className="wt-today">
          <p className="wt-today-val"><Ltr className="wt-today-kg">{fmtKg(todayEntry.weight_kg)}</Ltr> <span className="unit">ק״ג</span></p>
          {todayEntry.note && <p className="wt-today-note">{todayEntry.note}</p>}
          <div className="wt-today-actions">
            <Button size="sm" icon="edit" onClick={() => openForm(today, todayEntry)}>עריכה</Button>
            <Button size="sm" variant="ghost" icon="calendar" onClick={() => openForm(plusDays(today, -1), entryOn(trend.entries, plusDays(today, -1)))}>רישום לתאריך קודם</Button>
          </div>
        </div>
      ) : null}
    </section>
  );

  const head = (
    <section className="glass-panel wt-headline" aria-labelledby="wt-hl-h">
      <h2 id="wt-hl-h" className="block-title">ממוצע השבוע <span className="muted wt-hl-range">{hl.week && <Ltr>{weekRange(hl.week)}</Ltr>}</span></h2>
      <p className="wt-avg-row">
        <Ltr className="wt-avg">{fmtKg(hl.average)}</Ltr> <span className="unit">ק״ג</span>
        <span className="wt-count">{hl.countText}</span>
      </p>
      {hl.average == null && <p className="muted small">עוד אין שקילות השבוע.</p>}
      <p className="wt-change" data-dir={hl.comparison?.dir ?? 'none'}>
        {hl.comparison ? (
          <>
            <span className="wt-dir" aria-hidden="true"><Icon name={DIR_ICON[hl.comparison.dir]} size={15} /></span>
            <span className="wt-change-text">{hl.comparisonText}</span>
          </>
        ) : (
          <span className="muted">{hl.comparisonText}</span>
        )}
      </p>
      {trend.latest && <p className="muted small wt-latest">שקילה אחרונה: <Ltr>{fmtKg(trend.latest.weight_kg)}</Ltr> ק״ג, {trend.latest.date === today ? 'היום' : shortDate(trend.latest.date)}</p>}
    </section>
  );

  const chart = (
    <section className="chart-block wt-chart" aria-labelledby="wt-chart-h">
      <div className="chart-controls">
        <h2 id="wt-chart-h" className="block-title">מגמה</h2>
        <Segmented<string> label="טווח הגרף" size="sm" value={String(weeks)} onChange={(v) => setRange(Number(v) as RangeWeeks)} options={RANGES.map((r) => ({ value: String(r), label: `${r} שב׳`, aria: `${r} שבועות` }))} />
      </div>
      {daily.length ? (
        <LineChart points={daily} overlay={{ points: weekly, label: 'ממוצע שבועי' }} dotsOnly start={trend.from} end={trend.to} unit="ק״ג" label={`שקילות יומיות וממוצע שבועי, ${weeks} שבועות`} format={(v) => fmtKg(v)} />
      ) : (
        <div className="chart-empty">אין שקילות בטווח הזה</div>
      )}
      <p className="wt-legend" aria-hidden="true">
        <span><svg width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="4" className="lg-dot" /></svg> שקילה יומית</span>
        <span><svg width="22" height="12" viewBox="0 0 22 12"><line x1="1" y1="6" x2="21" y2="6" className="lg-line" /><rect x="7.5" y="2.5" width="7" height="7" rx="1.5" className="lg-sq" /></svg> ממוצע שבועי</span>
      </p>
    </section>
  );

  const table = (
    <section className="glass-panel wt-weeks-block" aria-labelledby="wt-weeks-h">
      <h2 id="wt-weeks-h" className="block-title">לפי שבוע</h2>
      <div className="wt-table-wrap">
        <table className="wt-weeks">
          <thead>
            <tr><th scope="col">שבוע</th><th scope="col">ממוצע</th><th scope="col">שקילות</th><th scope="col">טווח</th><th scope="col">שינוי</th></tr>
          </thead>
          <tbody>
            {weeksNewestFirst(trend).map((w) => (
              <tr key={w.week_start} className="wt-week-row" data-week={w.week_start}>
                <th scope="row"><Ltr>{weekRange(w)}</Ltr></th>
                <td className="wt-num"><Ltr>{fmtKg(w.average_kg)}</Ltr></td>
                <td className="wt-num">{w.days_logged}</td>
                <td className="wt-num"><Ltr>{minMaxText(w)}</Ltr></td>
                <td className="wt-num wt-wchange">
                  {w.change_kg == null ? <span className="muted" title="אין מספיק נתונים להשוואה">—</span> : (
                    <span title={TREND_LABEL[w.trend || 'flat']}><Ltr>{fmtSigned(w.change_kg)}</Ltr><span className="sr"> ק״ג, {TREND_LABEL[w.trend || 'flat']}</span></span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="muted small">ממוצע של הימים שנשקלת בלבד. שבוע בלי שקילה לא מושווה (—), ושום ערך לא מומצא.</p>
    </section>
  );

  const list = (
    <section className="glass-panel wt-recent" aria-labelledby="wt-recent-h">
      <h2 id="wt-recent-h" className="block-title">שקילות אחרונות <span className="muted" style={{ fontWeight: 400 }}>({recent.length})</span></h2>
      {recent.length === 0 ? (
        <p className="muted">עוד אין שקילות בטווח הזה.</p>
      ) : (
        <ul className="wt-list">
          {shown.map((e) => (
            <li key={e.date} className="wt-entry" data-date={e.date}>
              <div className="wt-entry-main">
                <span className="wt-entry-date">{e.date === today ? 'היום' : shortDate(e.date)}</span>
                <span className="wt-entry-kg"><Ltr>{fmtKg(e.weight_kg)}</Ltr> <small>ק״ג</small></span>
                {e.note && <span className="wt-entry-note">{e.note}</span>}
              </div>
              {confirm?.date === e.date ? (
                <div className="wt-confirm" role="group" aria-label="אישור מחיקה">
                  <span className="small">למחוק?</span>
                  <Button size="sm" variant="danger" disabled={deleting} onClick={() => void remove(confirm)}>כן, למחוק</Button>
                  <Button size="sm" variant="ghost" onClick={() => setConfirm(null)}>ביטול</Button>
                </div>
              ) : (
                <div className="wt-entry-actions">
                  <IconButton icon="edit" label={`עריכת השקילה של ${shortDate(e.date)}`} onClick={() => { setRowMsg(null); setEditing(e); }} />
                  <IconButton icon="trash" label={`מחיקת השקילה של ${shortDate(e.date)}`} onClick={() => { setRowMsg(null); setConfirm(e); }} />
                </div>
              )}
              {rowMsg?.date === e.date && <p className="note note-danger wt-row-msg" role="alert"><Icon name="alert" size={14} /> {rowMsg.text}</p>}
            </li>
          ))}
        </ul>
      )}
      {rowMsg && !recent.some((e) => e.date === rowMsg.date) && <p className="note note-danger wt-row-msg" role="alert"><Icon name="alert" size={14} /> {rowMsg.text}</p>}
      {recent.length > RECENT && <Button variant="ghost" size="sm" onClick={() => setShowAll((s) => !s)}>{showAll ? 'הצג פחות' : `הצג את כל ${recent.length}`}</Button>}
    </section>
  );

  return (
    <div className="page weight">
      {header}
      <div className="wt-grid">
        <div className="wt-col">
          {quick}
          {head}
          <p className="scope-note wt-caption"><Icon name="info" size={14} /><span>המשקל היומי משתנה מיום ליום (מים, מלח, אוכל). הממוצע השבועי הוא הסימן המשמעותי.</span></p>
        </div>
        <div className="wt-col">
          {chart}
          {table}
          {list}
        </div>
      </div>
      {loadErr && <p className="note note-warn"><Icon name="alert" size={14} /> {loadErr}</p>}
      {editing && (
        <EditSheet
          key={`${editing.date}-${editing.revision}`}
          entry={editing}
          {...formProps}
          onClose={() => setEditing(null)}
          onSaved={async () => { setEditing(null); await refresh(); }}
        />
      )}
    </div>
  );
}
