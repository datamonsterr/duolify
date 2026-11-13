// Package narrationtest is a deterministic Go twin of the Rust mock SpeechProvider
// for disposable test servers only. Production never imports it.
package narrationtest

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	voiceID    = "mock-default-voice"
	modelID    = "eleven_flash_v2_5"
	msPerChar  = 50
	sampleRate = 8000
)

func toU32(n int) uint32 {
	if n < 0 || n > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(n)
}

func fingerprint() string {
	h := uint64(0xcbf29ce484222325)
	for _, b := range []byte(fmt.Sprintf("v1|%s|%.3f|%.3f|%.3f", voiceID, 0.5, 0.75, 1.0)) {
		h = (h ^ uint64(b)) * 0x100000001b3
	}
	return fmt.Sprintf("fp1-%016x", h)
}

// Synthesize mirrors engine_core::narration::MockSpeechProvider and the RPC response.
func Synthesize(req *enginev1.SynthesizeNarrationRequest) (*enginev1.SynthesizeNarrationResponse, error) {
	if req.GetVoiceKey() != "default" {
		return nil, status.Error(codes.FailedPrecondition, "voice unavailable")
	}
	model := req.GetLimits().GetModel()
	if model != "" && model != modelID {
		return nil, status.Error(codes.FailedPrecondition, "quote route differs from evaluated route")
	}
	out := &enginev1.SynthesizeNarrationResponse{VoiceId: voiceID, ModelId: modelID, SettingsFingerprint: fingerprint()}
	if req.GetResolveOnly() {
		return out, nil
	}
	if strings.TrimSpace(req.GetText()) == "" {
		return nil, status.Error(codes.InvalidArgument, "narration text missing")
	}
	runes := []rune(req.GetText())
	n := toU32(len(runes))
	duration := n * msPerChar
	samples := (sampleRate / 1000) * duration
	audio := make([]byte, 0, 44+samples)
	audio = append(audio, "RIFF"...)
	audio = binary.LittleEndian.AppendUint32(audio, 36+samples)
	audio = append(audio, "WAVEfmt "...)
	audio = binary.LittleEndian.AppendUint32(audio, 16)
	audio = binary.LittleEndian.AppendUint16(audio, 1)
	audio = binary.LittleEndian.AppendUint16(audio, 1)
	audio = binary.LittleEndian.AppendUint32(audio, sampleRate)
	audio = binary.LittleEndian.AppendUint32(audio, sampleRate)
	audio = binary.LittleEndian.AppendUint16(audio, 1)
	audio = binary.LittleEndian.AppendUint16(audio, 8)
	audio = append(audio, "data"...)
	audio = binary.LittleEndian.AppendUint32(audio, samples)
	for i := uint32(0); i < samples; i++ {
		if (i/20)%2 == 0 {
			audio = append(audio, 136)
		} else {
			audio = append(audio, 120)
		}
	}
	alignment := &enginev1.NarrationAlignment{}
	for i, r := range runes {
		alignment.Characters = append(alignment.Characters, string(r))
		alignment.StartMs = append(alignment.StartMs, uint32(i)*msPerChar)
		alignment.EndMs = append(alignment.EndMs, uint32(i+1)*msPerChar)
	}
	out.Audio, out.AudioFormat, out.DurationMs, out.Alignment = audio, "audio/wav", duration, alignment
	out.Characters, out.AudioBytes = n, toU32(len(audio))
	out.Usage = &enginev1.Usage{Provider: "elevenlabs", Model: modelID, Component: "audio", NarrationCharacters: n, Attempt: 1, Delivered: true}
	return out, nil
}

// Engine is an in-process gRPC EngineService with injectable per-text faults.
type Engine struct {
	enginev1.UnimplementedEngineServiceServer
	mu sync.Mutex
	// Faults maps exact narration text to "engine" (provider outage) or "alignment" (gap).
	faults    map[string]string
	synthCall int
	texts     []string
}

// SetFaults replaces the fault table.
func (e *Engine) SetFaults(f map[string]string) { e.mu.Lock(); e.faults = f; e.mu.Unlock() }

// CallsContaining counts synthesis requests whose text contains the marker, so parallel
// e2e runs can observe only their own salted narration.
func (e *Engine) CallsContaining(marker string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, t := range e.texts {
		if strings.Contains(t, marker) {
			n++
		}
	}
	return n
}

// Calls returns how many non-resolve synthesis requests reached the provider.
func (e *Engine) Calls() int { e.mu.Lock(); defer e.mu.Unlock(); return e.synthCall }

// SynthesizeNarration implements the engine RPC.
func (e *Engine) SynthesizeNarration(_ context.Context, req *enginev1.SynthesizeNarrationRequest) (*enginev1.SynthesizeNarrationResponse, error) {
	e.mu.Lock()
	fault := e.faults[req.GetText()]
	if !req.GetResolveOnly() {
		e.synthCall++
		e.texts = append(e.texts, req.GetText())
	}
	e.mu.Unlock()
	if fault == "engine" && !req.GetResolveOnly() {
		return nil, status.Error(codes.Unavailable, "speech provider unavailable")
	}
	out, err := Synthesize(req)
	if err == nil && fault == "alignment" && len(out.GetAlignment().GetCharacters()) > 1 {
		a := out.Alignment
		a.Characters, a.StartMs, a.EndMs = a.Characters[1:], a.StartMs[1:], a.EndMs[1:]
	}
	return out, err
}
