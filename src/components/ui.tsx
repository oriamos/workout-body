import { useEffect, useId, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { Icon, type IconName } from './Icon';
import { useStore } from '../state/store';

type BtnProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'ghost' | 'danger'; size?: 'sm' | 'md'; icon?: IconName };
export function Button({ variant = 'secondary', size = 'md', icon, className = '', children, ...rest }: BtnProps) {
  return (
    <button type="button" className={`btn btn-${variant} btn-${size} ${className}`} {...rest}>
      {icon && <Icon name={icon} size={size === 'sm' ? 16 : 18} />}
      {children}
    </button>
  );
}

export function IconButton({ icon, label, className = '', size = 20, ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { icon: IconName; label: string; size?: number }) {
  return (
    <button type="button" className={`iconbtn ${className}`} aria-label={label} title={label} {...rest}>
      <Icon name={icon} size={size} />
    </button>
  );
}

/** English names, numbers and units keep their own direction inside RTL text. */
export function Ltr({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <bdi dir="ltr" className={`ltr ${className}`}>{children}</bdi>;
}

export function Segmented<T extends string>({ value, onChange, options, label, size = 'md' }: { value: T; onChange: (v: T) => void; options: { value: T; label: ReactNode; aria?: string }[]; label: string; size?: 'sm' | 'md' }) {
  return (
    <div className={`seg seg-${size}`} role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" role="radio" aria-checked={o.value === value} aria-label={o.aria} className="seg-item" onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

export function Chip({ children, tone = 'neutral', icon, className = '' }: { children: ReactNode; tone?: 'neutral' | 'accent' | 'warn' | 'assistant' | 'danger'; icon?: IconName; className?: string }) {
  return (
    <span className={`chip chip-${tone} ${className}`}>
      {icon && <Icon name={icon} size={13} />}
      {children}
    </span>
  );
}

export function Skeleton({ w, h = 14, className = '' }: { w?: number | string; h?: number; className?: string }) {
  return <span className={`skeleton ${className}`} style={{ width: w, height: h }} aria-hidden="true" />;
}

export function EmptyState({ icon, title, children, action }: { icon?: IconName; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty">
      {icon && <Icon name={icon} size={26} className="empty-icon" />}
      <h2>{title}</h2>
      {children && <p>{children}</p>}
      {action}
    </div>
  );
}

export function Sheet({ open, onClose, title, children, footer, wide = false }: { open: boolean; onClose: () => void; title: string; children: ReactNode; footer?: ReactNode; wide?: boolean }) {
  const ref = useRef<HTMLDialogElement>(null);
  const id = useId();
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      className={`sheet ${wide ? 'sheet-wide' : ''}`}
      aria-labelledby={id}
      onClose={onClose}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
    >
      {open && (
        <div className="sheet-inner">
          <header className="sheet-head">
            <h2 id={id}>{title}</h2>
            <IconButton icon="x" label="סגירה" onClick={onClose} />
          </header>
          <div className="sheet-body">{children}</div>
          {footer && <footer className="sheet-foot">{footer}</footer>}
        </div>
      )}
    </dialog>
  );
}

export function Menu({ label, items, align = 'end' }: { label: string; items: { label: string; icon?: IconName; danger?: boolean; onSelect: () => void }[]; align?: 'start' | 'end' }) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => !wrap.current?.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('pointerdown', away);
    document.addEventListener('keydown', esc);
    return () => {
      document.removeEventListener('pointerdown', away);
      document.removeEventListener('keydown', esc);
    };
  }, [open]);
  return (
    <div className="menu" ref={wrap}>
      <IconButton icon="more" label={label} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((o) => !o)} />
      {open && (
        <div className={`menu-list menu-${align}`} role="menu">
          {items.map((it) => (
            <button key={it.label} role="menuitem" type="button" className={`menu-item ${it.danger ? 'danger' : ''}`} onClick={() => { setOpen(false); it.onSelect(); }}>
              {it.icon && <Icon name={it.icon} size={17} />}
              {it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

export function Toasts() {
  const { state, dispatch } = useStore();
  return (
    <div className="toasts" role="status" aria-live="polite">
      {state.toasts.map((t) => (
        <div key={t.id} className={`toast toast-${t.tone}`}>
          {t.tone === 'assistant' && <Icon name="assistant" size={16} />}
          {t.tone === 'error' && <Icon name="alert" size={16} />}
          <span className="toast-text">{t.text}</span>
          {t.action && (
            <button type="button" className="toast-action" onClick={() => { t.action!.run(); dispatch({ t: 'dismissToast', id: t.id }); }}>
              {t.action.label}
            </button>
          )}
        </div>
      ))}
    </div>
  );
}

/** Save state, kept small and honest: the prototype stores in memory only. */
export function SaveStatus() {
  const { state, actions } = useStore();
  const saving = Object.values(state.pending).includes('saving');
  const failedKeys = Object.keys(state.failed);
  const dirty = Object.keys(state.drafts).length > 0;
  if (failedKeys.length)
    return (
      <button type="button" className="status status-danger" onClick={actions.retryAll}>
        <Icon name="alert" size={14} /> השמירה נכשלה · נסה שוב
      </button>
    );
  if (dirty) return <span className="status status-warn"><span className="dot" /> שינויים לא שמורים</span>;
  if (saving) return <span className="status"><span className="spin" /> שומר…</span>;
  return <span className="status" title="פרוטוטיפ: השינויים נשמרים בזיכרון הדפדפן בלבד"><Icon name="check" size={14} /> נשמר (מקומי)</span>;
}
