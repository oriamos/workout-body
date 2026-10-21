import { useMemo, useState } from 'react';
import { useStore } from '../state/store';
import { Sheet, Ltr } from './ui';
import { Icon } from './Icon';
import { MUSCLE } from '../lib/labels';

export function AddExerciseSheet({ open, onClose, onPick, exclude = [] }: { open: boolean; onClose: () => void; onPick: (exerciseId: string) => void; exclude?: string[] }) {
  const { state } = useStore();
  const [q, setQ] = useState('');
  const list = useMemo(() => state.data.exercises.filter((e) => !exclude.includes(e.id) && e.name.toLowerCase().includes(q.trim().toLowerCase())), [state.data.exercises, q, exclude]);
  return (
    <Sheet open={open} onClose={() => { setQ(''); onClose(); }} title="הוספת תרגיל">
      <label className="search">
        <Icon name="search" size={18} />
        <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="חיפוש תרגיל" aria-label="חיפוש תרגיל" dir="auto" />
      </label>
      <ul className="picklist">
        {list.map((e) => (
          <li key={e.id}>
            <button type="button" onClick={() => { onPick(e.id); setQ(''); onClose(); }}>
              <Ltr className="pick-name">{e.name}</Ltr>
              <span className="pick-meta">{MUSCLE[e.muscle]} · {e.equipment}</span>
            </button>
          </li>
        ))}
        {list.length === 0 && <li className="pick-empty">לא נמצא תרגיל בשם הזה.</li>}
      </ul>
    </Sheet>
  );
}
