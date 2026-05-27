# Issue tracker — local markdown

Issues for this project are stored as markdown files under `.scratch/` in the
repository root. This keeps everything self-contained and requires no external
services.

## Layout

```
.scratch/
  <slug>/
    issue.md      ← the issue itself
    notes.md      ← optional scratch notes, investigation logs, etc.
```

`<slug>` is a short kebab-case identifier derived from the issue title,
e.g. `.scratch/add-player-avatar/issue.md`.

## Issue file format

```markdown
---
id: <slug>
title: <one-line title>
status: needs-triage | needs-info | ready-for-agent | ready-for-human | wontfix
type: bug | feature | chore | docs
created: YYYY-MM-DD
---

## Description
What is the problem or request?

## Acceptance criteria
- [ ] …

## Notes
Investigation findings, links, context.
```

## Workflow

- **Create**: add a new `<slug>/issue.md` file, set `status: needs-triage`.
- **Triage**: update the `status` field and fill in acceptance criteria.
- **Close**: set `status: wontfix` or delete the directory once the work is
  merged.

## Agent instructions

When creating or updating issues, write directly to `.scratch/<slug>/issue.md`.
Do not use `gh issue create` or any external CLI — this repo tracks work locally.
