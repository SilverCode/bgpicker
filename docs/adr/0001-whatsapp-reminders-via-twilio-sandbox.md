# 0001 — WhatsApp session reminders fan out to individuals via Twilio Sandbox

**Date**: 2026-07-06
**Status**: Accepted

## Context

We want Session reminders (date, current picker, their picked game if any,
and an attendance ask) delivered over WhatsApp. The original ask was to post
into a single WhatsApp group without requiring a business WhatsApp account.

Neither half of that is achievable as stated:

- **No group messaging.** WhatsApp's Business Platform — which every
  third-party sender (Twilio included) is built on — has no API for posting
  into a WhatsApp group chat. Only 1:1 messages to individual numbers are
  supported.
- **No business-account-free production sender.** Every tier of WhatsApp
  Business Platform access requires Meta business verification for a
  persistent, un-expiring number. The only tier that doesn't is Twilio's
  **Sandbox**: a shared, free-to-use number that requires each recipient to
  opt in once via a join code, and restricts proactive (business-initiated)
  messages to pre-approved template strings rather than free text.

## Decision

Fan out a reminder to each Person's individual phone number (rather than a
group post), sent through Twilio's WhatsApp Sandbox (rather than a verified
business sender). Recipients join once via Twilio's sandbox join code.
Reminder content is limited to two pre-approved templates — one for "no pick
yet", one for "game already picked" — selected based on whether the current
picker has a Pending pick.

## Consequences

- Recipients must complete a one-time WhatsApp opt-in (texting a join code to
  Twilio's shared sandbox number) before they can receive anything — this
  isn't a self-service in-app step, it happens outside bgpicker entirely.
- The sandbox number is shared with every other Twilio developer using
  sandbox mode; sandbox sessions can require recipients to re-join
  periodically if inactive.
- Reminder copy is frozen to two fixed templates with numbered variable
  slots — no free-text or conditional content within a template. Adding a
  third scenario (e.g. a different message when nobody's attending) means
  submitting and waiting on approval for a new template, not just a code
  change.
- Moving to a verified business sender later is possible but not free: it
  requires Meta business verification and reconfiguring the sender, and
  existing sandbox opt-ins don't carry over.
