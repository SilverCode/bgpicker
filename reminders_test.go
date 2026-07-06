package main

import (
	"testing"
)

// ── DueForReminder ────────────────────────────────────────────────────────────

func TestDueForReminder(t *testing.T) {
	session := date(2026, 6, 23) // a Tuesday

	t.Run("no next session is never due", func(t *testing.T) {
		s := &State{}
		if s.DueForReminder(date(2026, 6, 23)) {
			t.Fatal("want not due")
		}
	})

	t.Run("more than 2 days out is not due", func(t *testing.T) {
		s := &State{NextSession: &session}
		if s.DueForReminder(date(2026, 6, 20)) {
			t.Fatal("want not due")
		}
	})

	t.Run("exactly 2 days out is due", func(t *testing.T) {
		s := &State{NextSession: &session}
		if !s.DueForReminder(date(2026, 6, 21)) {
			t.Fatal("want due")
		}
	})

	t.Run("day of session is still due", func(t *testing.T) {
		s := &State{NextSession: &session}
		if !s.DueForReminder(session) {
			t.Fatal("want due")
		}
	})

	t.Run("already reminded for this session is not due again", func(t *testing.T) {
		reminded := session
		s := &State{NextSession: &session, RemindedForSession: &reminded}
		if s.DueForReminder(date(2026, 6, 22)) {
			t.Fatal("want not due — already reminded")
		}
	})

	t.Run("reminded for a stale session value is due again", func(t *testing.T) {
		staleReminded := date(2026, 6, 9) // an earlier session cycle
		s := &State{NextSession: &session, RemindedForSession: &staleReminded}
		if !s.DueForReminder(date(2026, 6, 22)) {
			t.Fatal("want due — RemindedForSession is stale")
		}
	})
}

func TestMarkReminded(t *testing.T) {
	session := date(2026, 6, 23)
	s := &State{NextSession: &session}
	s.MarkReminded()
	if s.RemindedForSession == nil || !s.RemindedForSession.Equal(session) {
		t.Fatalf("want RemindedForSession == %v, got %v", session, s.RemindedForSession)
	}
	if s.DueForReminder(session) {
		t.Fatal("want not due immediately after marking")
	}
}

// ── BuildReminderContent ──────────────────────────────────────────────────────

func TestBuildReminderContent(t *testing.T) {
	session := date(2026, 6, 23)

	t.Run("no next session", func(t *testing.T) {
		s := &State{People: []Person{{ID: "alice", Name: "alice", Position: 0}}}
		if _, ok := s.BuildReminderContent(); ok {
			t.Fatal("want not ok")
		}
	})

	t.Run("no people", func(t *testing.T) {
		s := &State{NextSession: &session}
		if _, ok := s.BuildReminderContent(); ok {
			t.Fatal("want not ok")
		}
	})

	t.Run("no pending pick for the current picker", func(t *testing.T) {
		s := &State{
			NextSession: &session,
			People: []Person{
				{ID: "alice", Name: "alice", Position: 0},
				{ID: "bob", Name: "bob", Position: 1},
			},
		}
		content, ok := s.BuildReminderContent()
		if !ok {
			t.Fatal("want ok")
		}
		if content.PickerName != "alice" || content.GameName != "" {
			t.Fatalf("got %+v", content)
		}
	})

	t.Run("pending pick for the current picker is included", func(t *testing.T) {
		s := &State{
			NextSession: &session,
			People: []Person{
				{ID: "alice", Name: "alice", Position: 0},
				{ID: "bob", Name: "bob", Position: 1},
			},
			PendingPick: &PendingPick{PersonID: "alice", GameName: "Catan"},
		}
		content, ok := s.BuildReminderContent()
		if !ok {
			t.Fatal("want ok")
		}
		if content.GameName != "Catan" {
			t.Fatalf("want GameName Catan, got %q", content.GameName)
		}
	})

	t.Run("pending pick for someone other than the current picker is not included", func(t *testing.T) {
		s := &State{
			NextSession: &session,
			People: []Person{
				{ID: "alice", Name: "alice", Position: 0},
				{ID: "bob", Name: "bob", Position: 1},
			},
			PendingPick: &PendingPick{PersonID: "bob", GameName: "Wingspan"},
		}
		content, ok := s.BuildReminderContent()
		if !ok {
			t.Fatal("want ok")
		}
		if content.GameName != "" {
			t.Fatalf("want empty GameName, got %q", content.GameName)
		}
	})
}

// ── ReminderRecipients ────────────────────────────────────────────────────────

func TestReminderRecipients(t *testing.T) {
	alice := Person{ID: "alice", Name: "alice", Position: 0, Phone: "+15551110000"}
	bob := Person{ID: "bob", Name: "bob", Position: 1} // no phone
	charlie := Person{ID: "charlie", Name: "charlie", Position: 2, Phone: "+15551112222"}

	s := &State{People: []Person{alice, bob, charlie}}
	got := s.ReminderRecipients()
	if len(got) != 2 {
		t.Fatalf("want 2 recipients, got %d: %+v", len(got), got)
	}
	if got[0].ID != "alice" || got[1].ID != "charlie" {
		t.Fatalf("want [alice charlie], got %+v", got)
	}
}

// ── SetPhone ──────────────────────────────────────────────────────────────────

func TestSetPhone(t *testing.T) {
	s := &State{People: []Person{{ID: "alice", Name: "alice", Position: 0}}}

	if err := s.SetPhone("alice", "+15551110000"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.People[0].Phone != "+15551110000" {
		t.Fatalf("want phone set, got %q", s.People[0].Phone)
	}

	if err := s.SetPhone("alice", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.People[0].Phone != "" {
		t.Fatal("want phone cleared")
	}

	err := s.SetPhone("nobody", "+15551110000")
	wantDomainErr(t, err, 404)
}

// ── ApplyWhatsAppReply ────────────────────────────────────────────────────────

func TestApplyWhatsAppReply(t *testing.T) {
	fresh := func() *State {
		alice := Person{ID: "alice", Name: "alice", Position: 0, Phone: "+15551110000"}
		return &State{People: []Person{alice}}
	}

	t.Run("exact yes sets attendance", func(t *testing.T) {
		s := fresh()
		matched, recognized := s.ApplyWhatsAppReply("+15551110000", "yes")
		if !matched || !recognized {
			t.Fatalf("want matched and recognized, got %v %v", matched, recognized)
		}
		if s.People[0].Attending != AttendanceYes {
			t.Fatalf("want AttendanceYes, got %v", s.People[0].Attending)
		}
	})

	t.Run("exact no sets attendance", func(t *testing.T) {
		s := fresh()
		matched, recognized := s.ApplyWhatsAppReply("+15551110000", "No")
		if !matched || !recognized {
			t.Fatalf("want matched and recognized, got %v %v", matched, recognized)
		}
		if s.People[0].Attending != AttendanceNo {
			t.Fatalf("want AttendanceNo, got %v", s.People[0].Attending)
		}
	})

	t.Run("case-insensitive and whitespace-trimmed", func(t *testing.T) {
		s := fresh()
		matched, recognized := s.ApplyWhatsAppReply("+15551110000", "  YES  ")
		if !matched || !recognized {
			t.Fatalf("want matched and recognized, got %v %v", matched, recognized)
		}
		if s.People[0].Attending != AttendanceYes {
			t.Fatalf("want AttendanceYes, got %v", s.People[0].Attending)
		}
	})

	t.Run("non-exact match is matched but not recognized, no mutation", func(t *testing.T) {
		s := fresh()
		matched, recognized := s.ApplyWhatsAppReply("+15551110000", "yep, I'll be there")
		if !matched {
			t.Fatal("want matched")
		}
		if recognized {
			t.Fatal("want not recognized")
		}
		if s.People[0].Attending != AttendanceUnknown {
			t.Fatalf("want no attendance change, got %v", s.People[0].Attending)
		}
	})

	t.Run("unknown phone number is not matched", func(t *testing.T) {
		s := fresh()
		matched, recognized := s.ApplyWhatsAppReply("+19995550000", "yes")
		if matched || recognized {
			t.Fatalf("want neither matched nor recognized, got %v %v", matched, recognized)
		}
	})
}
