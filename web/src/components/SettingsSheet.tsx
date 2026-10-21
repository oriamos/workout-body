import { Sheet, Segmented } from './ui';
import { setPref, usePrefs } from '../lib/prefs';

/** Secondary and deliberately small. Only the choices the prototype can honour. */
export function SettingsSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { autoRest, theme } = usePrefs();
  return (
    <Sheet open={open} onClose={onClose} title="הגדרות">
      <div className="form">
        <div className="form-row">
          <div>
            <b>יחידת משקל</b>
            <p className="muted small">ק״ג היא ברירת המחדל. יחידות נוספות לא כלולות בפרוטוטיפ.</p>
          </div>
          <Segmented label="יחידת משקל" value="kg" onChange={() => {}} options={[{ value: 'kg', label: 'ק״ג' }]} />
        </div>
        <div className="form-row">
          <div>
            <b>מראה</b>
            <p className="muted small">בהיר עם זכוכית שקופה הוא ברירת המחדל.</p>
          </div>
          <Segmented label="מראה" value={theme} onChange={(v) => setPref('theme', v)} options={[{ value: 'light', label: 'בהיר' }, { value: 'dark', label: 'כהה' }]} />
        </div>
        <div className="form-row">
          <div>
            <b>טיימר מנוחה אחרי סט</b>
            <p className="muted small">מופיע כסרגל קטן ולא חוסם הקלדה.</p>
          </div>
          <Segmented label="טיימר מנוחה" value={autoRest ? 'on' : 'off'} onChange={(v) => setPref('autoRest', v === 'on')} options={[{ value: 'on', label: 'פועל' }, { value: 'off', label: 'כבוי' }]} />
        </div>
        <p className="muted small">פרוטוטיפ: הנתונים לדוגמה ונשמרים בזיכרון הדף בלבד. טעינה מחדש מחזירה אותם למצב ההתחלתי.</p>
      </div>
    </Sheet>
  );
}
