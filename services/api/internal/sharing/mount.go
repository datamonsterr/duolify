package sharing

import "net/http"

// MountRoutes lets the router mount the sharing routes without importing this package.
func (s *Service) MountRoutes(mux *http.ServeMux, protect, optional func(http.Handler) http.Handler) {
	Mount(mux, s, protect, optional)
}

// Mount registers the owner-only routes (behind protect) and the anonymous public preview
// (behind optional, which may add the signed-in viewer). Shared by the API and the e2e server.
func Mount(mux *http.ServeMux, s *Service, protect, optional func(http.Handler) http.Handler) {
	owner := protect(NewHandler(s))
	for _, pattern := range []string{
		"POST /v1/courses/{id}/publication-drafts", "GET /v1/courses/{id}/publication-drafts/{draftId}",
		"POST /v1/courses/{id}/publication-drafts/{draftId}/screen", "POST /v1/courses/{id}/publish",
		"GET /v1/courses/{id}/share-link", "POST /v1/courses/{id}/share-link",
		"DELETE /v1/courses/{id}/share-link", "POST /v1/courses/{id}/share-link/rotate",
	} {
		mux.Handle(pattern, owner)
	}
	mux.Handle("GET /v1/public/shares/{token}", optional(NewPreviewHandler(s)))
	enroll := protect(NewEnrollHandler(s))
	mux.Handle("POST /v1/public/shares/{token}/enroll", enroll)
	mux.Handle("GET /v1/public/shares/{token}/enrollment", enroll)
	mountMissing(mux, s, protect)
}
