package main

import (
	"strings"
	"time"
)

// ── Reminders — Session reminder domain logic ────────────────────────────────
//
// These methods are the pure domain interface for Session reminders: deciding
// whether one is due, building its content, listing who receives it, and
// applying an inbound WhatsApp reply to Attendance. They do no network I/O —
// sending the actual WhatsApp message is the Twilio client's job (twilio.go),
// orchestrated by RunReminders. Methods take `now` explicitly where relevant
// so the lifecycle is deterministic under test.

// ReminderLeadDays is how many days before NextSession a Session reminder
// becomes due.
const ReminderLeadDays = 2

// DueForReminder reports whether a Session reminder should fire right now:
// NextSession is set, today is within ReminderLeadDays of it, and no reminder
// has already been sent for this NextSession value.
func (s *State) DueForReminder(now time.Time) bool {
	if s.NextSession == nil {
		return false
	}
	if s.RemindedForSession != nil && s.RemindedForSession.Equal(*s.NextSession) {
		return false
	}
	leadStart := s.NextSession.AddDate(0, 0, -ReminderLeadDays)
	return !now.Before(leadStart)
}

// MarkReminded records that a reminder has been sent for the current
// NextSession, so DueForReminder returns false until NextSession advances.
func (s *State) MarkReminded() {
	next := *s.NextSession
	s.RemindedForSession = &next
}

// ReminderContent is what a Session reminder says: the session date, the
// current picker's name, and — if they've already chosen — the game.
type ReminderContent struct {
	SessionDate string // formatted, e.g. "Tue, Jun 24"
	PickerName  string
	GameName    string // empty if no pending pick
}

// BuildReminderContent derives the reminder content from NextSession, the
// current picker, and the pending pick. Returns false if there's no session
// date set or no current picker (empty queue).
func (s *State) BuildReminderContent() (ReminderContent, bool) {
	if s.NextSession == nil {
		return ReminderContent{}, false
	}
	q := NewQueue(s.People)
	cur := q.Current()
	if cur == nil {
		return ReminderContent{}, false
	}
	content := ReminderContent{
		SessionDate: s.NextSession.Format("Mon, Jan 2"),
		PickerName:  cur.Name,
	}
	if s.PendingPick != nil && s.PendingPick.PersonID == cur.ID {
		content.GameName = s.PendingPick.GameName
	}
	return content, true
}

// ReminderRecipients returns every Person with a phone number set, in queue
// order. People without a phone number are silently skipped.
func (s *State) ReminderRecipients() []Person {
	recipients := make([]Person, 0, len(s.People))
	for _, p := range s.People {
		if p.Phone != "" {
			recipients = append(recipients, p)
		}
	}
	return recipients
}

// SetPhone updates a person's phone number (E.164 format), or clears it when
// phone is empty.
func (s *State) SetPhone(id, phone string) error {
	for i := range s.People {
		if s.People[i].ID == id {
			s.People[i].Phone = phone
			return nil
		}
	}
	return domainErr(404, "person not found")
}

// ApplyWhatsAppReply maps an inbound WhatsApp reply to a Person by phone
// number and, on an exact "yes"/"no" match (case-insensitive, whitespace
// trimmed), sets their Attendance directly rather than cycling it. matched
// reports whether phone corresponds to a known Person; recognized reports
// whether body was an actionable yes/no. Both false cases are expected,
// ordinary inputs (unknown sender, unparseable reply) — not errors.
func (s *State) ApplyWhatsAppReply(phone, body string) (matched bool, recognized bool) {
	for i := range s.People {
		if s.People[i].Phone != phone {
			continue
		}
		matched = true
		switch strings.ToLower(strings.TrimSpace(body)) {
		case "yes":
			s.People[i].Attending = AttendanceYes
			recognized = true
		case "no":
			s.People[i].Attending = AttendanceNo
			recognized = true
		}
		return matched, recognized
	}
	return false, false
}
