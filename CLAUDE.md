# bgpicker — Claude Code instructions

## Project overview

Go backend + Vue 3 frontend for tracking whose turn it is to pick the board game
at a fortnightly game night. Single binary; the frontend is embedded via
`go:embed`. State persisted to `data.json` locally or an S3 object in Lambda.

## Agent skills

### Issue tracker

Issues are tracked as local markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-state vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout — one `CONTEXT.md` at the repo root, ADRs under `docs/adr/`. See `docs/agents/domain.md`.
