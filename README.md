# bgpicker 🎲

A dead-simple mobile-first web app for tracking whose turn it is to pick the next board game.

## Features

- **No login** — just open the URL on any device
- **Queue management** — players rotate at the bottom after picking
- **Skip** — drop to just behind the next person who's marked as going (not the back of the line)
- **Done** — after a game night ends, confirm and the picker rotates to the bottom
- **History** — last 8 picks are shown
- **Auto-sync** — all devices poll every 10 seconds to stay current
- **Single binary** — Go server embeds the compiled Vue frontend; one file to deploy

## Development

```bash
# Install frontend deps (first time only)
cd frontend && npm install && cd ..

# Run dev mode (Go API on :8080, Vite HMR on :5173)
make dev
# Then open http://localhost:5173
```

## Production Build

```bash
make build
./bgpicker          # serves on :8080
PORT=3000 ./bgpicker
```

The binary embeds `frontend/dist` — no separate static file hosting needed.

## Deploying to AWS

See **[DEPLOY.md](DEPLOY.md)** for full instructions. Two options are covered:

- **Lambda + S3** (~$0/month) — recommended; truly serverless, no idle cost
- **EC2 t4g.nano** (~$3.50/month or ~$0.85/month if stopped between sessions)

## Data

State is persisted to `data.json` locally, or to an S3 object when running in Lambda
(set the `STATE_BUCKET` environment variable).

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/state` | Full state (people, history, pending pick, next session, suggestions) |
| POST | `/api/people` | Add a person `{"name":"…"}` |
| DELETE | `/api/people/:id` | Remove a person |
| POST | `/api/people/:id/skip` | Skip current picker (must be position 0) — moves behind the next attending person |
| POST | `/api/people/:id/pick` | Set or edit the pending pick `{"gameName":"…"}` (must be position 0) — queue does not rotate yet |
| POST | `/api/people/:id/done` | Finalise the pending pick — records history, rotates picker to end, resets attendance, advances next session |
| POST | `/api/people/:id/attend` | Cycle attendance: unknown → yes → no → unknown |
| PUT | `/api/people/reorder` | Set explicit order `{"ids":[…]}` |
| DELETE | `/api/history/:id` | Remove one past pick or skip from history (queue unchanged) |
| PUT | `/api/session` | Override next session date `{"date":"YYYY-MM-DD"}` |
| POST | `/api/reset` | Clear history, pending pick, attendance and suggestions (queue order and session date kept) |
| POST | `/api/suggestions` | Suggest a game `{"gameName":"…","personId":"…"}` — 409 if already suggested |
| DELETE | `/api/suggestions/:id` | Remove a suggestion |
| POST | `/api/suggestions/:id/vote` | Vote `{"personId":"…","direction":"up"\|"down"\|""}` — `""` retracts |
