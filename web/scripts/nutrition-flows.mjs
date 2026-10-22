// End-to-end check of the Nutrition module against a REAL workout engine (no mocks).
// Starts the engine (go run ./cmd/training-mcp) on a temp database and Vite with the token-injecting
// proxy, then drives Chromium. Prints every step; exits non-zero on any failure.
//
//   node scripts/nutrition-flows.mjs      (from web/; needs Go and /opt/pw-browsers)
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
const TMP = mkdtempSync(path.join(process.env.TMPDIR ?? tmpdir(), 'wb-nut-'));
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
async function api(method, p, body, headers = {}) {
  const r = await fetch(API + p, { method, headers: { Authorization: `Bearer ${TOKEN}`, ...(body !== undefined && { 'Content-Type': 'application/json' }), ...headers }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await r.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!r.ok) throw new Error(`${method} ${p} -> ${r.status} ${text.slice(0, 200)}`);
  return { data, etag: r.headers.get('etag') };
}
const addDays = (d, n) => { const t = new Date(`${d}T12:00:00Z`); t.setUTCDate(t.getUTCDate() + n); return t.toISOString().slice(0, 10); };

let browser;
try {
  log(`engine: go run ./cmd/training-mcp --addr 127.0.0.1:${apiPort} (db ${TMP}/e.db)`);
  start('engine', 'go', ['run', './cmd/training-mcp', '--addr', `127.0.0.1:${apiPort}`], { cwd: ROOT, env: { ...process.env, MCP_BEARER_TOKEN: TOKEN, TRAINING_DB_PATH: path.join(TMP, 'e.db') } });
  await waitFor(async () => (await fetch(`http://127.0.0.1:${apiPort}/health`)).ok, 240000, 'engine /health');
  const { data: config } = await api('GET', '/config');
  const today = config.today;
  const past = addDays(today, -3);
  pass(`engine up, today=${today} (${config.timezone})`);
  start('vite', path.join(WEB, 'node_modules', '.bin', 'vite'), ['--port', String(webPort), '--strictPort', '--host', '127.0.0.1'], { cwd: WEB, env: { ...process.env, VITE_DATA_SOURCE: 'api', WORKOUT_API_URL: `http://127.0.0.1:${apiPort}`, WORKOUT_API_TOKEN: TOKEN } });
  await waitFor(async () => (await fetch(WEBURL)).ok, 60000, 'vite');

  // Background data through the API: historical targets and a past meal (for Browse).
  await api('PUT', `/nutrition/targets/${addDays(today, -5)}`, { calories_kcal: 2000, protein_g: 120 }, { 'If-Match': '"0"', 'Content-Type': 'application/merge-patch+json' });
  const { data: shak } = await api('POST', '/nutrition/entries', { date: past, name: 'שקשוקה', calories_kcal: 450, protein_g: 25, carbs_g: 20, fat_g: 30 }, { 'Idempotency-Key': 'e2e-past-1' });
  pass(`API setup: targets from ${addDays(today, -5)} (2000 kcal, 120 g protein), past meal on ${past}`);

  browser = await chromium.launch({ executablePath: process.env.CHROMIUM ?? '/opt/pw-browsers/chromium', args: ['--no-sandbox'] }).catch(() => chromium.launch({ args: ['--no-sandbox'] }));
  const p = await (await browser.newContext({ viewport: { width: 1280, height: 1000 } })).newPage();
  const errs = [];
  p.on('pageerror', (e) => errs.push(String(e)));
  p.on('console', (m) => m.type() === 'error' && !/Failed to load resource/.test(m.text()) && errs.push(m.text()));
  const sheet = p.locator('dialog.sheet[open]');
  const field = (name) => sheet.locator(`input[name="${name}"]`);
  const row = (k) => p.locator(`.nut-row[data-nutrient="${k}"]`);
  const rowText = async (k) => (await row(k).innerText()).replace(/\s+/g, ' ');
  const entry = (name) => p.locator('.nut-entries .nut-entry', { hasText: name });
  const dayApi = async (d = today) => (await api('GET', `/nutrition/days/${d}`)).data;
  async function fillForm(v) {
    for (const [k, val] of Object.entries(v)) await field(k).fill(val);
  }

  // ---- 1. open Nutrition from the tab bar ----
  await p.goto(`${WEBURL}/#/today`);
  await p.waitForSelector('[data-set-id], .today-empty', { timeout: 20000 });
  await p.locator('.nav-item', { hasText: 'תזונה' }).click();
  await p.waitForSelector('.nut-summary');
  await check(p.url().endsWith('#/nutrition') && (await p.locator('.nut-not-today').count()) === 0, 'Nutrition tab opens today\'s day (no "not today" badge)');
  await check((await rowText('carbs_g')).includes('אין יעד') && (await rowText('calories_kcal')).includes('2,000'), 'targets in effect today come from the server; unset target shows "אין יעד"', () => rowText('carbs_g'));

  // ---- 2. set targets from today ----
  await p.getByRole('button', { name: 'יעדים יומיים' }).click();
  await check((await sheet.innerText()).includes(addDays(today, -5)), 'targets sheet shows the targets in effect and their effective_from date');
  await fillForm({ calories_kcal: '1000', protein_g: '50', carbs_g: '100' });
  await sheet.getByRole('button', { name: 'שמירת יעדים' }).click();
  await sheet.waitFor({ state: 'detached' }).catch(() => {});
  const tToday = (await api('GET', `/nutrition/targets?date=${today}`)).data.targets;
  const tPast = (await api('GET', `/nutrition/targets?date=${past}`)).data.targets;
  await check(tToday.effective_from === today && tToday.calories_kcal === 1000 && tToday.protein_g === 50 && tToday.carbs_g === 100 && tToday.fat_g == null, 'targets saved from today forward (1000 kcal / 50 P / 100 C, fat unset)', JSON.stringify(tToday));
  await check(tPast.calories_kcal === 2000 && tPast.carbs_g == null, 'an earlier day keeps its targets (2000 kcal)', JSON.stringify(tPast));

  // ---- 3. add two meals via the form, one with a blank field ----
  await p.getByRole('button', { name: 'הוספת ארוחה' }).click();
  await fillForm({ name: 'חביתה', calories_kcal: '400', protein_g: '25', fat_g: '30' }); // carbs left blank
  await sheet.getByRole('button', { name: 'הוספה' }).click();
  await sheet.waitFor({ state: 'detached' }).catch(() => {});
  await p.getByRole('button', { name: 'הוספת ארוחה' }).click();
  await fillForm({ name: 'פסטה', calories_kcal: '700', protein_g: '20', carbs_g: '90', fat_g: '15.5' });
  await sheet.getByRole('button', { name: 'הוספה' }).click();
  await sheet.waitFor({ state: 'detached' }).catch(() => {});
  await until(async () => (await entry('פסטה').count()) === 1);
  let d = await dayApi();
  const omelette = d.entries.find((e) => e.name === 'חביתה');
  await check(d.entry_count === 2 && omelette && omelette.carbs_g === null && omelette.calories_kcal === 400, 'two meals stored; the blank carbs field is null on the server (not 0)', JSON.stringify(d.entries));
  await check((await entry('חביתה').innerText()).includes('—'), 'the missing value is shown as "—" in the list');

  // ---- 4. totals / remaining / over ----
  await check((await rowText('calories_kcal')).includes('1,100') && (await rowText('calories_kcal')).includes('חריגה של 100'), 'calories: 1,100 / 1,000 -> "חריגה של 100"', () => rowText('calories_kcal'));
  await check((await rowText('protein_g')).includes('45') && (await rowText('protein_g')).includes('נותרו 5'), 'protein: 45 / 50 -> "נותרו 5"', () => rowText('protein_g'));
  await check((await rowText('carbs_g')).includes('90') && (await rowText('carbs_g')).includes('נותרו 10') && (await rowText('carbs_g')).includes('חסר בארוחה אחת'), 'carbs: 90 / 100 -> "נותרו 10" + "חסר בארוחה אחת"', () => rowText('carbs_g'));
  await check((await rowText('fat_g')).includes('45.5') && (await rowText('fat_g')).includes('אין יעד'), 'fat: 45.5 with no target -> "אין יעד"', () => rowText('fat_g'));
  await p.screenshot({ path: path.join(SHOTS, 'nut-1-day.png') });

  // ---- 5. edit one (PATCH with If-Match) ----
  await entry('פסטה').click();
  await field('calories_kcal').fill('500');
  await sheet.getByRole('button', { name: 'שמירה' }).click();
  await sheet.waitFor({ state: 'detached' }).catch(() => {});
  await until(async () => (await rowText('calories_kcal')).includes('900'));
  d = await dayApi();
  const pasta = d.entries.find((e) => e.name === 'פסטה');
  await check(pasta.calories_kcal === 500 && pasta.revision === 2 && (await rowText('calories_kcal')).includes('נותרו 100'), 'editing a meal PATCHes it (500 kcal, revision 2) and totals update ("נותרו 100")', () => rowText('calories_kcal'));

  // ---- 6. failed validation keeps the typed values ----
  await p.getByRole('button', { name: 'הוספת ארוחה' }).click();
  await fillForm({ name: 'בדיקה', calories_kcal: '-5', protein_g: 'abc', carbs_g: '2500' });
  await sheet.getByRole('button', { name: 'הוספה' }).click();
  const errTexts = await sheet.locator('.field-err').allInnerTexts();
  await check(errTexts.length === 3 && (await field('calories_kcal').inputValue()) === '-5' && (await field('protein_g').inputValue()) === 'abc' && (await field('name').inputValue()) === 'בדיקה', `invalid values are flagged inline and kept (${errTexts.join(' | ')})`);
  await check((await dayApi()).entry_count === 2, 'nothing was sent to the server');
  await sheet.getByRole('button', { name: 'ביטול' }).click();

  // ---- 7. browse previous meals and copy one to today ----
  await p.getByRole('button', { name: 'ארוחות קודמות' }).click();
  await sheet.getByLabel('חיפוש ארוחה לפי שם').fill('שקשוקה');
  await until(async () => (await sheet.locator('.nut-entry').count()) === 1);
  await check((await sheet.locator('.nut-entry').count()) === 1, 'search by name finds the past meal');
  await sheet.getByLabel('מתאריך').fill(today);
  await until(async () => (await sheet.locator('.pick-empty').count()) === 1);
  await check((await sheet.locator('.pick-empty').count()) === 1, 'date filter (from today) excludes it');
  await sheet.getByLabel('מתאריך').fill(past);
  await until(async () => (await sheet.locator('.nut-entry').count()) === 1);
  await sheet.locator('.nut-entry', { hasText: 'שקשוקה' }).click();
  await check((await sheet.locator('.nut-detail').innerText()).includes('450'), 'entry details shown');
  await sheet.getByRole('button', { name: 'העתקה להיום' }).click();
  await until(async () => (await p.locator('.toast', { hasText: 'הועתקה להיום' }).count()) > 0, 5000);
  await check((await p.locator('.toast', { hasText: 'הועתקה להיום' }).count()) > 0, 'copy shows a toast');
  await sheet.getByRole('button', { name: 'סגירה' }).click();
  await until(async () => (await entry('שקשוקה').count()) === 1);
  d = await dayApi();
  const copy = d.entries.find((e) => e.name === 'שקשוקה');
  await check(copy && copy.copied_from === shak.entry.id && copy.date === today && (await entry('שקשוקה').count()) === 1, 'the copy is a new entry today (copied_from set) and listed', JSON.stringify(copy));

  // ---- 8. delete one; totals recalculate ----
  await entry('חביתה').click();
  await sheet.getByRole('button', { name: 'מחיקה' }).click();
  await sheet.getByRole('button', { name: 'כן, למחוק' }).click();
  await sheet.waitFor({ state: 'detached' }).catch(() => {});
  await until(async () => (await entry('חביתה').count()) === 0);
  d = await dayApi();
  await check(!d.entries.some((e) => e.name === 'חביתה') && d.calories_kcal.consumed === 950, 'delete (after confirmation) removes it on the server (950 kcal left)');
  await check((await rowText('calories_kcal')).includes('950') && !(await rowText('carbs_g')).includes('חסר'), 'totals recalculated in the UI (950; no carbs missing any more)', () => rowText('calories_kcal'));

  // ---- 9. an entry added elsewhere appears without disturbing an open form ----
  await p.getByRole('button', { name: 'הוספת ארוחה' }).click();
  await fillForm({ name: 'טופס פתוח', calories_kcal: '123' });
  await field('protein_g').focus();
  const t0 = Date.now();
  await api('POST', '/nutrition/entries', { name: 'בננה', calories_kcal: 105 }, { 'Idempotency-Key': 'e2e-ext-1' });
  const appeared = await until(async () => (await entry('בננה').count()) === 1, 12000);
  await check(appeared, `entry POSTed to the API appears in the day list within ~10 s (${((Date.now() - t0) / 1000).toFixed(1)}s)`);
  await check((await sheet.count()) === 1 && (await field('name').inputValue()) === 'טופס פתוח' && (await field('calories_kcal').inputValue()) === '123' && (await p.evaluate(() => document.activeElement?.getAttribute('name'))) === 'protein_g', 'the open form kept its values and focus');
  await p.screenshot({ path: path.join(SHOTS, 'nut-2-open-form.png') });
  await sheet.getByRole('button', { name: 'ביטול' }).click();

  // ---- 10. stale edit (412): message, typed values kept, server values refreshed ----
  const cur = (await dayApi()).entries.find((e) => e.name === 'פסטה');
  await entry('פסטה').click();
  await api('PATCH', `/nutrition/entries/${cur.id}`, { calories_kcal: 600 }, { 'If-Match': `"${cur.revision}"`, 'Content-Type': 'application/merge-patch+json' });
  await field('calories_kcal').fill('550');
  await sheet.getByRole('button', { name: 'שמירה' }).click();
  await until(async () => (await sheet.locator('.nut-form-msg').count()) === 1, 6000);
  const msg = (await sheet.locator('.nut-form-msg').innerText().catch(() => '')).trim();
  const server = (await api('GET', `/nutrition/entries/${cur.id}`)).data;
  await check(msg.includes('שונתה במקום אחר') && msg.includes('600'), `stale save shows a Hebrew message with the server value ("${msg.slice(0, 70)}…")`);
  await check((await field('calories_kcal').inputValue()) === '550' && server.calories_kcal === 600, 'typed value kept in the form; server value not overwritten (600)');
  await until(async () => (await rowText('calories_kcal')).includes('1,155'), 6000);
  await check((await rowText('calories_kcal')).includes('1,155'), 'day totals refreshed with the server value (600 + 450 + 105 = 1,155)', () => rowText('calories_kcal'));
  await sheet.getByRole('button', { name: 'ביטול' }).click();

  // ---- 11. previous day: badge and that day's historical targets ----
  for (let i = 0; i < 3; i++) await p.getByRole('button', { name: 'היום הקודם' }).click();
  await until(async () => p.url().endsWith(`#/nutrition/${past}`) && (await entry('שקשוקה').count()) === 1);
  await check(p.url().endsWith(`#/nutrition/${past}`) && (await p.locator('.nut-not-today').innerText()) === 'לא היום', `navigating back 3 days shows ${past} with the "לא היום" badge`);
  await check((await rowText('calories_kcal')).includes('450') && (await rowText('calories_kcal')).includes('2,000') && (await rowText('carbs_g')).includes('אין יעד'), 'that day shows its own meal and its historical targets (2,000 kcal, no carbs target)', () => rowText('calories_kcal'));
  await p.getByRole('button', { name: 'הוספת ארוחה' }).click();
  await check((await sheet.locator('.nut-date-note').innerText()).includes('לא להיום'), 'the add form says the meal goes to that day, not today');
  await sheet.getByRole('button', { name: 'ביטול' }).click();
  await p.getByRole('button', { name: 'בחירת תאריך' }).click();
  await until(async () => (await sheet.locator('.cal-day.trained').count()) >= 2, 5000);
  await check((await sheet.locator('.cal-day.trained').count()) >= 2 && (await sheet.locator('.cal-day.sel').innerText()) === String(Number(past.slice(8))), 'calendar picker marks days with meals and the selected day', async () => `${await sheet.locator('.cal-day.trained').count()} marked`);
  await sheet.getByRole('button', { name: 'היום', exact: true }).click();
  await until(async () => p.url().endsWith('#/nutrition'));
  await check((await p.locator('.nut-not-today').count()) === 0, '"היום" in the calendar returns to today');
  await p.screenshot({ path: path.join(SHOTS, 'nut-3-today.png') });

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
