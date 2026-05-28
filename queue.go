package main

// Queue is an ordered list of Person values. Position 0 is the current picker.
//
// Invariant: after any method call, the internal slice is sorted by position
// and positions are contiguous 0..n-1. Callers never touch positions directly.
type Queue struct {
	people []Person
}

// NewQueue constructs a Queue from an existing slice, normalizing positions.
// The original slice is not modified.
func NewQueue(people []Person) Queue {
	q := Queue{people: make([]Person, len(people))}
	copy(q.people, people)
	q.normalize()
	return q
}

// Current returns the person at position 0, or nil if the queue is empty.
// Returns a copy — mutations to the returned value do not affect the queue.
func (q Queue) Current() *Person {
	if len(q.people) == 0 {
		return nil
	}
	p := q.people[0]
	return &p
}

// Find returns the person with the given ID, or nil if not found.
// Returns a copy — mutations to the returned value do not affect the queue.
func (q Queue) Find(id string) *Person {
	for _, p := range q.people {
		if p.ID == id {
			p := p
			return &p
		}
	}
	return nil
}

// Len returns the number of people in the queue.
func (q Queue) Len() int { return len(q.people) }

// People returns the queue as a slice sorted by position.
// Returns a copy — mutations do not affect the queue.
func (q Queue) People() []Person {
	out := make([]Person, len(q.people))
	copy(out, q.people)
	return out
}

// Add appends a new person at the last position and returns the person with
// its Position field set. The caller should use the returned value, not the
// original, since Position is assigned here.
func (q *Queue) Add(p Person) Person {
	p.Position = len(q.people)
	q.people = append(q.people, p)
	return p
}

// Remove deletes the person with the given ID and renormalizes positions.
// Returns false if the ID was not found.
func (q *Queue) Remove(id string) bool {
	for i, p := range q.people {
		if p.ID == id {
			q.people = append(q.people[:i], q.people[i+1:]...)
			for j := range q.people {
				q.people[j].Position = j
			}
			return true
		}
	}
	return false
}

// Skip swaps the current picker (position 0) with the person at position 1.
// No-op if the queue has fewer than 2 people.
func (q *Queue) Skip() {
	if len(q.people) < 2 {
		return
	}
	q.people[0], q.people[1] = q.people[1], q.people[0]
	q.people[0].Position = 0
	q.people[1].Position = 1
}

// Rotate moves the current picker (position 0) to the last position and
// shifts everyone else up by one. No-op if the queue is empty.
func (q *Queue) Rotate() {
	if len(q.people) == 0 {
		return
	}
	first := q.people[0]
	copy(q.people, q.people[1:])
	q.people[len(q.people)-1] = first
	for i := range q.people {
		q.people[i].Position = i
	}
}

// Reorder applies a new ordering given a list of person IDs. IDs not present
// in the queue are ignored; people whose IDs are absent from the list retain
// their existing relative positions.
func (q *Queue) Reorder(ids []string) {
	posMap := map[string]int{}
	for i, id := range ids {
		posMap[id] = i
	}
	for i := range q.people {
		if pos, ok := posMap[q.people[i].ID]; ok {
			q.people[i].Position = pos
		}
	}
	q.normalize()
}

// normalize sorts the internal slice by position and renumbers to 0..n-1.
func (q *Queue) normalize() {
	for i := 0; i < len(q.people)-1; i++ {
		for j := i + 1; j < len(q.people); j++ {
			if q.people[j].Position < q.people[i].Position {
				q.people[i], q.people[j] = q.people[j], q.people[i]
			}
		}
	}
	for i := range q.people {
		q.people[i].Position = i
	}
}
