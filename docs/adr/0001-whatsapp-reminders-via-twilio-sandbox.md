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
  opt in once via a join code.
- **No custom templates on the Sandbox, at any price.** Proactive
  (business-initiated) WhatsApp messages must use a pre-approved Content
  template rather than free text. We initially assumed a custom-worded
  template could still be submitted and approved while staying on the
  Sandbox — that's wrong. The Sandbox only permits its three fixed,
  Twilio-provided sample templates (Appointment Reminders, Order
  Notifications, Verification Codes); submitting your own wording for
  approval requires a verified business sender, the exact thing we're
  avoiding.

## Decision

Fan out a reminder to each Person's individual phone number (rather than a
group post), sent through Twilio's WhatsApp Sandbox (rather than a verified
business sender). Recipients join once via Twilio's sandbox join code.

Reminder content is repurposed into the Sandbox's fixed **"Appointment
Reminders"** sample template — `"Your appointment is coming up on {{1}} at
{{2}}"` — since it's the best fit of the three fixed options. `{{1}}` carries
the session date as the template intends; `{{2}}` (nominally a time) instead
carries who's picking and, if chosen, their game (e.g. `"alice's pick: Catan"`),
since no fixed template has a slot actually meant for that. The other two
fixed templates (order-shipping, verification-code wording) were rejected as
worse fits — they'd read as e-commerce spam or a stolen 2FA code.

## Consequences

- Recipients must complete a one-time WhatsApp opt-in (texting a join code to
  Twilio's shared sandbox number) before they can receive anything — this
  isn't a self-service in-app step, it happens outside bgpicker entirely.
- The sandbox number is shared with every other Twilio developer using
  sandbox mode; sandbox sessions can require recipients to re-join
  periodically if inactive.
- Reminder copy visibly reads as a repurposed appointment reminder, not
  bespoke wording — this was accepted as a stopgap in exchange for not
  requiring business verification. There's no way to improve the copy
  without either accepting this, or revisiting the decision below.
- Moving to a verified business sender later is possible but not free: it
  requires Meta business verification, reconfiguring the sender, and
  submitting the originally-envisioned custom templates for approval;
  existing sandbox opt-ins don't carry over.
