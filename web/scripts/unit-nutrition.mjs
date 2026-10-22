// Unit tests for src/nutrition/logic.ts (form parsing, patches, day totals, display text).
// Run: node scripts/unit-nutrition.mjs
import { build } from 'esbuild';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';

const entry = fileURLToPath(new URL('../src/nutrition/logic.ts', import.meta.url));
const out = await build({ entryPoints: [entry], bundle: true, format: 'esm', platform: 'node', write: false, logLevel: 'error' });
const L = await import('data:text/javascript;base64,' + Buffer.from(out.outputFiles[0].text).toString('base64'));

let failed = 0;
const t = (name, fn) => {
  try { fn(); console.log('PASS', name); } catch (e) { failed++; console.log('FAIL', name, '\n ', e.message); }
};

t('parseAmount: empty is not provided, decimals, comma, limits', () => {
  assert.deepEqual(L.parseAmount('', 'protein_g'), { ok: true, value: null });
  assert.deepEqual(L.parseAmount('  ', 'protein_g'), { ok: true, value: null });
  assert.deepEqual(L.parseAmount('0', 'protein_g'), { ok: true, value: 0 });
  assert.deepEqual(L.parseAmount('12.5', 'fat_g'), { ok: true, value: 12.5 });
  assert.deepEqual(L.parseAmount('12,5', 'fat_g'), { ok: true, value: 12.5 });
  assert.equal(L.parseAmount('abc', 'fat_g').ok, false);
  assert.equal(L.parseAmount('1e3', 'fat_g').ok, false);
  assert.match(L.parseAmount('-1', 'carbs_g').msg, /שלילי/);
  assert.equal(L.parseAmount('20000', 'calories_kcal').ok, true);
  assert.match(L.parseAmount('20000.5', 'calories_kcal').msg, /20,000/);
  assert.match(L.parseAmount('2001', 'protein_g').msg, /2,000/);
});

t('entry form validation keeps blanks as null and requires a name', () => {
  const r = L.validateEntryForm({ name: '  טוסט   גבינה ', calories_kcal: '350', protein_g: '', carbs_g: '30.5', fat_g: '' });
  assert.equal(r.name, 'טוסט גבינה');
  assert.deepEqual(r.values, { calories_kcal: 350, protein_g: null, carbs_g: 30.5, fat_g: null });
  assert.deepEqual(r.errors, {});
  const bad = L.validateEntryForm({ name: '', calories_kcal: 'x', protein_g: '-2', carbs_g: '', fat_g: '3000' });
  assert.deepEqual(Object.keys(bad.errors).sort(), ['calories_kcal', 'fat_g', 'name', 'protein_g']);
});

const base = { id: 1, date: '2026-10-22', name: 'Oats', calories_kcal: 300, protein_g: 10, carbs_g: null, fat_g: 5, copied_from: null, revision: 2, created_at: '', updated_at: '' };
t('entry patch: only changes, null clears', () => {
  assert.deepEqual(L.entryPatch(base, 'Oats', { calories_kcal: 300, protein_g: 10, carbs_g: null, fat_g: 5 }), {});
  assert.deepEqual(L.entryPatch(base, 'Oats + milk', { calories_kcal: 420, protein_g: null, carbs_g: 50, fat_g: 5 }), { name: 'Oats + milk', calories_kcal: 420, protein_g: null, carbs_g: 50 });
});

t('target patch: changed values only, blank removes a target', () => {
  const cur = { effective_from: '2026-10-01', calories_kcal: 2200, protein_g: 150, carbs_g: null, fat_g: 70, revision: 4 };
  assert.deepEqual(L.targetPatch(cur, { calories_kcal: 2200, protein_g: 150, carbs_g: null, fat_g: 70 }), {});
  assert.deepEqual(L.targetPatch(cur, { calories_kcal: 2400, protein_g: 150, carbs_g: 250, fat_g: null }), { calories_kcal: 2400, carbs_g: 250, fat_g: null });
  assert.deepEqual(L.targetPatch({ calories_kcal: null, protein_g: null, carbs_g: null, fat_g: null, revision: 0 }, { calories_kcal: 2000, protein_g: null, carbs_g: null, fat_g: null }), { calories_kcal: 2000 });
});

t('day totals match the engine (missing counted, remaining/over, no target)', () => {
  const e = (id, c, p, cb, f) => ({ ...base, id, calories_kcal: c, protein_g: p, carbs_g: cb, fat_g: f });
  const targets = { effective_from: '2026-10-01', calories_kcal: 1000, protein_g: 20, carbs_g: null, fat_g: 0, revision: 1 };
  const d = L.buildDay('2026-10-22', '2026-10-22', [e(1, 600, 15, null, null), e(2, 500.25, null, null, null)], targets);
  assert.equal(d.today, true);
  assert.equal(d.entry_count, 2);
  assert.deepEqual(d.calories_kcal, { consumed: 1100.25, entries_missing_value: 0, target: 1000, remaining: 0, over: 100.25 });
  assert.deepEqual(d.protein_g, { consumed: 15, entries_missing_value: 1, target: 20, remaining: 5, over: 0 });
  assert.deepEqual(d.carbs_g, { consumed: 0, entries_missing_value: 2, target: null, remaining: null, over: null });
  assert.deepEqual(d.fat_g, { consumed: 0, entries_missing_value: 2, target: 0, remaining: 0, over: 0 });
  assert.equal(L.consumedText(d.carbs_g, 2), '—'); // all entries lack it: unknown, not 0
  assert.equal(L.consumedText(d.protein_g, 2), '15');
  assert.equal(L.consumedText(L.summarize(0, 0, null), 0), '0');
});

t('status text, missing note, progress', () => {
  assert.deepEqual(L.statusText(L.summarize(5, 0, null), 'protein_g'), { text: 'אין יעד', tone: 'muted' });
  assert.equal(L.statusText(L.summarize(1100, 0, 1000), 'calories_kcal').text, 'חריגה של 100 קק״ל');
  assert.equal(L.statusText(L.summarize(15, 0, 20), 'protein_g').text, 'נותרו 5 ג׳');
  assert.equal(L.statusText(L.summarize(20, 0, 20), 'protein_g').text, 'היעד הושג');
  assert.equal(L.missingNote(0), null);
  assert.equal(L.missingNote(1), 'חסר בארוחה אחת');
  assert.equal(L.missingNote(2), 'חסר ב־2 ארוחות');
  assert.equal(L.progress(L.summarize(5, 0, null)), null);
  assert.equal(L.progress(L.summarize(5, 0, 10)), 0.5);
  assert.equal(L.progress(L.summarize(50, 0, 10)), 1);
  assert.equal(L.fmt(null), '—');
  assert.equal(L.fmt(0), '0');
});

t('targets in effect on a date (history applies until the next change)', () => {
  const h = [{ effective_from: '2026-10-01', calories_kcal: 2000, protein_g: null, carbs_g: null, fat_g: null, revision: 1 }, { effective_from: '2026-10-15', calories_kcal: 2500, protein_g: null, carbs_g: null, fat_g: null, revision: 3 }];
  assert.equal(L.targetsAt(h, '2026-09-30').revision, 0);
  assert.equal(L.targetsAt(h, '2026-10-14').calories_kcal, 2000);
  assert.equal(L.targetsAt(h, '2026-10-15').calories_kcal, 2500);
});

console.log(failed ? `\n${failed} failed` : '\nall passed');
process.exit(failed ? 1 : 0);
