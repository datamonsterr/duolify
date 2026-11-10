package characters

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
)

// Handler serves HTTP endpoints for default cast and lesson manifests.
type Handler struct {
	service *Service
}

// NewHandler creates a new characters HTTP handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if (path == "/v1/characters" || strings.HasPrefix(path, "/v1/characters/") && path != "/v1/characters/default-cast") || strings.HasSuffix(path, "/cast") {
		h.library(w, r)
		return
	}
	if r.Method == http.MethodGet && path == "/v1/characters/default-cast" {
		h.handleDefaultCast(w, r)
		return
	}
	if r.Method == http.MethodGet && (strings.HasPrefix(path, "/v1/lessons/") || strings.Contains(path, "/lessons/")) && strings.HasSuffix(path, "/manifest") {
		h.handleManifest(w, r)
		return
	}
	http.NotFound(w, r)
}

func (h *Handler) library(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !CustomCharactersEnabled() {
		http.NotFound(w, r)
		return
	}
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		w.WriteHeader(401)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/characters/creation") || strings.HasPrefix(r.URL.Path, "/v1/characters/jobs") {
		h.creation(w, r, owner)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var result any
	var err error
	decode := func(out any) error {
		r.Body = http.MaxBytesReader(w, r.Body, 12288)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if e := d.Decode(out); e != nil {
			return errors.New("invalid_spec")
		}
		if !errors.Is(d.Decode(new(any)), io.EOF) {
			return errors.New("invalid_spec")
		}
		return nil
	}
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		result, err = h.service.List(r.Context(), owner)
	case len(parts) == 3 && parts[1] == "characters":
		switch r.Method {
		case http.MethodGet:
			result, err = h.service.Get(r.Context(), owner, parts[2])
		case http.MethodPatch:
			var in EditRequest
			err = decode(&in)
			if err == nil {
				result, err = h.service.Edit(r.Context(), owner, parts[2], in)
			} else {
				// Malformed owned edit attempts consume the same durable quota.
				result, err = h.service.Edit(r.Context(), owner, parts[2], EditRequest{})
			}
		case http.MethodDelete:
			err = h.service.Delete(r.Context(), owner, parts[2])
			result = map[string]bool{"deleted": true}
		default:
			w.WriteHeader(405)
			return
		}
	case len(parts) == 4 && parts[1] == "courses" && parts[3] == "cast":
		switch r.Method {
		case http.MethodGet:
			result, err = h.service.ReadCast(r.Context(), owner, parts[2])
		case http.MethodPut:
			var in CastSelection
			err = decode(&in)
			if err == nil {
				result, err = h.service.SelectCast(r.Context(), owner, parts[2], in)
			}
		default:
			w.WriteHeader(405)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		code := err.Error()
		status := 400
		switch code {
		case "not_found":
			status = 404
		case "screening_unavailable":
			status = 503
		case "limit_exceeded":
			status = 429
		case "version_conflict":
			status = 409
		case "invalid_spec", "invalid_cast", "real_person", "third_party_ip", "sexual_content", "age_inappropriate", "prompt_injection":
		default:
			code = "unavailable"
			status = 503
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (h *Handler) handleDefaultCast(w http.ResponseWriter, _ *http.Request) {
	cast := GetDefaultCast()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"default_cast": cast,
		"count":        len(cast),
	})
}

func (h *Handler) handleManifest(w http.ResponseWriter, r *http.Request) {
	learnerID := accounts.LearnerID(r.Context())
	if learnerID == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	lessonID := "fixture-lesson"
	courseID := "c7222222-2222-4222-8222-222222222222"

	for i, part := range parts {
		if part == "lessons" && i+1 < len(parts) {
			lessonID = parts[i+1]
		}
		if part == "courses" && i+1 < len(parts) {
			courseID = parts[i+1]
		}
	}

	manifest, err := h.service.ProjectManifest(r.Context(), learnerID, courseID, lessonID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(manifest)
}

func (h *Handler) creation(w http.ResponseWriter, r *http.Request, owner string) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var result any
	var err error
	decode := func(out any) error {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(out) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
			return errors.New("invalid_spec")
		}
		return nil
	}
	switch {
	case r.URL.Path == "/v1/characters/creation" && r.Method == http.MethodGet:
		result = h.service.CreateAvailability(r.Context())
	case r.URL.Path == "/v1/characters/jobs" && r.Method == http.MethodPost:
		var in CreateRequest
		err = decode(&in)
		if err == nil {
			result, err = h.service.QuoteCreate(r.Context(), owner, in)
		}
	case len(parts) == 4 && parts[2] == "jobs":
		switch r.Method {
		case http.MethodGet:
			result, err = h.service.GetCreate(r.Context(), owner, parts[3])
		case http.MethodDelete:
			result, err = h.service.CancelCreate(r.Context(), owner, parts[3])
		default:
			w.WriteHeader(405)
			return
		}
	case len(parts) == 5 && parts[2] == "jobs" && parts[4] == "confirm" && r.Method == http.MethodPost:
		var in struct {
			QuoteID string `json:"quote_id"`
		}
		err = decode(&in)
		if err == nil {
			var j CreateJob
			j, err = h.service.ConfirmCreate(r.Context(), owner, parts[3], in.QuoteID)
			result = j
			if err == nil && j.State == "queued" {
				go func() {
					ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
					defer cancel()
					_ = h.service.ExecuteCreate(ctx, owner, j.ID)
				}()
			}
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		code := createReason(err.Error())
		status := http.StatusConflict
		switch err.Error() {
		case "creation_disabled":
			status = 503
		case "not_found":
			status = 404
			code = "not_found"
		case "limit_exceeded":
			status = 429
		case "insufficient_credits":
			status = 402
			code = "insufficient_credits"
		case "invalid_spec", "invalid_request_key":
			status = 400
			code = "invalid_spec"
		case "quote_mismatch", "quote_expired":
			code = err.Error()
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}
