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
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
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

// VoteDirection is the direction of a vote on a Suggestion.
type VoteDirection string

const (
	VoteNone VoteDirection = ""     // no vote / retracted
	VoteUp   VoteDirection = "up"   // thumbs up
	VoteDown VoteDirection = "down" // thumbs down
)

type Suggestion struct {
	ID          string                   `json:"id"`
	GameName    string                   `json:"gameName"`
	SuggestedBy string                   `json:"suggestedBy"`
	SuggestedAt time.Time                `json:"suggestedAt"`
	Votes       map[string]VoteDirection `json:"votes"`
}

type Person struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Position  int             `json:"position"`
	Attending AttendanceState `json:"attending"`
	Phone     string          `json:"phone,omitempty"` // E.164, optional; used for WhatsApp Session reminders
}

// UnmarshalJSON handles both the current string format and the legacy boolean
// format so that existing data.json files continue to load correctly.
func (p *Person) UnmarshalJSON(data []byte) error {
	type wire struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Position  int             `json:"position"`
		Attending json.RawMessage `json:"attending"`
		Phone     string          `json:"phone"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	p.ID, p.Name, p.Position, p.Phone = w.ID, w.Name, w.Position, w.Phone
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
	People             []Person     `json:"people"`
	History            []Pick       `json:"history"`
	PendingPick        *PendingPick `json:"pendingPick,omitempty"`
	NextSession        *time.Time   `json:"nextSession,omitempty"`
	Suggestions        []Suggestion `json:"suggestions"`
	RemindedForSession *time.Time   `json:"remindedForSession,omitempty"` // NextSession value already covered by a sent Session reminder
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
	if s.Suggestions == nil {
		s.Suggestions = []Suggestion{}
	}
	for i := range s.Suggestions {
		if s.Suggestions[i].Votes == nil {
			s.Suggestions[i].Votes = map[string]VoteDirection{}
		}
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

// POST /api/suggestions  body: {"gameName": string, "personId": string}
func makeHandleAddSuggestion(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			GameName string `json:"gameName"`
			PersonID string `json:"personId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GameName == "" || req.PersonID == "" {
			jsonResponse(w, 400, map[string]string{"error": "gameName and personId required"})
			return
		}
		var result *State
		err := store.Update(func(s *State) error {
			if err := s.AddSuggestion(req.PersonID, req.GameName, time.Now()); err != nil {
				return err
			}
			result = s
			return nil
		})
		if err != nil {
			httpErr(w, err)
			return
		}
		jsonResponse(w, 201, result)
	}
}

// DELETE /api/suggestions/{id}
func makeHandleDeleteSuggestion(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var result *State
		err := store.Update(func(s *State) error {
			if err := s.RemoveSuggestion(id); err != nil {
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

// POST /api/suggestions/{id}/vote  body: {"personId": string, "direction": "up"|"down"|""}
func makeHandleVote(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			PersonID  string        `json:"personId"`
			Direction VoteDirection `json:"direction"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PersonID == "" {
			jsonResponse(w, 400, map[string]string{"error": "personId required"})
			return
		}
		var result *State
		err := store.Update(func(s *State) error {
			if err := s.VoteOnSuggestion(id, req.PersonID, req.Direction); err != nil {
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

// PUT /api/people/{id}/phone  body: {"phone": "+15551234567"}
func makeHandleSetPhone(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			Phone string `json:"phone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonResponse(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		var result *State
		err := store.Update(func(s *State) error {
			if err := s.SetPhone(id, req.Phone); err != nil {
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

// POST /api/whatsapp/inbound — Twilio's webhook for inbound WhatsApp replies
// (form-encoded: From, Body). Maps the sender's phone number to a Person and
// applies an exact "yes"/"no" match to Attendance; anything else from a known
// number gets a short nudge. Unknown numbers are ignored silently. Does not
// validate Twilio's request signature — consistent with the rest of the
// app's no-auth model (see CONTEXT.md).
func makeHandleWhatsAppInbound(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			jsonResponse(w, 400, map[string]string{"error": "invalid form body"})
			return
		}
		from := strings.TrimPrefix(r.FormValue("From"), "whatsapp:")
		body := r.FormValue("Body")

		var matched, recognized bool
		err := store.Update(func(s *State) error {
			matched, recognized = s.ApplyWhatsAppReply(from, body)
			return nil
		})
		if err != nil {
			jsonResponse(w, 500, map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		if matched && !recognized {
			io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Response><Message>Sorry, I didn't understand — reply YES or NO.</Message></Response>`)
			return
		}
		io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`)
	}
}

// POST /api/reminders/send-now — manual trigger for testing. Bypasses both
// the 2-day lead window and the RemindedForSession idempotency gate.
func makeHandleSendReminderNow(store StateStore, cfg *ReminderConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sent, sendErrs, err := RunReminders(store, cfg, time.Now(), true)
		if err != nil {
			httpErr(w, err)
			return
		}
		errStrs := make([]string, len(sendErrs))
		for i, e := range sendErrs {
			errStrs[i] = e.Error()
		}
		jsonResponse(w, 200, map[string]interface{}{"sent": sent, "errors": errStrs})
	}
}

// ── Wiring ────────────────────────────────────────────────────────────────────

func buildMux(store StateStore, reminderCfg *ReminderConfig) http.Handler {
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
	mux.HandleFunc("POST /api/suggestions", makeHandleAddSuggestion(store))
	mux.HandleFunc("DELETE /api/suggestions/{id}", makeHandleDeleteSuggestion(store))
	mux.HandleFunc("POST /api/suggestions/{id}/vote", makeHandleVote(store))
	mux.HandleFunc("PUT /api/people/{id}/phone", makeHandleSetPhone(store))
	mux.HandleFunc("POST /api/whatsapp/inbound", makeHandleWhatsAppInbound(store))
	mux.HandleFunc("POST /api/reminders/send-now", makeHandleSendReminderNow(store, reminderCfg))

	return corsMiddleware(mux)
}

// makeLambdaHandler dispatches a raw Lambda event to one of two paths:
//   - An HTTP request (Function URL / API Gateway V2, payload format "2.0")
//     goes through the existing httpadapter-backed mux.
//   - Anything else (an EventBridge Scheduler trigger has no "version" field)
//     runs the daily Session reminder check instead. A "not due yet" result
//     is the common case and isn't treated as fatal — it's logged and the
//     invocation succeeds with sent=0.
//
// This lets one Lambda function serve both the HTTP API and the scheduled
// reminder job, with no second function to deploy (see CONTEXT.md's
// Reminders module entry).
func makeLambdaHandler(store StateStore, reminderCfg *ReminderConfig, mux http.Handler) func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
	adapter := httpadapter.NewV2(mux)
	return func(ctx context.Context, raw json.RawMessage) (interface{}, error) {
		var probe struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(raw, &probe)

		if probe.Version == "2.0" {
			var req events.APIGatewayV2HTTPRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, err
			}
			return adapter.ProxyWithContext(ctx, req)
		}

		sent, sendErrs, err := RunReminders(store, reminderCfg, time.Now(), false)
		if err != nil {
			log.Printf("reminder check: %v", err)
			return map[string]any{"sent": 0}, nil
		}
		for _, e := range sendErrs {
			log.Printf("reminder send failed: %v", e)
		}
		return map[string]any{"sent": sent}, nil
	}
}

func main() {
	reminderCfg := loadReminderConfig()

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
		log.Printf("bgpicker starting in Lambda mode (bucket: %s, reminders configured: %v)", bucket, reminderCfg != nil)
		lambda.Start(makeLambdaHandler(store, reminderCfg, buildMux(store, reminderCfg)))
		return
	}

	store := newBlobStore(&fileBlob{path: "data.json"})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("bgpicker listening on :%s (reminders configured: %v)", port, reminderCfg != nil)
	log.Fatal(http.ListenAndServe(":"+port, buildMux(store, reminderCfg)))
}
