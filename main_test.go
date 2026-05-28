package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ── in-memory adapter ─────────────────────────────────────────────────────────

// memStore is a zero-I/O StateStore for tests. Update passes the live *State
// directly to fn; each test owns its own store so there are no cross-test races.
type memStore struct{ s State }

func newMemStore(s State) *memStore {
	normalizeState(&s)
	return &memStore{s: s}
}

func (m *memStore) Get() (*State, error)               { return &m.s, nil }
func (m *memStore) Update(fn func(*State) error) error { return fn(&m.s) }

// ── test helpers ──────────────────────────────────────────────────────────────

func call(h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func callWithID(h http.HandlerFunc, method, path, id, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func mustState(t *testing.T, data []byte) State {
	t.Helper()
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("decode State: %v\nbody: %s", err, data)
	}
	return s
}

func mustPerson(t *testing.T, data []byte) Person {
	t.Helper()
	var p Person
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("decode Person: %v\nbody: %s", err, data)
	}
	return p
}

// queue builds a State with people at sequential positions 0, 1, 2...
// ID and Name are both set to the supplied strings.
func queue(ids ...string) State {
	people := make([]Person, len(ids))
	for i, id := range ids {
		people[i] = Person{ID: id, Name: id, Position: i}
	}
	return State{People: people}
}

// posMap returns a map of position → person ID for easy assertions.
func posMap(people []Person) map[int]string {
	m := map[int]string{}
	for _, p := range people {
		m[p.Position] = p.ID
	}
	return m
}

// ── GET /api/state ────────────────────────────────────────────────────────────

func TestGetState(t *testing.T) {
	store := newMemStore(queue("alice", "bob"))
	rec := call(makeHandleGetState(store), http.MethodGet, "/api/state", "")

	if rec.Code != 200 {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	s := mustState(t, rec.Body.Bytes())
	if len(s.People) != 2 {
		t.Errorf("want 2 people, got %d", len(s.People))
	}
}

// ── POST /api/people ──────────────────────────────────────────────────────────

func TestAddPerson(t *testing.T) {
	t.Run("creates person and returns 201", func(t *testing.T) {
		store := newMemStore(State{})
		rec := call(makeHandleAddPerson(store), http.MethodPost, "/api/people", `{"name":"Alice"}`)

		if rec.Code != 201 {
			t.Fatalf("want 201, got %d: %s", rec.Code, rec.Body)
		}
		p := mustPerson(t, rec.Body.Bytes())
		if p.Name != "Alice" {
			t.Errorf("want Alice, got %q", p.Name)
		}
		if p.ID == "" {
			t.Error("want non-empty ID")
		}
		if len(store.s.People) != 1 {
			t.Errorf("want 1 person in store, got %d", len(store.s.People))
		}
	})

	t.Run("missing name → 400", func(t *testing.T) {
		store := newMemStore(State{})
		rec := call(makeHandleAddPerson(store), http.MethodPost, "/api/people", `{}`)
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})
}

// ── DELETE /api/people/{id} ───────────────────────────────────────────────────

func TestDeletePerson(t *testing.T) {
	t.Run("removes person and renumbers positions", func(t *testing.T) {
		store := newMemStore(queue("alice", "bob", "charlie"))
		rec := callWithID(makeHandleDeletePerson(store), http.MethodDelete, "/api/people/alice", "alice", "")

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		if len(s.People) != 2 {
			t.Errorf("want 2 people, got %d", len(s.People))
		}
		pm := posMap(s.People)
		if pm[0] != "bob" {
			t.Errorf("want bob at 0, got %q", pm[0])
		}
		if pm[1] != "charlie" {
			t.Errorf("want charlie at 1, got %q", pm[1])
		}
	})

	t.Run("unknown id → 404", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandleDeletePerson(store), http.MethodDelete, "/api/people/nobody", "nobody", "")
		if rec.Code != 404 {
			t.Fatalf("want 404, got %d", rec.Code)
		}
	})
}

// ── POST /api/people/{id}/skip ────────────────────────────────────────────────

func TestSkip(t *testing.T) {
	t.Run("swaps position 0 with position 1 and records skip", func(t *testing.T) {
		store := newMemStore(queue("alice", "bob", "charlie"))
		rec := callWithID(makeHandleSkip(store), http.MethodPost, "/api/people/alice/skip", "alice", "")

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		pm := posMap(s.People)
		if pm[0] != "bob" {
			t.Errorf("want bob at 0, got %q", pm[0])
		}
		if pm[1] != "alice" {
			t.Errorf("want alice at 1, got %q", pm[1])
		}
		if pm[2] != "charlie" {
			t.Errorf("want charlie at 2, got %q", pm[2])
		}
		if len(s.History) != 1 || !s.History[0].Skipped || s.History[0].PersonID != "alice" {
			t.Errorf("want 1 skip for alice in history, got %v", s.History)
		}
	})

	t.Run("single-person queue — 200 with no queue change", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandleSkip(store), http.MethodPost, "/api/people/alice/skip", "alice", "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
	})

	t.Run("not current picker → 400", func(t *testing.T) {
		store := newMemStore(queue("alice", "bob"))
		rec := callWithID(makeHandleSkip(store), http.MethodPost, "/api/people/bob/skip", "bob", "")
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})

	t.Run("unknown person → 404", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandleSkip(store), http.MethodPost, "/api/people/nobody/skip", "nobody", "")
		if rec.Code != 404 {
			t.Fatalf("want 404, got %d", rec.Code)
		}
	})
}

// ── POST /api/people/{id}/pick ────────────────────────────────────────────────

func TestPick(t *testing.T) {
	t.Run("sets pending pick", func(t *testing.T) {
		store := newMemStore(queue("alice", "bob"))
		rec := callWithID(makeHandlePick(store), http.MethodPost, "/api/people/alice/pick", "alice", `{"gameName":"Catan"}`)

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		if s.PendingPick == nil {
			t.Fatal("want non-nil PendingPick")
		}
		if s.PendingPick.GameName != "Catan" {
			t.Errorf("want Catan, got %q", s.PendingPick.GameName)
		}
		if s.PendingPick.PersonID != "alice" {
			t.Errorf("want alice, got %q", s.PendingPick.PersonID)
		}
	})

	t.Run("overwrites previous pending pick (edit)", func(t *testing.T) {
		store := newMemStore(queue("alice", "bob"))
		callWithID(makeHandlePick(store), http.MethodPost, "/api/people/alice/pick", "alice", `{"gameName":"Catan"}`)
		rec := callWithID(makeHandlePick(store), http.MethodPost, "/api/people/alice/pick", "alice", `{"gameName":"Ticket to Ride"}`)

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		if s.PendingPick.GameName != "Ticket to Ride" {
			t.Errorf("want Ticket to Ride, got %q", s.PendingPick.GameName)
		}
	})

	t.Run("not current picker → 400", func(t *testing.T) {
		store := newMemStore(queue("alice", "bob"))
		rec := callWithID(makeHandlePick(store), http.MethodPost, "/api/people/bob/pick", "bob", `{"gameName":"Catan"}`)
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})

	t.Run("missing game name → 400", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandlePick(store), http.MethodPost, "/api/people/alice/pick", "alice", `{}`)
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})
}

// ── POST /api/people/{id}/done ────────────────────────────────────────────────

func TestDone(t *testing.T) {
	// june9 is a Tuesday so advanceSession lands exactly 14 days later (also a Tuesday).
	june9 := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)

	// setup returns a store with alice as current picker, a pending pick set, and
	// all three players marked as attending.
	setup := func() *memStore {
		q := queue("alice", "bob", "charlie")
		q.NextSession = &june9
		store := newMemStore(q)
		callWithID(makeHandlePick(store), http.MethodPost, "/api/people/alice/pick", "alice", `{"gameName":"Catan"}`)
		for i := range store.s.People {
			store.s.People[i].Attending = AttendanceYes
		}
		return store
	}

	t.Run("rotates queue: picker moves to last position", func(t *testing.T) {
		store := setup()
		rec := callWithID(makeHandleDone(store), http.MethodPost, "/api/people/alice/done", "alice", "")

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		pm := posMap(s.People)
		if pm[0] != "bob" {
			t.Errorf("want bob at 0, got %q", pm[0])
		}
		if pm[1] != "charlie" {
			t.Errorf("want charlie at 1, got %q", pm[1])
		}
		if pm[2] != "alice" {
			t.Errorf("want alice at 2, got %q", pm[2])
		}
	})

	t.Run("records pick in history", func(t *testing.T) {
		store := setup()
		rec := callWithID(makeHandleDone(store), http.MethodPost, "/api/people/alice/done", "alice", "")
		s := mustState(t, rec.Body.Bytes())

		if len(s.History) != 1 {
			t.Fatalf("want 1 history entry, got %d", len(s.History))
		}
		h := s.History[0]
		if h.PersonID != "alice" || h.GameName != "Catan" || h.Skipped {
			t.Errorf("unexpected history entry: %+v", h)
		}
	})

	t.Run("clears pending pick", func(t *testing.T) {
		store := setup()
		rec := callWithID(makeHandleDone(store), http.MethodPost, "/api/people/alice/done", "alice", "")
		s := mustState(t, rec.Body.Bytes())
		if s.PendingPick != nil {
			t.Errorf("want nil PendingPick, got %+v", s.PendingPick)
		}
	})

	t.Run("resets all attendance flags", func(t *testing.T) {
		store := setup()
		rec := callWithID(makeHandleDone(store), http.MethodPost, "/api/people/alice/done", "alice", "")
		s := mustState(t, rec.Body.Bytes())
		for _, p := range s.People {
			if p.Attending != AttendanceUnknown {
				t.Errorf("want attending=unknown after done, got %q for %s", p.Attending, p.ID)
			}
		}
	})

	t.Run("advances session date by 14 days", func(t *testing.T) {
		store := setup()
		rec := callWithID(makeHandleDone(store), http.MethodPost, "/api/people/alice/done", "alice", "")
		s := mustState(t, rec.Body.Bytes())

		want := advanceSession(june9) // 2026-06-23
		if s.NextSession == nil || !s.NextSession.Equal(want) {
			t.Errorf("want NextSession %v, got %v", want, s.NextSession)
		}
	})

	t.Run("no pending pick → 400", func(t *testing.T) {
		store := newMemStore(queue("alice", "bob"))
		rec := callWithID(makeHandleDone(store), http.MethodPost, "/api/people/alice/done", "alice", "")
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})

	t.Run("wrong person's pending pick → 400", func(t *testing.T) {
		store := setup()
		rec := callWithID(makeHandleDone(store), http.MethodPost, "/api/people/bob/done", "bob", "")
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})
}

// ── POST /api/people/{id}/attend ──────────────────────────────────────────────

func TestToggleAttendance(t *testing.T) {
	t.Run("unknown → yes", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandleToggleAttendance(store), http.MethodPost, "/api/people/alice/attend", "alice", "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d", rec.Code)
		}
		s := mustState(t, rec.Body.Bytes())
		if s.People[0].Attending != AttendanceYes {
			t.Errorf("want yes, got %q", s.People[0].Attending)
		}
	})

	t.Run("yes → no", func(t *testing.T) {
		q := queue("alice")
		q.People[0].Attending = AttendanceYes
		store := newMemStore(q)
		rec := callWithID(makeHandleToggleAttendance(store), http.MethodPost, "/api/people/alice/attend", "alice", "")
		s := mustState(t, rec.Body.Bytes())
		if s.People[0].Attending != AttendanceNo {
			t.Errorf("want no, got %q", s.People[0].Attending)
		}
	})

	t.Run("no → unknown", func(t *testing.T) {
		q := queue("alice")
		q.People[0].Attending = AttendanceNo
		store := newMemStore(q)
		rec := callWithID(makeHandleToggleAttendance(store), http.MethodPost, "/api/people/alice/attend", "alice", "")
		s := mustState(t, rec.Body.Bytes())
		if s.People[0].Attending != AttendanceUnknown {
			t.Errorf("want unknown, got %q", s.People[0].Attending)
		}
	})

	t.Run("unknown person → 404", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandleToggleAttendance(store), http.MethodPost, "/api/people/nobody/attend", "nobody", "")
		if rec.Code != 404 {
			t.Fatalf("want 404, got %d", rec.Code)
		}
	})
}

// ── PUT /api/session ──────────────────────────────────────────────────────────

func TestSetSession(t *testing.T) {
	t.Run("valid date sets NextSession", func(t *testing.T) {
		store := newMemStore(State{})
		rec := call(makeHandleSetSession(store), http.MethodPut, "/api/session", `{"date":"2026-07-14"}`)
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		if s.NextSession == nil {
			t.Fatal("want non-nil NextSession")
		}
		if got := s.NextSession.Format("2006-01-02"); got != "2026-07-14" {
			t.Errorf("want 2026-07-14, got %q", got)
		}
	})

	t.Run("invalid format → 400", func(t *testing.T) {
		store := newMemStore(State{})
		rec := call(makeHandleSetSession(store), http.MethodPut, "/api/session", `{"date":"not-a-date"}`)
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})

	t.Run("missing date field → 400", func(t *testing.T) {
		store := newMemStore(State{})
		rec := call(makeHandleSetSession(store), http.MethodPut, "/api/session", `{}`)
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})
}

// ── POST /api/reset ───────────────────────────────────────────────────────────

func TestReset(t *testing.T) {
	setup := func() *memStore {
		q := queue("alice", "bob")
		q.People[0].Attending = AttendanceYes
		q.People[1].Attending = AttendanceNo
		store := newMemStore(q)
		// give alice a pending pick and some history
		callWithID(makeHandlePick(store), http.MethodPost, "/api/people/alice/pick", "alice", `{"gameName":"Catan"}`)
		store.s.History = []Pick{{PersonID: "alice", GameName: "Pandemic"}}
		return store
	}

	t.Run("clears history", func(t *testing.T) {
		store := setup()
		rec := call(makeHandleReset(store), http.MethodPost, "/api/reset", "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		if len(s.History) != 0 {
			t.Errorf("want empty history, got %d entries", len(s.History))
		}
	})

	t.Run("clears pending pick", func(t *testing.T) {
		store := setup()
		rec := call(makeHandleReset(store), http.MethodPost, "/api/reset", "")
		s := mustState(t, rec.Body.Bytes())
		if s.PendingPick != nil {
			t.Errorf("want nil PendingPick, got %+v", s.PendingPick)
		}
	})

	t.Run("resets attendance to unknown", func(t *testing.T) {
		store := setup()
		rec := call(makeHandleReset(store), http.MethodPost, "/api/reset", "")
		s := mustState(t, rec.Body.Bytes())
		for _, p := range s.People {
			if p.Attending != AttendanceUnknown {
				t.Errorf("want attending=unknown after reset, got %q for %s", p.Attending, p.ID)
			}
		}
	})

	t.Run("preserves queue order", func(t *testing.T) {
		store := setup()
		rec := call(makeHandleReset(store), http.MethodPost, "/api/reset", "")
		s := mustState(t, rec.Body.Bytes())
		pm := posMap(s.People)
		if pm[0] != "alice" || pm[1] != "bob" {
			t.Errorf("want [alice bob], got %v", pm)
		}
	})
}

// ── PUT /api/people/reorder ───────────────────────────────────────────────────

func TestReorder(t *testing.T) {
	t.Run("applies new position order", func(t *testing.T) {
		store := newMemStore(queue("alice", "bob", "charlie"))
		body := `{"ids":["charlie","alice","bob"]}`
		rec := call(makeHandleReorder(store), http.MethodPut, "/api/people/reorder", body)
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		pm := posMap(s.People)
		if pm[0] != "charlie" {
			t.Errorf("want charlie at 0, got %q", pm[0])
		}
		if pm[1] != "alice" {
			t.Errorf("want alice at 1, got %q", pm[1])
		}
		if pm[2] != "bob" {
			t.Errorf("want bob at 2, got %q", pm[2])
		}
	})
}
