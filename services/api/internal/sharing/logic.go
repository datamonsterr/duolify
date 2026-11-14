package sharing

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// Outcome is the validated, fail-closed screening result.
type Outcome struct {
	Verdict      string
	ReasonCodes  []string
	CostNanos    int64
	Attempts     int
	FallbackUsed bool
}

var allowedKeys = map[string]bool{
	"title": true, "description": true, "units": true, "lessons": true, "goal": true, "generated": true,
	"slides": true, "questions": true, "retry_pool": true, "narration_refs": true, "id": true, "html": true,
	"narration": true, "topic": true, "prompt": true, "choices": true, "answer_index": true,
	"explanation": true, "variants": true, "question_id": true,
}

// CheckAllowlist rejects any JSON object key outside the snapshot allowlist, at any depth.
func CheckAllowlist(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ErrOutOfAllowlist
	}
	var walk func(any) bool
	walk = func(n any) bool {
		switch t := n.(type) {
		case map[string]any:
			for k, c := range t {
				if !allowedKeys[k] || !walk(c) {
					return false
				}
			}
		case []any:
			for _, c := range t {
				if !walk(c) {
					return false
				}
			}
		}
		return true
	}
	if !walk(v) {
		return ErrOutOfAllowlist
	}
	return nil
}

var (
	emailRe  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}`)
	phoneRe  = regexp.MustCompile(`(?:\+|\b)\d[\d\s().\-]{7,}\d\b`)
	secretRe = regexp.MustCompile(`sk-[A-Za-z0-9_\-]{20,}|AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{30,}|xox[baprs]-[A-Za-z0-9\-]{10,}|AIza[0-9A-Za-z_\-]{35}|-----BEGIN [A-Z ]*PRIVATE KEY-----|(?i:api[_-]?key|secret|token|password)\s*[:=]\s*["']?[A-Za-z0-9/+_\-]{16,}`)
)

func textFindings(text, loc string, add func(kind, loc string)) {
	if emailRe.MatchString(text) {
		add(KindPersonalData, loc)
	}
	for _, m := range phoneRe.FindAllString(text, -1) {
		digits := 0
		for _, r := range m {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits >= 9 && digits <= 15 {
			add(KindPersonalData, loc)
			break
		}
	}
	if secretRe.MatchString(text) {
		add(KindSecret, loc)
	}
}

// ScanFindings flags emails, phone numbers and API keys. Findings carry a kind and a position,
// never the matched text.
func ScanFindings(s Snapshot) []Finding {
	var out []Finding
	seen := map[Finding]bool{}
	add := func(kind, loc string) {
		f := Finding{Kind: kind, Location: loc}
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	textFindings(s.Title+"\n"+s.Description, "course", add)
	n := 0
	for _, u := range s.Units {
		textFindings(u.Title, "course", add)
		for _, l := range u.Lessons {
			n++
			base := fmt.Sprintf("lesson %d", n)
			textFindings(l.Title+"\n"+l.Goal, base, add)
			for i, sl := range l.Slides {
				textFindings(sl.HTML+"\n"+sl.Narration, fmt.Sprintf("%s · slide %d", base, i+1), add)
			}
			for _, q := range l.Questions {
				textFindings(q.Prompt+"\n"+strings.Join(q.Choices, "\n")+"\n"+q.Explanation, base+" · test", add)
			}
			for _, r := range l.RetryPool {
				textFindings(string(r.Variants), base+" · retry", add)
			}
		}
	}
	return out
}

type sampleLesson struct {
	N         int           `json:"n"`
	Title     string        `json:"title"`
	Slides    []sampleSlide `json:"slides"`
	Questions []string      `json:"questions,omitempty"`
}

type sampleSlide struct {
	HTML      string `json:"html"`
	Narration string `json:"narration,omitempty"`
}

func cut(s string, left *int) string {
	if *left <= 0 {
		return ""
	}
	if len(s) > *left {
		s = strings.ToValidUTF8(s[:*left], "")
	}
	*left -= len(s)
	return s
}

// BuildSample picks at most cfg.SampleMaxLessons generated lessons, evenly spread, and caps the
// total text at cfg.SampleMaxBytes so the platform-funded call stays inside its token envelope.
func BuildSample(s Snapshot, cfg Config) (string, int) {
	type ref struct {
		n int
		l SnapshotLesson
	}
	var gen []ref
	var outline []string
	n := 0
	for _, u := range s.Units {
		outline = append(outline, u.Title)
		for _, l := range u.Lessons {
			n++
			outline = append(outline, l.Title)
			if l.Generated {
				gen = append(gen, ref{n, l})
			}
		}
	}
	pick := gen
	if len(gen) > cfg.SampleMaxLessons {
		pick = make([]ref, 0, cfg.SampleMaxLessons)
		for i := 0; i < cfg.SampleMaxLessons; i++ {
			pick = append(pick, gen[i*len(gen)/cfg.SampleMaxLessons])
		}
	}
	per := 0
	if len(pick) > 0 {
		per = cfg.SampleMaxBytes / len(pick)
	}
	out := struct {
		Title       string         `json:"title"`
		Description string         `json:"description"`
		Outline     []string       `json:"outline"`
		Lessons     []sampleLesson `json:"lessons"`
	}{Title: s.Title, Description: s.Description, Outline: outline, Lessons: []sampleLesson{}}
	for _, p := range pick {
		left := per
		sl := sampleLesson{N: p.n, Title: p.l.Title}
		for _, sd := range p.l.Slides {
			sl.Slides = append(sl.Slides, sampleSlide{HTML: cut(sd.HTML, &left), Narration: cut(sd.Narration, &left)})
		}
		for _, q := range p.l.Questions {
			sl.Questions = append(sl.Questions, cut(q.Prompt+" "+strings.Join(q.Choices, " | ")+" "+q.Explanation, &left))
		}
		out.Lessons = append(out.Lessons, sl)
	}
	raw, _ := json.Marshal(out)
	return string(raw), len(pick)
}

var reasonCodes = []string{"personal_data", "secret", "real_person", "third_party_ip", "sexual_content", "age_inappropriate", "prompt_injection", "hidden_payload", "harmful_content"}

func attemptCost(a *pb.Usage, r Rates) int64 {
	total := uint64(0)
	for _, p := range [][2]uint64{{uint64(a.InputTokens), r.Input}, {uint64(a.OutputTokens), r.Output}, {uint64(a.CacheWriteTokens), r.CacheWrite}, {uint64(a.CacheReadTokens), r.CacheRead}} {
		if p[0] > 0 && p[1] > math.MaxInt64/p[0] {
			return math.MaxInt64
		}
		term := p[0] * p[1]
		if term > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += term
	}
	return int64(total)
}

// Decide validates the engine reply against the envelope and merges scanner findings. Anything
// unexpected is pending_screening: the draft waits and nothing becomes visible.
func Decide(findings []Finding, r *pb.ScreenCourseResponse, callErr error, cfg Config, rates Rates) Outcome {
	out := Outcome{Verdict: VerdictPending}
	if r != nil {
		for _, a := range r.Attempts {
			if a == nil {
				continue
			}
			out.Attempts++
			out.FallbackUsed = out.FallbackUsed || a.FallbackUsed
			c := attemptCost(a, rates)
			if c > math.MaxInt64-out.CostNanos {
				out.CostNanos = math.MaxInt64
			} else {
				out.CostNanos += c
			}
		}
	}
	if callErr != nil || r == nil || r.PolicyVersion != PolicyVersion || len(r.Attempts) == 0 || len(r.Attempts) > int(cfg.MaxAttempts) || out.CostNanos > cfg.MaxCostNanos {
		return out
	}
	for _, a := range r.Attempts {
		if a == nil || a.UsageUnavailable || a.Attempt == 0 || a.Attempt > cfg.MaxAttempts || a.LatencyMs > cfg.TimeoutMs ||
			uint64(a.InputTokens)+uint64(a.CacheWriteTokens)+uint64(a.CacheReadTokens) > uint64(cfg.MaxInputTokens) || a.OutputTokens > cfg.MaxOutputTokens {
			return out
		}
	}
	if last := r.Attempts[len(r.Attempts)-1]; !last.Delivered {
		return out
	}
	switch r.Verdict {
	case VerdictPass:
		if len(r.ReasonCodes) != 0 {
			return out
		}
	case VerdictReview, VerdictBlock:
		if len(r.ReasonCodes) == 0 || len(r.ReasonCodes) > 4 {
			return out
		}
	default:
		return out
	}
	codes := slices.Clone(r.ReasonCodes)
	for i, c := range codes {
		if !slices.Contains(reasonCodes, c) || slices.Contains(codes[:i], c) {
			return out
		}
	}
	out.Verdict, out.ReasonCodes = r.Verdict, codes
	for _, f := range findings {
		if !slices.Contains(out.ReasonCodes, f.Kind) {
			out.ReasonCodes = append(out.ReasonCodes, f.Kind)
		}
		if out.Verdict == VerdictPass {
			out.Verdict = VerdictReview
		}
	}
	if out.ReasonCodes == nil {
		out.ReasonCodes = []string{}
	}
	return out
}

func boundedUint64(n int64) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}

func boundedUint32(n int) uint32 {
	if n < 0 || n > math.MaxUint32 {
		return 0
	}
	return uint32(n)
}
