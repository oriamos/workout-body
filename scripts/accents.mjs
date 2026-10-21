import { chromium } from 'playwright-core';
import path from 'node:path';
const out = process.env.SHOTS ?? '/tmp/shots';
const opts = {
  indigo: '--accent: oklch(0.58 0.2 277); --accent-ink: oklch(0.99 0 0); --accent-soft: oklch(0.93 0.045 277 / 0.9); --accent-text: oklch(0.44 0.19 277); --chart-1: oklch(0.55 0.2 277); --wallpaper: radial-gradient(55% 42% at 88% -4%, oklch(0.9 0.08 285 / 0.75), transparent 70%), radial-gradient(50% 40% at -4% 30%, oklch(0.92 0.06 215 / 0.7), transparent 70%), radial-gradient(60% 45% at 80% 105%, oklch(0.93 0.06 20 / 0.6), transparent 70%);',
  coral: '--accent: oklch(0.66 0.19 38); --accent-ink: oklch(0.99 0 0); --accent-soft: oklch(0.94 0.04 45 / 0.9); --accent-text: oklch(0.5 0.17 35); --chart-1: oklch(0.62 0.19 38); --wallpaper: radial-gradient(55% 42% at 88% -4%, oklch(0.93 0.07 50 / 0.75), transparent 70%), radial-gradient(50% 40% at -4% 30%, oklch(0.92 0.05 240 / 0.7), transparent 70%), radial-gradient(60% 45% at 80% 105%, oklch(0.93 0.05 330 / 0.6), transparent 70%);',
  graphite: '--accent: oklch(0.27 0.02 260); --accent-ink: oklch(0.99 0 0); --accent-soft: oklch(0.3 0.03 260 / 0.1); --accent-text: oklch(0.27 0.02 260); --chart-1: oklch(0.34 0.03 260); --wallpaper: radial-gradient(55% 42% at 88% -4%, oklch(0.93 0.03 250 / 0.8), transparent 70%), radial-gradient(50% 40% at -4% 30%, oklch(0.92 0.04 235 / 0.7), transparent 70%), radial-gradient(60% 45% at 80% 105%, oklch(0.94 0.03 60 / 0.6), transparent 70%);',
};
const b = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium', args: ['--no-sandbox'] });
for (const [n, css] of Object.entries(opts)) {
  const p = await (await b.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, hasTouch: true })).newPage();
  await p.goto('http://127.0.0.1:5173/#/today'); await p.waitForSelector('.wv');
  await p.addStyleTag({ content: `:root, :root[data-theme='light'] { ${css} }` });
  await p.evaluate(() => window.scrollTo(0, 330));
  await p.waitForTimeout(300);
  await p.screenshot({ path: path.join(out, `A-${n}.png`) });
}
await b.close();
