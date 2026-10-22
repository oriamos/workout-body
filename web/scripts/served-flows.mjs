// End-to-end check of the UI as served BY THE ENGINE BINARY (no Vite, no proxy):
// builds the UI, embeds it (scripts/build-ui.sh), builds and starts the binary on a
// temp database, then drives Chromium through login -> week -> weigh-in -> nutrition
// -> logout. Checks that the CSP blocks nothing the app needs. Exits non-zero on failure.
//
//   node scripts/served-flows.mjs      (from web/; needs Go, npm and /opt/pw-browsers)
import { chromium } from 'playwright-core';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import net from 'node:net';
import { randomBytes } from 'node:crypto';
import { fileURLToPath } from 'node:url';

const WEB = fileURLToPath(new URL('..', import.meta.url));
const ROOT = path.resolve(WEB, '..');
const TMP = mkdtempSync(path.join(process.env.TMPDIR ?? tmpdir(), 'wb-served-'));
const TOKEN = randomBytes(24).toString('hex');
let child;
let failures = 0;
let step = 0;
const log = (...a) => console.log(...a);
async function check(cond, msg, extra) {
  if (cond) return log(`PASS ${++step}. ${msg}`);
  failures++;
  log(`FAIL ${++step}. ${msg}${extra ? `\n       ${typeof extra === 'function' ? await extra() : extra}` : ''}`);
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const freePort = () => new Promise((res, rej) => { const s = net.createServer(); s.listen(0, '127.0.0.1', () => { const { port } = s.address(); s.close(() => res(port)); }); s.on('error', rej); });
async function waitFor(fn, ms, what) {
  const t0 = Date.now();
  while (Date.now() - t0 < ms) { try { if (await fn()) return; } catch { /* retry */ } await sleep(200); }
  throw new Error(`timed out waiting for ${what}`);
}
const until = (fn, ms = 8000) => waitFor(fn, ms, 'condition').then(() => true, () => false);

let browser;
try {
  log('building UI and embedding it…');
  execFileSync(path.join(ROOT, 'scripts/build-ui.sh'), { stdio: 'inherit' });
  const bin = path.join(TMP, 'training-mcp');
  execFileSync('go', ['build', '-o', bin, './cmd/training-mcp'], { cwd: ROOT, stdio: 'inherit' });
  const port = await freePort();
  const base = `http://127.0.0.1:${port}`;
  let out = '';
  child = spawn(bin, ['--addr', `127.0.0.1:${port}`], { env: { ...process.env, MCP_BEARER_TOKEN: TOKEN, TRAINING_DB_PATH: path.join(TMP, 'e.db') }, stdio: ['ignore', 'pipe', 'pipe'] });
  child.stdout.on('data', (d) => (out += d));
  child.stderr.on('data', (d) => (out += d));
  await waitFor(async () => (await fetch(`${base}/health`)).ok, 30000, 'engine /health');
  await check(out.includes('web UI served at /'), 'the binary reports the embedded UI', out);

  // Seed one weigh-in through the API with the bearer token, as an assistant would.
  const today = (await (await fetch(`${base}/api/v1/schedule`, { headers: { Authorization: `Bearer ${TOKEN}` } })).json()).today;
  await fetch(`${base}/api/v1/weight/entries/${today}`, { method: 'PUT', headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ weight_kg: 81.4 }) });

  browser = await chromium.launch({ executablePath: process.env.CHROMIUM ?? '/opt/pw-browsers/chromium', args: ['--no-sandbox'] }).catch(() => chromium.launch({ args: ['--no-sandbox'] }));
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const page = await ctx.newPage();
  const problems = [];
  page.on('pageerror', (e) => problems.push(`pageerror: ${e.message}`));
  page.on('console', (m) => { if (m.type() === 'error') problems.push(`console: ${m.text()}`); });

  await page.goto(base + '/');
  await check(page.url().endsWith('/login'), 'an anonymous visit lands on the login page', page.url());
  await page.fill('#token', 'not-the-token');
  await page.click('button[type=submit]');
  await check(await page.locator('.err').isVisible(), 'a wrong token shows an error');
  await page.fill('#token', TOKEN);
  await page.click('button[type=submit]');
  await check(await until(async () => (await page.locator('nav.nav').count()) > 0, 15000), 'the right token opens the app');
  const cookies = await ctx.cookies();
  const sess = cookies.find((c) => c.name === 'wb_session');
  await check(sess && sess.httpOnly && sess.sameSite === 'Strict' && !sess.value.includes(TOKEN), 'session cookie is HttpOnly, SameSite=Strict and is not the token', JSON.stringify(cookies));
  await check(await page.evaluate((t) => !JSON.stringify(localStorage).includes(t) && !document.cookie.includes(t), TOKEN), 'the token is not stored in the browser');

  // Week screen talks to the API with the cookie.
  await check(await until(async () => (await page.locator('[aria-current="date"]').count()) > 0, 10000), 'the week screen loads from the server (today highlighted)');
  await page.goto(base + '/#/weight');
  await check(await until(async () => (await page.content()).includes('81.4') || (await page.content()).includes('81,4'), 10000), 'the weigh-in logged via the API shows on the weight screen');

  // A write through the UI with the cookie (nutrition: add a simple meal).
  await page.goto(base + '/#/nutrition');
  await page.getByRole('button', { name: /הוספת ארוחה/ }).first().click();
  const simple = page.getByLabel(/ארוחה פשוטה/);
  if (await simple.count()) await simple.check();
  await page.getByLabel(/^שם/).first().fill('Served-UI test meal');
  await page.getByLabel(/קלוריות/).first().fill('321');
  await page.getByRole('button', { name: /^שמירה|^הוספה|^שמור/ }).last().click();
  await check(await until(async () => {
    const r = await fetch(`${base}/api/v1/nutrition/days/${today}`, { headers: { Authorization: `Bearer ${TOKEN}` } });
    return (await r.json()).entries.some((e) => e.name === 'Served-UI test meal' && e.calories_kcal === 321);
  }, 10000), 'a meal added in the UI is stored by the server (cookie-authenticated write)');

  // Logout from settings.
  await page.goto(base + '/#/today');
  await page.getByRole('button', { name: /הגדרות/ }).first().click();
  await page.getByRole('button', { name: 'התנתק' }).click();
  await check(await until(async () => page.url().endsWith('/login')), 'logout returns to the login page', page.url());
  const after = (await ctx.request.get(base + '/api/v1/schedule')).status(); // the browser context's cookies
  await check(after === 401, 'after logout the API refuses the browser', String(after));
  await check(problems.filter((p) => !p.includes('401')).length === 0, 'no page errors and no CSP violations', problems.join('\n       '));
} catch (e) {
  failures++;
  log(`FAIL ${++step}. ${e.stack ?? e}`);
} finally {
  await browser?.close().catch(() => {});
  try { child?.kill('SIGTERM'); } catch { /* gone */ }
  if (!process.env.KEEP_TMP) rmSync(TMP, { recursive: true, force: true });
}
log(failures ? `\n${failures} check(s) failed` : `\nall ${step} checks passed`);
process.exit(failures ? 1 : 0);
