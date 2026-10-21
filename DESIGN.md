# Design

Mode: Operate. A light "liquid glass" logbook: familiar patterns (floating tab bar, set tables) over expression. Dark is an option in Settings.

- Materials (apple-design): a soft pale wallpaper with a few tinted blooms sits behind translucent glass panels (`backdrop-filter` blur + saturate, bright top edge, hairline ring, soft shadow). Panels are the Today list, summary, calendar, plan editor and progress blocks. Glass is never stacked on glass; small controls are near-opaque white; bigger surfaces are thicker (more blur). Reduced-transparency and high-contrast fall back to solid surfaces.
- Navigation floats as a glass capsule (bottom on phones, rail on desktop); the active item is a lifted white capsule.
- Accent: one lime (`--accent`) for primary action, completion and selection; dark green for accent text on light. Semantics are separate hues: amber = missing/attention, sky = changed by the assistant, red = error.
- State is never colour-only: planned = ghost placeholder in a dashed field, entered = solid field, completed = filled check glyph + row tint.
- Type: Heebo variable (Hebrew + Latin), tabular numerals for data, 15px base, 12px minimum, tighter tracking on large text.
- Numbers, units and English exercise names are LTR-isolated inside RTL text.
- Charts stay LTR (time runs left to right), one unit per chart, gaps for missing data.
- Motion (Emil Kowalski): custom ease-out curves, <=320ms, press scale .97, transitions not keyframes, sheets materialise (blur + scale + fade) and can be dragged down with rubber-banding and momentum projection, reduced-motion respected.
- Tokens live in `src/styles/theme.css` (light + dark); materials in `src/styles/glass.css`.
