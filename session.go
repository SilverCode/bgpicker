package main

import "time"

// ── Session — the night lifecycle as behaviour on State ──────────────────────
//
// These methods are the domain interface for everything that happens over the
// course of a game night: choosing a game, skipping a turn, finishing the
// night, signalling attendance, and resetting. Handlers call exactly one of
// them inside store.Update and do no domain work themselves.
//
// All methods assume the State has been through normalizeState (People,
// History, and NextSession initialised). Methods take `now` explicitly so the
// lifecycle is deterministic under test; errors are domainErr values so the
// store and httpErr can distinguish caller faults from system faults.

// requireCurrentPicker validates that id names an existing person who is the
// current picker, returning the queue for further mutation. action appears in
// the error message ("only the current picker can <action>").
func (s *State) requireCurrentPicker(id, action string) (Queue, error) {
	q := NewQueue(s.People)
	if q.Find(id) == nil {
		return Queue{}, domainErr(404, "person not found")
	}
	cur := q.Current()
	if cur == nil || cur.ID != id {
		return Queue{}, domainErr(400, "only the current picker can "+action)
	}
	return q, nil
}

// SetPendingPick records (or updates) the current picker's game choice.
// The queue does not rotate until FinishNight.
func (s *State) SetPendingPick(id, gameName string, now time.Time) error {
	q, err := s.requireCurrentPicker(id, "pick")
	if err != nil {
		return err
	}
	s.PendingPick = &PendingPick{
		PersonID: id,
		GameName: gameName,
		SetAt:    now,
	}
	s.People = q.People()
	return nil
}

// SkipTurn defers the current picker to the next person without sending them
// to the back of the queue, and records a skipped entry in history.
func (s *State) SkipTurn(id string, now time.Time) error {
	q, err := s.requireCurrentPicker(id, "skip")
	if err != nil {
		return err
	}
	q.Skip()
	s.History = append(s.History, Pick{
		PersonID: id,
		PickedAt: now,
		Skipped:  true,
	})
	s.People = q.People()
	return nil
}

// FinishNight finalises the pending pick: records it in history, rotates the
// queue, resets all attendance to unknown, and advances the next-session date.
func (s *State) FinishNight(id string, now time.Time) error {
	if s.PendingPick == nil || s.PendingPick.PersonID != id {
		return domainErr(400, "no pending pick for this person")
	}
	q, err := s.requireCurrentPicker(id, "finalise")
	if err != nil {
		return err
	}
	q.Rotate()
	s.History = append(s.History, Pick{
		PersonID: id,
		GameName: s.PendingPick.GameName,
		PickedAt: now,
	})
	s.People = q.People()
	s.PendingPick = nil
	for i := range s.People {
		s.People[i].Attending = AttendanceUnknown
	}
	next := advanceSession(*s.NextSession)
	s.NextSession = &next
	return nil
}

// CycleAttendance advances a person's attendance: unknown → yes → no → unknown.
func (s *State) CycleAttendance(id string) error {
	for i := range s.People {
		if s.People[i].ID == id {
			switch s.People[i].Attending {
			case AttendanceUnknown:
				s.People[i].Attending = AttendanceYes
			case AttendanceYes:
				s.People[i].Attending = AttendanceNo
			case AttendanceNo:
				s.People[i].Attending = AttendanceUnknown
			}
			return nil
		}
	}
	return domainErr(404, "person not found")
}

// Reset clears history, the pending pick, and all attendance flags.
// Queue order and the next-session date are unchanged.
func (s *State) Reset() {
	s.History = []Pick{}
	s.PendingPick = nil
	for i := range s.People {
		s.People[i].Attending = AttendanceUnknown
	}
}

// ── Session dates ─────────────────────────────────────────────────────────────

// nextUpcomingTuesdayFrom returns the next Tuesday on or after t (midnight local).
func nextUpcomingTuesdayFrom(t time.Time) time.Time {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	daysUntil := (int(time.Tuesday) - int(t.Weekday()) + 7) % 7
	return t.AddDate(0, 0, daysUntil)
}

// advanceSession returns the session date 14 days after base, snapped to Tuesday.
func advanceSession(base time.Time) time.Time {
	return nextUpcomingTuesdayFrom(base.AddDate(0, 0, 14))
}
