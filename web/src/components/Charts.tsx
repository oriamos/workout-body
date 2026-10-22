import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { addDays, dayMonth, diffDays, num, parseDate, shortDate } from '../lib/format';
import { Ltr } from './ui';

function useWidth() {
  const ref = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(320);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setW(Math.max(200, Math.round(e.contentRect.width))));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w] as const;
}

function niceTicks(min: number, max: number, count = 4): number[] {
  if (max === min) {
    const pad = Math.max(1, Math.abs(max) * 0.1);
    min -= pad;
    max += pad;
  }
  const raw = (max - min) / count;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) as number;
  const out: number[] = [];
  let v = Math.floor(min / step) * step;
  for (;;) {
    out.push(Math.round(v * 1000) / 1000);
    if (v >= max - step * 0.001) break;
    v += step;
  }
  return out;
}

export interface Point {
  date: string;
  value: number | null; // null = no recorded value: drawn as a gap, never as zero
  detail?: string;
}

interface LineProps {
  points: Point[];
  start: string; // inclusive range start (ISO)
  end: string;
  unit: string;
  label: string; // accessible description of what is plotted
  estimate?: boolean;
  zeroBased?: boolean;
  compact?: boolean;
  height?: number;
  format?: (v: number) => string;
  /** Draw the main series as separate points (no connecting line), e.g. daily weigh-ins. */
  dotsOnly?: boolean;
  /** A second series in the same unit (e.g. a weekly average), drawn as a line; null breaks the line. */
  overlay?: { points: Point[]; label: string };
}

function segments(pts: Point[], X: (d: string) => number, Y: (v: number) => number) {
  const segs: { x: number; y: number }[][] = [];
  let run: { x: number; y: number }[] = [];
  for (const p of pts) {
    if (p.value == null) {
      if (run.length) segs.push(run);
      run = [];
    } else run.push({ x: X(p.date), y: Y(p.value) });
  }
  if (run.length) segs.push(run);
  return segs;
}

/** One series, one unit (plus an optional overlay in the same unit). Time runs left to right even in RTL pages; labels are LTR-isolated. */
export function LineChart({ points, start, end, unit, label, estimate, zeroBased, compact, height, format = num, dotsOnly, overlay }: LineProps) {
  const [wrap, W] = useWidth();
  const H = height ?? (compact ? 92 : 190);
  const m = compact ? { l: 6, r: 6, t: 28, b: 16 } : { l: 38, r: 10, t: 10, b: 24 };
  const vis = points.filter((p) => p.date >= start && p.date <= end);
  const valid = vis.filter((p) => p.value != null) as (Point & { value: number })[];
  const [sel, setSel] = useState<number | null>(null);
  const selIdx = sel != null && sel < vis.length ? sel : vis.length - 1 - [...vis].reverse().findIndex((p) => p.value != null);
  const cur = vis[selIdx] && vis[selIdx].value != null ? vis[selIdx] : null;

  const ov = (overlay?.points ?? []).filter((p) => p.date >= start && p.date <= end);
  const all = [...valid.map((p) => p.value), ...(ov.filter((p) => p.value != null).map((p) => p.value) as number[])];
  const lo = all.length ? Math.min(...all) : 0;
  const hi = all.length ? Math.max(...all) : 1;
  const ticks = niceTicks(zeroBased ? 0 : lo, hi, compact ? 2 : 3);
  const y0 = zeroBased ? 0 : ticks[0];
  const y1 = ticks[ticks.length - 1];
  const span = Math.max(1, diffDays(end, start));
  const X = (d: string) => m.l + (diffDays(d, start) / span) * (W - m.l - m.r);
  const Y = (v: number) => H - m.b - ((v - y0) / (y1 - y0 || 1)) * (H - m.t - m.b);

  const segs = dotsOnly ? [] : segments(vis, X, Y);
  const ovSegs = segments(ov, X, Y);

  const xt: string[] = [];
  const stepDays = compact ? span : span > 40 ? 14 : 7;
  for (let d = start; d <= end; d = addDays(d, stepDays)) xt.push(d);
  const missing = vis.filter((p) => p.value == null);

  const nearest = (clientX: number, el: SVGSVGElement) => {
    const x = clientX - el.getBoundingClientRect().left;
    let best = 0;
    vis.forEach((p, i) => {
      if (Math.abs(X(p.date) - x) < Math.abs(X(vis[best].date) - x)) best = i;
    });
    setSel(best);
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    setSel(Math.max(0, Math.min(vis.length - 1, selIdx + (e.key === 'ArrowRight' ? 1 : -1))));
  };

  if (!vis.length) return <div className="chart-empty" ref={wrap}>אין נתונים בטווח הזה</div>;
  const first = valid[0];
  const last = valid[valid.length - 1];

  return (
    <div className="chart" ref={wrap}>
      {!compact && (
        <div className="chart-readout" aria-live="polite">
          {cur ? (
            <>
              <span className="chart-readout-val"><Ltr>{estimate && '~'}{format(cur.value as number)}</Ltr> <span className="unit">{unit}</span></span>
              <span className="chart-readout-date">{shortDate(cur.date)}{cur.detail && <> · <Ltr>{cur.detail}</Ltr></>}</span>
            </>
          ) : (
            <span className="chart-readout-date">{vis[selIdx] ? `${shortDate(vis[selIdx].date)} · אין ערך מתועד` : ''}</span>
          )}
        </div>
      )}
      <svg width={W} height={H} style={{ direction: "ltr" }} role="img" aria-label={`${label}. ${valid.length} מדידות בטווח.`} tabIndex={compact ? -1 : 0} onKeyDown={onKey} onPointerMove={(e) => !compact && nearest(e.clientX, e.currentTarget)} onPointerDown={(e) => !compact && nearest(e.clientX, e.currentTarget)}>
        {!compact &&
          ticks.map((t) => (
            <g key={t}>
              <line className="grid" x1={m.l} x2={W - m.r} y1={Y(t)} y2={Y(t)} />
              <text className="tick" x={m.l - 6} y={Y(t) + 4} textAnchor="end">{format(t)}</text>
            </g>
          ))}
        {compact && <line className="grid" x1={m.l} x2={W - m.r} y1={H - m.b} y2={H - m.b} />}
        {xt.map((d, i) => (
          <text key={d} className="tick" x={X(d)} y={H - 6} textAnchor={compact ? (i === 0 ? 'start' : 'end') : 'middle'}>{dayMonth(d)}</text>
        ))}
        {segs.map((s, i) => (
          <polyline key={i} className="series" points={s.map((p) => `${p.x},${p.y}`).join(' ')} />
        ))}
        {ovSegs.map((s, i) => (
          <polyline key={`o${i}`} className="series series-2" points={s.map((p) => `${p.x},${p.y}`).join(' ')} />
        ))}
        {ov.map((p) => p.value != null && <rect key={`o${p.date}`} className="pt-2" x={X(p.date) - 3.5} y={Y(p.value) - 3.5} width={7} height={7} rx={1.5}><title>{`${overlay?.label}: ${format(p.value)} ${unit}`}</title></rect>)}
        {vis.map((p, i) =>
          p.value == null ? (
            <path key={p.date} className="missing" d={`M${X(p.date)} ${H - m.b - 6}l4 4-4 4-4-4z`}><title>{`${shortDate(p.date)}: אין ערך מתועד`}</title></path>
          ) : (
            <circle key={p.date} className={`pt ${i === selIdx && !compact ? 'sel' : ''}`} cx={X(p.date)} cy={Y(p.value)} r={i === selIdx && !compact ? 5.5 : compact ? 2.5 : 3.5} />
          ),
        )}
        {!compact && cur && <line className="cursor" x1={X(cur.date)} x2={X(cur.date)} y1={m.t} y2={H - m.b} />}
        {compact && first && last && (
          <>
            <text className="endlabel" x={X(first.date)} y={Y(first.value) - 7} textAnchor="start">{estimate && '~'}{format(first.value)}</text>
            {valid.length > 1 && <text className="endlabel strong" x={X(last.date)} y={Y(last.value) - 7} textAnchor="end">{estimate && '~'}{format(last.value)}</text>}
          </>
        )}
      </svg>
      {!compact && (
        <p className="chart-note">
          {!zeroBased && 'הציר האנכי אינו מתחיל באפס. '}
          {missing.length > 0 && <><svg width="10" height="10" viewBox="-5 -5 10 10" aria-hidden="true"><path className="missing" d="M0 -4L4 0 0 4-4 0z" /></svg> סשן ללא ערך מתועד (לא אפס). </>}
          {estimate && 'הערכה בלבד, לפי נוסחת Epley עד 10 חזרות.'}
        </p>
      )}
    </div>
  );
}

export interface Bar { key: string; label: string; value: number; partial?: boolean; sub?: string }

export function BarChart({ bars, label, unit, height = 150, format = num }: { bars: Bar[]; label: string; unit: string; height?: number; format?: (n: number) => string }) {
  const [wrap, W] = useWidth();
  const H = height;
  const m = { l: 4, r: 4, t: 20, b: 22 };
  const max = Math.max(1, ...bars.map((b) => b.value));
  const bw = (W - m.l - m.r) / bars.length;
  const bar = Math.min(34, bw * 0.62);
  const dense = bw < 30;
  return (
    <div className="chart" ref={wrap}>
      <svg width={W} height={H} style={{ direction: "ltr" }} role="img" aria-label={`${label}: ${bars.map((b) => `${b.label} ${format(b.value)} ${unit}`).join(', ')}`}>
        <defs>
          <pattern id="hatch" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
            <rect width="6" height="6" className="hatch-bg" />
            <line x1="0" y1="0" x2="0" y2="6" className="hatch-line" strokeWidth="2.5" />
          </pattern>
        </defs>
        <line className="grid" x1={m.l} x2={W - m.r} y1={H - m.b} y2={H - m.b} />
        {bars.map((b, i) => {
          const h = (b.value / max) * (H - m.t - m.b);
          const cx = m.l + bw * i + bw / 2;
          return (
            <g key={b.key}>
              <rect x={cx - bar / 2} y={H - m.b - h} width={bar} height={Math.max(h, b.value > 0 ? 2 : 0)} rx="3" className={b.partial ? 'bar bar-partial' : 'bar'} fill={b.partial ? 'url(#hatch)' : undefined} />
              {b.value === 0 && <line className="zero-mark" x1={cx - bar / 2} x2={cx + bar / 2} y1={H - m.b - 1} y2={H - m.b - 1} />}
              <text className="barval" x={cx} y={H - m.b - h - 6} textAnchor="middle">{format(b.value)}</text>
              {(!dense || i % 2 === bars.length % 2 || i === bars.length - 1) && <text className="tick" x={cx} y={H - 6} textAnchor="middle">{b.label}</text>}
            </g>
          );
        })}
      </svg>
      {bars.some((b) => b.partial) && <p className="chart-note">עמודה מפוספסת: השבוע הנוכחי, עד היום בלבד.</p>}
    </div>
  );
}

export const weekLabel = (d: string) => (parseDate(d).getDate() + '/' + (parseDate(d).getMonth() + 1));
