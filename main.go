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
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
)

//go:embed frontend/dist
var staticFiles embed.FS

// ── Domain types ──────────────────────────────────────────────────────────────

// AttendanceState is the three-way attendance signal for a Person.
type AttendanceState string

const (
	AttendanceUnknown AttendanceState = ""    // not yet signalled
	AttendanceYes     AttendanceState = "yes" // definitely going
	AttendanceNo      AttendanceState = "no"  // definitely not going
)

type Person struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Position  int             `json:"position"`
	Attending AttendanceState `json:"attending"`
}

// UnmarshalJSON handles both the current string format and the legacy boolean
// format so that existing data.json files continue to load correctly.
func (p *Person) UnmarshalJSON(data []byte) error {
	type wire struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Position  int             `json:"position"`
		Attending json.RawMessage `json:"attending"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	p.ID, p.Name, p.Position = w.ID, w.Name, w.Position
	if w.Attending == nil {
		p.Attending = AttendanceUnknown
		return nil
	}
	var s string
	if err := json.Unmarshal(w.Attending, &s); err == nil {
		p.Attending = AttendanceState(s)
		return nil
	}
	// Legacy boolean: true → yes, false → unknown
	var b bool
	if err := json.Unmarshal(w.Attending, &b); err == nil {
		if b {
			p.Attending = AttendanceYes
		} else {
			p.Attending = AttendanceUnknown
		}
		return nil
	}
	p.Attending = AttendanceUnknown
	return nil
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

// ── blobStore — StateStore over a byte-level blob seam ───────────────────────

// blob is the persistence seam: one durable byte slice. Read returns
// (nil, nil) when no data exists yet (fresh start); any other error must
// propagate so it aborts the caller's operation instead of being mistaken
// for an empty state.
type blob interface {
	Read() ([]byte, error)
	Write([]byte) error
}

// blobStore implements StateStore over any blob. It owns locking, JSON
// encoding, and state normalisation; blob adapters do pure byte I/O.
type blobStore struct {
	mu sync.RWMutex
	b  blob
}

func newBlobStore(b blob) *blobStore { return &blobStore{b: b} }

func (bs *blobStore) load() (*State, error) {
	data, err := bs.b.Read()
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

func (bs *blobStore) save(s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return bs.b.Write(data)
}

func (bs *blobStore) Get() (*State, error) {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return bs.load()
}

func (bs *blobStore) Update(fn func(*State) error) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	s, err := bs.load()
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return bs.save(s)
}

// ── fileBlob — local file adapter ─────────────────────────────────────────────

type fileBlob struct {
	path string
}

func (f *fileBlob) Read() ([]byte, error) {
	data, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func (f *fileBlob) Write(data []byte) error {
	return os.WriteFile(f.path, data, 0644)
}

// ── s3Blob — AWS S3 adapter ───────────────────────────────────────────────────

type s3Blob struct {
	client *s3.Client
	bucket string
	key    string
}

func (sb *s3Blob) Read() ([]byte, error) {
	out, err := sb.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(sb.bucket),
		Key:    aws.String(sb.key),
	})
	// Only a genuinely-missing object is a fresh start. Any other error
	// (auth, network, throttling) must abort the operation — treating it as
	// empty would let an Update overwrite real data with a fresh state.
	var noKey *types.NoSuchKey
	if errors.As(err, &noKey) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (sb *s3Blob) Write(data []byte) error {
	_, err := sb.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(sb.bucket),
		Key:         aws.String(sb.key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	return err
}

// ── Utility functions ─────────────────────────────────────────────────────────

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
			if err := s.SkipTurn(id, time.Now()); err != nil {
				return err
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
			if err := s.SetPendingPick(id, req.GameName, time.Now()); err != nil {
				return err
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

// POST /api/people/{id}/done
// Finalises the pending pick: records history, rotates queue, resets attendance,
// advances the next-session date.
func makeHandleDone(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var result *State
		err := store.Update(func(s *State) error {
			if err := s.FinishNight(id, time.Now()); err != nil {
				return err
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

// POST /api/people/{id}/attend — cycle attendance: unknown → yes → no → unknown
func makeHandleToggleAttendance(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var result *State
		err := store.Update(func(s *State) error {
			if err := s.CycleAttendance(id); err != nil {
				return err
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

// POST /api/reset — clear history, pending pick, and all attendance flags.
// Queue order and next-session date are unchanged.
func makeHandleReset(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var result *State
		err := store.Update(func(s *State) error {
			s.Reset()
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
	mux.HandleFunc("POST /api/reset", makeHandleReset(store))

	return corsMiddleware(mux)
}

func main() {
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		cfg, err := awsconfig.LoadDefaultConfig(context.Background())
		if err != nil {
			log.Fatalf("failed to load AWS config: %v", err)
		}
		bucket := os.Getenv("STATE_BUCKET")
		store := newBlobStore(&s3Blob{
			client: s3.NewFromConfig(cfg),
			bucket: bucket,
			key:    "data.json",
		})
		log.Printf("bgpicker starting in Lambda mode (bucket: %s)", bucket)
		lambda.Start(httpadapter.NewV2(buildMux(store)).ProxyWithContext)
		return
	}

	store := newBlobStore(&fileBlob{path: "data.json"})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("bgpicker listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, buildMux(store)))
}
