// End-to-end check of the UI against a REAL workout engine (no mocks).
// Starts the engine (go run ./cmd/training-mcp) on a temp database and the Vite dev server with the
// token-injecting proxy, then drives Chromium. Prints every step; exits non-zero on any failure.
//
//   node scripts/api-flows.mjs            (from web/; needs Go and /opt/pw-browsers)
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
const TMP = mkdtempSync(path.join(process.env.TMPDIR ?? tmpdir(), 'wb-e2e-'));
const TOKEN = randomBytes(24).toString('hex');
const SHOTS = process.env.SHOTS ?? TMP;
const children = [];
let failures = 0;
let step = 0;

const log = (...a) => console.log(...a);
const pass = (msg) => log(`PASS ${++step}. ${msg}`);
const fail = (msg, extra) => { failures++; log(`FAIL ${++step}. ${msg}${extra ? `\n       ${extra}` : ''}`); };
const check = (cond, msg, extra) => (cond ? pass(msg) : fail(msg, typeof extra === 'function' ? extra() : extra));
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
  return c;
}
async function waitFor(fn, ms, what) {
  const t0 = Date.now();
  let last;
  while (Date.now() - t0 < ms) {
    try { last = await fn(); if (last) return last; } catch (e) { last = e; }
    await sleep(250);
  }
  throw new Error(`timed out waiting for ${what}${last instanceof Error ? `: ${last.message}` : ''}`);
}
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

let browser;
try {
  // ---- servers ----
  log(`engine: go run ./cmd/training-mcp --addr 127.0.0.1:${apiPort} (db ${TMP}/e.db)`);
  start('engine', 'go', ['run', './cmd/training-mcp', '--addr', `127.0.0.1:${apiPort}`], { cwd: ROOT, env: { ...process.env, MCP_BEARER_TOKEN: TOKEN, TRAINING_DB_PATH: path.join(TMP, 'e.db') } });
  await waitFor(async () => (await fetch(`http://127.0.0.1:${apiPort}/health`)).ok, 240000, 'engine /health');
  const { data: config } = await api('GET', '/config');
  pass(`engine up, today=${config.today} (${config.timezone}), media=${config.media.enabled}`);

  log(`web: vite --port ${webPort} (WORKOUT_API_URL=http://127.0.0.1:${apiPort}, token via proxy)`);
  const viteBin = path.join(WEB, 'node_modules', '.bin', 'vite');
  start('vite', viteBin, ['--port', String(webPort), '--strictPort', '--host', '127.0.0.1'], { cwd: WEB, env: { ...process.env, VITE_DATA_SOURCE: 'api', WORKOUT_API_URL: `http://127.0.0.1:${apiPort}`, WORKOUT_API_TOKEN: TOKEN } });
  await waitFor(async () => (await fetch(WEBURL)).ok, 60000, 'vite');
  const viaProxy = await fetch(`${WEBURL}/api/v1/config`);
  check(viaProxy.ok, 'proxy injects the bearer token (/api/v1/config through Vite is 200 without a client token)', `status ${viaProxy.status}`);

  // ---- 1. import a plan through the API ----
  const PLAN = 'E2E Push';
  const { data: imp } = await api('POST', '/plans/import', { format: 'workout-plan', version: 1, name: PLAN, items: [
    { exercise: 'bench press', sets: 3, rep_min: 6, rep_max: 8 },
    { exercise: 'overhead press', sets: 2, rep_min: 8, rep_max: 10 },
  ] });
  check(imp.created && imp.plan.id > 0, `plan "${PLAN}" imported via API (id ${imp.plan.id})`);

  // ---- browser ----
  browser = await chromium.launch({ executablePath: process.env.CHROMIUM ?? '/opt/pw-browsers/chromium', args: ['--no-sandbox'] }).catch(() => chromium.launch({ args: ['--no-sandbox'] }));
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 1000 } });
  const p = await ctx.newPage();
  const errs = [];
  p.on('pageerror', (e) => errs.push(String(e)));
  p.on('console', (m) => m.type() === 'error' && !/Failed to load resource/.test(m.text()) && errs.push(m.text()));
  const card = (name) => p.locator('section.ex', { has: p.locator('h3', { hasText: name }) });
  const rows = (name) => card(name).locator('.set');
  const ready = () => p.waitForSelector('[data-set-id], .today-empty', { timeout: 20000 });
  const settle = () => waitFor(async () => (await p.locator('.status .spin').count()) === 0, 15000, 'saves to finish');
  const closeRest = async () => { if (await p.locator('.rest-x').count()) await p.locator('.rest-x').first().click().catch(() => {}); };
  const todayW = async () => (await api('GET', '/workouts/today')).data;

  await p.goto(`${WEBURL}/#/today`);
  await ready();
  check(await p.locator('.today-empty').count() === 1, 'UI loads from the engine: no workout today yet (empty state)');
  check(await p.locator('button[aria-label^="כלי הדגמה"]').count() === 0, 'prototype controls (flask) are hidden in api mode');

  // ---- 2. start today's workout from that plan ----
  await p.locator('.start-list button', { hasText: PLAN }).click();
  await p.waitForSelector('section.ex [data-set-id]');
  await settle();
  let w = await todayW();
  check(w.plan_name === PLAN && w.progress.filter((x) => x.target_sets > 0).length === 2 && w.sets.length === 0, 'starting from the plan creates today\'s workout on the server (follows the plan, no sets yet)', () => JSON.stringify({ plan: w.plan_name, progress: w.progress, sets: w.sets.length }));
  check(await rows('Bench Press').count() === 3 && await rows('Overhead Press').count() === 2, 'planned sets shown as draft rows (3 bench, 2 OHP)');

  // ---- 3. complete two sets ----
  const entered = [[60, 8], [62.5, 7]];
  for (let i = 0; i < 2; i++) {
    const r = rows('Bench Press').nth(i);
    await r.locator('.cell-weight').fill(String(entered[i][0]));
    await r.locator('.cell-reps').fill(String(entered[i][1]));
    await r.locator('.check').click();
    await closeRest();
    await settle();
  }
  await p.screenshot({ path: path.join(SHOTS, 'e2e-1-two-sets.png') });
  w = await todayW();
  const benchSets = w.sets.filter((s) => s.exercise === 'bench press');
  check(w.sets.length === 2 && benchSets.map((s) => [s.weight_kg, s.reps]).join('|') === '60,8|62.5,7', 'API has exactly the two completed sets with the entered values', () => JSON.stringify(w.sets.map((s) => [s.exercise, s.weight_kg, s.reps])));

  // ---- 4. reload: values come back from the server ----
  await p.reload();
  await ready();
  const done = rows('Bench Press').locator('.check[aria-pressed="true"]');
  const vals = [];
  for (let i = 0; i < 2; i++) vals.push([await rows('Bench Press').nth(i).locator('.cell-weight').inputValue(), await rows('Bench Press').nth(i).locator('.cell-reps').inputValue()]);
  check(await done.count() === 2 && vals.join('|') === '60,8|62.5,7' && await rows('Bench Press').count() === 3, 'after reload the two sets are shown as done with their values (+1 remaining draft)', () => JSON.stringify(vals));

  // ---- 5. change one set's reps in the UI ----
  const set2 = benchSets[1];
  await rows('Bench Press').nth(1).locator('.cell-reps').fill('9');
  await rows('Bench Press').nth(1).locator('.cell-reps').press('Enter');
  await settle();
  const s2 = (await api('GET', `/sets/${set2.id}`)).data;
  check(s2.reps === 9 && s2.weight_kg === 62.5, 'editing reps in the UI PATCHes the set (API shows 62.5 × 9)', () => JSON.stringify(s2));

  // ---- 6. a change made elsewhere (assistant) shows up ----
  const t0 = Date.now();
  await api('POST', '/workouts/today/sets', { sets: [{ exercise: 'overhead press', weight_kg: 40, reps: 10 }] }, { 'Idempotency-Key': `e2e-assistant-${TOKEN.slice(0, 8)}` });
  const ohpFirst = rows('Overhead Press').first();
  const seen = await waitFor(async () => (await ohpFirst.locator('.check').getAttribute('aria-pressed')) === 'true' && (await ohpFirst.locator('.cell-weight').inputValue()) === '40', 12000, 'assistant set in UI').then(() => true, () => false);
  const secs = ((Date.now() - t0) / 1000).toFixed(1);
  check(seen, `set POSTed directly to the API appears in the UI within ~10 s (took ${secs}s, filled the first planned OHP row)`);
  check(await p.locator('.toast-assistant').count() >= 1, 'assistant change announced with a toast', async () => await p.locator('.toasts').innerText());
  check(await rows('Overhead Press').count() === 2, 'no extra row: the remote set filled the empty planned row');
  await p.screenshot({ path: path.join(SHOTS, 'e2e-2-assistant.png') });

  // ---- 7. stale edit: no silent overwrite ----
  let staleOk = false;
  for (let attempt = 1; attempt <= 3 && !staleOk; attempt++) {
    await settle();
    await sleep(300);
    const target = (await todayW()).sets.find((s) => s.exercise === 'bench press' && s.id === benchSets[0].id);
    const row0 = rows('Bench Press').nth(0);
    const before = await row0.locator('.cell-reps').inputValue();
    const remoteReps = 3 + attempt; // 4, 5, 6
    const mine = remoteReps + 10; // must differ from what is displayed, or no edit happens
    await api('PATCH', `/sets/${target.id}`, { reps: remoteReps }, { 'If-Match': `"${target.revision}"`, 'Content-Type': 'application/merge-patch+json' });
    if ((await row0.locator('.cell-reps').inputValue()) !== before) { log(`       (attempt ${attempt}: the poll delivered the remote change before the edit; retrying)`); continue; }
    await row0.locator('.cell-reps').fill(String(mine));
    await row0.locator('.cell-reps').press('Enter');
    const surfaced = await waitFor(async () => {
      const failed = await row0.locator('.note-danger').count();
      const conflict = await row0.locator('.conflict').count();
      return failed || conflict ? (conflict ? 'conflict' : 'failed') : false;
    }, 8000, 'conflict or failed save').catch(() => null);
    const server = (await api('GET', `/sets/${target.id}`)).data;
    const errToast = await p.locator('.toast-error').allInnerTexts();
    await p.screenshot({ path: path.join(SHOTS, `e2e-3-stale-${attempt}.png`) });
    check(!!surfaced && errToast.length <= 1, `stale UI edit is surfaced to the user (${surfaced ?? 'nothing'}${errToast.length ? `; toast: "${errToast[0]}"` : ''})`);
    check(server.reps === remoteReps, `server value not clobbered (API reps ${server.reps}, remote ${remoteReps}, typed ${mine})`);
    if (surfaced === 'failed') {
      const shown = await waitFor(async () => (await row0.locator('.cell-reps').inputValue()) === String(remoteReps), 5000, 'fresh value').then(() => true, () => false);
      check(shown, `the fresh server value (${remoteReps}) is displayed after the failed save`);
    }
    staleOk = true;
  }
  if (!staleOk) fail('stale-edit scenario could not be staged (poll always won the race)');

  // ---- 8. add a library exercise (through plan import with a new exercise name) ----
  await p.goto(`${WEBURL}/#/plans`);
  await p.getByRole('button', { name: 'ייבוא JSON' }).click();
  await p.getByRole('button', { name: 'או הדבק JSON' }).click();
  await p.locator('.paste textarea').fill(JSON.stringify({ name: 'E2E Carry', exercises: [{ name: 'Zercher Carry', sets: 3, reps: [20, 30] }, { name: 'Bench Press', sets: 2, reps: 5 }] }));
  await p.getByRole('button', { name: 'תצוגה מקדימה' }).click();
  await p.getByRole('button', { name: 'ייבא תוכנית' }).click();
  await p.waitForSelector('.plan-detail');
  await waitFor(async () => (await api('GET', '/plans')).data.plans.some((x) => x.name === 'E2E Carry'), 10000, 'plan on server').catch(() => {});
  const exs = (await api('GET', '/exercises')).data.exercises;
  const zc = exs.find((e) => e.name === 'zercher carry');
  check(!!zc, 'new library exercise created on the server (GET /exercises has "zercher carry")', () => exs.map((e) => e.name).join(', '));
  const plans = (await api('GET', '/plans')).data.plans;
  const carry = plans.find((x) => x.name === 'E2E Carry');
  if (!carry) log('       toasts:', await p.locator('.toasts').innerText(), '| status:', await p.locator('.page-sub').first().innerText().catch(() => ''));
  const carryDetail = carry && (await api('GET', `/plans/${carry.id}`)).data;
  check(carryDetail && carryDetail.items.map((i) => `${i.exercise}:${i.target_sets}:${i.rep_min}-${i.rep_max}`).join('|') === 'zercher carry:3:20-30|bench press:2:5-5', 'imported plan saved on the server with its items', () => JSON.stringify(carryDetail?.items));

  // ---- 9. plan edit round trip (PUT with If-Match) ----
  if (!carry) throw new Error('imported plan missing on the server');
  await p.locator('.pex-main').first().click();
  await p.getByRole('button', { name: 'הוסף סטים' }).first().click();
  const carry2 = await waitFor(async () => { const d = (await api('GET', `/plans/${carry.id}`)).data; return d.revision > carryDetail.revision && d; }, 10000, 'plan revision').catch(async () => (await api('GET', `/plans/${carry.id}`)).data);
  check(carry2.items[0].target_sets === 4 && carry2.revision > carryDetail.revision, 'editing the plan in the UI replaces it on the server (sets 3 -> 4, revision bumped)', () => JSON.stringify(carry2.items[0]));

  // ---- 10. un-done / re-done a set (DELETE, then POST with a fresh idempotency key) ----
  await p.goto(`${WEBURL}/#/today`);
  await ready();
  const b2 = rows('Bench Press').nth(1);
  await b2.locator('.check').click();
  await waitFor(async () => (await todayW()).sets.filter((x) => x.exercise === 'bench press').length === 1, 8000, 'set deleted').catch(() => {});
  w = await todayW();
  check(w.sets.filter((x) => x.exercise === 'bench press').length === 1 && (await b2.locator('.cell-reps').inputValue()) === '9', 'marking a set not done deletes it on the server; the row keeps its values as a draft');
  await b2.locator('.check').click();
  await closeRest();
  await waitFor(async () => (await todayW()).sets.filter((x) => x.exercise === 'bench press').length === 2, 8000, 'set re-created').catch(() => {});
  w = await todayW();
  check(w.sets.filter((x) => x.exercise === 'bench press').map((x) => `${x.weight_kg}x${x.reps}`).join('|') === '60x4|62.5x9', 're-completing it creates the set again (60x4 | 62.5x9)', () => JSON.stringify(w.sets.map((x) => [x.exercise, x.weight_kg, x.reps])));

  // ---- 11. note on a prescribed exercise -> prescription notes ----
  await card('Bench Press').getByRole('button', { name: 'אפשרויות, Bench Press' }).click();
  await p.getByRole('menuitem', { name: 'הוספת הערה' }).click();
  await p.getByLabel('הערה, Bench Press').fill('עצירה בחזה');
  await p.getByRole('button', { name: 'שמור הערה' }).click();
  await waitFor(async () => (await todayW()).progress.find((x) => x.exercise === 'bench press')?.notes === 'עצירה בחזה', 8000, 'note').catch(() => {});
  check((await todayW()).progress.find((x) => x.exercise === 'bench press')?.notes === 'עצירה בחזה', 'note on a planned exercise is saved as the prescription note');

  // ---- 12. unplanned exercise: local until its first set, survives reload, then persisted ----
  await p.getByRole('button', { name: 'הוסף תרגיל' }).click();
  await p.getByLabel('חיפוש תרגיל').fill('zercher');
  await p.locator('.picklist button', { hasText: 'Zercher Carry' }).click();
  await card('Zercher Carry').waitFor();
  check((await todayW()).sets.every((x) => x.exercise !== 'zercher carry'), 'added exercise without a set stays local (nothing on the server)');
  await card('Zercher Carry').getByRole('button', { name: 'אפשרויות, Zercher Carry' }).click();
  await p.getByRole('menuitem', { name: 'הוספת הערה' }).click();
  await p.getByLabel('הערה, Zercher Carry').fill('רצועות');
  await p.getByRole('button', { name: 'שמור הערה' }).click();
  await p.reload();
  await ready();
  check(await card('Zercher Carry').count() === 1 && (await card('Zercher Carry').locator('.ex-note').innerText()).includes('רצועות'), 'after reload the pending exercise and its local note are still shown (localStorage)');
  const zr = rows('Zercher Carry').first();
  await zr.locator('.cell-weight').fill('40');
  await zr.locator('.cell-reps').fill('25');
  await zr.locator('.check').click();
  await closeRest();
  await waitFor(async () => (await todayW()).sets.some((x) => x.exercise === 'zercher carry'), 8000, 'zercher set').catch(() => {});
  const zset = (await todayW()).sets.find((x) => x.exercise === 'zercher carry');
  check(zset && zset.weight_kg === 40 && zset.reps === 25, 'completing its first set creates it on the server (40 x 25)');

  // ---- 13. set video: upload on a done set, refused on a draft ----
  if (config.media.enabled) {
    const video = path.join(WEB, 'public', 'demo-placeholder.mp4');
    await zr.locator('.more').click();
    await zr.locator('input[type=file]').setInputFiles(video);
    await waitFor(async () => (await api('GET', `/sets/${zset.id}/videos`)).data.videos.length === 1, 15000, 'video upload').catch(() => {});
    const vids = (await api('GET', `/sets/${zset.id}/videos`)).data.videos;
    check(vids.length === 1 && (await zr.locator('video.video').getAttribute('src')) === `/api/v1/media/${vids[0].id}`, 'video attached in the UI is uploaded to the set and played from /api/v1/media/{id}');
    const media = await fetch(`${WEBURL}/api/v1/media/${vids[0]?.id}`, { headers: { Range: 'bytes=0-15' } });
    check(media.status === 206, 'the video streams through the proxy (Range -> 206)', `status ${media.status}`);
    const draftRow = rows('Bench Press').nth(2);
    await draftRow.locator('.more').click();
    await draftRow.locator('input[type=file]').setInputFiles(video);
    await p.waitForSelector('.toast-error', { timeout: 5000 }).catch(() => {});
    check((await p.locator('.toast-error').allInnerTexts()).some((x) => x.includes('סמן את הסט כהושלם')), 'a video on a not-done set is refused with a Hebrew hint');
  }

  // ---- 14. remove an exercise, then undo ----
  await card('Zercher Carry').getByRole('button', { name: 'אפשרויות, Zercher Carry' }).click();
  await p.getByRole('menuitem', { name: 'הסר תרגיל' }).click();
  await waitFor(async () => !(await todayW()).sets.some((x) => x.exercise === 'zercher carry'), 8000, 'removed').catch(() => {});
  check(!(await todayW()).sets.some((x) => x.exercise === 'zercher carry'), 'removing an exercise deletes its sets on the server');
  await p.locator('.toast', { hasText: 'הוסר' }).getByRole('button', { name: 'ביטול' }).click();
  await waitFor(async () => (await todayW()).sets.some((x) => x.exercise === 'zercher carry'), 8000, 'restored').catch(() => {});
  const back = (await todayW()).sets.filter((x) => x.exercise === 'zercher carry');
  check(back.length === 1 && back[0].weight_kg === 40 && back[0].reps === 25, 'undo re-creates the set on the server');
  await p.screenshot({ path: path.join(SHOTS, 'e2e-4-final.png') });

  check(errs.length === 0, 'no page errors', () => errs.join('\n       '));
} catch (e) {
  fail(`aborted: ${e.message}`);
  for (const { name, out } of children) log(`--- ${name} output (tail) ---\n${out().split('\n').slice(-15).join('\n')}`);
} finally {
  await browser?.close().catch(() => {});
  cleanup();
}
log(failures ? `\n${failures} check(s) FAILED` : `\nall ${step} checks passed`);
process.exit(failures ? 1 : 0);
