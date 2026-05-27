# bgpicker — Domain context

## Purpose

A mobile-first web app for a small, trusted group of friends to manage whose
turn it is to pick the board game at a fortnightly game night. No authentication.

## Core concepts

**Person** — a player in the rotation. Has a name, a queue position, and an
attendance flag for the upcoming session.

**Queue** — the ordered list of people. The person at position 0 is the
*current picker*. After a game night ends the picker moves to the end (position
n−1) and everyone else shifts up by one.

**Skip** — the current picker defers to the next person *without* going to the
back of the queue. Their position swaps with position 1 so they remain close to
the front.

**Pending pick** — when the current picker types a game name and clicks
"Pick this game", the choice is immediately written to the server as a
`PendingPick` so all connected devices can see it. The queue does **not** rotate
yet. The picker can edit the pending pick freely until they press "Done".

**Done / end of night** — finalises the pending pick: records it in history,
rotates the queue, resets all attendance flags, and advances the next-session
date by 14 days (snapped to the nearest Tuesday).

**Next session** — a date stored on the state object. Defaults to the next
upcoming Tuesday. Advances automatically on Done. Can be overridden manually.

**Attendance** — a boolean per person indicating whether they plan to attend the
next session. Toggled independently of queue actions. Reset to false on Done.

## State persistence

- **Local / EC2**: `data.json` in the working directory, protected by a
  `sync.RWMutex`.
- **Lambda**: an S3 object (`data.json`) in a private bucket specified by the
  `STATE_BUCKET` environment variable. Same mutex guards in-process concurrency;
  S3 is the durable store.

## Tech stack

- **Backend**: Go standard library (`net/http`, pattern-based routing via
  `http.ServeMux` with `{id}` path values). No frameworks.
- **Frontend**: Vue 3 (Composition API, `<script setup>`), single-file
  component (`App.vue`), Vite build, `vuedraggable` for drag-and-drop reorder.
  The compiled `frontend/dist` is embedded into the Go binary via `go:embed`.
- **Deployment**: single binary. Runs locally, on EC2 (systemd), or as an AWS
  Lambda function (ARM64 Graviton, `provided.al2023` runtime) behind a Lambda
  Function URL optionally fronted by CloudFront.
