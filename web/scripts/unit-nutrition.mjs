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

const base = { id: 1, date: '2026-10-22', name: 'Oats', calories_kcal: 300, protein_g: 10, carbs_g: null, fat_g: 5, items: [], incomplete_values: [], copied_from: null, revision: 2, created_at: '', updated_at: '' };
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
  assert.equal(L.consumedText(d.carbs_g, d.entries, 'carbs_g'), '—'); // all entries lack it: unknown, not 0
  assert.equal(L.consumedText(d.protein_g, d.entries, 'protein_g'), '15');
  assert.equal(L.consumedText(L.summarize(0, 0, null), [], 'protein_g'), '0');
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

const it = (name, amount_g, c, p, cb, f) => ({ name, amount_g, calories_kcal: c, protein_g: p, carbs_g: cb, fat_g: f });

t('sumItems: sum of provided, null when none provides, incomplete + missing counts', () => {
  const r = L.sumItems([it('a', 100, 165, 31, null, 3.6), it('b', null, 130.333, 2.7, 28, null), it('c', 50, 10, null, null, null)]);
  assert.deepEqual(r.sum, { calories_kcal: 305.33, protein_g: 33.7, carbs_g: 28, fat_g: 3.6 });
  assert.deepEqual(r.incomplete, ['protein_g', 'carbs_g', 'fat_g']);
  assert.deepEqual(r.missing, { calories_kcal: 0, protein_g: 1, carbs_g: 2, fat_g: 2 });
  const none = L.sumItems([it('a', null, null, null, null, null)]);
  assert.deepEqual(none.sum, { calories_kcal: null, protein_g: null, carbs_g: null, fat_g: null });
  assert.deepEqual(none.incomplete, []); // none provides it: unknown, not partial
  assert.deepEqual(L.sumItems([it('z', null, 0, 0, null, null)]).sum.calories_kcal, 0); // a real 0 stays 0
  assert.equal(L.partialNote(0), null);
  assert.equal(L.partialNote(1), 'חלקי: חסר במרכיב אחד');
  assert.equal(L.partialNote(3), 'חלקי: חסר ב־3 מרכיבים');
});

t('scaleValues: proportional, rounded (whole kcal, 0.1 g), unknown stays unknown', () => {
  const b = { calories_kcal: 165, protein_g: 31, carbs_g: null, fat_g: 3.6 };
  assert.deepEqual(L.scaleValues(b, 100, 150), { values: { calories_kcal: 248, protein_g: 46.5, carbs_g: null, fat_g: 5.4 }, scaled: true });
  assert.deepEqual(L.scaleValues(b, 100, 33), { values: { calories_kcal: 54, protein_g: 10.2, carbs_g: null, fat_g: 1.2 }, scaled: true });
  assert.deepEqual(L.scaleValues(b, 100, 100), { values: b, scaled: false });
  assert.deepEqual(L.scaleValues(b, null, 150), { values: b, scaled: false });
  assert.deepEqual(L.scaleValues(b, 100, null), { values: b, scaled: false });
});

t('parseGrams: optional, > 0, at most 5,000', () => {
  assert.deepEqual(L.parseGrams(''), { ok: true, value: null });
  assert.deepEqual(L.parseGrams('150,5'), { ok: true, value: 150.5 });
  assert.equal(L.parseGrams('0').ok, false);
  assert.equal(L.parseGrams('-3').ok, false);
  assert.match(L.parseGrams('5001').msg, /5,000/);
  assert.equal(L.parseGrams('x').ok, false);
});

t('picking a suggestion fills the row; a typed amount scales; changing the amount rescales from the pick', () => {
  const sug = { ...it('חזה עוף', 100, 165, 31, null, 3.6), last_used: '2026-10-20', uses: 2 };
  const fresh = L.pickSuggestion(L.emptyRow(), sug);
  assert.equal(fresh.name, 'חזה עוף');
  assert.equal(fresh.amount_g, '100');
  assert.deepEqual([fresh.calories_kcal, fresh.protein_g, fresh.carbs_g, fresh.fat_g], ['165', '31', '', '3.6']);
  assert.equal(fresh.scaled, false);
  const typedFirst = L.pickSuggestion({ ...L.emptyRow(), amount_g: '150' }, sug);
  assert.deepEqual([typedFirst.amount_g, typedFirst.calories_kcal, typedFirst.protein_g, typedFirst.fat_g, typedFirst.scaled], ['150', '248', '46.5', '5.4', true]);
  const re = L.setRowAmount(typedFirst, '200');
  assert.deepEqual([re.calories_kcal, re.protein_g, re.carbs_g, re.fat_g, re.scaled], ['330', '62', '', '7.2', true]);
  const back = L.setRowAmount(re, '100');
  assert.deepEqual([back.calories_kcal, back.scaled], ['165', false]);
  const bad = L.setRowAmount(re, '2x'); // not a number yet: values stay as they were
  assert.equal(bad.calories_kcal, '330');
  const manual = L.setRowField(re, 'calories_kcal', '300'); // typed by hand: stops following the pick
  assert.equal(manual.base, null);
  assert.equal(L.setRowAmount(manual, '50').calories_kcal, '300');
  const noAmount = L.pickSuggestion({ ...L.emptyRow(), amount_g: '80' }, { ...sug, amount_g: null });
  assert.deepEqual([noAmount.amount_g, noAmount.calories_kcal, noAmount.scaled], ['80', '165', false]);
});

t('live totals and item validation (blank rows ignored, empty = null, errors by row)', () => {
  const rows = [L.rowFrom(it('אורז', 180, 234, 4.3, 51, null)), L.emptyRow(), { ...L.emptyRow(), name: ' סלט  ירקות ', calories_kcal: '30', fat_g: 'abc' }];
  const t = L.liveTotals(rows);
  assert.equal(t.count, 2);
  assert.deepEqual(t.sum, { calories_kcal: 264, protein_g: 4.3, carbs_g: 51, fat_g: null });
  assert.deepEqual(t.incomplete, ['protein_g', 'carbs_g']);
  const v = L.validateItems(rows);
  assert.equal(v.items.length, 2);
  assert.deepEqual(Object.keys(v.errors).map(Number), [rows[2].id]);
  assert.ok(v.errors[rows[2].id].fat_g);
  const ok = L.validateItems([rows[0], { ...rows[2], fat_g: '' }]);
  assert.deepEqual(ok.errors, {});
  assert.deepEqual(ok.items[1], { name: 'סלט ירקות', amount_g: null, calories_kcal: 30, protein_g: null, carbs_g: null, fat_g: null });
  assert.ok(L.validateItems([L.emptyRow()]).listError);
  assert.ok(L.validateItems([{ ...L.emptyRow(), calories_kcal: '5' }]).errors);
  assert.equal(Object.values(L.validateItems([{ ...L.emptyRow(), calories_kcal: '5' }]).errors)[0].name, 'חסר שם למרכיב');
});

t('patches for meals with ingredients: list only when changed; converting to simple sends items: []', () => {
  const items = [it('a', 100, 100, 10, null, 1), it('b', null, 50, null, null, 2)];
  const meal = { ...base, name: 'Bowl', calories_kcal: 150, protein_g: 10, carbs_g: null, fat_g: 3, items, incomplete_values: ['protein_g'] };
  assert.deepEqual(L.itemsPatch(meal, 'Bowl', items.map((x) => ({ ...x }))), {});
  const changed = [items[0], { ...items[1], calories_kcal: 60 }];
  assert.deepEqual(L.itemsPatch(meal, 'Bowl 2', changed), { name: 'Bowl 2', items: changed });
  assert.deepEqual(L.entryPatch(meal, 'Bowl', { calories_kcal: 150, protein_g: 10, carbs_g: null, fat_g: 3 }), { items: [] });
  assert.deepEqual(L.entryPatch(meal, 'Bowl', { calories_kcal: 170, protein_g: 10, carbs_g: null, fat_g: 3 }), { items: [], calories_kcal: 170 });
  assert.deepEqual(L.itemsPatch(base, 'Oats', [it('Oats', 40, 150, 5, 27, 3)]), { items: [it('Oats', 40, 150, 5, 27, 3)] }); // breaking a simple meal down
});

t('day totals: a partial meal counts its known part and is flagged missing; consumed not "—"', () => {
  const targets = { calories_kcal: null, protein_g: null, carbs_g: null, fat_g: null, revision: 0 };
  const partial = { ...base, id: 5, calories_kcal: 300, protein_g: 20, carbs_g: null, fat_g: 4, items: [it('x', null, 200, 20, null, 4), it('y', null, 100, null, null, null)], incomplete_values: ['protein_g', 'fat_g'] };
  const d = L.buildDay('2026-10-22', '2026-10-22', [partial], targets);
  assert.deepEqual([d.calories_kcal.entries_missing_value, d.protein_g.entries_missing_value, d.carbs_g.entries_missing_value, d.fat_g.entries_missing_value], [0, 1, 1, 1]);
  assert.equal(d.protein_g.consumed, 20);
  assert.equal(L.consumedText(d.protein_g, d.entries, 'protein_g'), '20'); // partial, not unknown
  assert.equal(L.consumedText(d.carbs_g, d.entries, 'carbs_g'), '—');
});

console.log(failed ? `\n${failed} failed` : '\nall passed');
process.exit(failed ? 1 : 0);
