// Interaction smoke test for the prototype. Run with the dev server up: node scripts/flows.mjs
import { chromium } from 'playwright-core';
import path from 'node:path';
const out = process.env.SHOTS ?? '/tmp/shots';
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium', args: ['--no-sandbox'] });
const ctx = await b.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, hasTouch: true });
const p = await ctx.newPage();
const errs = [];
p.on('pageerror', (e) => errs.push(String(e)));
p.on('console', (m) => m.type() === 'error' && errs.push(m.text()));
const ok = (c, msg) => console.log(c ? 'PASS' : 'FAIL', msg);
const shot = (n) => p.screenshot({ path: path.join(out, n + '.png') });
const row = (id) => p.locator(`[data-set-id="${id}"]`);
const days = () => p.locator('.week-days > .day');
const dayCard = (date) => p.locator(`.week-days > .day[data-date="${date}"]`);
const planStatus = (date, name) => dayCard(date).locator(`.day-plan[data-plan="${name}"]`).getAttribute('data-status');
await p.goto('http://127.0.0.1:5173/#/today');
await p.waitForSelector('.week-days > .day');

// 0. home = this week: 7 days Sunday -> Saturday, schedule statuses, today highlighted
const dates = await days().evaluateAll((els) => els.map((e) => `${e.dataset.date}:${e.dataset.weekday}`));
ok(dates.join(',') === '2026-10-18:sun,2026-10-19:mon,2026-10-20:tue,2026-10-21:wed,2026-10-22:thu,2026-10-23:fri,2026-10-24:sat', `week shows 7 days Sun 18 -> Sat 24 Oct (${dates.join(',')})`);
ok((await p.locator('.nav-item[aria-current="page"]').innerText()).includes('השבוע'), 'bottom tab is "השבוע"');
ok((await p.locator('h1').innerText()).replace(/[\u2066-\u2069]/g, '') === '18–24 באוקטובר', 'header shows the week range');
ok(await p.locator('.day[aria-current="date"]').getAttribute('data-date') === '2026-10-21', 'today (Wed 21) is aria-current and highlighted');
ok(await planStatus('2026-10-19', 'Upper A') === 'done' && (await dayCard('2026-10-19').locator('.chip').innerText()) === 'בוצע', 'Upper A on Monday: done (בוצע)');
ok(await planStatus('2026-10-20', 'Lower A') === 'done', 'Lower A on Tuesday: done');
ok(await planStatus('2026-10-22', 'Upper B') === 'upcoming' && (await dayCard('2026-10-22').locator('.chip').innerText()) === 'מתוכנן', 'Upper B on Thursday: upcoming (מתוכנן)');
ok(await planStatus('2026-10-23', 'Lower B') === 'upcoming', 'Lower B on Friday: upcoming');
ok((await dayCard('2026-10-19').locator('.day-stats').innerText()).includes('סטים'), 'a past day shows sets done and volume');
ok((await p.locator('.freq').innerText()).includes('Full Body') && (await p.locator('.freq').innerText()).includes('1 מתוך 1'), 'weekly frequency line for Full Body');
ok((await p.locator('.week-count').innerText()).includes('מתוך 5'), 'week summary: done / planned');
await shot('w1-week');
// prev / next week
await p.getByRole('button', { name: 'שבוע קודם' }).click();
await p.waitForSelector('.day[data-date="2026-10-11"]');
ok((await days().first().getAttribute('data-date')) === '2026-10-11' && (await days().last().getAttribute('data-date')) === '2026-10-17', 'previous week: Sun 11 -> Sat 17');
for (let i = 0; i < 2; i++) await p.getByRole('button', { name: 'שבוע קודם' }).click();
await p.waitForSelector('.day[data-date="2026-09-27"]');
ok(await planStatus('2026-10-02', 'Lower B') === 'missed' && (await dayCard('2026-10-02').locator('.chip').innerText()) === 'פוספס', 'a skipped scheduled day shows פוספס (Fri 2 Oct)');
await p.getByRole('button', { name: 'השבוע', exact: true }).click();
await p.waitForSelector('.day[data-date="2026-10-18"]');
ok(await p.locator('.day[aria-current="date"]').count() === 1, '"השבוע" shortcut returns to the current week');
await p.getByRole('button', { name: 'שבוע הבא' }).click();
await p.waitForSelector('.day[data-date="2026-10-25"]');
ok(await planStatus('2026-10-26', 'Upper A') === 'upcoming' && await p.locator('.day[aria-current="date"]').count() === 0, 'next week: Sun 25 -> Sat 31, Upper A upcoming on Monday');
await p.getByRole('button', { name: 'השבוע', exact: true }).click();
await p.waitForSelector('.day[data-date="2026-10-18"]');
// a past day opens its workout in History
await dayCard('2026-10-19').locator('.day-link').click();
await p.waitForSelector('.hist-detail-col .ex');
ok(p.url().endsWith('#/history/2026-10-19'), 'tapping a past day opens that workout');
await p.goBack();
await p.waitForSelector('.week-days > .day');
// a future day links to its plan
await dayCard('2026-10-22').getByRole('link', { name: 'Upper B' }).click();
await p.waitForSelector('.plan-detail');
ok(p.url().endsWith('#/plans/upper-b'), 'a future day links to the planned plan');
await p.goBack();
await p.waitForSelector('.week-days > .day');
// open today's workout
await p.getByRole('button', { name: 'פתח את האימון של היום' }).click();
await p.waitForSelector('.ex');
ok(p.url().endsWith('#/workout/2026-10-21'), "today's workout opens at #/workout/2026-10-21");

// 1. inline edit: bench set 3 reps -> 6 via typing; blur commits
const s3 = row('2026-10-21-bench-2');
const reps = s3.locator('.cell-reps');
await reps.fill('9');
ok(await s3.locator('.note-muted').count() === 1, 'dirty draft shows unsaved note');
ok(await p.locator('.status-warn').count() === 1, 'header shows unsaved changes');
await shot('f1-unsaved');
await reps.press('Enter');
await p.waitForTimeout(100);
ok((await reps.inputValue()) === '9', 'reps committed 9');
// validation
await reps.fill('7.5');
await reps.press('Enter');
ok(await s3.locator('.note-danger').count() === 1 && (await reps.getAttribute('aria-invalid')) === 'true', 'invalid reps flagged');
await shot('f2-invalid');
await reps.press('Escape');
// 2. complete with ghost values
await s3.locator('.check').click();
ok((await s3.locator('.check').getAttribute('aria-pressed')) === 'true', 'set marked complete');
ok(await p.locator('.rest').count() === 1, 'rest timer appears');
await shot('f3-done-rest');
await p.locator('.rest-x').click();
// 3. missing weight: facepull set can't complete
const fp = row(await p.locator('[data-we$="facepull"] .set').first().getAttribute('data-set-id'));
await fp.locator('.check').click();
ok(await fp.locator('.note-danger, .note-warn').count() >= 1, 'missing weight blocks completion');
ok((await fp.locator('.check').getAttribute('aria-pressed')) === 'false', 'still not complete');
// weight 0 is distinct
await fp.locator('.cell-weight').fill('0');
await fp.locator('.cell-weight').press('Enter');
await fp.locator('.check').click();
ok((await fp.locator('.check').getAttribute('aria-pressed')) === 'true', 'zero weight accepted as a real value');
// 4. external update while editing elsewhere: focus must not move
await row('2026-10-21-bench-1').locator('.cell-reps').focus();
await p.locator('button[aria-label^="כלי הדגמה"]').click();
await p.getByRole('button', { name: 'רישום סט חדש' }).click();
await p.waitForTimeout(900);
await shot('f4-external');
ok(await p.locator('.toast-assistant').count() === 1, 'assistant toast shown');
ok(await p.locator('.set.is-fresh').count() >= 1, 'externally updated row highlighted');
// 5. conflict
await p.locator('button[aria-label^="כלי הדגמה"]').click();
await p.getByRole('button', { name: 'התנגשות עם עריכה שלי' }).click();
await p.waitForSelector('.conflict', { timeout: 5000 });
await shot('f5-conflict');
ok(await p.locator('.conflict').count() === 1, 'conflict resolution shown');
const before = await row('2026-10-21-bench-1').locator('.cell-reps').inputValue();
await p.getByRole('button', { name: /קבל את העדכון/ }).click();
await p.waitForTimeout(200);
ok(await p.locator('.conflict').count() === 0, 'conflict resolved');
console.log('  reps before/after:', before, await row('2026-10-21-bench-1').locator('.cell-reps').inputValue());
// 6. failed save
await p.locator('button[aria-label^="כלי הדגמה"]').click();
await p.getByLabel('השמירה הבאה תיכשל').check();
await p.keyboard.press('Escape');
await row('2026-10-21-squat-0').locator('.check').click();
await p.waitForTimeout(800);
await shot('f6-failed');
ok(await p.locator('.status-danger').count() === 1, 'failed save surfaced');
await p.locator('.status-danger').click();
await p.waitForTimeout(700);
ok(await p.locator('.status-danger').count() === 0, 'retry succeeds');
// 7. scenarios
await p.locator('button[aria-label^="כלי הדגמה"]').click();
await p.getByRole('radio', { name: 'ללא אימון' }).click();
await p.keyboard.press('Escape');
await p.waitForTimeout(300);
await shot('f7-empty');
ok(await p.locator('.today-empty').count() === 1, 'empty state');
// back to the week: nothing logged today, so the card offers to start the plan still due this week
await p.getByRole('button', { name: 'חזרה לשבוע' }).click();
await p.waitForSelector('.week-days > .day');
const startBtn = dayCard('2026-10-21').getByRole('button', { name: /התחל אימון/ });
ok(await startBtn.count() === 1 && (await startBtn.innerText()).includes('Full Body'), 'back to week; today offers "התחל אימון · Full Body" (frequency plan still due)');
await shot('w2-start');
await startBtn.click();
await p.waitForSelector('.ex');
ok(p.url().endsWith('#/workout/2026-10-21') && (await p.locator('.routine').innerText()).includes('Full Body'), 'starting from the week creates today\'s workout from that plan and opens it');
const firstRow = p.locator('section.ex .set').first();
await firstRow.locator('.cell-weight').fill('50');
await firstRow.locator('.cell-reps').fill('5');
await firstRow.locator('.check').click();
ok((await firstRow.locator('.check').getAttribute('aria-pressed')) === 'true', 'logging a set in the opened workout works');
if (await p.locator('.rest-x').count()) await p.locator('.rest-x').click();
await p.getByRole('button', { name: 'חזרה לשבוע' }).click();
await p.waitForSelector('.week-days > .day');
const tStats = await dayCard('2026-10-21').locator('.day-stats').innerText();
ok(tStats.startsWith('סט אחד'), `the week reflects the logged set (${tStats.replace(/\n/g, ' ')})`);
await p.locator('button[aria-label^="כלי הדגמה"]').click();
await p.getByRole('radio', { name: 'הושלם' }).click();
await p.keyboard.press('Escape');
await p.waitForTimeout(300);
await shot('f8-completed');
ok(await p.locator('.summary .chip-accent').count() === 1, 'completed chip');
// 8. loading + load error
await p.locator('button[aria-label^="כלי הדגמה"]').click();
await p.getByRole('button', { name: 'טעינה איטית' }).click();
await p.waitForTimeout(500);
await shot('f9-loading');
ok(await p.locator('.skeleton').count() > 0, 'skeleton while loading');
await p.waitForSelector('.ex', { timeout: 5000 });
await p.locator('button[aria-label^="כלי הדגמה"]').click();
await p.getByRole('button', { name: 'כשל טעינה' }).click();
await p.waitForSelector('.empty');
await shot('f10-loaderror');
await p.getByRole('button', { name: 'נסה שוב' }).click();
await p.waitForSelector('.ex');
// 9. exercise detail + chart range
await p.goto('http://127.0.0.1:5173/#/exercise/squat');
await p.reload();
await p.waitForSelector('.chart svg');
await p.getByRole('radio', { name: '4 שבועות' }).click();
const pts4 = await p.locator('.chart-block svg circle.pt').count();
await p.getByRole('radio', { name: '8 שבועות' }).click();
const pts8 = await p.locator('.chart-block svg circle.pt').count();
ok(pts4 < pts8, `range changes chart points (${pts4} < ${pts8})`);
await p.getByRole('radio', { name: '1RM משוער' }).click();
ok((await p.locator('.chart-note').innerText()).includes('הערכה'), 'e1RM labelled as estimate');
await p.goto('http://127.0.0.1:5173/#/exercise/lateral');
await p.reload();
await p.waitForSelector('.chart svg');
ok(await p.locator('.chart-block svg path.missing').count() >= 1, 'missing-weight session drawn as gap marker, not zero');
await shot('f11-gap');
// 10. import
await p.goto('http://127.0.0.1:5173/#/plans');
await p.reload();
await p.getByRole('button', { name: 'ייבוא JSON' }).click();
await p.getByRole('button', { name: 'דוגמה עם שגיאות' }).click();
await p.waitForSelector('.issues-error');
await shot('f12-import-invalid');
ok(await p.locator('.issues-error li').count() >= 3, 'validation errors listed');
await p.getByRole('button', { name: 'בחירה מחדש' }).click();
await p.getByRole('button', { name: 'קובץ שבור' }).click();
ok((await p.locator('.issues-error').innerText()).includes('JSON'), 'broken JSON reported');
await p.getByRole('button', { name: 'בחירה מחדש' }).click();
await p.getByRole('button', { name: 'דוגמה תקינה' }).click();
await p.waitForSelector('.preview-table');
await shot('f13-import-preview');
await p.getByRole('button', { name: 'ייבא תוכנית' }).click();
await p.waitForSelector('.plan-detail');
await shot('f14-imported');
ok((await p.locator('.plan-title-input').inputValue()) === 'Push Day', 'imported plan opened');
// 11. plan schedule editor drives the week
await p.goto('http://127.0.0.1:5173/#/plans/upper-b');
await p.reload();
await p.waitForSelector('.sched');
await p.getByRole('button', { name: 'שבת', exact: true }).click();
ok((await p.getByRole('button', { name: 'שבת', exact: true }).getAttribute('aria-pressed')) === 'true', 'schedule editor: Saturday added to Upper B');
await p.locator('.nav-item', { hasText: 'השבוע' }).click();
await p.waitForSelector('.week-days > .day');
ok(await planStatus('2026-10-24', 'Upper B') === 'upcoming' && await planStatus('2026-10-22', 'Upper B') === 'upcoming', 'the week shows Upper B on Thursday and Saturday');
console.log('errors:', errs.length ? errs : 'none');
await b.close();
