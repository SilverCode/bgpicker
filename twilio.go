package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ── Twilio — the WhatsApp send seam ───────────────────────────────────────────
//
// TwilioClient is a thin wrapper over Twilio's REST API. Session reminders are
// business-initiated (not replies to something the recipient just sent), so
// WhatsApp requires them to use a pre-approved Content template rather than
// free text — see ADR-0001.

// TwilioClient sends WhatsApp Content template messages via Twilio's REST API.
type TwilioClient struct {
	AccountSID string
	AuthToken  string
	From       string // e.g. "whatsapp:+14155238886"
	HTTPClient *http.Client
}

// SendTemplate sends an approved Content template to a WhatsApp number. to is
// a bare E.164 number (no "whatsapp:" prefix); variables are substituted into
// the template's numbered placeholders (e.g. "1" -> {{1}}).
func (t *TwilioClient) SendTemplate(to, contentSID string, variables map[string]string) error {
	varsJSON, err := json.Marshal(variables)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("To", "whatsapp:"+to)
	form.Set("From", t.From)
	form.Set("ContentSid", contentSID)
	form.Set("ContentVariables", string(varsJSON))

	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", t.AccountSID)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(t.AccountSID, t.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := t.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio: %s: %s", resp.Status, body)
	}
	return nil
}

// ReminderConfig holds everything needed to send Session reminders: Twilio
// credentials and the two approved Content template SIDs (with vs. without a
// pending pick for the current picker — see ADR-0001).
type ReminderConfig struct {
	Client              *TwilioClient
	TemplateNoPickSID   string
	TemplateWithPickSID string
}

// loadReminderConfig reads Twilio credentials and template SIDs from the
// environment. Returns nil if any are unset, disabling reminders entirely
// (rather than failing startup) — a deploy without WhatsApp configured should
// still serve the rest of the app.
func loadReminderConfig() *ReminderConfig {
	sid := os.Getenv("TWILIO_ACCOUNT_SID")
	token := os.Getenv("TWILIO_AUTH_TOKEN")
	from := os.Getenv("TWILIO_WHATSAPP_FROM")
	noPick := os.Getenv("TWILIO_TEMPLATE_NO_PICK_SID")
	withPick := os.Getenv("TWILIO_TEMPLATE_WITH_PICK_SID")
	if sid == "" || token == "" || from == "" || noPick == "" || withPick == "" {
		return nil
	}
	return &ReminderConfig{
		Client:              &TwilioClient{AccountSID: sid, AuthToken: token, From: from},
		TemplateNoPickSID:   noPick,
		TemplateWithPickSID: withPick,
	}
}

// RunReminders sends a Session reminder to every Person with a phone number.
// If force is false, the natural gating applies (DueForReminder: 2-day lead
// window + RemindedForSession idempotency); if force is true (the manual
// send-now trigger), both are bypassed and only "is there a session and a
// current picker" is checked.
//
// Returns the number of people actually messaged and any per-person send
// errors (collected, not fatal — one bad number doesn't stop the rest of the
// fan-out). err is non-nil only when the run couldn't start at all (not
// configured, not due, no session/picker).
func RunReminders(store StateStore, cfg *ReminderConfig, now time.Time, force bool) (sent int, sendErrs []error, err error) {
	if cfg == nil {
		return 0, nil, domainErr(503, "reminders are not configured")
	}

	var recipients []Person
	var content ReminderContent

	err = store.Update(func(s *State) error {
		if !force && !s.DueForReminder(now) {
			return domainErr(409, "no reminder due")
		}
		c, ok := s.BuildReminderContent()
		if !ok {
			return domainErr(400, "no next session or current picker")
		}
		content = c
		recipients = s.ReminderRecipients()
		s.MarkReminded()
		return nil
	})
	if err != nil {
		return 0, nil, err
	}

	templateSID := cfg.TemplateNoPickSID
	variables := map[string]string{"1": content.SessionDate, "2": content.PickerName}
	if content.GameName != "" {
		templateSID = cfg.TemplateWithPickSID
		variables["3"] = content.GameName
	}

	for _, p := range recipients {
		if sendErr := cfg.Client.SendTemplate(p.Phone, templateSID, variables); sendErr != nil {
			sendErrs = append(sendErrs, fmt.Errorf("%s: %w", p.Name, sendErr))
			continue
		}
		sent++
	}
	return sent, sendErrs, nil
}
