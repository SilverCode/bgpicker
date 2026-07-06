package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ── PUT /api/people/{id}/phone ────────────────────────────────────────────────

func TestSetPhoneHandler(t *testing.T) {
	t.Run("sets phone and returns 200", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandleSetPhone(store), http.MethodPut, "/api/people/alice/phone", "alice",
			`{"phone":"+15551110000"}`)
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		s := mustState(t, rec.Body.Bytes())
		if s.People[0].Phone != "+15551110000" {
			t.Errorf("want phone set, got %q", s.People[0].Phone)
		}
	})

	t.Run("unknown person → 404", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandleSetPhone(store), http.MethodPut, "/api/people/nobody/phone", "nobody",
			`{"phone":"+15551110000"}`)
		if rec.Code != 404 {
			t.Fatalf("want 404, got %d", rec.Code)
		}
	})

	t.Run("invalid body → 400", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := callWithID(makeHandleSetPhone(store), http.MethodPut, "/api/people/alice/phone", "alice", `not json`)
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d", rec.Code)
		}
	})
}

// ── POST /api/whatsapp/inbound ───────────────────────────────────────────────

// callForm issues a form-encoded POST, matching how Twilio delivers webhooks.
func callForm(h http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestWhatsAppInboundHandler(t *testing.T) {
	t.Run("exact yes from a known number sets attendance and replies empty TwiML", func(t *testing.T) {
		q := queue("alice")
		q.People[0].Phone = "+15551110000"
		store := newMemStore(q)

		rec := callForm(makeHandleWhatsAppInbound(store), "/api/whatsapp/inbound", url.Values{
			"From": {"whatsapp:+15551110000"},
			"Body": {"yes"},
		})

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/xml" {
			t.Errorf("want text/xml, got %q", ct)
		}
		if body := rec.Body.String(); strings.Contains(body, "<Message>") {
			t.Errorf("want no <Message> for a recognized reply, got %s", body)
		}
		if store.s.People[0].Attending != AttendanceYes {
			t.Errorf("want AttendanceYes, got %q", store.s.People[0].Attending)
		}
	})

	t.Run("unrecognized body from a known number gets a nudge", func(t *testing.T) {
		q := queue("alice")
		q.People[0].Phone = "+15551110000"
		store := newMemStore(q)

		rec := callForm(makeHandleWhatsAppInbound(store), "/api/whatsapp/inbound", url.Values{
			"From": {"whatsapp:+15551110000"},
			"Body": {"maybe"},
		})

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		if body := rec.Body.String(); !strings.Contains(body, "<Message>") {
			t.Errorf("want a <Message> nudge, got %s", body)
		}
		if store.s.People[0].Attending != AttendanceUnknown {
			t.Errorf("want no attendance change, got %q", store.s.People[0].Attending)
		}
	})

	t.Run("unknown phone number is ignored with no reply message", func(t *testing.T) {
		store := newMemStore(queue("alice"))

		rec := callForm(makeHandleWhatsAppInbound(store), "/api/whatsapp/inbound", url.Values{
			"From": {"whatsapp:+19995550000"},
			"Body": {"yes"},
		})

		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		if body := rec.Body.String(); strings.Contains(body, "<Message>") {
			t.Errorf("want no reply message for an unknown number, got %s", body)
		}
	})
}

// ── POST /api/reminders/send-now ─────────────────────────────────────────────

// fakeTwilioTransport is an http.RoundTripper stub so tests never make a real
// network call to Twilio. failFor lists bare (no "whatsapp:") phone numbers
// that should simulate a send failure.
type fakeTwilioTransport struct {
	calls   []url.Values
	failFor map[string]bool
}

func (f *fakeTwilioTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	vals, _ := url.ParseQuery(string(body))
	f.calls = append(f.calls, vals)

	to := strings.TrimPrefix(vals.Get("To"), "whatsapp:")
	status := 201
	respBody := `{"sid":"SM_fake"}`
	if f.failFor[to] {
		status = 400
		respBody = `{"message":"invalid number"}`
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Header:     make(http.Header),
	}, nil
}

func testReminderConfig(transport *fakeTwilioTransport) *ReminderConfig {
	return &ReminderConfig{
		Client: &TwilioClient{
			AccountSID: "ACtest",
			AuthToken:  "tokentest",
			From:       "whatsapp:+14155238886",
			HTTPClient: &http.Client{Transport: transport},
		},
		TemplateSID: "HXappointment",
	}
}

func TestSendReminderNowHandler(t *testing.T) {
	t.Run("sends to every recipient with a phone, no game yet", func(t *testing.T) {
		q := queue("alice", "bob")
		q.People[0].Phone = "+15551110000"
		q.People[1].Phone = "" // no phone — skipped
		session := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
		q.NextSession = &session
		store := newMemStore(q)

		transport := &fakeTwilioTransport{}
		cfg := testReminderConfig(transport)

		rec := call(makeHandleSendReminderNow(store, cfg), http.MethodPost, "/api/reminders/send-now", "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		if len(transport.calls) != 1 {
			t.Fatalf("want 1 Twilio call (alice only), got %d", len(transport.calls))
		}
		if got := transport.calls[0].Get("ContentSid"); got != "HXappointment" {
			t.Errorf("want the appointment template, got %q", got)
		}
		if got := transport.calls[0].Get("ContentVariables"); !strings.Contains(got, `"2":"alice's pick"`) {
			t.Errorf("want {{2}} = \"alice's pick\" with no game, got %q", got)
		}
		if store.s.RemindedForSession == nil || !store.s.RemindedForSession.Equal(session) {
			t.Errorf("want RemindedForSession set to session, got %v", store.s.RemindedForSession)
		}
	})

	t.Run("includes the game when the current picker has chosen", func(t *testing.T) {
		q := queue("alice")
		q.People[0].Phone = "+15551110000"
		session := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
		q.NextSession = &session
		q.PendingPick = &PendingPick{PersonID: "alice", GameName: "Catan"}
		store := newMemStore(q)

		transport := &fakeTwilioTransport{}
		cfg := testReminderConfig(transport)

		rec := call(makeHandleSendReminderNow(store, cfg), http.MethodPost, "/api/reminders/send-now", "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		if got := transport.calls[0].Get("ContentVariables"); !strings.Contains(got, "alice's pick: Catan") {
			t.Errorf("want {{2}} to include the game, got %q", got)
		}
	})

	t.Run("bypasses the 2-day lead window and idempotency gate", func(t *testing.T) {
		q := queue("alice")
		q.People[0].Phone = "+15551110000"
		farFuture := time.Now().AddDate(0, 1, 0) // a month out — not naturally due
		q.NextSession = &farFuture
		alreadyReminded := farFuture
		q.RemindedForSession = &alreadyReminded // already "sent" — would block a non-forced run
		store := newMemStore(q)

		transport := &fakeTwilioTransport{}
		cfg := testReminderConfig(transport)

		rec := call(makeHandleSendReminderNow(store, cfg), http.MethodPost, "/api/reminders/send-now", "")
		if rec.Code != 200 {
			t.Fatalf("want 200 (forced bypass), got %d: %s", rec.Code, rec.Body)
		}
		if len(transport.calls) != 1 {
			t.Fatalf("want 1 Twilio call, got %d", len(transport.calls))
		}
	})

	t.Run("continues past a per-recipient send failure", func(t *testing.T) {
		q := queue("alice", "bob")
		q.People[0].Phone = "+15551110000"
		q.People[1].Phone = "+15552220000"
		session := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
		q.NextSession = &session
		store := newMemStore(q)

		transport := &fakeTwilioTransport{failFor: map[string]bool{"+15552220000": true}}
		cfg := testReminderConfig(transport)

		rec := call(makeHandleSendReminderNow(store, cfg), http.MethodPost, "/api/reminders/send-now", "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body)
		}
		if len(transport.calls) != 2 {
			t.Fatalf("want both recipients attempted, got %d calls", len(transport.calls))
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"sent":1`) {
			t.Errorf("want sent:1 (one success, one failure), got %s", body)
		}
	})

	t.Run("not configured → 503", func(t *testing.T) {
		store := newMemStore(queue("alice"))
		rec := call(makeHandleSendReminderNow(store, nil), http.MethodPost, "/api/reminders/send-now", "")
		if rec.Code != 503 {
			t.Fatalf("want 503, got %d: %s", rec.Code, rec.Body)
		}
	})

	t.Run("no next session or picker → 400", func(t *testing.T) {
		store := newMemStore(State{})
		transport := &fakeTwilioTransport{}
		cfg := testReminderConfig(transport)
		rec := call(makeHandleSendReminderNow(store, cfg), http.MethodPost, "/api/reminders/send-now", "")
		if rec.Code != 400 {
			t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body)
		}
	})
}
