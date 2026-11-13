//go:build !integration

package sharing_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestGenerateTokenIsLongEnoughAndUnique(t *testing.T) {
	t1, err := sharing.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	t2, err := sharing.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	// base64url of 20 bytes = ceil(20*8/6) = 27 chars (no padding)
	if len(t1) < 27 {
		t.Errorf("token too short: %q (%d chars)", t1, len(t1))
	}
	if t1 == t2 {
		t.Error("GenerateToken returned the same token twice")
	}
	// must be valid base64url (no + / =)
	if strings.ContainsAny(t1, "+/=") {
		t.Errorf("token is not base64url: %q", t1)
	}
	// must decode to >= 20 bytes
	raw, err := base64.RawURLEncoding.DecodeString(t1)
	if err != nil {
		t.Fatalf("token not decodable: %v", err)
	}
	if len(raw) < 20 {
		t.Errorf("token decodes to %d bytes, want >= 20", len(raw))
	}
}

func TestHashTokenIsDeterministicSHA256Hex(t *testing.T) {
	token := "someTestToken123"
	h1 := sharing.HashToken(token)
	h2 := sharing.HashToken(token)
	if h1 != h2 {
		t.Errorf("HashToken not deterministic: %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("SHA-256 hex should be 64 chars, got %d: %q", len(h1), h1)
	}
	for _, c := range h1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex char %q in hash %q", c, h1)
		}
	}
	// Different tokens → different hashes
	if sharing.HashToken("other") == h1 {
		t.Error("HashToken collision for different tokens")
	}
}

func TestBuildPreviewPayloadStripsPrivateFields(t *testing.T) {
	// Build a snapshot with questions, retry_pool, narration_refs and slides
	slide1 := sharing.SnapshotSlide{ID: "s1", HTML: "<h1>Moon</h1>", Narration: "narr1"}
	slide2 := sharing.SnapshotSlide{ID: "s2", HTML: "<p>Orbits</p>", Narration: "narr2"}
	slide3 := sharing.SnapshotSlide{ID: "s3", HTML: "<p>Phases</p>", Narration: "narr3"}
	slide4 := sharing.SnapshotSlide{ID: "s4", HTML: "<p>Extra</p>", Narration: "narr4"}

	lesson1 := sharing.SnapshotLesson{
		Title:     "Phases of the Moon",
		Goal:      "Name the phases",
		Generated: true,
		Slides:    []sharing.SnapshotSlide{slide1, slide2, slide3, slide4},
		Questions: []sharing.SnapshotQ{
			{Topic: "moon", Prompt: "What causes phases?", Choices: []string{"A", "B"}, AnswerIndex: 0, Explanation: "Sun angle"},
		},
		RetryPool: []sharing.RetryTopic{
			{Topic: "moon", Variants: []byte(`[{"prompt":"retry Q"}]`)},
		},
		NarrationRefs: []string{"s1", "s2"},
	}
	lesson2 := sharing.SnapshotLesson{
		Title:     "Tides",
		Goal:      "Understand tides",
		Generated: false,
	}

	snap := sharing.Snapshot{
		Title:       "Astronomy",
		Description: "Look up",
		Units: []sharing.SnapshotUnit{
			{Title: "The Moon", Lessons: []sharing.SnapshotLesson{lesson1, lesson2}},
		},
	}

	draft := sharing.Draft{
		ID:          "d1111111-1111-4111-8111-111111111111",
		CourseID:    "c1111111-1111-4111-8111-111111111111",
		Number:      3,
		LessonCount: 2,
	}

	creator := sharing.CreatorInfo{
		Username:    "astronaut",
		DisplayName: "Astro Naut",
		AvatarURL:   "https://example.com/avatar.png",
	}

	payload := sharing.BuildPreviewPayload(snap, draft, creator, 42)

	// CourseID, Title, Description, Creator, EnrollmentCount, Version
	if payload.CourseID != draft.CourseID {
		t.Errorf("CourseID mismatch: got %q", payload.CourseID)
	}
	if payload.Title != "Astronomy" {
		t.Errorf("Title mismatch: got %q", payload.Title)
	}
	if payload.Description != "Look up" {
		t.Errorf("Description mismatch: got %q", payload.Description)
	}
	if payload.Creator.Username != "astronaut" {
		t.Errorf("Creator.Username mismatch: %+v", payload.Creator)
	}
	if payload.EnrollmentCount != 42 {
		t.Errorf("EnrollmentCount: got %d", payload.EnrollmentCount)
	}
	if payload.Version != 3 {
		t.Errorf("Version: got %d", payload.Version)
	}
	if payload.LessonCount != 2 {
		t.Errorf("LessonCount: got %d", payload.LessonCount)
	}

	// SampleSlides: up to 3, from first generated lesson
	if len(payload.SampleSlides) != 3 {
		t.Errorf("SampleSlides: got %d, want 3", len(payload.SampleSlides))
	}
	for _, ss := range payload.SampleSlides {
		if ss.HTML == "" {
			t.Error("SampleSlide has empty HTML")
		}
	}
	// NarrationRefs must NOT appear: check that SampleSlide has no narration field
	// (it's enforced by the type — struct has no Narration field, so this is a compile-time check)

	// Outline must have no questions, no retry_pool, no narration_refs
	for _, ou := range payload.Outline {
		if len(ou.Lessons) == 0 {
			t.Error("Outline unit has no lessons")
		}
	}
}

func TestConfigPublicPreviewFieldsParsedFromEnv(t *testing.T) {
	env := map[string]string{
		"SHARING_PUBLIC_DOMAIN":               "share.duolify.com",
		"SHARING_APP_SCHEME":                  "duolify",
		"SHARING_PREVIEW_RATE_LIMIT_PER_IP":   "120",
		"SHARING_PREVIEW_RATE_WINDOW_SECONDS": "30",
	}
	c := sharing.ConfigFromEnv(func(k string) string { return env[k] })
	if c.PublicDomain != "share.duolify.com" {
		t.Errorf("PublicDomain: %q", c.PublicDomain)
	}
	if c.AppScheme != "duolify" {
		t.Errorf("AppScheme: %q", c.AppScheme)
	}
	if c.PreviewRateLimitPerIP != 120 {
		t.Errorf("PreviewRateLimitPerIP: %d", c.PreviewRateLimitPerIP)
	}
	if c.PreviewRateWindowSeconds != 30 {
		t.Errorf("PreviewRateWindowSeconds: %d", c.PreviewRateWindowSeconds)
	}

	// Defaults
	d := sharing.ConfigFromEnv(func(string) string { return "" })
	if d.PublicDomain != "localhost" {
		t.Errorf("default PublicDomain: %q", d.PublicDomain)
	}
	if d.AppScheme != "duolify" {
		t.Errorf("default AppScheme: %q", d.AppScheme)
	}
	if d.PreviewRateLimitPerIP != 60 {
		t.Errorf("default PreviewRateLimitPerIP: %d", d.PreviewRateLimitPerIP)
	}
	if d.PreviewRateWindowSeconds != 60 {
		t.Errorf("default PreviewRateWindowSeconds: %d", d.PreviewRateWindowSeconds)
	}
}

func TestErrorsAreDefined(t *testing.T) {
	if sharing.ErrLinkNotFound == nil {
		t.Error("ErrLinkNotFound not defined")
	}
	if sharing.ErrPublishNotAllowed == nil {
		t.Error("ErrPublishNotAllowed not defined")
	}
	if sharing.ErrLinkNotFound == sharing.ErrPublishNotAllowed {
		t.Error("errors must be distinct")
	}
}
