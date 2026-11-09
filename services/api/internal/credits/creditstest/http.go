package creditstest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

// Handler exposes the normalized provider contract from a separate fixture
// server. It is deliberately not a real Stripe adapter or deployment endpoint.
func (s *Stripe) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		var request credits.PaymentRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		session, err := s.CreateSession(r.Context(), request)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(session)
	})
	mux.HandleFunc("GET /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		session, err := s.RetrieveSession(r.Context(), r.PathValue("id"))
		if err != nil {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(session)
	})
	return mux
}

// RemoteStripe exercises real HTTP transport against an isolated fake server.
type RemoteStripe struct {
	Endpoint string
	Client   *http.Client
}

// CreateSession sends immutable configured terms and an order idempotency key.
func (s RemoteStripe) CreateSession(ctx context.Context, request credits.PaymentRequest) (credits.PaymentSession, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return credits.PaymentSession{}, err
	}
	return s.call(ctx, http.MethodPost, "/sessions", raw)
}

// RetrieveSession fetches canonical state from the fixture server.
func (s RemoteStripe) RetrieveSession(ctx context.Context, id string) (credits.PaymentSession, error) {
	return s.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id), nil)
}
func (s RemoteStripe) call(ctx context.Context, method, path string, raw []byte) (credits.PaymentSession, error) {
	var session credits.PaymentSession
	request, err := http.NewRequestWithContext(ctx, method, s.Endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return session, err
	}
	response, err := s.Client.Do(request)
	if err != nil {
		return session, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return session, errors.New("fixture provider unavailable")
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&session)
	return session, err
}
