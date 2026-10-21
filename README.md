# workout-body

Interface prototype for a personal strength-training tracker (Hebrew, RTL, light liquid-glass look with an optional dark theme, mobile-first).
Frontend only: all data is synthetic and lives in memory behind a small replaceable data layer.
No backend, MCP server, auth or workout engine is implemented or implied.

```bash
npm install
npm run dev              # http://localhost:5173
npm run build            # typecheck + production build
node scripts/flows.mjs   # interaction smoke test (needs the dev server + Chromium)
```

## Layout

- `src/data/` types, deterministic 8-week seed, `mutations.ts` (every change is one `Mutation`), `repository.ts` (the only module the UI uses for data; a mock today).
- `src/state/store.tsx` reducer and actions: drafts, validation, conflicts, save status, external updates.
- `src/components/` reusable UI (`SetRow`, `ExerciseCard`, `LineChart`, `BarChart`, `Sheet`, ...).
- `src/screens/` Today, Plans, History, Exercise detail, Progress.
- Each exercise has a written how-to and a demo clip (one placeholder clip for all exercises), and any set can carry the user's own video (in-memory only; lost on reload).
- The flask icon in the header opens prototype controls that simulate assistant updates, a conflict, failed saves, slow or failed loads, and the three "today" states. They are labelled as simulated.

## Design skills

`.claude/skills/` holds Impeccable (pbakaus/impeccable) and Emil Kowalski's design-engineering skills (including `apple-design`, used for the glass materials and sheet gestures). `PRODUCT.md` and `DESIGN.md` record the context Impeccable reads.
