package characters_test

import (
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
)

func TestSegmentPlanKeepsSpeakerOrderAndTrustedVoiceKeys(t *testing.T) {
	manifest, _ := characters.ProjectDefaultCastManifest("", "")
	segments := []characters.SlideSegment{
		{SpeakerKey: "guide", Text: "Look closely."},
		{SpeakerKey: "supporter", Text: "Earth turns."},
		{SpeakerKey: "stranger", Text: "Not in the Cast."},
		{SpeakerKey: "guide", Text: "<b>markup</b>"},
		{SpeakerKey: "guide", Text: "Again."},
	}
	plan := characters.PlanSegments(manifest.Cast, "s1", segments)
	if len(plan) != 3 {
		t.Fatalf("plan %+v", plan)
	}
	// Positions stay the slide positions even when a segment is dropped, so ids are stable.
	wantIdx := []int{0, 1, 4}
	wantVoice := []string{"voice-provisional-guide", "voice-provisional-supporter", "voice-provisional-guide"}
	for i, p := range plan {
		if p.Index != wantIdx[i] || p.VoiceKey != wantVoice[i] || p.SlideID != "s1" {
			t.Fatalf("segment %d: %+v", i, p)
		}
	}
	if narration.SegmentID("s1", 4) != "s1__s4" || narration.SegmentID("s1", 0) == narration.SegmentID("s1", 1) {
		t.Fatal("segment ids must be stable and ordered")
	}
}

func TestVoiceAllowlistDefaultsAndFixturesOnly(t *testing.T) {
	yes, no := func() bool { return true }, func() bool { return false }
	test, err := narration.LoadVoices(true)
	if err != nil {
		t.Fatal(err)
	}
	if v, r := test.ResolveCharacter("voice-provisional-guide", yes); v != "voice-provisional-guide" || r != "" {
		t.Fatalf("approved voice: %s %s", v, r)
	}
	if v, r := test.ResolveCharacter("voice-provisional-guide", no); v != "default" || r != narration.ReasonNotEntitled {
		t.Fatalf("unentitled: %s %s", v, r)
	}
	for _, raw := range []string{"21m00Tcm4TlvDq8ikWAM", "clone-my-voice", "VOICE-PROVISIONAL-GUIDE", "", "default", "fixture-warm"} {
		if v, r := test.ResolveCharacter(raw, yes); v != "default" || r != narration.ReasonUnavailable {
			t.Fatalf("%q resolved %s %s", raw, v, r)
		}
	}
	// Production admits no fixture voice: every speaker uses the configured default voice.
	prod, err := narration.LoadVoices(false)
	if err != nil {
		t.Fatal(err)
	}
	if v, r := prod.ResolveCharacter("voice-provisional-guide", yes); v != "default" || r != narration.ReasonUnavailable {
		t.Fatalf("production: %s %s", v, r)
	}
	// A Learner can never select a Character voice as the narration voice.
	if _, ok := test.Find("voice-provisional-guide"); ok {
		t.Fatal("character voice is selectable")
	}
	if test.Hash() == prod.Hash() {
		t.Fatal("the config fingerprint must change with the approved set")
	}
}

func TestHashesSeparateVoicesAndDeduplicateIdenticalRequests(t *testing.T) {
	ctx := t.Context()
	resolve := func(voice string) *enginev1.SynthesizeNarrationResponse {
		r, err := narrationtest.Synthesize(&enginev1.SynthesizeNarrationRequest{VoiceKey: voice, SpeakerKey: "guide", ResolveOnly: true})
		if err != nil {
			t.Fatal(ctx, err)
		}
		return r
	}
	text := narration.NormalizeText("Earth  turns.")
	hash := func(r *enginev1.SynthesizeNarrationResponse) string {
		return narration.ContentHash(text, r.VoiceId, r.ModelId, r.SettingsFingerprint)
	}
	guide, supporter := resolve("voice-provisional-guide"), resolve("voice-provisional-supporter")
	if hash(guide) == hash(supporter) || guide.VoiceId == supporter.VoiceId {
		t.Fatal("different voices must hash differently")
	}
	if hash(guide) != hash(resolve("voice-provisional-guide")) {
		t.Fatal("identical requests must deduplicate")
	}
	if narration.ContentHash(text, guide.VoiceId, guide.ModelId, guide.SettingsFingerprint) == narration.ContentHash("Earth turn.", guide.VoiceId, guide.ModelId, guide.SettingsFingerprint) {
		t.Fatal("text is part of the hash")
	}
}

func TestSegmentBudgetsAreStricterThanE14(t *testing.T) {
	if narration.SegmentTimeout != 30*time.Second || narration.SegmentAttempts != 2 {
		t.Fatal("character ceilings changed")
	}
	for _, in := range []*enginev1.UsageLimits{{}, {TimeoutMs: 60000, MaxAttempts: 5}, {TimeoutMs: 10000, MaxAttempts: 1}} {
		in := in
		narration.SegmentLimits(in)
		if in.TimeoutMs == 0 || in.TimeoutMs > 30000 || in.MaxAttempts == 0 || in.MaxAttempts > 2 {
			t.Fatalf("limits not tightened: %+v", in)
		}
	}
	short := &enginev1.UsageLimits{TimeoutMs: 10000, MaxAttempts: 1}
	narration.SegmentLimits(short)
	if short.TimeoutMs != 10000 || short.MaxAttempts != 1 {
		t.Fatal("limits may only be lowered")
	}
}

func TestFallbackEventCarriesNoNamesOrText(t *testing.T) {
	p := narration.FallbackProperties("supporter", narration.ReasonRetired, "2026-11-13.1")
	if len(p) != 3 || p["speaker_key"] != "supporter" || p["reason_code"] != "voice_retired" || p["voice_config_version"] != "2026-11-13.1" {
		t.Fatalf("properties %v", p)
	}
	for _, v := range p {
		if s, _ := v.(string); strings.Contains(s, " ") {
			t.Fatalf("free text in %q", s)
		}
	}
}

func TestNarrationChargesUseOnlyTheNarrationAction(t *testing.T) {
	// Segments are priced as the existing narration action; no Character creation action is involved.
	src, err := readSource("../narration/service.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(src, "character_create") {
		t.Fatal("narration must never spend character_create Credits")
	}
}
