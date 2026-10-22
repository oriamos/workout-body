// End-to-end check of the Weight screen against a REAL workout engine (no mocks).
// Starts the engine (go run ./cmd/training-mcp) on a temp database and Vite with the token-injecting
// proxy, then drives Chromium. Prints every step; exits non-zero on any failure.
//
//   node scripts/weight-flows.mjs      (from web/; needs Go and /opt/pw-browsers)
import { chromium } from 'playwright-core';
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import net from 'node:net';
import { randomBytes } from 'node:crypto';
import { fileURLToPath } from 'node:url';

const WEB = fileURLToPath(new URL('..', import.meta.url));
const ROOT = path.resolve(WEB, '..');
const TMP = mkdtempSync(path.join(process.env.TMPDIR ?? tmpdir(), 'wb-wt-'));
const TOKEN = randomBytes(24).toString('hex');
const SHOTS = process.env.SHOTS ?? TMP;
const children = [];
let failures = 0;
let step = 0;

const log = (...a) => console.log(...a);
const pass = (msg) => log(`PASS ${++step}. ${msg}`);
const fail = (msg, extra) => { failures++; log(`FAIL ${++step}. ${msg}${extra ? `\n       ${extra}` : ''}`); };
async function check(cond, msg, extra) {
  if (cond) return pass(msg);
  fail(msg, typeof extra === 'function' ? await extra() : extra);
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
function freePort() {
  return new Promise((res, rej) => {
    const s = net.createServer();
    s.listen(0, '127.0.0.1', () => { const { port } = s.address(); s.close(() => res(port)); });
    s.on('error', rej);
  });
}
function start(name, cmd, args, opts) {
  const c = spawn(cmd, args, { ...opts, detached: true, stdio: ['ignore', 'pipe', 'pipe'] });
  let out = '';
  c.stdout.on('data', (d) => (out += d));
  c.stderr.on('data', (d) => (out += d));
  children.push({ name, c, out: () => out });
}
async function waitFor(fn, ms, what) {
  const t0 = Date.now();
  let last;
  while (Date.now() - t0 < ms) {
    try { last = await fn(); if (last) return last; } catch (e) { last = e; }
    await sleep(200);
  }
  throw new Error(`timed out waiting for ${what}${last instanceof Error ? `: ${last.message}` : ''}`);
}
const until = (fn, ms = 8000) => waitFor(fn, ms, 'condition').then(() => true, () => false);
function cleanup() {
  for (const { c } of children) { try { process.kill(-c.pid, 'SIGTERM'); } catch { /* gone */ } }
  if (!process.env.KEEP_TMP) rmSync(TMP, { recursive: true, force: true });
}
process.on('SIGINT', () => { cleanup(); process.exit(130); });

const apiPort = await freePort();
const webPort = await freePort();
const API = `http://127.0.0.1:${apiPort}/api/v1`;
const WEBURL = `http://127.0.0.1:${webPort}`;
async function api(method, p, body, headers = {}, okStatuses = []) {
  const r = await fetch(API + p, { method, headers: { Authorization: `Bearer ${TOKEN}`, ...(body !== undefined && { 'Content-Type': 'application/json' }), ...headers }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await r.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!r.ok && !okStatuses.includes(r.status)) throw new Error(`${method} ${p} -> ${r.status} ${text.slice(0, 200)}`);
  return { data, status: r.status, etag: r.headers.get('etag') };
}
const addDays = (d, n) => { const t = new Date(`${d}T12:00:00Z`); t.setUTCDate(t.getUTCDate() + n); return t.toISOString().slice(0, 10); };
const nf = new Intl.NumberFormat('en-US', { minimumFractionDigits: 1, maximumFractionDigits: 2 });
const kg = (v) => (v == null ? '—' : nf.format(v));
const norm = (s) => s.replace(/\s+/g, ' ').trim();

let browser;
try {
  log(`engine: go run ./cmd/training-mcp --addr 127.0.0.1:${apiPort} (db ${TMP}/e.db)`);
  start('engine', 'go', ['run', './cmd/training-mcp', '--addr', `127.0.0.1:${apiPort}`], { cwd: ROOT, env: { ...process.env, MCP_BEARER_TOKEN: TOKEN, TRAINING_DB_PATH: path.join(TMP, 'e.db') } });
  await waitFor(async () => (await fetch(`http://127.0.0.1:${apiPort}/health`)).ok, 240000, 'engine /health');
  const { data: config } = await api('GET', '/config');
  const today = config.today;
  // Week bounds come from the engine (Sunday→Saturday by default) — never recomputed here.
  const { data: t0w } = await api('GET', '/weight/trend?weeks=2');
  const thisWeek = t0w.weeks[1];
  const thisStart = thisWeek.week_start;
  const prevStart = t0w.weeks[0].week_start;
  const dow = (d) => new Date(`${d}T12:00:00Z`).getUTCDay();
  const past = addDays(today, -2);
  const ext = addDays(today, -1);
  await check(t0w.weeks.length === 2 && thisStart <= today && today <= thisWeek.week_end && addDays(thisStart, 6) === thisWeek.week_end && addDays(prevStart, 7) === thisStart,
    `engine up, today=${today} (${config.timezone}); this week ${thisStart}–${thisWeek.week_end} from /weight/trend`, JSON.stringify(t0w.weeks));
  await check(dow(thisStart) === 0 && dow(thisWeek.week_end) === 6, 'weeks run Sunday→Saturday (engine default)', `${thisStart} dow=${dow(thisStart)}`);
  start('vite', path.join(WEB, 'node_modules', '.bin', 'vite'), ['--port', String(webPort), '--strictPort', '--host', '127.0.0.1'], { cwd: WEB, env: { ...process.env, VITE_DATA_SOURCE: 'api', WORKOUT_API_URL: `http://127.0.0.1:${apiPort}`, WORKOUT_API_TOKEN: TOKEN } });
  await waitFor(async () => (await fetch(WEBURL)).ok, 60000, 'vite');

  // Background: last week's weigh-ins (days 1, 3, 5 of it), so this week has something to compare with.
  // They end before today-2 even when today is the first day of the week.
  for (const [d, w] of [[prevStart, 82.4], [addDays(prevStart, 2), 82.0], [addDays(prevStart, 4), 81.8]]) await api('PUT', `/weight/entries/${d}`, { weight_kg: w });
  pass(`API setup: 3 weigh-ins in the week of ${prevStart} (82.4 / 82.0 / 81.8)`);

  browser = await chromium.launch({ executablePath: process.env.CHROMIUM ?? '/opt/pw-browsers/chromium', args: ['--no-sandbox'] }).catch(() => chromium.launch({ args: ['--no-sandbox'] }));
  const p = await (await browser.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  const errs = [];
  p.on('pageerror', (e) => errs.push(String(e)));
  p.on('console', (m) => m.type() === 'error' && !/Failed to load resource/.test(m.text()) && errs.push(m.text()));
  const sheet = p.locator('dialog.sheet[open]');
  const quick = p.locator('.wt-quick');
  const qField = (n) => quick.locator(`input[name="${n}"]`);
  const sField = (n) => sheet.locator(`input[name="${n}"]`);
  const text = async (sel) => norm(await p.locator(sel).first().innerText());
  const trendApi = async (weeks = 8) => (await api('GET', `/weight/trend?weeks=${weeks}`)).data;
  const getApi = async (d) => (await api('GET', `/weight/entries/${d}`, undefined, {}, [404]));
  const row = (d) => p.locator(`.wt-entry[data-date="${d}"]`);
  async function headlineMatches() {
    const t = await trendApi();
    const w = t.weeks[t.weeks.length - 1];
    const avg = await text('.wt-avg');
    const count = await text('.wt-count');
    const change = await text('.wt-change');
    const countWant = w.days_logged === 0 ? 'אין שקילות' : w.days_logged === 1 ? 'שקילה אחת' : `${w.days_logged} שקילות`;
    let changeOk;
    if (w.change_kg == null) changeOk = change.includes('אין מספיק נתונים להשוואה');
    else if (w.trend === 'flat') changeOk = change.includes('ללא שינוי');
    else changeOk = change.includes(w.trend === 'down' ? 'ירידה' : 'עלייה') && change.includes(`${nf.format(Math.abs(w.change_kg))} ק״ג`) && change.includes(`${nf.format(Math.abs(w.change_pct))}%`);
    return { ok: avg === kg(w.average_kg) && count === countWant && changeOk, info: `UI: ${avg} | ${count} | ${change} — API: ${JSON.stringify(w)}` };
  }

  // ---- 1. header scale icon → weight screen; the icon is current ----
  // Starts from the Nutrition tab, so the check does not depend on the home screen's layout.
  await p.goto(`${WEBURL}/#/nutrition`);
  await p.waitForSelector('.page-head', { timeout: 20000 });
  await check((await p.getByRole('button', { name: 'משקל', exact: true }).getAttribute('aria-current')) === null, 'header scale icon is not current on another screen');
  await p.getByRole('button', { name: 'משקל', exact: true }).click();
  await p.waitForSelector('.wt-headline');
  await check(p.url().endsWith('#/weight') && (await p.getByRole('button', { name: 'משקל', exact: true }).getAttribute('aria-current')) === 'page', '#/weight opens; the header scale icon has aria-current="page"');
  await check((await p.locator('.nav-item', { hasText: 'משקל' }).count()) === 0, 'no weight tab in the bottom bar (header icon only)');

  // ---- 2. headline before any weigh-in this week: nothing invented ----
  let h = await headlineMatches();
  await check(h.ok && (await text('.wt-avg')) === '—' && (await text('.wt-change')).includes('אין מספיק נתונים להשוואה'), 'no weigh-in this week: average "—", "אין שקילות", "אין מספיק נתונים להשוואה"', h.info);
  await check((await text('.wt-caption')).includes('הממוצע השבועי'), 'caption explains daily fluctuation vs the weekly average');

  // ---- 3. log today with a comma decimal ----
  await check((await qField('weight_kg').getAttribute('inputmode')) === 'decimal' && (await qField('date').inputValue()) === today && (await qField('date').getAttribute('max')) === today, 'quick log: inputmode=decimal, date defaults to today with max=today');
  await qField('weight_kg').fill('abc');
  await quick.getByRole('button', { name: 'שמירת שקילה' }).click();
  await check((await quick.locator('.field-err').count()) === 1 && (await getApi(today)).status === 404, 'invalid weight flagged inline, nothing sent');
  await qField('weight_kg').fill('81,4');
  await quick.getByRole('button', { name: 'שמירת שקילה' }).click();
  await until(async () => (await p.locator('.wt-today-kg').count()) === 1);
  let r = await getApi(today);
  await check(r.status === 200 && r.data.weight_kg === 81.4 && r.data.revision === 1, `"81,4" saved for today as 81.4 kg (revision ${r.data?.revision})`, JSON.stringify(r.data));
  await check((await text('.wt-today-kg')) === '81.4', 'today\'s weigh-in is shown at the top');
  h = await headlineMatches();
  await check(h.ok, `headline matches /weight/trend (${await text('.wt-avg')} · ${await text('.wt-count')} · ${await text('.wt-change')})`, h.info);
  await check((await p.locator('.wt-change').getAttribute('data-dir')) === 'down', 'week-over-week direction indicator is "down" (82.07 → 81.4)');
  await p.screenshot({ path: path.join(SHOTS, 'wt-1-logged.png') });

  // ---- 4. edit today (If-Match revision) ----
  await quick.getByRole('button', { name: 'עריכה' }).click();
  await check((await qField('weight_kg').inputValue()) === '81.4', 'edit prefills today\'s value');
  await qField('weight_kg').fill('81.1');
  await qField('note').fill('אחרי ארוחה מלוחה');
  await quick.getByRole('button', { name: 'עדכון שקילה' }).click();
  await until(async () => (await p.locator('.wt-today-kg').count()) === 1 && (await text('.wt-today-kg')) === '81.1');
  r = await getApi(today);
  await check(r.data.weight_kg === 81.1 && r.data.revision === 2 && r.data.note === 'אחרי ארוחה מלוחה', 'edit replaced today\'s weigh-in (81.1, note, revision 2)', JSON.stringify(r.data));

  // ---- 5. log a past date ----
  await quick.getByRole('button', { name: 'רישום לתאריך קודם' }).click();
  await check((await qField('date').inputValue()) === ext, `"רישום לתאריך קודם" opens the form on yesterday (${ext})`);
  await qField('date').fill(prevStart);
  await until(async () => (await quick.locator('.wt-replace-note').count()) === 1);
  await check((await qField('weight_kg').inputValue()) === '82.4' && (await quick.locator('.wt-replace-note').innerText()).includes('82.4'), 'choosing a date that has a weigh-in prefills it and says it will be updated');
  await qField('date').fill(past);
  await until(async () => (await quick.locator('.wt-replace-note').count()) === 0);
  await check((await qField('weight_kg').inputValue()) === '' && (await quick.locator('.wt-date-note').innerText()).includes('לא להיום'), `a free past date (${past}) clears the prefill and says "לא להיום"`);
  await qField('weight_kg').fill('82.2');
  await quick.getByRole('button', { name: 'שמירת שקילה' }).click();
  await until(async () => (await row(past).count()) === 1);
  r = await getApi(past);
  await check(r.status === 200 && r.data.weight_kg === 82.2, `past weigh-in saved on ${past} (82.2) and listed`, JSON.stringify(r.data));
  await check((await p.locator('.wt-today-kg').count()) === 1, 'the quick-log card is back on today\'s weigh-in');
  h = await headlineMatches();
  await check(h.ok, `headline still matches the API after the past entry (${await text('.wt-avg')} · ${await text('.wt-count')})`, h.info);

  // ---- 6. weekly table and range ----
  let t8 = await trendApi(8);
  const firstRow = p.locator('.wt-week-row').first();
  await check((await p.locator('.wt-week-row').count()) === 8 && (await firstRow.getAttribute('data-week')) === thisStart, 'weekly table: 8 weeks, newest first');
  const lastWeekApi = t8.weeks.find((w) => w.week_start === prevStart);
  const lwRow = norm(await p.locator(`.wt-week-row[data-week="${prevStart}"]`).innerText());
  await check(lwRow.includes(kg(lastWeekApi.average_kg)) && lwRow.includes(`${kg(lastWeekApi.min_kg)}–${kg(lastWeekApi.max_kg)}`), `last week's row shows the API average/min–max (${lwRow})`);
  await p.getByRole('radio', { name: '4 שבועות' }).click();
  await until(async () => (await p.locator('.wt-week-row').count()) === 4);
  await check((await p.locator('.wt-week-row').count()) === 4, 'range 4 weeks → 4 rows');
  await check((await p.locator('.wt-chart circle.pt').count()) === (await trendApi(4)).entries.length && (await p.locator('.wt-chart rect.pt-2').count()) >= 2, 'chart draws one point per weigh-in plus weekly-average markers');
  await p.getByRole('radio', { name: '8 שבועות' }).click();
  await until(async () => (await p.locator('.wt-week-row').count()) === 8);

  // ---- 7. delete with confirmation ----
  await row(past).getByRole('button', { name: /מחיקת השקילה/ }).click();
  await check((await row(past).locator('.wt-confirm').count()) === 1 && (await getApi(past)).status === 200, 'delete asks for confirmation first (nothing deleted yet)');
  await row(past).getByRole('button', { name: 'כן, למחוק' }).click();
  await until(async () => (await row(past).count()) === 0);
  await check((await getApi(past)).status === 404 && (await row(past).count()) === 0, 'confirmed delete removes it on the server and from the list');

  // ---- 8. polling picks up a weigh-in added via HTTP; an open form keeps its values ----
  await row(today).getByRole('button', { name: /עריכת השקילה/ }).click();
  await sheet.waitFor();
  await sField('weight_kg').fill('80.5');
  await sField('note').focus();
  const t0 = Date.now();
  await api('PUT', `/weight/entries/${ext}`, { weight_kg: 81.9 }, { 'If-Match': '"0"' });
  const appeared = await until(async () => (await row(ext).count()) === 1, 12000);
  await check(appeared, `weigh-in PUT through the API appears in the list within ~10 s (${((Date.now() - t0) / 1000).toFixed(1)}s)`);
  h = await headlineMatches();
  await check(h.ok, 'headline refreshed from the API by polling', h.info);
  await check((await sheet.count()) === 1 && (await sField('weight_kg').inputValue()) === '80.5' && (await p.evaluate(() => document.activeElement?.getAttribute('name'))) === 'note', 'the open edit form kept its typed value and focus');

  // ---- 9. stale edit (412): message with the server value, typed value kept ----
  await api('PUT', `/weight/entries/${today}`, { weight_kg: 81.8 }, { 'If-Match': '"2"' });
  await sheet.getByRole('button', { name: 'שמירה' }).click();
  await until(async () => (await sheet.locator('.wt-form-msg').count()) === 1, 6000);
  const msg = norm(await sheet.locator('.wt-form-msg').innerText().catch(() => ''));
  r = await getApi(today);
  await check(msg.includes('השתנתה במקום אחר') && msg.includes('81.8'), `stale save shows a Hebrew message with the server value ("${msg.slice(0, 80)}…")`);
  await check((await sField('weight_kg').inputValue()) === '80.5' && r.data.weight_kg === 81.8 && r.data.revision === 3, 'typed value kept; server value not overwritten (81.8, revision 3)');
  await until(async () => (await text('.wt-today-kg')) === '81.8', 8000);
  await check((await text('.wt-today-kg')) === '81.8', 'the page shows the server value behind the form');
  await sheet.getByRole('button', { name: 'שמירה' }).click();
  await sheet.waitFor({ state: 'detached' }).catch(() => {});
  r = await getApi(today);
  await check(r.data.weight_kg === 80.5 && r.data.revision === 4, 'saving again after the warning is an explicit overwrite (80.5, revision 4)', JSON.stringify(r.data));

  // ---- 10. create race (If-Match "0"): someone logged the date first ----
  await quick.getByRole('button', { name: 'רישום לתאריך קודם' }).click();
  await qField('date').fill(past);
  await until(async () => (await qField('weight_kg').inputValue()) === '' && (await quick.locator('.wt-replace-note').count()) === 0);
  await qField('weight_kg').fill('83');
  await api('PUT', `/weight/entries/${past}`, { weight_kg: 82.6 });
  await quick.getByRole('button', { name: 'שמירת שקילה' }).click();
  await until(async () => (await quick.locator('.wt-form-msg').count()) === 1, 6000);
  r = await getApi(past);
  await check((await quick.locator('.wt-form-msg').innerText()).includes('82.6') && (await qField('weight_kg').inputValue()) === '83' && r.data.weight_kg === 82.6, 'a new weigh-in for a date logged elsewhere meanwhile is not overwritten (412 → shows 82.6, keeps 83)');
  await quick.getByRole('button', { name: 'ביטול' }).click();

  // ---- 11. reload: today's weight comes back from the server ----
  await p.reload();
  await p.waitForSelector('.wt-today-kg');
  await check((await text('.wt-today-kg')) === '80.5', 'after reload the quick-log card shows today\'s weight (80.5)');
  await p.screenshot({ path: path.join(SHOTS, 'wt-2-final.png') });

  // mobile width: no horizontal page scroll
  await p.setViewportSize({ width: 390, height: 844 });
  await sleep(300);
  await check((await p.evaluate(() => document.documentElement.scrollWidth)) <= 390, 'no horizontal page scroll at 390 px');

  await check(errs.length === 0, 'no page errors', () => errs.join('\n       '));
} catch (e) {
  fail(`aborted: ${e.message}`);
  for (const { name, out } of children) log(`--- ${name} output (tail) ---\n${out().split('\n').slice(-15).join('\n')}`);
} finally {
  await browser?.close().catch(() => {});
  cleanup();
}
log(failures ? `\n${failures} check(s) FAILED` : `\nall ${step} checks passed`);
process.exit(failures ? 1 : 0);
