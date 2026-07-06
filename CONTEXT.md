# bgpicker — Domain context

## Purpose

A mobile-first web app for a small, trusted group of friends to manage whose
turn it is to pick the board game at a fortnightly game night. No authentication.

## Core concepts

**Person** — a player in the rotation. Has a name, a queue position, an
attendance flag for the upcoming session, and an optional phone number (E.164
format) used to send WhatsApp session reminders. People without a phone number
are silently skipped when reminders go out.

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
rotates the queue, resets all attendance flags, advances the next-session
date by 14 days (snapped to the nearest Tuesday), and removes any Suggestion
whose game name matches the picked game (case-insensitive).

**Suggestion** — a game proposed by any person for consideration at a future
game night. Has a unique game name (case-insensitive across the list), the ID
of the person who suggested it, and a map of votes keyed by person ID. Stays
in the list until explicitly removed by any person, automatically removed when
the picked game matches on Done, or cleared entirely on Reset. If the suggester
is later removed from the queue the suggestion remains, attributed to an unknown
person.

**Vote** — a person's signal on a Suggestion: *up*, *down*, or *none* (no
vote). Each person has at most one vote per suggestion. Clicking a lit button
retracts it (returns to none); clicking the opposite button switches sides.
Votes from removed people are not audited — they remain in the tally.

**Suggestions list** — all current Suggestions ordered by net score (up count
minus down count) descending, ties broken by suggestion age (oldest first).

**Next session** — a date stored on the state object. Defaults to the next
upcoming Tuesday. Advances automatically on Done. Can be overridden manually.

**Session reminder** — a WhatsApp message fanned out to every Person with a
phone number, once per session cycle, when today falls within 2 days of Next
session (UTC). States the session date, names the current picker, and — if
that picker already has a Pending pick — the chosen game. Recipients are asked
to reply "yes" or "no" to set their Attendance. Gated by `RemindedForSession`
(the Next session value already covered by a sent reminder), so the daily
check is a no-op until Next session advances — which happens naturally via
Done's rotation. A manual trigger bypasses both the 2-day window and this
gate, for testing. Send failures for individual people don't block the rest
of the fan-out. See [ADR-0001](docs/adr/0001-whatsapp-reminders-via-twilio-sandbox.md)
for why this fans out to individuals over a Twilio Sandbox rather than posting
to a WhatsApp group or requiring a verified business sender.

**Attendance** — a three-state flag per person for the next session: *unknown*
(hasn't signalled), *yes* (definitely going), *no* (definitely not going).
Tapping in the app cycles unknown → yes → no → unknown; replying "yes" or "no"
(exact match, case-insensitive) to a Session reminder sets the flag directly
to that value instead of cycling. Independent of queue actions. Reset to
unknown on Done and on Reset. Reset also clears the Suggestions list.

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

**Reminders (backend module)** — behaviour for Session reminders: building the
fan-out list (People with a phone number), formatting the session date,
picker, and any Pending pick into the Sandbox's fixed "Appointment Reminders"
template (see ADR-0001), and handling the inbound WhatsApp reply webhook that
maps a phone number back to a Person and applies an exact yes/no match to
Attendance. Runs from the same Lambda as the HTTP API — invoked on a daily
EventBridge schedule, plus a manual `POST /api/reminders/send-now` route for
testing; `main()` distinguishes a scheduler event from an HTTP event before
routing. The inbound webhook does not validate Twilio's request signature,
consistent with the rest of the app's no-auth model — the risk of a spoofed
attendance flip was
judged negligible.

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
