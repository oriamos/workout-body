import { createContext, useContext, useState, type ReactNode } from 'react';
import { href, navigate, useActiveTab, useRoute, type Tab } from '../lib/router';
import { Icon, type IconName } from './Icon';
import { IconButton, SaveStatus, Toasts } from './ui';
import { SettingsSheet } from './SettingsSheet';
import { DemoSheet } from './DemoSheet';
import { RestProvider } from './RestTimer';
import { IS_MOCK } from '../data/source';

const TABS: { tab: Tab; label: string; icon: IconName; route: Parameters<typeof navigate>[0] }[] = [
  { tab: 'today', label: 'היום', icon: 'today', route: { name: 'today' } },
  { tab: 'plans', label: 'תוכניות', icon: 'plans', route: { name: 'plans' } },
  { tab: 'exercises', label: 'תרגילים', icon: 'list', route: { name: 'exercises' } },
  { tab: 'history', label: 'היסטוריה', icon: 'history', route: { name: 'history' } },
  { tab: 'nutrition', label: 'תזונה', icon: 'nutrition', route: { name: 'nutrition' } },
];

const Ctx = createContext<{ openSettings(): void; openDemo(): void }>({ openSettings() {}, openDemo() {} });
export const useShell = () => useContext(Ctx);

export function HeaderTools() {
  const { openSettings, openDemo } = useShell();
  const onProgress = useRoute().name === 'progress';
  return (
    <div className="head-tools">
      {IS_MOCK && <IconButton icon="flask" label="כלי הדגמה (פרוטוטיפ)" onClick={openDemo} />}
      {/* Progress lives in the header, not in the bottom tab bar. */}
      <IconButton icon="progress" label="התקדמות" aria-current={onProgress ? 'page' : undefined} onClick={() => navigate({ name: 'progress' })} />
      <IconButton icon="gear" label="הגדרות" onClick={openSettings} />
    </div>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const active = useActiveTab();
  const [settings, setSettings] = useState(false);
  const [demo, setDemo] = useState(false);
  return (
    <Ctx.Provider value={{ openSettings: () => setSettings(true), openDemo: () => setDemo(true) }}>
      <RestProvider>
        <div className="shell">
          <nav className="nav" aria-label="ניווט ראשי">
            <div className="nav-brand" aria-hidden="true"><Icon name="today" size={22} strokeWidth={2.2} /></div>
            <ul>
              {TABS.map((t) => (
                <li key={t.tab}>
                  <a href={href(t.route)} className={`nav-item ${active === t.tab ? 'active' : ''}`} aria-current={active === t.tab ? 'page' : undefined} onClick={(e) => { e.preventDefault(); navigate(t.route); }}>
                    <Icon name={t.icon} size={22} />
                    <span>{t.label}</span>
                  </a>
                </li>
              ))}
            </ul>
          </nav>
          <main className="main" id="main">{children}</main>
        </div>
        <Toasts />
        <SettingsSheet open={settings} onClose={() => setSettings(false)} />
        {IS_MOCK && <DemoSheet open={demo} onClose={() => setDemo(false)} />}
      </RestProvider>
    </Ctx.Provider>
  );
}

export function PageHeader({ title, sub, back, actions, status }: { title: ReactNode; sub?: ReactNode; back?: { label: string; onClick: () => void }; actions?: ReactNode; status?: boolean }) {
  return (
    <header className="page-head">
      <div className="page-head-row">
        {back && (
          <button type="button" className="backbtn" onClick={back.onClick} aria-label={back.label}>
            <Icon name="back" size={20} />
          </button>
        )}
        <div className="page-title">
          <h1>{title}</h1>
          {(sub || status) && <div className="page-sub">{sub}{status && <SaveStatus />}</div>}
        </div>
        <div className="page-actions">{actions}<HeaderTools /></div>
      </div>
    </header>
  );
}
