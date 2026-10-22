import { useEffect, useState } from 'react';
import { Button, IconButton, Sheet } from '../components/ui';
import { addDays, dayName, longDate, monthName, parseDate, toISO } from '../lib/format';
import { nutritionRepo } from './repository';

const DOW = ['א', 'ב', 'ג', 'ד', 'ה', 'ו', 'ש'];

/** Month grid like the history calendar; days with entries are marked. Future days cannot be picked. */
export function CalendarSheet({ open, selected, today, onClose, onPick }: { open: boolean; selected: string; today: string; onClose: () => void; onPick: (d: string) => void }) {
  const [month, setMonth] = useState(selected.slice(0, 8) + '01');
  const [logged, setLogged] = useState<Set<string>>(new Set());
  useEffect(() => { if (open) setMonth(selected.slice(0, 8) + '01'); }, [open, selected]);
  const first = parseDate(month);
  const days = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate();
  const last = toISO(new Date(first.getFullYear(), first.getMonth(), days));
  useEffect(() => {
    if (!open || month > today) return;
    let live = true;
    nutritionRepo.days(month, last < today ? last : today).then((ds) => live && setLogged(new Set(ds.filter((d) => d.entry_count > 0).map((d) => d.date))), () => {});
    return () => { live = false; };
  }, [open, month, last, today]);
  const cells: (string | null)[] = [...Array(first.getDay()).fill(null), ...Array.from({ length: days }, (_, i) => toISO(new Date(first.getFullYear(), first.getMonth(), i + 1)))];
  const shift = (n: number) => setMonth(toISO(new Date(first.getFullYear(), first.getMonth() + n, 1)));
  return (
    <Sheet open={open} onClose={onClose} title="בחירת תאריך" footer={<Button variant="secondary" onClick={() => onPick(today)}>היום</Button>}>
      <section className="cal nut-cal" aria-label="לוח תזונה">
        <div className="cal-head">
          <IconButton icon="right" label="חודש קודם" onClick={() => shift(-1)} />
          <h2 className="cal-title">{monthName(first.getMonth())} {first.getFullYear()}</h2>
          <IconButton icon="left" label="חודש הבא" disabled={addDays(last, 1) > today} onClick={() => shift(1)} />
        </div>
        <div className="cal-grid">
          {DOW.map((d, i) => <span key={d} className="cal-dow" aria-hidden="true" title={dayName(addDays('2026-10-18', i))}>{d}</span>)}
          {cells.map((d, i) => {
            if (!d) return <span key={`b${i}`} className="cal-day blank" />;
            const cls = ['cal-day', logged.has(d) && 'trained', d === today && 'today', d === selected && 'sel', d > today && 'future'].filter(Boolean).join(' ');
            const label = `${longDate(d)}${logged.has(d) ? ', יש ארוחות' : ''}${d === today ? ', היום' : ''}`;
            return d > today ? (
              <span key={d} className={cls} aria-label={label}>{parseDate(d).getDate()}</span>
            ) : (
              <button key={d} type="button" className={cls} aria-label={label} aria-current={d === selected ? 'date' : undefined} onClick={() => onPick(d)}>{parseDate(d).getDate()}</button>
            );
          })}
        </div>
        <p className="cal-legend"><span><span className="dot" /> יום עם ארוחות</span><span>מסגרת: היום</span></p>
      </section>
    </Sheet>
  );
}
