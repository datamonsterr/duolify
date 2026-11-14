package characters_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestPublishedCastContentIdentityPrivacyAndImmutability(t *testing.T) {
	m, _ := characters.ProjectDefaultCastManifest("", "")
	source := m.Cast
	source.Speakers["guide"] = characters.PinnedSpeaker{SpeakerKey: "guide", Role: "guide", CharacterID: "17111111-1111-4111-8111-111111111111", CharacterVersion: 99, Spec: characters.DefaultGuideFixture()}
	original, _ := json.Marshal(source)
	exported, err := characters.ExportCast(source)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if strings.Contains(string(raw), "17111111") || strings.Contains(string(raw), `"character_version":99`) {
		t.Fatal("private version link leaked")
	}
	if err := characters.ValidateExportCast(raw); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(source)
	if string(after) != string(original) {
		t.Fatal("projection mutated private payload")
	}
	changed := source
	changed.VoiceConfigVersion = "v-next"
	next, err := characters.ExportCast(changed)
	if err != nil {
		t.Fatal(err)
	}
	if next.Speakers["guide"].CharacterID == exported.Speakers["guide"].CharacterID {
		t.Fatal("version not covered by identity hash")
	}
	snapshot, _ := json.Marshal(sharing.Snapshot{Title: "Sky", Cast: &exported})
	if err := sharing.CheckAllowlist(snapshot); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"owner_id", "raw_prompt", "screening", "library_version"} {
		bad := strings.Replace(string(raw), `"spec":{`, `"spec":{"`+key+`":"private",`, 1)
		if characters.ValidateExportCast([]byte(bad)) == nil {
			t.Fatalf("accepted private field %s", key)
		}
	}
	bad := exported
	speaker := bad.Speakers["guide"]
	speaker.CharacterID = "17111111-1111-4111-8111-111111111111"
	bad.Speakers["guide"] = speaker
	rejected, _ := json.Marshal(bad)
	if characters.ValidateExportCast(rejected) == nil {
		t.Fatal("private ID accepted")
	}
}

func TestPublishedCastTakedownIsAnOverlayAndPreservesOtherSlots(t *testing.T) {
	m, _ := characters.ProjectDefaultCastManifest("", "")
	cast, err := characters.ExportCast(m.Cast)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(cast)
	safe := characters.SafeRuntimeCast(cast, map[string]bool{cast.Speakers["guide"].CharacterID: true})
	after, _ := json.Marshal(cast)
	if string(before) != string(after) {
		t.Fatal("suppression rewrote immutable content")
	}
	if !safe.FallbackUsed || safe.Speakers["guide"].CharacterID != "default-guide" || safe.Speakers["supporter"].CharacterID != cast.Speakers["supporter"].CharacterID {
		t.Fatalf("unsafe overlay: %+v", safe)
	}
}

func TestPublicCharacterExportsRequireEveryReleaseGate(t *testing.T) {
	keys := []string{"CHARACTER_PUBLIC_EXPORTS_ENABLED", "CHARACTER_REDISTRIBUTION_APPROVED", "CHARACTER_VOICES_APPROVED", "CHARACTER_PUBLICATION_LIVE_EVAL_APPROVED"}
	if sharing.DefaultConfig().CharacterExportsEnabled {
		t.Fatal("exports enabled by default")
	}
	for _, missing := range append([]string{"all"}, keys...) {
		cfg := sharing.ConfigFromEnv(func(key string) string {
			for _, k := range keys {
				if key == k && missing != k && missing != "all" {
					return "true"
				}
			}
			return ""
		})
		if cfg.CharacterExportsEnabled {
			t.Fatalf("enabled without %s evidence", missing)
		}
	}
	enabled := sharing.ConfigFromEnv(func(key string) string {
		for _, k := range keys {
			if key == k {
				return "true"
			}
		}
		return ""
	})
	if !enabled.CharacterExportsEnabled {
		t.Fatal("all explicit approvals ignored")
	}
}

func TestGeneratedPlayerObjectsValidateAndExcludeGrading(t *testing.T) {
	slides := []player.GeneratedSlide{{ID: "s1", HTML: `<div id="root"><p>Observe the sky.</p></div>`, Narration: "Observe the sky.", DurationMS: 3000}}
	key, docs, err := player.GeneratedObjects("lessons/generated/fixture", slides, "https://assets.example.test")
	if err != nil || key != "lessons/generated/fixture/manifest.json" || len(docs) != 2 {
		t.Fatalf("materialization: %s %v", key, err)
	}
	var manifest player.ObjectManifest
	if json.Unmarshal(docs[key], &manifest) != nil || player.ValidateManifest(manifest, "https://assets.example.test") != nil {
		t.Fatal("invalid generated manifest")
	}
	slides[0].HTML = `<iframe src="https://unsafe.test"></iframe>`
	if _, _, err = player.GeneratedObjects("lessons/generated/fixture", slides, "https://assets.example.test"); err == nil {
		t.Fatal("untrusted slide accepted")
	}
}
