package main

import (
	"errors"
	"testing"
	"time"
)

// date builds a midnight local time, matching what the session-date logic produces.
func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// wantDomainErr asserts err is a domainErr with the given code.
func wantDomainErr(t *testing.T, err error, code int) {
	t.Helper()
	var de *errDomain
	if !errors.As(err, &de) {
		t.Fatalf("want domainErr, got %v", err)
	}
	if de.code != code {
		t.Fatalf("want code %d, got %d (%s)", code, de.code, de.msg)
	}
}

// ── Session dates ─────────────────────────────────────────────────────────────

func TestNextUpcomingTuesdayFrom(t *testing.T) {
	cases := []struct {
		name string
		from time.Time
		want time.Time
	}{
		{"on a tuesday stays put", date(2026, 6, 9), date(2026, 6, 9)},
		{"wednesday goes to next week", date(2026, 6, 10), date(2026, 6, 16)},
		{"monday goes to tomorrow", date(2026, 6, 15), date(2026, 6, 16)},
		{"sunday goes to in two days", date(2026, 6, 14), date(2026, 6, 16)},
		{"time of day snaps to midnight", time.Date(2026, 6, 9, 15, 30, 0, 0, time.Local), date(2026, 6, 9)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextUpcomingTuesdayFrom(tc.from)
			if !got.Equal(tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestAdvanceSession(t *testing.T) {
	cases := []struct {
		name string
		base time.Time
		want time.Time
	}{
		{"tuesday base lands exactly a fortnight later", date(2026, 6, 9), date(2026, 6, 23)},
		{"non-tuesday base snaps forward to tuesday", date(2026, 6, 10), date(2026, 6, 30)},
		{"crosses a year boundary", date(2026, 12, 22), date(2027, 1, 5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := advanceSession(tc.base)
			if !got.Equal(tc.want) {
				t.Errorf("want %v, got %v", tc.want, got)
			}
			if got.Weekday() != time.Tuesday {
				t.Errorf("advanced session must be a Tuesday, got %v", got.Weekday())
			}
		})
	}
}

// ── SetPendingPick ────────────────────────────────────────────────────────────

func TestSetPendingPick(t *testing.T) {
	now := date(2026, 6, 11)

	t.Run("records the current picker's choice", func(t *testing.T) {
		s := queue("alice", "bob")
		if err := s.SetPendingPick("alice", "Catan", now); err != nil {
			t.Fatal(err)
		}
		if s.PendingPick == nil || s.PendingPick.GameName != "Catan" || s.PendingPick.PersonID != "alice" {
			t.Errorf("want pending pick Catan by alice, got %+v", s.PendingPick)
		}
		if !s.PendingPick.SetAt.Equal(now) {
			t.Errorf("want SetAt %v, got %v", now, s.PendingPick.SetAt)
		}
	})

	t.Run("overwrites an earlier choice", func(t *testing.T) {
		s := queue("alice", "bob")
		_ = s.SetPendingPick("alice", "Catan", now)
		_ = s.SetPendingPick("alice", "Wingspan", now)
		if s.PendingPick.GameName != "Wingspan" {
			t.Errorf("want Wingspan, got %q", s.PendingPick.GameName)
		}
	})

	t.Run("unknown person is 404", func(t *testing.T) {
		s := queue("alice")
		wantDomainErr(t, s.SetPendingPick("nobody", "Catan", now), 404)
	})

	t.Run("non-current picker is 400", func(t *testing.T) {
		s := queue("alice", "bob")
		wantDomainErr(t, s.SetPendingPick("bob", "Catan", now), 400)
		if s.PendingPick != nil {
			t.Error("pending pick must not be set on failure")
		}
	})
}

// ── SkipTurn ──────────────────────────────────────────────────────────────────

func TestSkipTurn(t *testing.T) {
	now := date(2026, 6, 11)

	t.Run("swaps the first two and records a skipped entry", func(t *testing.T) {
		s := queue("alice", "bob", "charlie")
		if err := s.SkipTurn("alice", now); err != nil {
			t.Fatal(err)
		}
		pos := posMap(s.People)
		if pos[0] != "bob" || pos[1] != "alice" || pos[2] != "charlie" {
			t.Errorf("want [bob alice charlie], got %v", pos)
		}
		if len(s.History) != 1 || !s.History[0].Skipped || s.History[0].PersonID != "alice" {
			t.Errorf("want one skipped history entry for alice, got %+v", s.History)
		}
		if !s.History[0].PickedAt.Equal(now) {
			t.Errorf("want PickedAt %v, got %v", now, s.History[0].PickedAt)
		}
	})

	t.Run("non-current picker is 400", func(t *testing.T) {
		s := queue("alice", "bob")
		wantDomainErr(t, s.SkipTurn("bob", now), 400)
	})

	t.Run("unknown person is 404", func(t *testing.T) {
		s := queue("alice")
		wantDomainErr(t, s.SkipTurn("nobody", now), 404)
	})
}

// ── FinishNight ───────────────────────────────────────────────────────────────

func TestFinishNight(t *testing.T) {
	now := date(2026, 6, 11)

	// finished builds a state mid-night: alice is current picker with a
	// pending pick, everyone has signalled attendance, session on 2026-06-09.
	setup := func() State {
		s := queue("alice", "bob", "charlie")
		sess := date(2026, 6, 9)
		s.NextSession = &sess
		s.PendingPick = &PendingPick{PersonID: "alice", GameName: "Catan", SetAt: now}
		s.People[0].Attending = AttendanceYes
		s.People[1].Attending = AttendanceNo
		return s
	}

	t.Run("rotates, records, resets attendance, advances the date", func(t *testing.T) {
		s := setup()
		if err := s.FinishNight("alice", now); err != nil {
			t.Fatal(err)
		}
		pos := posMap(s.People)
		if pos[0] != "bob" || pos[1] != "charlie" || pos[2] != "alice" {
			t.Errorf("want [bob charlie alice], got %v", pos)
		}
		if len(s.History) != 1 || s.History[0].GameName != "Catan" || s.History[0].Skipped {
			t.Errorf("want one Catan history entry, got %+v", s.History)
		}
		if s.PendingPick != nil {
			t.Error("pending pick must be cleared")
		}
		for _, p := range s.People {
			if p.Attending != AttendanceUnknown {
				t.Errorf("%s attendance must reset to unknown, got %q", p.ID, p.Attending)
			}
		}
		want := date(2026, 6, 23)
		if s.NextSession == nil || !s.NextSession.Equal(want) {
			t.Errorf("want next session %v, got %v", want, s.NextSession)
		}
	})

	t.Run("no pending pick is 400", func(t *testing.T) {
		s := setup()
		s.PendingPick = nil
		wantDomainErr(t, s.FinishNight("alice", now), 400)
	})

	t.Run("pending pick owned by someone else is 400", func(t *testing.T) {
		s := setup()
		wantDomainErr(t, s.FinishNight("bob", now), 400)
	})

	t.Run("failure leaves state untouched", func(t *testing.T) {
		s := setup()
		_ = s.FinishNight("bob", now)
		if len(s.History) != 0 || s.PendingPick == nil || posMap(s.People)[0] != "alice" {
			t.Error("failed FinishNight must not mutate state")
		}
	})
}

// ── CycleAttendance ───────────────────────────────────────────────────────────

func TestCycleAttendance(t *testing.T) {
	t.Run("cycles unknown → yes → no → unknown", func(t *testing.T) {
		s := queue("alice")
		steps := []AttendanceState{AttendanceYes, AttendanceNo, AttendanceUnknown}
		for _, want := range steps {
			if err := s.CycleAttendance("alice"); err != nil {
				t.Fatal(err)
			}
			if got := s.People[0].Attending; got != want {
				t.Fatalf("want %q, got %q", want, got)
			}
		}
	})

	t.Run("unknown person is 404", func(t *testing.T) {
		s := queue("alice")
		wantDomainErr(t, s.CycleAttendance("nobody"), 404)
	})
}

// ── Reset ─────────────────────────────────────────────────────────────────────

func TestSessionReset(t *testing.T) {
	s := queue("alice", "bob")
	sess := date(2026, 6, 9)
	s.NextSession = &sess
	s.History = []Pick{{PersonID: "alice", GameName: "Catan"}}
	s.PendingPick = &PendingPick{PersonID: "alice", GameName: "Catan"}
	s.People[0].Attending = AttendanceYes
	s.People[1].Attending = AttendanceNo

	s.Reset()

	if len(s.History) != 0 || s.PendingPick != nil {
		t.Error("history and pending pick must be cleared")
	}
	for _, p := range s.People {
		if p.Attending != AttendanceUnknown {
			t.Errorf("%s attendance must reset to unknown", p.ID)
		}
	}
	pos := posMap(s.People)
	if pos[0] != "alice" || pos[1] != "bob" {
		t.Error("queue order must be unchanged")
	}
	if !s.NextSession.Equal(sess) {
		t.Error("next-session date must be unchanged")
	}
}
