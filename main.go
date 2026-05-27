package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

//go:embed frontend/dist
var staticFiles embed.FS

// Person represents a player in the rotation
type Person struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Position  int    `json:"position"` // current position in queue
	Attending bool   `json:"attending"`
}

// Pick represents a board game selection event
type Pick struct {
	PersonID string    `json:"personId"`
	GameName string    `json:"gameName"`
	PickedAt time.Time `json:"pickedAt"`
	Skipped  bool      `json:"skipped"`
}

// PendingPick holds the game that has been chosen but not yet finalised.
// It is visible to all devices via /api/state so everyone can see what's
// being played, and it can be overwritten (edit) before the night is over.
type PendingPick struct {
	PersonID string    `json:"personId"`
	GameName string    `json:"gameName"`
	SetAt    time.Time `json:"setAt"`
}

// State is the full application state
type State struct {
	People      []Person     `json:"people"`
	History     []Pick       `json:"history"`
	PendingPick *PendingPick `json:"pendingPick,omitempty"`
	NextSession *time.Time   `json:"nextSession,omitempty"`
}

var (
	mu       sync.RWMutex
	dataFile = "data.json"
)

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

func loadState() (*State, error) {
	data, err := os.ReadFile(dataFile)
	if os.IsNotExist(err) {
		s := &State{People: []Person{}, History: []Pick{}}
		next := nextUpcomingTuesdayFrom(time.Now())
		s.NextSession = &next
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	// Populate a default session date if none is stored yet
	if s.NextSession == nil {
		next := nextUpcomingTuesdayFrom(time.Now())
		s.NextSession = &next
	}
	return &s, nil
}

func saveState(s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dataFile, data, 0644)
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// normalizePositions ensures positions are contiguous starting at 0
func normalizePositions(people []Person) []Person {
	// Sort by current position
	sorted := make([]Person, len(people))
	copy(sorted, people)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Position < sorted[i].Position {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	for i := range sorted {
		sorted[i].Position = i
	}
	return sorted
}

// CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonResponse(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// GET /api/state
func handleGetState(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, s)
}

// POST /api/people  body: {"name": "Alice"}
func handleAddPerson(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		jsonResponse(w, 400, map[string]string{"error": "name required"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	p := Person{
		ID:       generateID(),
		Name:     req.Name,
		Position: len(s.People),
	}
	s.People = append(s.People, p)
	if err := saveState(s); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 201, p)
}

// DELETE /api/people/{id}
func handleDeletePerson(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mu.Lock()
	defer mu.Unlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	found := false
	filtered := []Person{}
	for _, p := range s.People {
		if p.ID == id {
			found = true
		} else {
			filtered = append(filtered, p)
		}
	}
	if !found {
		jsonResponse(w, 404, map[string]string{"error": "person not found"})
		return
	}
	s.People = normalizePositions(filtered)
	if err := saveState(s); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, s)
}

// POST /api/people/{id}/skip
// Move the person to position+1 (swap with next person), not to the end
func handleSkip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mu.Lock()
	defer mu.Unlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}

	// Find the person and their position
	personIdx := -1
	for i, p := range s.People {
		if p.ID == id {
			personIdx = i
			break
		}
	}
	if personIdx == -1 {
		jsonResponse(w, 404, map[string]string{"error": "person not found"})
		return
	}

	// Sort people by position
	sorted := normalizePositions(s.People)

	// Find this person in sorted list
	sortedIdx := -1
	for i, p := range sorted {
		if p.ID == id {
			sortedIdx = i
			break
		}
	}

	// Only skip if not already at the end and this is the current picker (position 0)
	if sorted[sortedIdx].Position != 0 {
		jsonResponse(w, 400, map[string]string{"error": "only the current picker can skip"})
		return
	}

	if len(sorted) <= 1 {
		// Nothing to skip to
		jsonResponse(w, 200, s)
		return
	}

	// Move position-0 person to position 1 (swap with person at position 1)
	sorted[0].Position, sorted[1].Position = sorted[1].Position, sorted[0].Position

	// Record the skip
	s.History = append(s.History, Pick{
		PersonID: id,
		GameName: "",
		PickedAt: time.Now(),
		Skipped:  true,
	})

	s.People = sorted
	if err := saveState(s); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, s)
}

// POST /api/people/{id}/pick  body: {"gameName": "Catan"}
// Sets (or updates) the pending pick visible to all devices.
// Does NOT rotate the queue — call /done for that.
func handlePick(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		GameName string `json:"gameName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GameName == "" {
		jsonResponse(w, 400, map[string]string{"error": "gameName required"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}

	sorted := normalizePositions(s.People)

	// Must be the current picker (position 0)
	pickerIdx := -1
	for i, p := range sorted {
		if p.ID == id {
			pickerIdx = i
			break
		}
	}
	if pickerIdx == -1 {
		jsonResponse(w, 404, map[string]string{"error": "person not found"})
		return
	}
	if sorted[pickerIdx].Position != 0 {
		jsonResponse(w, 400, map[string]string{"error": "only the current picker can pick"})
		return
	}

	s.PendingPick = &PendingPick{
		PersonID: id,
		GameName: req.GameName,
		SetAt:    time.Now(),
	}
	s.People = sorted
	if err := saveState(s); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, s)
}

// POST /api/people/{id}/done
// Finalises the pending pick: records history, rotates queue, clears pending.
func handleDone(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mu.Lock()
	defer mu.Unlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}

	if s.PendingPick == nil || s.PendingPick.PersonID != id {
		jsonResponse(w, 400, map[string]string{"error": "no pending pick for this person"})
		return
	}

	sorted := normalizePositions(s.People)

	// Must still be position 0
	pickerIdx := -1
	for i, p := range sorted {
		if p.ID == id {
			pickerIdx = i
			break
		}
	}
	if pickerIdx == -1 {
		jsonResponse(w, 404, map[string]string{"error": "person not found"})
		return
	}
	if sorted[pickerIdx].Position != 0 {
		jsonResponse(w, 400, map[string]string{"error": "only the current picker can finalise"})
		return
	}

	// Rotate picker to the end
	n := len(sorted)
	for i := range sorted {
		if sorted[i].ID == id {
			sorted[i].Position = n - 1
		} else if sorted[i].Position > 0 {
			sorted[i].Position--
		}
	}

	// Record in history
	s.History = append(s.History, Pick{
		PersonID: id,
		GameName: s.PendingPick.GameName,
		PickedAt: time.Now(),
		Skipped:  false,
	})

	s.People = sorted
	s.PendingPick = nil

	// Reset attendance for next session
	for i := range s.People {
		s.People[i].Attending = false
	}

	// Advance session date by two weeks
	if s.NextSession != nil {
		next := advanceSession(*s.NextSession)
		s.NextSession = &next
	} else {
		next := advanceSession(time.Now())
		s.NextSession = &next
	}

	if err := saveState(s); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, s)
}

// POST /api/people/{id}/attend  — toggle attendance for a person
func handleToggleAttendance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mu.Lock()
	defer mu.Unlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	found := false
	for i := range s.People {
		if s.People[i].ID == id {
			s.People[i].Attending = !s.People[i].Attending
			found = true
			break
		}
	}
	if !found {
		jsonResponse(w, 404, map[string]string{"error": "person not found"})
		return
	}
	if err := saveState(s); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, s)
}

// PUT /api/session  body: {"date": "2026-06-17"}  — set next session date
func handleSetSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Date string `json:"date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Date == "" {
		jsonResponse(w, 400, map[string]string{"error": "date required (YYYY-MM-DD)"})
		return
	}
	t, err := time.ParseInLocation("2006-01-02", req.Date, time.Local)
	if err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid date format, use YYYY-MM-DD"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.NextSession = &t
	if err := saveState(s); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, s)
}

// PUT /api/people/reorder  body: {"ids": ["id1","id2",...]}  — full reorder
func handleReorder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid body"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s, err := loadState()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	posMap := map[string]int{}
	for i, id := range req.IDs {
		posMap[id] = i
	}
	for i := range s.People {
		if pos, ok := posMap[s.People[i].ID]; ok {
			s.People[i].Position = pos
		}
	}
	s.People = normalizePositions(s.People)
	if err := saveState(s); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, s)
}

func main() {
	mux := http.NewServeMux()

	// Serve embedded frontend (strip the "frontend/dist" prefix)
	distFS, err := fs.Sub(staticFiles, "frontend/dist")
	if err != nil {
		log.Fatalf("could not sub embed FS: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(distFS)))

	// API routes
	mux.HandleFunc("GET /api/state", handleGetState)
	mux.HandleFunc("POST /api/people", handleAddPerson)
	mux.HandleFunc("DELETE /api/people/{id}", handleDeletePerson)
	mux.HandleFunc("POST /api/people/{id}/skip", handleSkip)
	mux.HandleFunc("POST /api/people/{id}/pick", handlePick)
	mux.HandleFunc("POST /api/people/{id}/done", handleDone)
	mux.HandleFunc("POST /api/people/{id}/attend", handleToggleAttendance)
	mux.HandleFunc("PUT /api/people/reorder", handleReorder)
	mux.HandleFunc("PUT /api/session", handleSetSession)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	handler := corsMiddleware(mux)
	log.Printf("bgpicker listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
