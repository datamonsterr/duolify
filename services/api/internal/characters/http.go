package characters

import (
	"encoding/json"
	"net/http"
	"strings"

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
		learnerID = "00000000-0000-0000-0000-000000000001" // Default test learner if unauthenticated
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
