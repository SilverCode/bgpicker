# Domain docs — consumer rules

## Layout: single-context

This is a single-context repository. There is one domain model for the whole
project.

| Artefact | Path |
|---|---|
| Domain context | `CONTEXT.md` (repo root) |
| Architecture decisions | `docs/adr/` |

## How to use these files

**Before starting any non-trivial task**, read `CONTEXT.md` to load the
project's terminology (Person, Queue, Pending pick, Done, etc.). Use the exact
terms defined there in code, comments, commit messages, and issue descriptions —
do not invent synonyms.

**Before proposing an architectural change**, check `docs/adr/` for prior
decisions that may constrain or inform the approach. If your change constitutes
a new significant decision, create an ADR in `docs/adr/` using the template in
`docs/adr/README.md`.

## Staleness

`CONTEXT.md` should be updated whenever the domain model changes (new concepts,
renamed terms, changed behaviour). ADRs are append-only — add new ones rather
than editing old ones; mark superseded ones with `Status: Superseded by NNNN`.
