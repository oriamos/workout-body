import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { Icon } from './Icon';
import { IconButton } from './ui';

interface Rest { start(sec: number): void }
const Ctx = createContext<Rest>({ start() {} });
export const useRest = () => useContext(Ctx);

const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;

/** Secondary utility: a small pill that never overlaps the set table's inputs. */
export function RestProvider({ children }: { children: ReactNode }) {
  const [end, setEnd] = useState<number | null>(null);
  const [total, setTotal] = useState(0);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (end == null) return;
    const id = window.setInterval(() => setNow(Date.now()), 250);
    return () => window.clearInterval(id);
  }, [end]);
  const left = end == null ? 0 : Math.max(0, Math.ceil((end - now) / 1000));
  useEffect(() => {
    if (end != null && left === 0) {
      const id = window.setTimeout(() => setEnd(null), 4000);
      return () => window.clearTimeout(id);
    }
  }, [end, left]);
  useEffect(() => {
    document.body.classList.toggle('has-rest', end != null);
    return () => document.body.classList.remove('has-rest');
  }, [end]);
  const start = (sec: number) => {
    setNow(Date.now());
    setTotal(sec);
    setEnd(Date.now() + sec * 1000);
  };
  return (
    <Ctx.Provider value={{ start }}>
      {children}
      {end != null && (
        <div className={`rest ${left === 0 ? 'rest-done' : ''}`} role="timer" aria-label="טיימר מנוחה">
          <Icon name="timer" size={16} />
          <span className="rest-time"><bdi dir="ltr">{left === 0 ? 'מוכן' : mmss(left)}</bdi></span>
          <span className="rest-bar" aria-hidden="true"><span style={{ transform: `scaleX(${total ? left / total : 0})` }} /></span>
          <button type="button" className="rest-btn" onClick={() => { setEnd((e) => (e ?? Date.now()) + 15000); setTotal((t) => t + 15); }}>+15</button>
          <IconButton icon="x" label="עצור טיימר" size={16} className="rest-x" onClick={() => setEnd(null)} />
        </div>
      )}
    </Ctx.Provider>
  );
}
