// Small "שקילה היום" entry point on the Today screen: today's weight, or a button to log it.
import { useEffect, useState } from 'react';
import { Button, Ltr } from '../components/ui';
import { Icon } from '../components/Icon';
import { navigate } from '../lib/router';
import { weightRepo } from './repository';
import { fmtKg } from './logic';
import type { WeighIn } from './types';
import './weight.css';

export function WeighInCard({ today }: { today: string }) {
  const [entry, setEntry] = useState<WeighIn | null | undefined>(undefined);
  useEffect(() => {
    let live = true;
    const load = () => weightRepo.get(today).then((e) => { if (live) setEntry(e); }, () => { if (live) setEntry(undefined); });
    void load();
    window.addEventListener('focus', load);
    return () => { live = false; window.removeEventListener('focus', load); };
  }, [today]);
  if (entry === undefined) return null; // loading, or weight not available: stay quiet
  const go = () => navigate({ name: 'weight' });
  return (
    <section className="glass-panel wt-card" aria-label="שקילה היום">
      <div className="wt-card-text">
        <span className="wt-card-label"><Icon name="scale" size={15} /> שקילה היום</span>
        {entry ? <span className="wt-card-val"><Ltr>{fmtKg(entry.weight_kg)}</Ltr> <span className="unit">ק״ג</span></span> : <span className="muted small">עוד לא נרשמה</span>}
      </div>
      {entry ? <Button size="sm" variant="ghost" onClick={go}>למגמה</Button> : <Button size="sm" variant="primary" icon="plus" onClick={go}>רישום שקילה</Button>}
    </section>
  );
}
