package narration

import (
	"context"
	"fmt"
	"regexp"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// Character narration limits (E17-08): the stricter of E14's and the Character ceilings. The
// provider call is bounded at 30 s with at most two attempts (one retry); every segment of a Lesson
// shares the one per-Lesson reservation and its quoted cap.
const (
	SegmentTimeout  = 30 * time.Second
	SegmentAttempts = 2
	// MaxSegmentsPerSlide mirrors the manifest bound.
	MaxSegmentsPerSlide = 12
)

// Fixed reasons recorded when a Character voice is replaced by the safe default voice.
const (
	ReasonRetired       = "voice_retired"
	ReasonUnavailable   = "voice_unavailable"
	ReasonNotEntitled   = "voice_not_entitled"
	segmentIDSeparator  = "__s"
	maxSlideIDWithIndex = 100
)

var voiceKeyFormat = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// Segment is one pinned-speaker line of a Slide, in its original order.
type Segment struct {
	SlideID    string
	Index      int
	SpeakerKey string
	// VoiceKey is the Character's logical voice key from its pinned spec.
	VoiceKey string
	// Text is plain dialogue validated at job time; it is the caption and the synthesized text.
	Text string
}

// SegmentSource lists a Lesson's pinned-speaker segments (implemented by characters).
type SegmentSource interface {
	NarrationSegments(ctx context.Context, owner, lessonID string) ([]Segment, error)
}

// SegmentID is the stable per-slide, per-position id of a segment's narration row.
func SegmentID(slideID string, index int) string {
	return fmt.Sprintf("%s%s%d", slideID, segmentIDSeparator, index)
}

// ValidSegment bounds a segment before it is stored.
func ValidSegment(g Segment) bool {
	return g.Index >= 0 && g.Index < MaxSegmentsPerSlide && len(SegmentID(g.SlideID, g.Index)) <= maxSlideIDWithIndex && g.SpeakerKey != "" && len(g.SpeakerKey) <= 24
}

// ResolveCharacter picks the logical voice for new synthesis. Only the approved config resolves
// a Character's voice key: an unknown key (including a raw provider id) or a voice that is not
// approved in this deployment uses the configured default, and so does a curated voice for a plan
// without that benefit. The reason is a fixed code, never free text.
func (s VoiceSet) ResolveCharacter(key string, entitled func() bool) (voice, reason string) {
	v, ok := s.FindCharacter(key)
	if !ok || !voiceKeyFormat.MatchString(key) {
		return VoiceKey, ReasonUnavailable
	}
	if v.Curated && !entitled() {
		return VoiceKey, ReasonNotEntitled
	}
	return v.Key, ""
}

// SegmentLimits tightens usage limits to the Character ceilings; they can only be lowered.
func SegmentLimits(l *enginev1.UsageLimits) {
	ms, attempts := uint32(SegmentTimeout/time.Millisecond), uint32(SegmentAttempts)
	if l.TimeoutMs == 0 || l.TimeoutMs > ms {
		l.TimeoutMs = ms
	}
	if l.MaxAttempts == 0 || l.MaxAttempts > attempts {
		l.MaxAttempts = attempts
	}
}

// FallbackProperties are the content-free properties of character_voice_fallback.
func FallbackProperties(speakerKey, reason, configVersion string) map[string]any {
	return map[string]any{"speaker_key": speakerKey, "reason_code": reason, "voice_config_version": configVersion}
}

// VoiceConfigVersion is the approved voice config version recorded with each actual voice.
func (s *Service) VoiceConfigVersion() string { return s.voices.Version }

// segmentsBySlide groups the Lesson's pinned-speaker segments; a missing source means none.
func (s *Service) segmentsBySlide(ctx context.Context, owner, lessonID string) map[string][]Segment {
	out := map[string][]Segment{}
	if s.opts.Segments == nil {
		return out
	}
	list, err := s.opts.Segments.NarrationSegments(ctx, owner, lessonID)
	if err != nil {
		return out // slides without segments keep their E14 narration
	}
	for _, g := range list {
		if ValidSegment(g) {
			out[g.SlideID] = append(out[g.SlideID], g)
		}
	}
	return out
}

// segmentVoice normalizes the stored requested voice key.
func segmentVoice(key string) string {
	if voiceKeyFormat.MatchString(key) {
		return key
	}
	return VoiceKey
}
