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

	t.Run("auto-removes matching suggestion", func(t *testing.T) {
		s := setup()
		s.Suggestions = []Suggestion{
			{ID: "s1", GameName: "Catan", SuggestedBy: "bob", Votes: map[string]VoteDirection{}},
			{ID: "s2", GameName: "Wingspan", SuggestedBy: "bob", Votes: map[string]VoteDirection{}},
		}
		if err := s.FinishNight("alice", now); err != nil {
			t.Fatal(err)
		}
		if len(s.Suggestions) != 1 || s.Suggestions[0].ID != "s2" {
			t.Errorf("want only Wingspan remaining, got %+v", s.Suggestions)
		}
	})

	t.Run("auto-removal is case-insensitive", func(t *testing.T) {
		s := setup()
		s.Suggestions = []Suggestion{
			{ID: "s1", GameName: "catan", SuggestedBy: "bob", Votes: map[string]VoteDirection{}},
		}
		if err := s.FinishNight("alice", now); err != nil {
			t.Fatal(err)
		}
		if len(s.Suggestions) != 0 {
			t.Errorf("want empty suggestions after match, got %+v", s.Suggestions)
		}
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
	s.Suggestions = []Suggestion{{ID: "s1", GameName: "Pandemic", SuggestedBy: "bob", Votes: map[string]VoteDirection{}}}

	s.Reset()

	if len(s.History) != 0 || s.PendingPick != nil {
		t.Error("history and pending pick must be cleared")
	}
	if len(s.Suggestions) != 0 {
		t.Error("suggestions must be cleared")
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

// ── AddSuggestion ─────────────────────────────────────────────────────────────

func TestAddSuggestion(t *testing.T) {
	now := date(2026, 6, 11)

	t.Run("adds a suggestion", func(t *testing.T) {
		s := queue("alice", "bob")
		if err := s.AddSuggestion("alice", "Wingspan", now); err != nil {
			t.Fatal(err)
		}
		if len(s.Suggestions) != 1 {
			t.Fatalf("want 1 suggestion, got %d", len(s.Suggestions))
		}
		sg := s.Suggestions[0]
		if sg.GameName != "Wingspan" || sg.SuggestedBy != "alice" {
			t.Errorf("unexpected suggestion: %+v", sg)
		}
		if sg.ID == "" {
			t.Error("want non-empty ID")
		}
		if !sg.SuggestedAt.Equal(now) {
			t.Errorf("want SuggestedAt %v, got %v", now, sg.SuggestedAt)
		}
	})

	t.Run("trims whitespace from game name", func(t *testing.T) {
		s := queue("alice")
		if err := s.AddSuggestion("alice", "  Catan  ", now); err != nil {
			t.Fatal(err)
		}
		if s.Suggestions[0].GameName != "Catan" {
			t.Errorf("want trimmed name, got %q", s.Suggestions[0].GameName)
		}
	})

	t.Run("duplicate game name is 409", func(t *testing.T) {
		s := queue("alice", "bob")
		_ = s.AddSuggestion("alice", "Wingspan", now)
		wantDomainErr(t, s.AddSuggestion("bob", "Wingspan", now), 409)
	})

	t.Run("duplicate check is case-insensitive", func(t *testing.T) {
		s := queue("alice", "bob")
		_ = s.AddSuggestion("alice", "wingspan", now)
		wantDomainErr(t, s.AddSuggestion("bob", "WINGSPAN", now), 409)
	})

	t.Run("unknown person is 404", func(t *testing.T) {
		s := queue("alice")
		wantDomainErr(t, s.AddSuggestion("nobody", "Catan", now), 404)
	})
}

// ── RemoveSuggestion ──────────────────────────────────────────────────────────

func TestRemoveSuggestion(t *testing.T) {
	now := date(2026, 6, 11)

	t.Run("removes the suggestion by ID", func(t *testing.T) {
		s := queue("alice")
		_ = s.AddSuggestion("alice", "Wingspan", now)
		id := s.Suggestions[0].ID
		if err := s.RemoveSuggestion(id); err != nil {
			t.Fatal(err)
		}
		if len(s.Suggestions) != 0 {
			t.Error("want empty suggestions after removal")
		}
	})

	t.Run("unknown ID is 404", func(t *testing.T) {
		s := queue("alice")
		wantDomainErr(t, s.RemoveSuggestion("nope"), 404)
	})
}

// ── VoteOnSuggestion ──────────────────────────────────────────────────────────

func TestVoteOnSuggestion(t *testing.T) {
	now := date(2026, 6, 11)

	setup := func() (State, string) {
		s := queue("alice", "bob")
		_ = s.AddSuggestion("alice", "Wingspan", now)
		return s, s.Suggestions[0].ID
	}

	t.Run("records an up vote", func(t *testing.T) {
		s, id := setup()
		if err := s.VoteOnSuggestion(id, "bob", VoteUp); err != nil {
			t.Fatal(err)
		}
		if s.Suggestions[0].Votes["bob"] != VoteUp {
			t.Errorf("want up, got %q", s.Suggestions[0].Votes["bob"])
		}
	})

	t.Run("records a down vote", func(t *testing.T) {
		s, id := setup()
		if err := s.VoteOnSuggestion(id, "bob", VoteDown); err != nil {
			t.Fatal(err)
		}
		if s.Suggestions[0].Votes["bob"] != VoteDown {
			t.Errorf("want down, got %q", s.Suggestions[0].Votes["bob"])
		}
	})

	t.Run("retracts a vote", func(t *testing.T) {
		s, id := setup()
		_ = s.VoteOnSuggestion(id, "bob", VoteUp)
		if err := s.VoteOnSuggestion(id, "bob", VoteNone); err != nil {
			t.Fatal(err)
		}
		if _, exists := s.Suggestions[0].Votes["bob"]; exists {
			t.Error("want vote removed after retract")
		}
	})

	t.Run("switches from up to down", func(t *testing.T) {
		s, id := setup()
		_ = s.VoteOnSuggestion(id, "bob", VoteUp)
		_ = s.VoteOnSuggestion(id, "bob", VoteDown)
		if s.Suggestions[0].Votes["bob"] != VoteDown {
			t.Errorf("want down after switch, got %q", s.Suggestions[0].Votes["bob"])
		}
	})

	t.Run("unknown suggestion ID is 404", func(t *testing.T) {
		s := queue("alice")
		wantDomainErr(t, s.VoteOnSuggestion("nope", "alice", VoteUp), 404)
	})
}
