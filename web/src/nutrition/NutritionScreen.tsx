import { useCallback, useEffect, useRef, useState } from 'react';
import { useStore } from '../state/store';
import { PageHeader } from '../components/Shell';
import { Button, Chip, EmptyState, IconButton, Ltr, Skeleton } from '../components/ui';
import { Icon } from '../components/Icon';
import { addDays, longDate } from '../lib/format';
import { navigate } from '../lib/router';
import { nutritionRepo } from './repository';
import { consumedText, fmt, LABEL, missingNote, progress, SHORT_UNIT, statusText, UNIT } from './logic';
import { NUTRIENTS, type NutritionDay, type NutritionEntry } from './types';
import { EntrySheet } from './EntrySheet';
import { TargetsSheet } from './TargetsSheet';
import { CalendarSheet } from './CalendarSheet';
import { BrowseSheet } from './BrowseSheet';
import './nutrition.css';

const POLL_MS = 5000;
type EntrySheetState = { mode: 'add' } | { mode: 'edit'; entry: NutritionEntry };

function Summary({ day }: { day: NutritionDay }) {
  return (
    <section className="glass-panel nut-summary" aria-label="סיכום היום">
      {NUTRIENTS.map((k) => {
        const s = day[k];
        const st = statusText(s, k);
        const pct = progress(s);
        const miss = missingNote(s.entries_missing_value);
        return (
          <div key={k} className="nut-row" data-nutrient={k}>
            <div className="nut-row-head">
              <b className="nut-label">{LABEL[k]}</b>
              <span className="nut-amount">
                <Ltr className="nut-consumed">{consumedText(s, day.entry_count)}</Ltr>
                {s.target != null && <span className="nut-of"> / <Ltr>{fmt(s.target)}</Ltr></span>}
                <span className="nut-unit"> {UNIT[k]}</span>
              </span>
            </div>
            {pct != null && (
              <div className={`meter nut-meter ${(s.over ?? 0) > 0 ? 'over' : ''}`} role="progressbar" aria-label={`${LABEL[k]} מתוך היעד`} aria-valuemin={0} aria-valuemax={s.target ?? 0} aria-valuenow={s.consumed}>
                <span style={{ transform: `scaleX(${pct})` }} />
              </div>
            )}
            <p className="nut-status">
              <span className={`nut-st nut-st-${st.tone}`}>{st.text}</span>
              {miss && <span className="nut-miss"><Icon name="info" size={13} /> {miss}</span>}
            </p>
          </div>
        );
      })}
    </section>
  );
}

function EntryRow({ e, onOpen }: { e: NutritionEntry; onOpen: () => void }) {
  return (
    <li>
      <button type="button" className="nut-entry" onClick={onOpen} data-entry-id={e.id}>
        <span className="nut-entry-name">{e.name}</span>
        <span className="nut-entry-vals">
          <span><Ltr>{fmt(e.calories_kcal)}</Ltr> <small>{SHORT_UNIT.calories_kcal}</small></span>
          <span><small>ח׳</small> <Ltr>{fmt(e.protein_g)}</Ltr></span>
          <span><small>פ׳</small> <Ltr>{fmt(e.carbs_g)}</Ltr></span>
          <span><small>ש׳</small> <Ltr>{fmt(e.fat_g)}</Ltr></span>
        </span>
      </button>
    </li>
  );
}

export function NutritionScreen({ date }: { date?: string }) {
  const { state, actions } = useStore();
  const today = state.data.today;
  const selected = date ?? today;
  const isToday = selected === today;
  const [day, setDay] = useState<NutritionDay | null>(null);
  const [loadErr, setLoadErr] = useState<string | null>(null);
  const [entrySheet, setEntrySheet] = useState<EntrySheetState | null>(null);
  const [sheetN, setSheetN] = useState(0);
  const [targetsOpen, setTargetsOpen] = useState(false);
  const [calOpen, setCalOpen] = useState(false);
  const [browseOpen, setBrowseOpen] = useState(false);
  const last = useRef('');
  const current = useRef(selected);
  current.current = selected;

  const refresh = useCallback(async () => {
    const d = selected;
    try {
      const fresh = await nutritionRepo.day(d);
      if (current.current !== d) return;
      const json = JSON.stringify(fresh);
      if (json !== last.current) { last.current = json; setDay(fresh); }
      setLoadErr(null);
    } catch (e) {
      if (current.current === d) setLoadErr(e instanceof Error ? e.message : 'הטעינה נכשלה');
    }
  }, [selected]);

  useEffect(() => {
    setDay(null);
    last.current = '';
    void refresh();
    // Changes made elsewhere (e.g. the assistant) appear by polling the visible day. Open forms keep
    // their own values, so a refresh never touches what the user is typing.
    const tick = () => { if (document.visibilityState === 'visible') void refresh(); };
    const id = window.setInterval(tick, POLL_MS);
    document.addEventListener('visibilitychange', tick);
    window.addEventListener('focus', tick);
    return () => { window.clearInterval(id); document.removeEventListener('visibilitychange', tick); window.removeEventListener('focus', tick); };
  }, [refresh]);

  const go = (d: string) => navigate({ name: 'nutrition', date: d === today ? undefined : d });
  const openEntry = (s: EntrySheetState) => { setSheetN((n) => n + 1); setEntrySheet(s); };

  const header = (
    <>
      <PageHeader
        title="תזונה"
        sub={<><span className="nut-date-sub">{longDate(selected)}</span>{!isToday && <Chip tone="warn" className="nut-not-today">{selected > today ? 'תאריך עתידי' : 'לא היום'}</Chip>}</>}
        actions={
          <>
            <IconButton icon="target" label="יעדים יומיים" onClick={() => setTargetsOpen(true)} />
            <IconButton icon="search" label="ארוחות קודמות" onClick={() => setBrowseOpen(true)} />
          </>
        }
      />
      <nav className="nut-daybar" aria-label="ניווט בין ימים">
        <IconButton icon="right" label="היום הקודם" onClick={() => go(addDays(selected, -1))} />
        <button type="button" className="nut-daybar-date" onClick={() => setCalOpen(true)} aria-label={`בחירת תאריך, נבחר ${longDate(selected)}`}>
          <Icon name="calendar" size={17} /> <span>{isToday ? 'היום' : longDate(selected)}</span>
        </button>
        {!isToday && <Button size="sm" variant="secondary" className="nut-today-link" onClick={() => go(today)}>היום</Button>}
        <IconButton icon="left" label="היום הבא" disabled={selected >= today} onClick={() => go(addDays(selected, 1))} />
      </nav>
    </>
  );

  let body;
  if (loadErr && !day) {
    body = <EmptyState icon="alert" title="לא הצלחנו לטעון את התזונה" action={<Button variant="primary" icon="refresh" onClick={() => void refresh()}>נסה שוב</Button>}>{loadErr}</EmptyState>;
  } else if (!day) {
    body = (
      <div className="nut-grid" aria-busy="true">
        <div className="glass-panel">{[0, 1, 2, 3].map((i) => <div key={i} style={{ marginBottom: 14 }}><Skeleton w={120} h={16} /><div style={{ height: 8 }} /><Skeleton h={8} className="block" /></div>)}</div>
        <div className="glass-panel"><Skeleton h={44} className="block" /></div>
      </div>
    );
  } else {
    const future = selected > today;
    body = (
      <div className="nut-grid">
        <div className="nut-aside">
          <Summary day={day} />
          {day.targets.effective_from ? (
            <p className="scope-note"><Icon name="info" size={14} /><span>היעדים בתוקף מ־{longDate(day.targets.effective_from)}. <button type="button" className="linkbtn" onClick={() => setTargetsOpen(true)}>שינוי יעדים</button></span></p>
          ) : (
            <p className="scope-note"><Icon name="info" size={14} /><span>לא הוגדרו יעדים ליום הזה. <button type="button" className="linkbtn" onClick={() => setTargetsOpen(true)}>הגדרת יעדים</button></span></p>
          )}
        </div>
        <section className="glass-panel nut-entries" aria-label="ארוחות היום">
          <div className="nut-entries-head">
            <h2 className="block-title">ארוחות <span className="muted" style={{ fontWeight: 400 }}>({day.entry_count})</span></h2>
            <Button variant="primary" size="sm" icon="plus" disabled={future} onClick={() => openEntry({ mode: 'add' })}>הוספת ארוחה</Button>
          </div>
          {day.entries.length === 0 ? (
            <EmptyState icon="nutrition" title={future ? 'אי אפשר לרשום אוכל ליום עתידי' : 'עוד לא נרשמו ארוחות ביום הזה'}>
              {future ? 'אפשר להגדיר יעדים מראש.' : 'הוסף ארוחה כאן, או ספר לעוזר מה אכלת.'}
            </EmptyState>
          ) : (
            <ul className="nut-list">{day.entries.map((e) => <EntryRow key={e.id} e={e} onOpen={() => openEntry({ mode: 'edit', entry: e })} />)}</ul>
          )}
          {loadErr && <p className="note note-warn"><Icon name="alert" size={14} /> {loadErr}</p>}
        </section>
      </div>
    );
  }

  return (
    <div className="page nutrition">
      {header}
      {body}
      <EntrySheet
        key={sheetN}
        open={!!entrySheet}
        entry={entrySheet?.mode === 'edit' ? entrySheet.entry : null}
        date={selected}
        today={today}
        onClose={() => setEntrySheet(null)}
        onChanged={() => void refresh()}
        toast={actions.toast}
      />
      {day && <TargetsSheet key={`t-${selected}-${targetsOpen}`} open={targetsOpen} date={selected} today={today} targets={day.targets} onClose={() => setTargetsOpen(false)} onChanged={() => void refresh()} toast={actions.toast} />}
      <CalendarSheet open={calOpen} selected={selected} today={today} onClose={() => setCalOpen(false)} onPick={(d) => { setCalOpen(false); go(d); }} />
      <BrowseSheet open={browseOpen} today={today} selected={selected} onClose={() => setBrowseOpen(false)} onCopied={(d) => { if (d === selected) void refresh(); }} toast={actions.toast} />
    </div>
  );
}
