// Load the prototype's synthetic seed (5 plans + ~8 weeks of workouts + today's session in progress)
// into a running workout engine, through the public HTTP API only. For demos; all data is synthetic.
//
//   WORKOUT_API_URL=http://127.0.0.1:8080 WORKOUT_API_TOKEN=<MCP_BEARER_TOKEN> node scripts/seed-api.mjs [--skip-today]
//
// Dates are shifted so the seed's "today" (2026-10-21) lands on the server's today (GET /config).
// Safe to re-run: plans are imported with on_conflict=replace and workouts use idempotency keys.
import { build } from 'esbuild';
import { fileURLToPath } from 'node:url';

const URL_ = (process.env.WORKOUT_API_URL ?? 'http://127.0.0.1:8080').replace(/\/$/, '') + '/api/v1';
const TOKEN = process.env.WORKOUT_API_TOKEN;
const SKIP_TODAY = process.argv.includes('--skip-today');
if (!TOKEN) {
  console.error('WORKOUT_API_TOKEN is required (the engine\'s MCP_BEARER_TOKEN).');
  process.exit(2);
}

const entry = fileURLToPath(new URL('../src/data/seed.ts', import.meta.url));
const out = await build({ entryPoints: [entry], bundle: true, format: 'esm', platform: 'node', write: false, logLevel: 'error' });
const { buildSeed, TODAY } = await import('data:text/javascript;base64,' + Buffer.from(out.outputFiles[0].text).toString('base64'));

async function api(method, path, body, headers = {}) {
  const r = await fetch(URL_ + path, {
    method,
    headers: { Authorization: `Bearer ${TOKEN}`, ...(body !== undefined && { 'Content-Type': 'application/json' }), ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await r.text();
  const data = text ? (() => { try { return JSON.parse(text); } catch { return text; } })() : null;
  if (!r.ok) throw new Error(`${method} ${path} -> ${r.status} ${typeof data === 'string' ? data : data?.error?.message ?? ''}`);
  return { data, etag: r.headers.get('etag') };
}

const dayMs = 86400000;
const toUTC = (d) => Date.UTC(+d.slice(0, 4), +d.slice(5, 7) - 1, +d.slice(8, 10));
const iso = (ms) => new Date(ms).toISOString().slice(0, 10);

const { data: config } = await api('GET', '/config');
const shift = Math.round((toUTC(config.today) - toUTC(TODAY)) / dayMs);
const move = (d) => iso(toUTC(d) + shift * dayMs);
console.log(`server ${URL_}  today ${config.today} (${config.timezone}); shifting seed dates by ${shift} day(s)`);

const seed = buildSeed();
const exName = (id) => seed.exercises.find((e) => e.id === id)?.name.toLowerCase() ?? id;

// 1. Plans
const planIds = {};
for (const p of seed.plans) {
  const doc = {
    format: 'workout-plan', version: 1, name: p.name,
    items: p.exercises.map((e) => ({
      exercise: exName(e.exerciseId), sets: e.sets,
      ...(e.repMin > 0 && { rep_min: e.repMin }), ...(e.repMax > 0 && { rep_max: e.repMax }),
      ...(e.weight != null && { target_weight_kg: e.weight }), ...(e.rir != null && { target_rir: e.rir }), ...(e.rpe != null && { target_rpe: e.rpe }),
    })),
  };
  const { data } = await api('POST', '/plans/import?on_conflict=replace', doc);
  planIds[p.id] = data.plan.id;
  console.log(`plan  ${p.name.padEnd(10)} -> id ${data.plan.id} (${data.created ? 'created' : 'replaced'})`);
}

// 2. Workouts. Engine sets are performed sets: rows the prototype marks "not done" are skipped,
// except today's reps-only set (logged by voice, weight still missing), which is a performed set.
let nW = 0, nS = 0;
for (const w of [...seed.workouts].sort((a, b) => a.date.localeCompare(b.date))) {
  const date = move(w.date);
  const isToday = w.date === TODAY;
  if (isToday && SKIP_TODAY) continue;
  if (date > config.today) continue;
  const sets = [];
  for (const we of w.exercises)
    for (const s of we.sets) {
      if (!s.done && !(isToday && s.reps != null)) continue;
      if (s.reps === 0) continue;
      sets.push({
        exercise: exName(we.exerciseId),
        ...(s.weight != null && { weight_kg: s.weight }), ...(s.reps != null && { reps: s.reps }),
        ...(s.rpe != null && { rpe: s.rpe }), ...(s.rir != null && { rir: s.rir }),
        ...(s.type !== 'working' && { set_type: s.type }),
      });
    }
  const body = { ...(w.planId && planIds[w.planId] && { plan_id: planIds[w.planId] }), ...(sets.length ? { sets } : {}) };
  let res;
  try {
    res = await api('POST', `/workouts/${date}/sets`, body, { 'Idempotency-Key': `seed-api:${date}:v1` });
  } catch (e) {
    console.log(`skip  ${date}: ${e.message}`);
    continue;
  }
  nW++;
  nS += sets.length;
  let etag = res.etag;
  for (const we of w.exercises) {
    if (!we.note) continue;
    const r = await api('PATCH', `/workouts/${date}/prescriptions/${encodeURIComponent(exName(we.exerciseId))}`, { notes: we.note }, { 'If-Match': etag }).catch((e) => (console.log(`note  ${date}: ${e.message}`), null));
    if (r) etag = r.etag;
  }
  console.log(`workout ${date} ${w.name.padEnd(10)} ${String(sets.length).padStart(2)} sets${res.data.replayed ? ' (already seeded)' : ''}`);
}
console.log(`done: ${seed.plans.length} plans, ${nW} workouts, ${nS} sets`);
