---
name: weight-tracking
description: Log and review the user's daily body weight (weigh-ins) through the workout-body MCP server (log_weigh_in, get_weigh_in, get_weight_trend, delete_weigh_in). Use whenever the user reports their weight ("82.4 this morning", "weighed 181 lb today"), corrects or deletes a weigh-in, or asks how their weight is going, what they weighed on a day, or how this week compares with last week.
---

# Weight tracking

The workout-body MCP server keeps one body-weight value per day. Turn what the
user says into explicit tool calls, and report only what was actually stored.

## Core model

- One **weigh-in** per local date, in **kilograms**. Logging a date again
  replaces that day's value; sending the same value again changes nothing, so a
  retry after a timeout is harmless (no key needed).
- Dates are local `YYYY-MM-DD` in the configured timezone (default
  Asia/Jerusalem). Every weigh-in tool returns `today` and `timezone`.
- Body weight never creates or changes a workout or a food entry.

## Rules

1. **The user reports a weight -> log it for today.** Call `log_weigh_in` with
   `weight_kg` and no `date` (today in their timezone). Use a `date` only when
   the user names another day ("yesterday I was 82.6"), resolved from `today`
   in a previous tool result, not from your own clock. A weigh-in cannot be in
   the future.
2. **Kilograms only.** If the user says pounds (lb, lbs), convert
   (1 lb = 0.45359237 kg, round to 0.1 kg) and say that you converted, e.g.
   "181 lb = 82.1 kg". If the unit is unclear and both readings are plausible,
   ask once. Never store a pounds number as kg.
3. **Confirm from the response.** Quote the stored date and weight. If
   `created` is false, a weigh-in for that day already existed and was
   replaced (or was identical); say so briefly if the value changed.
4. **Don't nag.** Do not remind the user to weigh in, ask for missing days,
   comment on how often they log, or attach unsolicited advice to a logged
   weight. A short confirmation is enough.
5. **"How is it going?" -> `get_weight_trend`.** Report the latest week's
   `average_kg` (and `days_logged`) and its `change_kg` versus the previous
   week; mention `overall_change_kg` over a longer range if asked. Note that
   daily weight fluctuates (water, food, salt, time of day) and that the weekly
   average is the signal, not a single day.
6. **Never invent numbers.** A `null` average or change means there is not
   enough data (no weigh-in that week or the week before). Say so; never
   estimate, interpolate or compute a replacement from memory. Weekly averages
   use only the days that were logged.
7. **No medical advice.** Do not diagnose, judge whether a weight or rate of
   change is healthy, prescribe diets or calorie numbers, or set goals the user
   did not ask for. If they ask for medical guidance, suggest a doctor or a
   registered dietitian.
8. **Corrections.** "Actually it was 82.1" -> `log_weigh_in` for the same date
   with the corrected value (it replaces). To guard against a concurrent edit,
   pass the `revision` from `get_weigh_in` as `expected_revision`; on a
   conflict, re-read and retry.
9. **Deletes need a yes.** Before `delete_weigh_in`, name the date and weight
   and get confirmation, unless the user explicitly asked for that exact
   deletion.

## Worked examples

**"82.4 this morning."**

```json
log_weigh_in {"weight_kg": 82.4}
```

Reply: "Logged 82.4 kg for today (2026-03-10)."

**"Weighed 181 lb."**

```json
log_weigh_in {"weight_kg": 82.1}
```

Reply: "Logged 82.1 kg for today (converted from 181 lb)."

**"How's my weight going?"**

`get_weight_trend {}` -> last week `average_kg: 82.7`, this week
`average_kg: 82.3, days_logged: 4, change_kg: -0.4, trend: "down"`.

Reply: "This week's average is 82.3 kg over 4 weigh-ins, 0.4 kg below last
week's 82.7. Day-to-day numbers bounce around; the weekly average is the
better signal."

If this week's `change_kg` is null (e.g. nothing logged last week): "This
week's average is 82.3 kg over 4 weigh-ins; there is no weigh-in from last week
to compare with."

**"What did I weigh on Monday?"** -> `get_weigh_in {"date": "2026-03-09"}`;
if `found` is false, say nothing was logged that day.

**"Delete yesterday's weigh-in."** -> `get_weigh_in` for that date, confirm
("Delete 82.6 kg on 2026-03-09?") only if the request was not already explicit,
then `delete_weigh_in {"date": "2026-03-09", "expected_revision": 1}`.
