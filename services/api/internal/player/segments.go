package player

import (
	"context"
	"encoding/json"
)

// segmentNarration is one pinned-speaker segment's audio state, projected into the Slide's segment.
type segmentNarration struct {
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	AudioURL    string `json:"audio_url,omitempty"`
	AlignURL    string `json:"alignment_url,omitempty"`
	DurationMS  int    `json:"duration_ms,omitempty"`
	Characters  int    `json:"characters,omitempty"`
	VoiceKey    string `json:"voice_key,omitempty"`
	VoiceStatus string `json:"voice_fallback,omitempty"`
}

// segmentNarrations reads the Lesson's segment rows keyed by parent slide and position. Rows
// are owner-only; a slide without segment rows keeps its slide-level narration.
func (s *Service) segmentNarrations(ctx context.Context, owner, lesson string) map[string]map[int]segmentNarration {
	out := map[string]map[int]segmentNarration{}
	rows, err := s.opts.Pool.Query(ctx, `SELECT segment_slide_id,segment_index,slide_id,status,failure_code,duration_ms,characters,actual_voice_key,voice_fallback_reason
		FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND speaker_key<>''`, lesson, owner)
	if err != nil {
		return out // captions stay usable without audio
	}
	defer rows.Close()
	for rows.Next() {
		var slide, id, status, reason, voice, fallback string
		var index, duration, characters int
		if rows.Scan(&slide, &index, &id, &status, &reason, &duration, &characters, &voice, &fallback) != nil {
			continue
		}
		state := segmentNarration{Status: status, VoiceKey: voice, VoiceStatus: fallback}
		if status == "disabled" || status == "failed" {
			state.Reason = reason
		}
		if status == "ready" {
			base := "/v1/lessons/" + lesson + "/slides/" + id + "/narration/"
			state.AudioURL, state.AlignURL, state.DurationMS, state.Characters = base+"audio", base+"alignment", duration, characters
		}
		if out[slide] == nil {
			out[slide] = map[int]segmentNarration{}
		}
		out[slide][index] = state
	}
	return out
}

// decorateSegments adds each segment's narration state without changing its order or captions, and
// reports the slide-level status derived from them ("" when the slide has no segment rows).
func decorateSegments(raw json.RawMessage, states map[int]segmentNarration) (json.RawMessage, string, string) {
	if len(states) == 0 || len(raw) == 0 {
		return raw, "", ""
	}
	var segments []map[string]any
	if json.Unmarshal(raw, &segments) != nil {
		return raw, "", ""
	}
	ready, pending, reason := 0, 0, ""
	for i := range segments {
		state, ok := states[i]
		if !ok {
			continue
		}
		segments[i]["narration"] = state
		switch state.Status {
		case "ready":
			ready++
		case "pending":
			pending++
		default:
			if reason == "" {
				reason = state.Reason
			}
		}
	}
	status := "failed"
	switch {
	case pending > 0:
		status = "pending"
	case ready == len(states):
		status = "ready"
	case reason == "insufficient_credits" || reason == "narration_off" || reason == "not_entitled":
		status = "disabled"
	}
	out, err := json.Marshal(segments)
	if err != nil {
		return raw, "", ""
	}
	return out, status, reason
}
