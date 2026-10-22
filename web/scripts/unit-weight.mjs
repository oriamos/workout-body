// Unit tests for src/weight/logic.ts (input parsing, weekly trend like the engine, headline, display).
// Run: node scripts/unit-weight.mjs
import { build } from 'esbuild';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';

const entry = fileURLToPath(new URL('../src/weight/logic.ts', import.meta.url));
const out = await build({ entryPoints: [entry], bundle: true, format: 'esm', platform: 'node', write: false, logLevel: 'error' });
const L = await import('data:text/javascript;base64,' + Buffer.from(out.outputFiles[0].text).toString('base64'));

let failed = 0;
const t = (name, fn) => {
  try { fn(); console.log('PASS', name); } catch (e) { failed++; console.log('FAIL', name, '\n ', e.message); }
};

t('parseWeight: decimals, comma, range, rounding', () => {
  assert.deepEqual(L.parseWeight('81.4'), { ok: true, value: 81.4 });
  assert.deepEqual(L.parseWeight(' 81,4 '), { ok: true, value: 81.4 });
  assert.deepEqual(L.parseWeight('80'), { ok: true, value: 80 });
  assert.deepEqual(L.parseWeight('80.125'), { ok: true, value: 80.13 });
  assert.deepEqual(L.parseWeight('20'), { ok: true, value: 20 });
  assert.deepEqual(L.parseWeight('400'), { ok: true, value: 400 });
  assert.match(L.parseWeight('').msg, /חסר/);
  assert.match(L.parseWeight('19.99').msg, /20/);
  assert.match(L.parseWeight('400.1').msg, /400/);
  assert.equal(L.parseWeight('abc').ok, false);
  assert.equal(L.parseWeight('1e2').ok, false);
  assert.equal(L.parseWeight('-80').ok, false);
  assert.equal(L.parseWeight('81.4.2').ok, false);
});

t('form validation: date not in the future, note length', () => {
  const ok = L.validateForm({ date: '2026-10-20', weight: '81,25', note: '  אחרי ריצה ' }, '2026-10-22');
  assert.deepEqual(ok.values, { date: '2026-10-20', weight_kg: 81.25, note: 'אחרי ריצה' });
  const fut = L.validateForm({ date: '2026-10-23', weight: '80', note: '' }, '2026-10-22');
  assert.equal(fut.values, null);
  assert.match(fut.errors.date, /עתידי/);
  assert.match(L.validateForm({ date: '2026-02-30', weight: '80', note: '' }, '2026-10-22').errors.date, /תאריך/);
  assert.match(L.validateForm({ date: '2026-10-22', weight: '80', note: 'x'.repeat(201) }, '2026-10-22').errors.note, /200/);
  assert.equal(L.validateForm({ date: '2026-10-22', weight: '80', note: 'x'.repeat(200) }, '2026-10-22').values.note.length, 200);
});

t('dates: Sunday→Saturday weeks, day arithmetic across months/DST', () => {
  assert.equal(L.WEEK_START_DAY, 0);
  assert.equal(L.weekStartOf('2026-10-22'), '2026-10-18'); // Thursday
  assert.equal(L.weekStartOf('2026-10-18'), '2026-10-18'); // Sunday starts the week
  assert.equal(L.weekStartOf('2026-10-24'), '2026-10-18'); // Saturday ends it
  assert.equal(L.weekStartOf('2026-10-25'), '2026-10-25');
  assert.equal(L.weekStartOf('2026-11-01'), '2026-11-01');
  assert.equal(L.weekStartOf('2026-10-25', 1), '2026-10-19'); // a Monday start is still possible
  assert.equal(L.plusDays('2026-03-28', 2), '2026-03-30');
  assert.equal(L.plusDays('2026-10-31', 1), '2026-11-01');
  assert.equal(L.isISODate('2026-02-29'), false);
  assert.equal(L.isISODate('2028-02-29'), true);
});

const wi = (date, kg) => ({ id: 0, date, weight_kg: kg, revision: 1, created_at: '', updated_at: '' });
t('computeTrend mirrors the engine: averages, min/max, change, %, flat threshold, gaps', () => {
  const entries = [
    wi('2026-10-03', 85), // Saturday before the prior week: ignored
    wi('2026-10-05', 82), wi('2026-10-08', 82.5), // prior week (Sun Oct 4 – Sat Oct 10): comparison base only
    wi('2026-10-12', 81.6), wi('2026-10-13', 82.1), wi('2026-10-17', 81.2), // Oct 11–17 (Sat the 17th included)
    wi('2026-10-18', 81.7), wi('2026-10-22', 81.5), // Oct 18–24 (Sunday the 18th starts it; to = Oct 22)
    wi('2026-10-24', 99), // after `to`: ignored
  ];
  const tr = L.computeTrend(entries, '2026-10-22', 2, '2026-10-22');
  assert.equal(tr.from, '2026-10-11');
  assert.equal(tr.to, '2026-10-22');
  assert.deepEqual(tr.weeks.map((w) => w.week_start), ['2026-10-11', '2026-10-18']);
  assert.deepEqual(tr.entries.map((e) => e.date), ['2026-10-12', '2026-10-13', '2026-10-17', '2026-10-18', '2026-10-22']);
  const [w1, w2] = tr.weeks;
  assert.deepEqual(w1, { week_start: '2026-10-11', week_end: '2026-10-17', days_logged: 3, average_kg: 81.63, min_kg: 81.2, max_kg: 82.1, change_kg: -0.62, change_pct: -0.75, trend: 'down' });
  // 81.6 - 81.63 = -0.03: flat (|change| < 0.1)
  assert.deepEqual(w2, { week_start: '2026-10-18', week_end: '2026-10-24', days_logged: 2, average_kg: 81.6, min_kg: 81.5, max_kg: 81.7, change_kg: -0.03, change_pct: -0.04, trend: 'flat' });
  assert.equal(tr.overall_change_kg, -0.03);
  assert.equal(tr.latest.date, '2026-10-22');
});

t('computeTrend: a week without weigh-ins breaks the comparison on both sides (nothing interpolated)', () => {
  const entries = [wi('2026-10-06', 80), wi('2026-10-21', 81)];
  const tr = L.computeTrend(entries, '2026-10-22', 3, '2026-10-22');
  assert.deepEqual(tr.weeks.map((w) => [w.week_start, w.days_logged, w.average_kg, w.change_kg, w.trend]), [
    ['2026-10-04', 1, 80, null, undefined],
    ['2026-10-11', 0, null, null, undefined],
    ['2026-10-18', 1, 81, null, undefined],
  ]);
  assert.equal(tr.overall_change_kg, 1);
  const one = L.computeTrend([wi('2026-10-21', 81)], '2026-10-22', 4, '2026-10-22');
  assert.equal(one.overall_change_kg, null); // one week with data: no overall change
  const none = L.computeTrend([], '2026-10-22', 4, '2026-10-22');
  assert.equal(none.latest, null);
  assert.equal(none.weeks.length, 4);
  // latest may be older than the range
  assert.equal(L.computeTrend([wi('2026-01-05', 90)], '2026-10-22', 1, '2026-10-22').latest.date, '2026-01-05');
  assert.equal(L.trendOf(0.1), 'up');
  assert.equal(L.trendOf(-0.1), 'down');
  assert.equal(L.trendOf(0.09), 'flat');
});

t('headline: this week, count, comparison text; never invents a comparison', () => {
  const tr = L.computeTrend([wi('2026-10-13', 82), wi('2026-10-20', 81.5), wi('2026-10-21', 81.7)], '2026-10-22', 4, '2026-10-22');
  const h = L.headline(tr);
  assert.equal(h.average, 81.6);
  assert.equal(h.countText, '2 שקילות');
  assert.deepEqual(h.comparison, { kg: -0.4, pct: -0.49, dir: 'down', label: 'ירידה' });
  assert.equal(h.comparisonText, 'ירידה של 0.4 ק״ג (0.49%) לעומת השבוע שעבר');
  assert.equal(L.comparisonPhrase({ kg: -0.03, pct: -0.04, dir: 'flat' }), 'ללא שינוי משמעותי (−0.03 ק״ג)');
  assert.equal(L.comparisonPhrase({ kg: 1.25, pct: 1.5, dir: 'up' }), 'עלייה של 1.25 ק״ג (1.5%)');
  const up = L.headline(L.computeTrend([wi('2026-10-13', 80), wi('2026-10-20', 80.5)], '2026-10-22', 2, '2026-10-22'));
  assert.equal(up.comparison.dir, 'up');
  assert.equal(up.countText, 'שקילה אחת');
  const gap = L.headline(L.computeTrend([wi('2026-10-06', 80), wi('2026-10-20', 80.5)], '2026-10-22', 4, '2026-10-22'));
  assert.equal(gap.comparison, null);
  assert.equal(gap.comparisonText, 'אין מספיק נתונים להשוואה');
  const empty = L.headline(L.computeTrend([wi('2026-10-13', 80)], '2026-10-22', 4, '2026-10-22'));
  assert.equal(empty.average, null);
  assert.equal(empty.countText, 'אין שקילות');
  assert.equal(empty.comparison, null);
  assert.equal(L.headline(null).comparisonText, 'אין מספיק נתונים להשוואה');
  // the engine may omit "trend": derive it from change_kg
  const raw = { from: '', to: '', today: '', entries: [], latest: null, overall_change_kg: null, weeks: [{ week_start: '2026-10-18', week_end: '2026-10-24', days_logged: 2, average_kg: 80, min_kg: 79, max_kg: 81, change_kg: 0, change_pct: 0 }] };
  assert.equal(L.headline(raw).comparison.label, 'ללא שינוי');
  assert.equal(L.headline(raw).comparisonText, 'ללא שינוי משמעותי (0.0 ק״ג) לעומת השבוע שעבר');
});

t('display helpers', () => {
  assert.equal(L.fmtKg(81), '81.0');
  assert.equal(L.fmtKg(81.25), '81.25');
  assert.equal(L.fmtKg(null), '—');
  assert.equal(L.fmtSigned(0.4), '+0.4');
  assert.equal(L.fmtSigned(-0.35), '−0.35');
  assert.equal(L.fmtSigned(0), '0.0');
  assert.equal(L.fmtPct(-0.49), '−0.49%');
  assert.equal(L.fmtPct(null), '—');
  assert.equal(L.weekRange({ week_start: '2026-10-18', week_end: '2026-10-24' }), '18–24/10');
  assert.equal(L.weekRange({ week_start: '2026-09-27', week_end: '2026-10-03' }), '27/9–3/10');
  assert.equal(L.minMaxText({ min_kg: 80, max_kg: 81.5 }), '80.0–81.5');
  assert.equal(L.minMaxText({ min_kg: 80, max_kg: 80 }), '80.0');
  assert.equal(L.minMaxText({ min_kg: null, max_kg: null }), '—');
  assert.equal(L.weighCount(1), 'שקילה אחת');
});

t('chart series: daily points and mid-week averages clamped to the range end, gaps stay null', () => {
  const tr = L.computeTrend([wi('2026-10-06', 80), wi('2026-10-20', 81)], '2026-10-21', 3, '2026-10-21');
  const s = L.chartSeries(tr);
  assert.deepEqual(s.daily.map((p) => [p.date, p.value]), [['2026-10-06', 80], ['2026-10-20', 81]]);
  assert.deepEqual(s.weekly.map((p) => [p.date, p.value]), [['2026-10-07', 80], ['2026-10-14', null], ['2026-10-21', 81]]);
});

t('list order and lookup', () => {
  const list = [wi('2026-10-01', 80), wi('2026-10-03', 81), wi('2026-10-02', 82)];
  assert.deepEqual(L.entriesNewestFirst(list).map((e) => e.date), ['2026-10-03', '2026-10-02', '2026-10-01']);
  assert.equal(L.entryOn(list, '2026-10-02').weight_kg, 82);
  assert.equal(L.entryOn(list, '2026-10-04'), null);
  const tr = L.computeTrend(list, '2026-10-03', 1, '2026-10-03');
  assert.deepEqual(L.weeksNewestFirst(tr).map((w) => w.week_start), ['2026-09-27']);
});

console.log(failed ? `\n${failed} failed` : '\nall passed');
process.exit(failed ? 1 : 0);
