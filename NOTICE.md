# Attribution

This project builds on **Training MCP** by Francisco Manuel Olmedo Cortés:
<https://github.com/FullFran/training-mcp> (MIT License), imported at commit
`26ef0ec` ("fix: clamped volume advice described the wrong action").

The upstream copyright and permission notice are preserved verbatim in
[LICENSE](LICENSE). The upstream README is kept as
[UPSTREAM_README.md](UPSTREAM_README.md).

## What came from upstream

The ports-and-adapters layout, SQLite store and migration runner, the MCP
server skeleton and Bearer middleware, plans and session prescriptions, the
muscle-group catalogue, feedback / volume-landmark advice, load suggestions, the
SI metric and the server-rendered PWA (`internal/websrv`, including a bundled
`htmx.min.js` 2.0.4, 0BSD).

## What this project adds

One workout per local date (database-enforced) with a configurable timezone;
nullable weight/reps/RPE and new RIR, set type and notes; per-row revisions and
`If-Match`/`expected_revision` concurrency control; idempotent batch logging;
stable exercise ids and aliases; planned target weight and RIR; plan JSON
import/export with a JSON Schema; exercise statistics and training summaries; a
documented JSON HTTP API with OpenAPI; an extended MCP tool set with
annotations and stdio mode; a Claude Code plugin with assistant instructions;
WAL/immediate-transaction SQLite configuration; an integration/e2e test suite;
and the documentation set under `docs/`.

## Removed from the import

`.agents/` and `.claude/skills/` (third-party developer-agent skill files whose
licenses were not included) and `skills-lock.json`, so that nothing of
unclear license is redistributed. The Dokploy-specific backup/cron scripts were
replaced by a generic `backup` subcommand.

## Web UI and design skills (merged from `main`)

The web UI in `web/` (React + Vite, Hebrew RTL) was developed on `main` and
merged into this branch with its history; it uses React, React DOM, Vite and the
Heebo font (`@fontsource-variable/heebo`, SIL Open Font License), each under its
own license via npm.

`.claude/skills/` contains third-party design skills added on `main`
(Impeccable by pbakaus, declared Apache-2.0 in its SKILL.md; Emil Kowalski's
design-engineering skills including `apple-design`). Their license files are not
included in this repository. **Before publishing the repository, confirm each
skill's license and add the required license/NOTICE files, or remove the
skills** — they are developer tooling, not part of the shipped software.
