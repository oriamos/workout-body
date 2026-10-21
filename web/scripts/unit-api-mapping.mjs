// Unit tests for src/data/apiMapping.ts (engine JSON <-> UI types). No server, no browser.
// Run: node scripts/unit-api-mapping.mjs
import { build } from 'esbuild';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';

const entry = fileURLToPath(new URL('../src/data/apiMapping.ts', import.meta.url));
const out = await build({ entryPoints: [entry], bundle: true, format: 'esm', platform: 'node', write: false, logLevel: 'error' });
const M = await import('data:text/javascript;base64,' + Buffer.from(out.outputFiles[0].text).toString('base64'));

let failed = 0;
const t = (name, fn) => {
  try { fn(); console.log('PASS', name); } catch (e) { failed++; console.log('FAIL', name, '\n ', e.message); }
};

t('titleCase / normName', () => {
  assert.equal(M.titleCase('bench press'), 'Bench Press');
  assert.equal(M.titleCase('t-bar row (machine)'), 'T-Bar Row (Machine)');
  assert.equal(M.normName('  Bench   PRESS '), 'bench press');
});

t('muscle mapping (Spanish upstream taxonomy)', () => {
  assert.equal(M.mapMuscle('pecho'), 'chest');
  assert.equal(M.mapMuscle('trapecio'), 'back');
  assert.equal(M.mapMuscle('hombro posterior'), 'shoulders');
  assert.equal(M.mapMuscle('gluteo'), 'posterior');
  assert.equal(M.mapMuscle('antebrazo'), 'arms');
  assert.equal(M.mapMuscle('gemelo'), 'calves');
  assert.equal(M.mapMuscle('abdomen'), 'other');
  assert.equal(M.mapMuscle(undefined), 'other');
});

t('exercise: engine group, UI override, built-in defaults', () => {
  const e = M.toClientExercise({ id: 7, name: 'bench press', aliases: [], set_count: 3 }, undefined);
  assert.equal(e.id, '7');
  assert.equal(e.name, 'Bench Press');
  assert.equal(e.muscle, 'chest'); // from the prototype's known exercises
  assert.equal(e.restSec, 150);
  assert.ok(e.howTo && e.howTo.steps.length > 0);
  assert.equal(M.toClientExercise({ id: 7, name: 'bench press', aliases: [], muscle_group: 'triceps', set_count: 0 }, undefined).muscle, 'arms');
  assert.equal(M.toClientExercise({ id: 7, name: 'bench press', aliases: [], muscle_group: 'triceps', set_count: 0 }, { muscle: 'back', restSec: 30 }).muscle, 'back');
  const u = M.toClientExercise({ id: 9, name: 'zercher carry', aliases: [], set_count: 0 }, undefined);
  assert.deepEqual([u.muscle, u.equipment, u.restSec, u.howTo], ['other', '', 120, undefined]);
  assert.deepEqual(M.howToFromGuide('  Keep the bar close.  '), { summary: 'Keep the bar close.', steps: [], cues: [], mistakes: [] });
  assert.equal(M.howToFromGuide(''), undefined);
});

t('set input omits unknown metrics; patch clears with null; backoff is not resent', () => {
  assert.deepEqual(M.setInput({ exercise_id: 1 }, { weight: 0, reps: 8, rir: null, rpe: null, type: 'working' }), { exercise_id: 1, weight_kg: 0, reps: 8 });
  assert.deepEqual(M.setInput({ exercise: 'squat' }, { weight: null, reps: 5, rir: 2, rpe: 8, type: 'warmup' }), { exercise: 'squat', reps: 5, rir: 2, rpe: 8, set_type: 'warmup' });
  assert.deepEqual(M.setPatchBody({ weight: null, by: 'me' }, 'working'), { weight_kg: null });
  assert.deepEqual(M.setPatchBody({ type: 'working' }, 'backoff'), {});
  assert.deepEqual(M.setPatchBody({ type: 'drop' }, 'backoff'), { set_type: 'drop' });
  assert.equal(M.toSetType('backoff'), 'working');
});

t('plan document omits null/0 targets and keeps schedule + item notes', () => {
  const plan = { id: 'p', name: 'Upper', exercises: [
    { id: 'a', exerciseId: '1', sets: 3, repMin: 6, repMax: 8, weight: null, rir: 0, rpe: null },
    { id: 'b', exerciseId: '2', sets: 2, repMin: 0, repMax: 0, weight: 40, rir: null, rpe: 8 },
  ] };
  const doc = M.planDocument(plan, (id) => (id === '1' ? 'bench press' : 'face pull'), { schedule: { type: 'weekdays', days: ['mon'] }, items: [{ position: 1, exercise: 'Bench Press', target_sets: 3, notes: 'pause' }] });
  assert.deepEqual(doc, { format: 'workout-plan', version: 1, name: 'Upper', schedule: { type: 'weekdays', days: ['mon'] }, items: [
    { exercise: 'bench press', sets: 3, rep_min: 6, rep_max: 8, target_rir: 0, notes: 'pause' },
    { exercise: 'face pull', sets: 2, target_weight_kg: 40, target_rpe: 8 },
  ] });
  assert.equal('schedule' in M.planDocument(plan, String, { schedule: null }), false);
});

const sw = {
  id: 5, date: '2026-10-22', plan_id: 2, plan_name: 'Upper A', revision: 9,
  progress: [
    { exercise: 'bench press', target_sets: 3, done_sets: 1, rep_min: 6, rep_max: 8, target_rpe: 8, notes: 'pause' },
    { exercise: 'barbell row', target_sets: 2, done_sets: 0, rep_min: 8, rep_max: 10, skipped: true },
    { exercise: 'face pull', target_sets: 0, done_sets: 1 },
  ],
  sets: [
    { id: 12, session_id: 5, position: 3, exercise_id: 3, exercise: 'face pull', weight_kg: null, reps: 15, rpe: null, rir: null, set_type: 'working', revision: 1 },
    { id: 10, session_id: 5, position: 1, exercise_id: 1, exercise: 'bench press', weight_kg: 40, reps: 8, rpe: null, rir: null, set_type: 'warmup', revision: 1 },
    { id: 11, session_id: 5, position: 2, exercise_id: 1, exercise: 'bench press', weight_kg: 60, reps: 8, rpe: null, rir: null, set_type: 'backoff', revision: 2 },
  ],
};
const ids = { 'bench press': '1', 'barbell row': '2', 'face pull': '3', 'lateral raise': '4' };
const exId = (n) => ids[M.normName(n)];

t('workout: progress order, sets, drafts for remaining planned sets', () => {
  const w = M.toClientWorkout(sw, exId, { pending: ['lateral raise'], notes: { 'face pull': 'rope' }, startedAt: '18:00' });
  assert.equal(w.id, '5');
  assert.equal(w.planId, '2');
  assert.equal(w.name, 'Upper A');
  assert.equal(w.startedAt, '18:00');
  assert.deepEqual(w.exercises.map((e) => e.id), ['5:1', '5:2', '5:3', '5:4']); // pending (UI-only) exercise last
  assert.deepEqual(w.exercises[3].sets.map((s) => [s.id, s.done]), [['draft:5:4:1', false]]);
  const bench = w.exercises[0];
  assert.deepEqual(bench.planned, { sets: 3, repMin: 6, repMax: 8, weight: null, rir: null, rpe: 8 });
  assert.equal(bench.note, 'pause');
  assert.deepEqual(bench.sets.map((s) => [s.id, s.type, s.done]), [['10', 'warmup', true], ['11', 'working', true], ['draft:5:1:2', 'working', false], ['draft:5:1:3', 'working', false]]);
  const row = w.exercises[1];
  assert.deepEqual(row.sets.map((s) => s.id), ['draft:5:2:1']); // skipped: no planned drafts, one empty row
  const fp = w.exercises[2];
  assert.equal(fp.planned, null);
  assert.equal(fp.note, 'rope');
  assert.deepEqual([fp.sets[0].weight, fp.sets[0].reps], [null, 15]);
  assert.equal(M.toClientWorkout({ ...sw, plan_id: null, plan_name: null }, exId).name, 'אימון חופשי');
  assert.equal(M.toClientWorkout(sw, exId).startedAt, '');
});

t('diff: added, changed fields only, removed, new prescription, note', () => {
  const old = M.snapshotOf(sw);
  const next = {
    ...sw, revision: 12,
    sets: [
      { ...sw.sets[1] },
      { ...sw.sets[2], reps: 6, revision: 3 },
      { id: 13, session_id: 5, position: 3, exercise_id: 1, exercise: 'bench press', weight_kg: 60, reps: 6, rpe: null, rir: null, set_type: 'working', revision: 1 },
    ],
    progress: [{ ...sw.progress[0], notes: 'slow' }, sw.progress[1], { exercise: 'squat', target_sets: 3, done_sets: 0, rep_min: 5, rep_max: 5 }],
  };
  const d = M.diffWorkout(old, next);
  assert.deepEqual(d.added.map((s) => s.id), [13]);
  assert.deepEqual(d.changed.map((c) => [c.set.id, c.patch]), [[11, { reps: 6 }]]);
  assert.deepEqual(d.removed.map((s) => s.id), [12]);
  assert.deepEqual(d.newPrescriptions.map((p) => p.exercise), ['squat']);
  assert.deepEqual(d.notes, [{ exercise: 'bench press', note: 'slow' }]);
  assert.equal(M.workingNumber(next, next.sets[2]), 2);
  assert.equal(M.setSummary('Bench Press', 2, next.sets[2]), '⁦Bench Press⁩ · סט 2 · ⁦60 × 6⁩');
});

console.log(failed ? `\n${failed} failed` : '\nall passed');
process.exit(failed ? 1 : 0);
