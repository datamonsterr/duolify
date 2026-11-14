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
	invites := protect(NewInviteHandler(s))
	for _, pattern := range InvitePatterns {
		mux.Handle(pattern, invites)
	}
	// E15-07 Marketplace: public reads are anonymous-safe (the session only adds the block filter),
	// enrollment and the owner listing routes need a session. Every route is a neutral 404 while the flag is off.
	market := NewMarketHandler(s)
	mux.Handle("GET /v1/marketplace/courses", optional(market))
	mux.Handle("GET /v1/marketplace/courses/{id}", optional(market))
	mux.Handle("POST /v1/marketplace/courses/{id}/enroll", protect(market))
	listing := protect(NewListingHandler(s))
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		mux.Handle(method+" /v1/courses/{id}/marketplace-listing", listing)
	}
}
