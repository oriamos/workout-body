# Design

Mode: Operate. A dark, quiet logbook. Familiar patterns (bottom nav, set tables) over expression.

- Surfaces: three cool near-black neutrals (bg / surface / raised), 1px hairlines instead of shadows, no nested cards.
- Accent: one lime (`--accent`) for primary action, completion and selection. Semantics are separate hues: amber = missing/attention, sky = changed by the assistant, red = error.
- State is never colour-only: planned = ghost placeholder in a dashed field, entered = solid field, completed = filled check glyph + row marker.
- Type: Heebo variable (Hebrew + Latin), tabular numerals for all data, 15px base, 12px minimum, scale ratio ~1.15.
- Numbers, units and English exercise names are LTR-isolated inside RTL text.
- Charts stay LTR (time runs left to right), one unit per chart, gaps for missing data.
- Motion (Emil Kowalski): custom ease-out curves, <=260ms, press scale .97, transitions not keyframes, no motion on frequent actions (tab switching, typing), reduced-motion respected.
