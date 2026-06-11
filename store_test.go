package main

import (
	"errors"
	"path/filepath"
	"testing"
)

// memBlob is the in-memory blob adapter: the second tested adapter at the
// blob seam, and a handle for injecting I/O failures.
type memBlob struct {
	data     []byte
	readErr  error
	writeErr error
	writes   int
}

func (m *memBlob) Read() ([]byte, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	return m.data, nil
}

func (m *memBlob) Write(data []byte) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.data = append([]byte(nil), data...)
	m.writes++
	return nil
}

// ── Conformance suite — run against every blob adapter ───────────────────────

// testBlobStore exercises the StateStore behaviour of blobStore over a blob
// adapter. mk must return a fresh, empty blob each call.
func testBlobStore(t *testing.T, mk func(t *testing.T) blob) {
	t.Run("fresh blob yields a normalised empty state", func(t *testing.T) {
		st := newBlobStore(mk(t))
		s, err := st.Get()
		if err != nil {
			t.Fatal(err)
		}
		if s.People == nil || s.History == nil {
			t.Error("People and History must be initialised")
		}
		if s.NextSession == nil {
			t.Error("NextSession must default to the next Tuesday")
		}
	})

	t.Run("update persists across store instances", func(t *testing.T) {
		b := mk(t)
		st1 := newBlobStore(b)
		err := st1.Update(func(s *State) error {
			q := NewQueue(s.People)
			q.Add(Person{ID: "alice", Name: "alice"})
			s.People = q.People()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// a second store over the same blob sees the saved state
		st2 := newBlobStore(b)
		s, err := st2.Get()
		if err != nil {
			t.Fatal(err)
		}
		if len(s.People) != 1 || s.People[0].ID != "alice" {
			t.Errorf("want alice persisted, got %+v", s.People)
		}
	})

	t.Run("domain error aborts the update without saving", func(t *testing.T) {
		b := mk(t)
		st := newBlobStore(b)
		err := st.Update(func(s *State) error {
			s.People = []Person{{ID: "ghost"}}
			return domainErr(400, "rejected")
		})
		if err == nil {
			t.Fatal("want error from Update")
		}
		s, err := st.Get()
		if err != nil {
			t.Fatal(err)
		}
		if len(s.People) != 0 {
			t.Error("rejected update must not be persisted")
		}
	})

	t.Run("legacy boolean attendance decodes", func(t *testing.T) {
		b := mk(t)
		raw := `{"people":[
			{"id":"a","name":"a","position":0,"attending":true},
			{"id":"b","name":"b","position":1,"attending":false}
		],"history":[]}`
		if err := b.Write([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		st := newBlobStore(b)
		s, err := st.Get()
		if err != nil {
			t.Fatal(err)
		}
		if s.People[0].Attending != AttendanceYes {
			t.Errorf("legacy true must decode as yes, got %q", s.People[0].Attending)
		}
		if s.People[1].Attending != AttendanceUnknown {
			t.Errorf("legacy false must decode as unknown, got %q", s.People[1].Attending)
		}
	})
}

func TestBlobStore_File(t *testing.T) {
	testBlobStore(t, func(t *testing.T) blob {
		return &fileBlob{path: filepath.Join(t.TempDir(), "data.json")}
	})
}

func TestBlobStore_Memory(t *testing.T) {
	testBlobStore(t, func(t *testing.T) blob {
		return &memBlob{}
	})
}

// ── Strictness — the data-loss regression the seam exists to prevent ─────────

// A read failure must abort an Update entirely: treating it as an empty state
// would let the subsequent save overwrite real data with a fresh state.
func TestBlobStore_ReadErrorAbortsUpdate(t *testing.T) {
	b := &memBlob{readErr: errors.New("s3 unavailable")}
	st := newBlobStore(b)

	err := st.Update(func(s *State) error {
		t.Error("update callback must not run when the read fails")
		return nil
	})
	if err == nil {
		t.Fatal("want read error from Update")
	}
	if b.writes != 0 {
		t.Error("nothing may be written after a failed read")
	}
}

func TestBlobStore_ReadErrorPropagatesFromGet(t *testing.T) {
	st := newBlobStore(&memBlob{readErr: errors.New("s3 unavailable")})
	if _, err := st.Get(); err == nil {
		t.Fatal("want read error from Get")
	}
}

func TestBlobStore_WriteErrorPropagates(t *testing.T) {
	st := newBlobStore(&memBlob{writeErr: errors.New("disk full")})
	err := st.Update(func(s *State) error { return nil })
	if err == nil {
		t.Fatal("want write error from Update")
	}
}
