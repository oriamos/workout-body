# Product

<!-- impeccable:product-schema 1 -->

## Platform

web (mobile-first PWA-style interface, desktop layout included)

## Stack

delegated: Vite + React + TypeScript, plain CSS with tokens, hand-built SVG charts (no chart library, so RTL, missing values and units stay under our control). Chosen because the repo was empty and the brief asked for a prototype that is easy to connect to a backend later.

## Users

One person (the owner), training for strength 4x/week, logging sets on a phone between sets in a gym, often one-handed, tired, sometimes hands-free by talking to an external assistant. Reviews history and trends at a desk afterward.

## Product Purpose

A personal strength-training tracker interface. A separate workout engine stores plans, workouts, exercises and sets; an external conversational assistant logs and edits the same records. This surface shows and edits those records directly. Success: logging a set takes seconds, assistant changes appear without disrupting an active edit, and history/progress are trustworthy.

## Positioning

Not a social fitness app. A quiet, dense, single-user logbook where the assistant and the UI are two editors of the same record.

## Constraints

- Hebrew UI, full RTL; exercise names may be English; kg default.
- No in-app chatbot. No backend/MCP/auth/engine work in this repo yet; mock data behind a replaceable data layer.
- One workout per local calendar date; first logged set may create it.
- Never present estimates (e1RM) as exact; missing values are never zero.

## Open assumptions (inferred from the brief, not interviewed)

Brief was detailed and the user asked to proceed without stopping; no interview round was held. Assumed: dark theme only; single accent; no scheduling/periodization.
