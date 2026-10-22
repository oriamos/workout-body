// Unit tests for src/data/schedule.ts (week view rules) and the GET /schedule mapping. No server, no browser.
// Run: node scripts/unit-schedule.mjs
import { build } from 'esbuild';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';

async function load(rel) {
  const entry = fileURLToPath(new URL(rel, import.meta.url));
  const out = await build({ entryPoints: [entry], bundle: true, format: 'esm', platform: 'node', write: false, logLevel: 'error' });
  return import('data:text/javascript;base64,' + Buffer.from(out.outputFiles[0].text).toString('base64'));
}
const S = await load('../src/data/schedule.ts');
const M = await load('../src/data/apiMapping.ts');
const { buildSeed } = await load('../src/data/seed.ts');

let failed = 0;
const t = (name, fn) => {
  try { fn(); console.log('PASS', name); } catch (e) { failed++; console.log('FAIL', name, '\n ', e.message); }
};

t('weekday and week start (Sunday default, configurable)', () => {
  assert.equal(S.weekdayOf('2026-10-21'), 'wed');
  assert.equal(S.startOfWeek('2026-10-21'), '2026-10-18'); // Sunday
  assert.equal(S.startOfWeek('2026-10-18'), '2026-10-18');
  assert.equal(S.startOfWeek('2026-10-24'), '2026-10-18'); // Saturday ends the week
  assert.equal(S.startOfWeek('2026-10-21', 'mon'), '2026-10-19');
  assert.equal(S.startOfWeek('2026-10-25', 'mon'), '2026-10-19');
  assert.deepEqual(S.weekOrder(), ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat']);
  assert.deepEqual(S.weekOrder('mon'), ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']);
});

const set = (done, type = 'working') => ({ id: Math.random().toString(36), type, weight: 50, reps: 5, rir: null, rpe: null, done });
const wk = (date, planId, sets) => ({ id: `w-${date}`, date, planId, name: planId ?? 'free', startedAt: '', exercises: [{ id: `${date}-e`, exerciseId: 'x', planned: null, note: '', sets }] });
const data = {
  today: '2026-10-21', // Wednesday
  plans: [
    { id: 'A', name: 'Upper', exercises: [], schedule: { type: 'weekdays', days: ['mon', 'wed', 'fri'] } },
    { id: 'B', name: 'Lower', exercises: [], schedule: { type: 'weekdays', days: ['tue', 'sat'] } },
    { id: 'C', name: 'Cardio', exercises: [], schedule: { type: 'times_per_week', times_per_week: 3 } },
    { id: 'D', name: 'Free', exercises: [] },
  ],
  workouts: [
    wk('2026-10-19', 'A', [set(true)]), // Mon: done
    wk('2026-10-20', 'C', [set(true)]), // Tue: Lower scheduled, Cardio done instead
    wk('2026-10-21', 'A', [set(true, 'warmup'), set(false)]), // Wed (today): started, only a warm-up
    wk('2026-10-18', 'C', [set(true)]), // Sun: cardio
    wk('2026-10-11', 'C', [set(true)]), // previous week: does not count
  ],
};

t('computeSchedule: 7 days Sunday->Saturday with engine statuses', () => {
  const v = S.computeSchedule(data, '2026-10-18', '2026-10-24');
  assert.deepEqual(v.days.map((d) => d.weekday), ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat']);
  const st = (date) => v.days.find((d) => d.date === date).scheduled.map((s) => `${s.planId}:${s.status}`).join(',');
  assert.equal(st('2026-10-18'), '');
  assert.equal(st('2026-10-19'), 'A:done');
  assert.equal(st('2026-10-20'), 'B:missed'); // another plan's workout does not satisfy it
  assert.equal(st('2026-10-21'), 'A:started'); // warm-up only
  assert.equal(st('2026-10-23'), 'A:upcoming');
  assert.equal(st('2026-10-24'), 'B:upcoming');
  const tue = v.days.find((d) => d.date === '2026-10-20');
  assert.deepEqual(tue.workout, { id: 'w-2026-10-20', planId: 'C', planName: 'Cardio', workingSets: 1 });
  assert.equal(v.days.find((d) => d.date === '2026-10-21').today, true);
  assert.deepEqual(v.unscheduled, [{ planId: 'D', name: 'Free' }]);
});

t('computeSchedule: frequency target counts distinct done dates in the week', () => {
  const v = S.computeSchedule(data, '2026-10-18', '2026-10-24');
  assert.deepEqual(v.weeklyTargets, [{ weekStart: '2026-10-18', planId: 'C', name: 'Cardio', target: 3, completed: 2, remaining: 1, dates: ['2026-10-18', '2026-10-20'] }]);
  const mon = S.computeSchedule(data, '2026-10-19', '2026-10-25', 'mon');
  assert.equal(mon.weeklyTargets[0].weekStart, '2026-10-19');
  assert.equal(mon.weeklyTargets[0].completed, 1); // Sunday 18th belongs to the previous Monday week
});

t('computeSchedule: due today when nothing logged', () => {
  const v = S.computeSchedule({ ...data, workouts: [] }, '2026-10-18', '2026-10-24');
  assert.equal(v.days[3].scheduled[0].status, 'due');
  assert.equal(v.days[1].scheduled[0].status, 'missed');
});

t('weekTotals', () => {
  const v = S.computeSchedule(data, '2026-10-18', '2026-10-24');
  assert.deepEqual(S.weekTotals(v), { done: 3, planned: 5 + 3, hasSchedule: true });
  const none = S.computeSchedule({ ...data, plans: [data.plans[3]] }, '2026-10-18', '2026-10-24');
  assert.equal(S.weekTotals(none).hasSchedule, false);
});

t('mock seed: schedules + a meaningful current week (Sun 18 – Sat 24 Oct 2026)', () => {
  const seed = buildSeed();
  const v = S.computeSchedule(seed, S.startOfWeek(seed.today), '2026-10-24');
  const by = Object.fromEntries(v.days.map((d) => [d.weekday, d.scheduled.map((s) => `${s.name}:${s.status}`).join(',')]));
  assert.equal(v.from, '2026-10-18');
  assert.equal(by.mon, 'Upper A:done');
  assert.equal(by.tue, 'Lower A:done');
  assert.equal(by.thu, 'Upper B:upcoming');
  assert.equal(by.fri, 'Lower B:upcoming');
  assert.equal(by.sun + by.sat + by.wed, '');
  assert.equal(v.days[3].workout?.planId, 'full-body');
  assert.equal(v.weeklyTargets[0].name, 'Full Body');
  assert.equal(v.weeklyTargets[0].completed, 1);
});

t('apiMapping: plan schedule and GET /schedule view', () => {
  assert.deepEqual(M.toPlanSchedule({ type: 'weekdays', days: ['mon', 'thu'] }), { type: 'weekdays', days: ['mon', 'thu'] });
  assert.deepEqual(M.toPlanSchedule({ type: 'times_per_week', times_per_week: 3 }), { type: 'times_per_week', times_per_week: 3 });
  assert.equal(M.toPlanSchedule(null), null);
  assert.equal(M.toClientPlan({ id: 4, name: 'X', schedule: null, revision: 1, total_sets: 0, items: [] }, String).schedule, null);
  const plan = { id: 'p', name: 'Upper', exercises: [], schedule: null };
  assert.equal('schedule' in M.planDocument(plan, String, { schedule: { type: 'weekdays', days: ['mon'] } }), false, 'UI cleared it: no schedule');
  assert.deepEqual(M.planDocument({ ...plan, schedule: { type: 'times_per_week', times_per_week: 2 } }, String, {}).schedule, { type: 'times_per_week', times_per_week: 2 });
  const v = M.toWeekSchedule({
    from: '2026-10-18', to: '2026-10-24', today: '2026-10-21', timezone: 'Asia/Jerusalem',
    days: [
      { date: '2026-10-18', weekday: 'sun', today: false, scheduled: null, workout: null },
      { date: '2026-10-19', weekday: 'mon', today: false, scheduled: [{ plan_id: 7, name: 'Upper', status: 'done' }], workout: { id: 31, plan_id: 7, plan_name: 'Upper', working_sets: 9, revision: 4 } },
    ],
    weekly_targets: [{ week_start: '2026-10-18', plan_id: 8, name: 'Cardio', target: 3, completed: 1, remaining: 2, dates: ['2026-10-19'] }],
    unscheduled_plans: null,
  }, (sid) => (sid === 7 ? 'p-local' : String(sid)));
  assert.equal(v.days[0].weekday, 'sun');
  assert.deepEqual(v.days[0].scheduled, []);
  assert.deepEqual(v.days[1].scheduled, [{ planId: 'p-local', name: 'Upper', status: 'done' }]);
  assert.deepEqual(v.days[1].workout, { id: '31', planId: 'p-local', planName: 'Upper', workingSets: 9 });
  assert.deepEqual(v.weeklyTargets[0], { weekStart: '2026-10-18', planId: '8', name: 'Cardio', target: 3, completed: 1, remaining: 2, dates: ['2026-10-19'] });
  assert.deepEqual(v.unscheduled, []);
});

console.log(failed ? `\n${failed} test(s) FAILED` : '\nall schedule unit tests passed');
process.exit(failed ? 1 : 0);
