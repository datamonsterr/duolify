//go:build integration

package narration_test

import (
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
)

func TestExhaustedBudgetKeepsRegenerationRequestsQueuedWithoutALiveCall(t *testing.T) {
	r := newBudgetRig(t)
	ctx := t.Context()
	svc := r.build(narration.Budget{Limit: 20, StopPercent: 95, CycleDay: 1})
	// A lost asset whose text and settings still match its hash, so only the budget can stop it.
	resolved, err := r.engine.SynthesizeNarration(ctx, &enginev1.SynthesizeNarrationRequest{SlideId: "regenerate", VoiceKey: narration.VoiceKey, ResolveOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	text := "The sky looks blue because air scatters short wavelengths of sunlight."
	hash := narration.ContentHash(text, resolved.VoiceId, resolved.ModelId, resolved.SettingsFingerprint)
	pool := r.f.DB.Pool
	if _, err = pool.Exec(ctx, `INSERT INTO narration_assets(content_hash,text_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms,state,created_at)
		VALUES ($1,repeat('b',64),$2,$3,$4,'lessons/narration/x','lessons/narration/y',70,1,1,'missing',now() - interval '90 days')`, hash, resolved.VoiceId, resolved.ModelId, resolved.SettingsFingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,voice_key,status,narration_text,content_hash) VALUES ($1,$2,'budget-regen','default','pending',$3,$4)`, lesson, owner, text, hash); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO narration_regenerations(content_hash) VALUES ($1)`, hash); err != nil {
		t.Fatal(err)
	}
	done, err := svc.RegenerateMissing(ctx)
	if err != nil || done != 0 {
		t.Fatalf("regenerate: %d %v", done, err)
	}
	if r.engine.Calls() != 0 {
		t.Fatal("an exhausted budget must not reach the provider for regeneration either")
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM narration_regenerations WHERE content_hash=$1`, hash).Scan(&status); err != nil || status != "requested" {
		t.Fatalf("the request must stay queued: %q %v", status, err)
	}
}
