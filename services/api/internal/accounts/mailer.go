package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Mailer isolates the delivery provider; tests never call Resend.
type Mailer interface {
	SendCode(context.Context, string, string) error
}

// LogMailer is explicitly local-only; production startup disallows it.
type LogMailer struct{ Logger *slog.Logger }

// SendCode writes local-only credentials to the development log.
func (m LogMailer) SendCode(ctx context.Context, email, code string) error {
	m.Logger.InfoContext(ctx, "DEV ONLY sign-in mail", "email", email, "code", code)
	return nil
}

// MailSink is an in-memory local/test mailbox, never registered by production.
type MailSink struct {
	mu    sync.Mutex
	codes map[string]string
}

// SendCode records a local test message.
func (m *MailSink) SendCode(_ context.Context, email, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.codes == nil {
		m.codes = make(map[string]string)
	}
	m.codes[email] = code
	return nil
}

// Code returns the last code delivered to a local test mailbox.
func (m *MailSink) Code(email string) string { m.mu.Lock(); defer m.mu.Unlock(); return m.codes[email] }

// ResendMailer sends plain text via the documented /emails API.
type ResendMailer struct {
	APIKey, From string
	Client       *http.Client
	Endpoint     string
}

// SendCode delivers an OTP through Resend.
func (m ResendMailer) SendCode(ctx context.Context, email, code string) error {
	body, err := json.Marshal(map[string]any{"from": m.From, "to": []string{email}, "subject": "Your Duolify sign-in code", "text": "Your Duolify code is " + code + ". It expires in 10 minutes. If you did not request this, ignore this email."})
	if err != nil {
		return err
	}
	endpoint := m.Endpoint
	if endpoint == "" {
		endpoint = "https://api.resend.com/emails"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	// #nosec G704 -- production endpoint is fixed; override is only used by the local HTTP unit test.
	response, err := client.Do(req)
	if err != nil {
		return errors.New("mail delivery failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("mail delivery failed")
	}
	return nil
}
