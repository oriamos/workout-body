// Guide sheet + own-set-video smoke test. Needs the dev server.
import { chromium } from 'playwright-core';
import path from 'node:path';
const out = process.env.SHOTS ?? '/tmp/shots';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium', args: ['--no-sandbox'] });
const p = await (await b.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, hasTouch: true })).newPage();
const errs = [];
p.on('pageerror', (e) => errs.push(String(e)));
p.on('console', (m) => m.type() === 'error' && errs.push(m.text()));
const ok = (c, m) => console.log(c ? 'PASS' : 'FAIL', m);
await p.goto('http://127.0.0.1:5173/#/workout');
await p.waitForSelector('.ex');
await p.getByRole('button', { name: /הסבר וסרטון, Bench Press/ }).click();
await p.waitForSelector('.howto');
await p.screenshot({ path: path.join(out, 'v1-guide.png') });
ok((await p.locator('.howto-steps li').count()) >= 3, 'how-to steps shown');
await p.getByRole('radio', { name: 'סרטון הדגמה' }).click();
ok((await p.locator('.sheet video').count()) === 1, 'demo video player present');
await p.screenshot({ path: path.join(out, 'v2-demo.png') });
await p.getByRole('radio', { name: /הסרטונים שלי/ }).click();
ok((await p.locator('.video-empty').count()) === 1, 'empty state for my videos');
await p.keyboard.press('Escape');
// upload to bench set 3
const set = p.locator('[data-set-id="2026-10-21-bench-2"]');
await set.locator('.more').click();
await set.locator('input[type=file]').setInputFiles('public/demo-placeholder.mp4');
await p.waitForSelector('[data-set-id="2026-10-21-bench-2"] video');
ok(true, 'video attached to set');
ok((await set.locator('.more-video').count()) === 1, 'row shows video marker');
await set.scrollIntoViewIfNeeded();
await p.screenshot({ path: path.join(out, 'v3-attached.png') });
// bad file
await set.locator('input[type=file]').setInputFiles({ name: 'x.txt', mimeType: 'text/plain', buffer: Buffer.from('hi') });
await p.waitForTimeout(150);
ok((await p.locator('.toast-error').count()) === 1, 'non-video rejected with message');
// shows up under my videos on exercise page
await p.goto('http://127.0.0.1:5173/#/exercise/bench');
await p.waitForSelector('.guide');
await p.getByRole('radio', { name: /הסרטונים שלי/ }).click();
ok((await p.locator('.my-videos video').count()) === 1, 'attached video listed under my videos on the exercise page');
await p.reload();
await p.waitForSelector('.guide');
await p.getByRole('radio', { name: /הסרטונים שלי/ }).click();
ok((await p.locator('.my-videos video').count()) === 0, 'reload clears the in-memory video (documented in the UI)');
console.log('errors:', errs.length ? errs : 'none');
await b.close();
