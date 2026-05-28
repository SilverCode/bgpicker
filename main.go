package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
)

//go:embed frontend/dist
var staticFiles embed.FS

// ── Domain types ──────────────────────────────────────────────────────────────

type Person struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	Attending bool   `json:"attending"`
}

type Pick struct {
	PersonID string    `json:"personId"`
	GameName string    `json:"gameName"`
	PickedAt time.Time `json:"pickedAt"`
	Skipped  bool      `json:"skipped"`
}

// PendingPick holds the game chosen but not yet finalised. Visible to all
// devices; the queue does not rotate until Done is called.
type PendingPick struct {
	PersonID string    `json:"personId"`
	GameName string    `json:"gameName"`
	SetAt    time.Time `json:"setAt"`
}

type State struct {
	People      []Person     `json:"people"`
	History     []Pick       `json:"history"`
	PendingPick *PendingPick `json:"pendingPick,omitempty"`
	NextSession *time.Time   `json:"nextSession,omitempty"`
}

// ── StateStore — the seam between handlers and persistence ───────────────────

// StateStore is the single interface handlers use to read and mutate state.
// Implementations hide locking, serialisation, and I/O backend.
type StateStore interface {
	// Get returns a snapshot of current state.
	Get() (*State, error)
	// Update loads state, calls fn, and saves if fn returns nil.
	// fn must not retain the *State after it returns.
	Update(fn func(*State) error) error
}

// errDomain signals a caller-facing (4xx) error from inside an Update callback.
// Update callbacks return this to abort without saving; the store propagates it
// so httpErr can pick the right HTTP status.
type errDomain struct {
	code int
	msg  string
}

func (e *errDomain) Error() string { return e.msg }

func domainErr(code int, msg string) error { return &errDomain{code: code, msg: msg} }

// httpErr writes the appropriate JSON error response for an error from Update.
// errDomain → its own status code; everything else → 500.
func httpErr(w http.ResponseWriter, err error) {
	var de *errDomain
	if errors.As(err, &de) {
		jsonResponse(w, de.code, map[string]string{"error": de.msg})
	} else {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
	}
}

// normalizeState applies defaults to a freshly-loaded State so callers always
// receive a fully-initialised value.
func normalizeState(s *State) {
	if s.People == nil {
		s.People = []Person{}
	}
	if s.History == nil {
		s.History = []Pick{}
	}
	if s.NextSession == nil {
		next := nextUpcomingTuesdayFrom(time.Now())
		s.NextSession = &next
	}
}

// ── fileStore — local file adapter ───────────────────────────────────────────

type fileStore struct {
	mu   sync.RWMutex
	path string
}

func (f *fileStore) load() (*State, error) {
	data, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		data, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := &State{}
	if data != nil {
		if err := json.Unmarshal(data, s); err != nil {
			return nil, err
		}
	}
	normalizeState(s)
	return s, nil
}

func (f *fileStore) save(s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.path, data, 0644)
}

func (f *fileStore) Get() (*State, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.load()
}

func (f *fileStore) Update(fn func(*State) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.load()
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return f.save(s)
}

// ── s3Store — AWS S3 adapter ──────────────────────────────────────────────────

type s3Store struct {
	mu     sync.RWMutex
	client *s3.Client
	bucket string
	key    string
}

func (ss *s3Store) load() (*State, error) {
	out, err := ss.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(ss.bucket),
		Key:    aws.String(ss.key),
	})
	var data []byte
	if err == nil {
		defer out.Body.Close()
		data, err = io.ReadAll(out.Body)
		if err != nil {
			return nil, err
		}
	}
	// Any GetObject error (including NoSuchKey) → start with empty state.
	s := &State{}
	if data != nil {
		if err := json.Unmarshal(data, s); err != nil {
			return nil, err
		}
	}
	normalizeState(s)
	return s, nil
}

func (ss *s3Store) save(s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	_, err = ss.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(ss.bucket),
		Key:         aws.String(ss.key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	return err
}

func (ss *s3Store) Get() (*State, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.load()
}

func (ss *s3Store) Update(fn func(*State) error) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	s, err := ss.load()
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return ss.save(s)
}

// ── Utility functions ─────────────────────────────────────────────────────────

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

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}


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

// ── Handlers (closures over StateStore) ──────────────────────────────────────

// GET /api/state
func makeHandleGetState(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, err := store.Get()
		if err != nil {
			jsonResponse(w, 500, map[string]string{"error": err.Error()})
			return
		}
		jsonResponse(w, 200, s)
	}
}

// POST /api/people  body: {"name": "Alice"}
func makeHandleAddPerson(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			jsonResponse(w, 400, map[string]string{"error": "name required"})
			return
		}
		var created Person
		err := store.Update(func(s *State) error {
			q := NewQueue(s.People)
			created = q.Add(Person{ID: generateID(), Name: req.Name})
			s.People = q.People()
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 201, created)
	}
}

// DELETE /api/people/{id}
func makeHandleDeletePerson(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var result *State
		err := store.Update(func(s *State) error {
			q := NewQueue(s.People)
			if !q.Remove(id) {
				return domainErr(404, "person not found")
			}
			s.People = q.People()
			result = s
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 200, result)
	}
}

// POST /api/people/{id}/skip
func makeHandleSkip(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var result *State
		err := store.Update(func(s *State) error {
			q := NewQueue(s.People)
			if q.Find(id) == nil {
				return domainErr(404, "person not found")
			}
			cur := q.Current()
			if cur == nil || cur.ID != id {
				return domainErr(400, "only the current picker can skip")
			}
			q.Skip()
			s.History = append(s.History, Pick{
				PersonID: id,
				PickedAt: time.Now(),
				Skipped:  true,
			})
			s.People = q.People()
			result = s
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 200, result)
	}
}

// POST /api/people/{id}/pick  body: {"gameName": "Catan"}
// Sets (or updates) the pending pick. Does NOT rotate the queue.
func makeHandlePick(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			GameName string `json:"gameName"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GameName == "" {
			jsonResponse(w, 400, map[string]string{"error": "gameName required"})
			return
		}
		var result *State
		err := store.Update(func(s *State) error {
			q := NewQueue(s.People)
			if q.Find(id) == nil {
				return domainErr(404, "person not found")
			}
			cur := q.Current()
			if cur == nil || cur.ID != id {
				return domainErr(400, "only the current picker can pick")
			}
			s.PendingPick = &PendingPick{
				PersonID: id,
				GameName: req.GameName,
				SetAt:    time.Now(),
			}
			s.People = q.People()
			result = s
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 200, result)
	}
}

// POST /api/people/{id}/done
// Finalises the pending pick: records history, rotates queue, resets attendance,
// advances the next-session date.
func makeHandleDone(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var result *State
		err := store.Update(func(s *State) error {
			if s.PendingPick == nil || s.PendingPick.PersonID != id {
				return domainErr(400, "no pending pick for this person")
			}
			q := NewQueue(s.People)
			if q.Find(id) == nil {
				return domainErr(404, "person not found")
			}
			cur := q.Current()
			if cur == nil || cur.ID != id {
				return domainErr(400, "only the current picker can finalise")
			}
			q.Rotate()
			s.History = append(s.History, Pick{
				PersonID: id,
				GameName: s.PendingPick.GameName,
				PickedAt: time.Now(),
			})
			s.People = q.People()
			s.PendingPick = nil
			for i := range s.People {
				s.People[i].Attending = false
			}
			next := advanceSession(*s.NextSession)
			s.NextSession = &next
			result = s
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 200, result)
	}
}

// POST /api/people/{id}/attend — toggle attendance for a person
func makeHandleToggleAttendance(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var result *State
		err := store.Update(func(s *State) error {
			found := false
			for i := range s.People {
				if s.People[i].ID == id {
					s.People[i].Attending = !s.People[i].Attending
					found = true
					break
				}
			}
			if !found {
				return domainErr(404, "person not found")
			}
			result = s
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 200, result)
	}
}

// PUT /api/session  body: {"date": "2026-06-17"}
func makeHandleSetSession(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		var result *State
		err = store.Update(func(s *State) error {
			s.NextSession = &t
			result = s
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 200, result)
	}
}

// PUT /api/people/reorder  body: {"ids": ["id1","id2",...]}
func makeHandleReorder(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		var result *State
		err := store.Update(func(s *State) error {
			q := NewQueue(s.People)
			q.Reorder(req.IDs)
			s.People = q.People()
			result = s
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 200, result)
	}
}

// ── Wiring ────────────────────────────────────────────────────────────────────

func buildMux(store StateStore) http.Handler {
	mux := http.NewServeMux()

	distFS, err := fs.Sub(staticFiles, "frontend/dist")
	if err != nil {
		log.Fatalf("could not sub embed FS: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(distFS)))

	mux.HandleFunc("GET /api/state", makeHandleGetState(store))
	mux.HandleFunc("POST /api/people", makeHandleAddPerson(store))
	mux.HandleFunc("DELETE /api/people/{id}", makeHandleDeletePerson(store))
	mux.HandleFunc("POST /api/people/{id}/skip", makeHandleSkip(store))
	mux.HandleFunc("POST /api/people/{id}/pick", makeHandlePick(store))
	mux.HandleFunc("POST /api/people/{id}/done", makeHandleDone(store))
	mux.HandleFunc("POST /api/people/{id}/attend", makeHandleToggleAttendance(store))
	mux.HandleFunc("PUT /api/people/reorder", makeHandleReorder(store))
	mux.HandleFunc("PUT /api/session", makeHandleSetSession(store))

	return corsMiddleware(mux)
}

func main() {
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		cfg, err := awsconfig.LoadDefaultConfig(context.Background())
		if err != nil {
			log.Fatalf("failed to load AWS config: %v", err)
		}
		store := &s3Store{
			client: s3.NewFromConfig(cfg),
			bucket: os.Getenv("STATE_BUCKET"),
			key:    "data.json",
		}
		log.Printf("bgpicker starting in Lambda mode (bucket: %s)", store.bucket)
		lambda.Start(httpadapter.NewV2(buildMux(store)).ProxyWithContext)
		return
	}

	store := &fileStore{path: "data.json"}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("bgpicker listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, buildMux(store)))
}
