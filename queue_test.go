package main

import "testing"

// makePersons builds a slice of Person values with IDs and positions as given.
func makePersons(ids ...string) []Person {
	people := make([]Person, len(ids))
	for i, id := range ids {
		people[i] = Person{ID: id, Name: id, Position: i}
	}
	return people
}

// ids returns the IDs of people in position order.
func ids(q Queue) []string {
	out := make([]string, q.Len())
	for i, p := range q.People() {
		out[i] = p.ID
	}
	return out
}

// ── NewQueue ──────────────────────────────────────────────────────────────────

func TestNewQueue_NormalizesPositions(t *testing.T) {
	// Positions out of order and non-contiguous; NewQueue must fix them.
	people := []Person{
		{ID: "c", Position: 10},
		{ID: "a", Position: 0},
		{ID: "b", Position: 5},
	}
	q := NewQueue(people)
	got := ids(q)
	want := []string{"a", "b", "c"}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("position %d: want %q, got %q", i, id, got[i])
		}
	}
}

func TestNewQueue_DoesNotMutateInput(t *testing.T) {
	people := makePersons("a", "b")
	_ = NewQueue(people)
	people[0].Position = 99 // mutate original
	q := NewQueue(makePersons("a", "b"))
	if q.Current().ID != "a" {
		t.Error("NewQueue modified input slice")
	}
}

// ── Current ───────────────────────────────────────────────────────────────────

func TestCurrent_EmptyQueue(t *testing.T) {
	q := NewQueue(nil)
	if q.Current() != nil {
		t.Error("want nil for empty queue")
	}
}

func TestCurrent_ReturnsPositionZero(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob", "charlie"))
	cur := q.Current()
	if cur == nil || cur.ID != "alice" {
		t.Errorf("want alice, got %v", cur)
	}
}

func TestCurrent_ReturnsCopy(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob"))
	cur := q.Current()
	cur.ID = "mutated"
	if q.Current().ID != "alice" {
		t.Error("Current() returned a reference into the queue")
	}
}

// ── Find ──────────────────────────────────────────────────────────────────────

func TestFind_ExistingPerson(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob"))
	p := q.Find("bob")
	if p == nil || p.ID != "bob" {
		t.Errorf("want bob, got %v", p)
	}
}

func TestFind_MissingPerson(t *testing.T) {
	q := NewQueue(makePersons("alice"))
	if q.Find("nobody") != nil {
		t.Error("want nil for unknown ID")
	}
}

// ── Add ───────────────────────────────────────────────────────────────────────

func TestAdd_AppendsAtEnd(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob"))
	created := q.Add(Person{ID: "charlie", Name: "charlie"})

	if q.Len() != 3 {
		t.Fatalf("want len 3, got %d", q.Len())
	}
	if created.Position != 2 {
		t.Errorf("want position 2, got %d", created.Position)
	}
	got := ids(q)
	if got[2] != "charlie" {
		t.Errorf("want charlie at position 2, got %q", got[2])
	}
}

func TestAdd_EmptyQueue(t *testing.T) {
	q := NewQueue(nil)
	created := q.Add(Person{ID: "alice"})
	if created.Position != 0 {
		t.Errorf("want position 0, got %d", created.Position)
	}
	if q.Current().ID != "alice" {
		t.Error("alice should be current picker")
	}
}

// ── Remove ────────────────────────────────────────────────────────────────────

func TestRemove_KnownPerson(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob", "charlie"))
	ok := q.Remove("bob")

	if !ok {
		t.Fatal("want true, got false")
	}
	if q.Len() != 2 {
		t.Fatalf("want len 2, got %d", q.Len())
	}
	got := ids(q)
	if got[0] != "alice" || got[1] != "charlie" {
		t.Errorf("want [alice charlie], got %v", got)
	}
}

func TestRemove_RenumbersPositions(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob", "charlie"))
	q.Remove("alice")
	// bob should now be at 0, charlie at 1
	got := ids(q)
	if got[0] != "bob" {
		t.Errorf("want bob at 0, got %q", got[0])
	}
	if got[1] != "charlie" {
		t.Errorf("want charlie at 1, got %q", got[1])
	}
}

func TestRemove_UnknownPerson(t *testing.T) {
	q := NewQueue(makePersons("alice"))
	if q.Remove("nobody") {
		t.Error("want false for unknown ID")
	}
	if q.Len() != 1 {
		t.Error("queue should be unchanged")
	}
}

// ── Skip ──────────────────────────────────────────────────────────────────────

func TestSkip_SwapsFirstTwo(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob", "charlie"))
	q.Skip()

	got := ids(q)
	if got[0] != "bob" {
		t.Errorf("want bob at 0, got %q", got[0])
	}
	if got[1] != "alice" {
		t.Errorf("want alice at 1, got %q", got[1])
	}
	if got[2] != "charlie" {
		t.Errorf("want charlie at 2 (unchanged), got %q", got[2])
	}
}

func TestSkip_SinglePerson_Noop(t *testing.T) {
	q := NewQueue(makePersons("alice"))
	q.Skip()
	if q.Current().ID != "alice" {
		t.Error("alice should still be current after skip on single-person queue")
	}
}

func TestSkip_EmptyQueue_Noop(t *testing.T) {
	q := NewQueue(nil)
	q.Skip() // must not panic
}

func TestSkip_TwiceMirrorsOriginal(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob", "charlie"))
	q.Skip()
	q.Skip()
	got := ids(q)
	if got[0] != "alice" || got[1] != "bob" || got[2] != "charlie" {
		t.Errorf("two skips should restore original order, got %v", got)
	}
}

// ── Rotate ────────────────────────────────────────────────────────────────────

func TestRotate_MovesCurrentToEnd(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob", "charlie"))
	q.Rotate()

	got := ids(q)
	if got[0] != "bob" {
		t.Errorf("want bob at 0, got %q", got[0])
	}
	if got[1] != "charlie" {
		t.Errorf("want charlie at 1, got %q", got[1])
	}
	if got[2] != "alice" {
		t.Errorf("want alice at 2, got %q", got[2])
	}
}

func TestRotate_SinglePerson_NoPositionChange(t *testing.T) {
	q := NewQueue(makePersons("alice"))
	q.Rotate()
	if q.Current().ID != "alice" || q.Current().Position != 0 {
		t.Error("single-person rotate should leave alice at position 0")
	}
}

func TestRotate_EmptyQueue_Noop(t *testing.T) {
	q := NewQueue(nil)
	q.Rotate() // must not panic
}

func TestRotate_FullCycleRestoresOrder(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob", "charlie"))
	q.Rotate()
	q.Rotate()
	q.Rotate()
	got := ids(q)
	if got[0] != "alice" || got[1] != "bob" || got[2] != "charlie" {
		t.Errorf("three rotates should restore original order, got %v", got)
	}
}

// ── Reorder ───────────────────────────────────────────────────────────────────

func TestReorder_AppliesNewOrder(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob", "charlie"))
	q.Reorder([]string{"charlie", "alice", "bob"})

	got := ids(q)
	if got[0] != "charlie" || got[1] != "alice" || got[2] != "bob" {
		t.Errorf("want [charlie alice bob], got %v", got)
	}
}

func TestReorder_UnknownIDsIgnored(t *testing.T) {
	q := NewQueue(makePersons("alice", "bob"))
	q.Reorder([]string{"bob", "alice", "nobody"}) // "nobody" not in queue
	got := ids(q)
	if got[0] != "bob" || got[1] != "alice" {
		t.Errorf("want [bob alice], got %v", got)
	}
}
