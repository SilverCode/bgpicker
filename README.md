# bgpicker 🎲

A dead-simple mobile-first web app for tracking whose turn it is to pick the next board game.

## Features

- **No login** — just open the URL on any device
- **Queue management** — players rotate at the bottom after picking
- **Skip** — move to the next slot (not the back of the line); the next person becomes the picker
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

## Data

State is persisted to `data.json` in the working directory.

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/state` | Full state (people + history) |
| POST | `/api/people` | Add a person `{"name":"…"}` |
| DELETE | `/api/people/:id` | Remove a person |
| POST | `/api/people/:id/skip` | Skip current picker (must be position 0) |
| POST | `/api/people/:id/pick` | Pick a game `{"gameName":"…"}` — moves picker to end |
| PUT | `/api/people/reorder` | Set explicit order `{"ids":[…]}` |
