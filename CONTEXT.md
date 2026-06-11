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

**Attendance** — a three-state flag per person for the next session: *unknown*
(hasn't signalled), *yes* (definitely going), *no* (definitely not going).
Each tap cycles unknown → yes → no → unknown. Independent of queue actions.
Reset to unknown on Done and on Reset.

**Session (backend module)** — the night lifecycle as behaviour on `State`
(`session.go`): `SetPendingPick`, `SkipTurn`, `FinishNight`, `CycleAttendance`,
`Reset`, plus the session-date functions (14 days, snapped to Tuesday). Each
handler calls exactly one Session method inside `store.Update` and does no
domain work itself. Methods take `now` explicitly and assume a normalised
State; errors are `domainErr` values (caller fault vs system fault).

**Game night (frontend module)** — the evening as the client sees it: queue,
attendance, pending pick, history, next session. The `useGameNight()`
composable (`frontend/src/composables/useGameNight.ts`) owns all client↔server
coordination — polling, the edit-vs-poll overlay, drag sync with rollback, and
busy/error plumbing — behind a fetcher seam injected at construction.
Invariant: the composable's `state` always mirrors the last server response;
local editing overlays it (via the `pendingPick` computed) and never mutates
it. `App.vue` is presentation only.

## State persistence

One `blobStore` implements the `StateStore` interface (Get/Update) and owns
locking, JSON encoding, and state normalisation. Behind it, the `blob` seam
(`Read() ([]byte, error)` / `Write([]byte) error`) does pure byte I/O:

- **Local / EC2**: `fileBlob` — `data.json` in the working directory.
- **Lambda**: `s3Blob` — an S3 object (`data.json`) in a private bucket named
  by the `STATE_BUCKET` environment variable.

Convention at the blob seam: `Read` returns `(nil, nil)` only for
genuinely-absent data (no file / S3 `NoSuchKey`) — a fresh start. Every other
error propagates and aborts the operation; treating a transient failure as an
empty state would let an Update overwrite real data.

## Tech stack

- **Backend**: Go standard library (`net/http`, pattern-based routing via
  `http.ServeMux` with `{id}` path values). No frameworks.
- **Frontend**: Vue 3 (Composition API, `<script setup>`), single-file
  component (`App.vue`), Vite build, `vuedraggable` for drag-and-drop reorder.
  The compiled `frontend/dist` is embedded into the Go binary via `go:embed`.
- **Deployment**: single binary. Runs locally, on EC2 (systemd), or as an AWS
  Lambda function (ARM64 Graviton, `provided.al2023` runtime) behind a Lambda
  Function URL optionally fronted by CloudFront.
