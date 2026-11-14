package narration

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

// voicesJSON is the approved curated no-surcharge voice set (ADR 0012 E14-05): the default
// voice plus test fixtures. Prices never live here; they come from the Pricing config.
//
//go:embed voices.json
var voicesJSON []byte

const (
	// BenefitCuratedVoices is the exact plan benefit that entitles curated voices (Plus, Pro).
	BenefitCuratedVoices = "Curated voices at the same synthesis rate"
	// BenefitNoUpsell withholds upgrade guidance from the plan (ADR 0011 E5-06).
	BenefitNoUpsell = "No upsell banners"
)

// ErrVoice rejects an unknown or unapproved voice key.
var ErrVoice = errors.New("narration voice unavailable")

// Voice is one approved logical voice. Vendor voice ids stay in the Engine config.
type Voice struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Curated bool   `json:"curated"`
	// Fixture voices exist only for tests and are dropped unless fixtures are allowed.
	Fixture bool `json:"fixture,omitempty"`
	// Character voices serve pinned Cast speakers (E17-08). A Learner can never choose one as the
	// narration voice, and it is only a logical key: vendor ids stay in the Engine config.
	Character bool `json:"character,omitempty"`
}

// VoiceSet is the approved set with a fingerprint of its content.
type VoiceSet struct {
	Version string
	// Voices are the Learner-selectable voices; Characters are the approved speaker voices.
	Voices     []Voice
	Characters []Voice
	hash       string
}

// LoadVoices parses the embedded config. Fixture voices are included only for tests.
func LoadVoices(fixtures bool) (VoiceSet, error) {
	var raw struct {
		Version string  `json:"version"`
		Voices  []Voice `json:"voices"`
	}
	if err := json.Unmarshal(voicesJSON, &raw); err != nil {
		return VoiceSet{}, err
	}
	set := VoiceSet{Version: raw.Version}
	h := sha256.New()
	h.Write([]byte(raw.Version))
	for _, v := range raw.Voices {
		if v.Fixture && !fixtures {
			continue
		}
		if v.Key == "" || v.Key != strings.ToLower(v.Key) {
			return VoiceSet{}, errors.New("narration voices: invalid key")
		}
		curated := "0"
		if v.Curated {
			curated = "1"
		}
		if v.Character {
			set.Characters = append(set.Characters, v)
			h.Write([]byte("|character:" + v.Key + ":" + curated))
			continue
		}
		set.Voices = append(set.Voices, v)
		h.Write([]byte("|" + v.Key + ":" + curated))
	}
	if len(set.Voices) == 0 || set.Voices[0].Key != VoiceKey || set.Voices[0].Curated {
		return VoiceSet{}, errors.New("narration voices: the default voice must come first")
	}
	set.hash = hex.EncodeToString(h.Sum(nil))
	return set, nil
}

// Hash fingerprints the approved set so a consent given for one set is not reused for another.
func (s VoiceSet) Hash() string { return s.hash }

// Find returns an approved voice.
func (s VoiceSet) Find(key string) (Voice, bool) {
	for _, v := range s.Voices {
		if v.Key == key {
			return v, true
		}
	}
	return Voice{}, false
}

// FindCharacter returns an approved Character speaker voice.
func (s VoiceSet) FindCharacter(key string) (Voice, bool) {
	for _, v := range s.Characters {
		if v.Key == key {
			return v, true
		}
	}
	return Voice{}, false
}

// AllKeys lists every approved logical key (Learner voices, then Character speaker voices).
func (s VoiceSet) AllKeys() []string {
	out := s.Keys()
	for _, v := range s.Characters {
		out = append(out, v.Key)
	}
	return out
}

// Keys lists the approved Learner-selectable voice keys in config order.
func (s VoiceSet) Keys() []string {
	out := make([]string, 0, len(s.Voices))
	for _, v := range s.Voices {
		out = append(out, v.Key)
	}
	return out
}

func hasBenefit(plan *credits.FundedPlan, name string) bool {
	if plan == nil || !plan.Active || plan.Revoked {
		return false
	}
	for _, b := range plan.Benefits {
		if strings.EqualFold(b, name) {
			return true
		}
	}
	return false
}

// EntitledToCuratedVoices checks the verified funded plan; balance and client claims never count.
func EntitledToCuratedVoices(plan *credits.FundedPlan) bool {
	return hasBenefit(plan, BenefitCuratedVoices)
}

// UpsellSuppressed is true for plans whose benefits include "No upsell banners".
func UpsellSuppressed(plan *credits.FundedPlan) bool { return hasBenefit(plan, BenefitNoUpsell) }
