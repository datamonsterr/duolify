//go:build integration

package narration_test

import (
	"errors"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/narration"
)

// voiceHarness rebuilds the asset harness service with the verified Postgres entitlement
// lookup (the same credits service) and the test-only fixture voices.
func voiceHarness(t *testing.T) *assetHarness {
	t.Helper()
	h := newAssetHarness(t, 30*24*time.Hour)
	entitlements, ok := h.opts.Financial.(narration.Entitlements)
	if !ok {
		t.Fatal("credits service must expose the verified funded plan")
	}
	h.opts.Entitlements, h.opts.FixtureVoices = entitlements, true
	h.service = narration.New(h.opts)
	if _, err := h.service.Queue(); err != nil {
		t.Fatal(err)
	}
	return h
}

// plan switches the fixture subscription's stored entitlement to a configured plan, taking the
// benefit list from the Pricing config row itself (never from code). The disposable database
// lets the test lift the append-only guard for this one rewrite.
func (h *assetHarness) plan(id string) {
	h.t.Helper()
	for _, statement := range []string{
		`ALTER TABLE credit_subscription_terms DISABLE TRIGGER credit_subscription_terms_immutable`,
		`UPDATE credit_subscription_terms t SET plan_id=p->>'id', plan_name=p->>'name', benefits=p->'benefits'
			FROM pricing_versions v, jsonb_array_elements(v.config->'plans') p WHERE v.version=t.pricing_version AND p->>'id'='` + id + `'`,
		`ALTER TABLE credit_subscription_terms ENABLE TRIGGER credit_subscription_terms_immutable`,
	} {
		if _, err := h.pool.Exec(h.ctx, statement); err != nil {
			h.t.Fatal(err)
		}
	}
}

// effective lists the status of the row the player would serve for each slide.
func (h *assetHarness) effective(lessonID string) string {
	h.t.Helper()
	var out string
	if err := h.pool.QueryRow(h.ctx, `SELECT string_agg(status||':'||voice_key, ',' ORDER BY slide_id) FROM (
		SELECT DISTINCT ON (slide_id) slide_id,status,voice_key FROM slide_narrations WHERE lesson_id=$1 AND learner_id=$2
		ORDER BY slide_id,(status='ready') DESC,(voice_key=COALESCE((SELECT narration_voice FROM learner_profiles WHERE learner_id=$2),'default')) DESC,updated_at DESC) e`, lessonID, owner).Scan(&out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *assetHarness) rows(lessonID, voice string) int {
	var n int
	_ = h.pool.QueryRow(h.ctx, `SELECT count(*) FROM slide_narrations WHERE lesson_id=$1 AND voice_key=$2`, lessonID, voice).Scan(&n)
	return n
}

func (h *assetHarness) selected() string {
	var v string
	_ = h.pool.QueryRow(h.ctx, `SELECT narration_voice FROM learner_profiles WHERE learner_id=$1`, owner).Scan(&v)
	return v
}

func TestSelectEntitledVoicesWithExplicitSynthesisQuotesIntegration(t *testing.T) {
	h := voiceHarness(t)
	ctx := h.ctx
	total := h.chars[0] + h.chars[1] + h.chars[2]

	// Baseline: the default voice narrates the lesson once.
	h.enqueue(lesson)
	if err := h.service.SynthesizeLesson(ctx, owner, lesson, true); err != nil {
		t.Fatal(err)
	}
	baseSpent, baseCalls := h.spent(), h.mock.Calls()
	if baseSpent != ceilCredits(total) || h.effective(lesson) != "ready:default,ready:default,ready:default" {
		t.Fatal("baseline", baseSpent, h.effective(lesson))
	}

	// 1. The fixture subscription is Learn: it has no curated voices, so selection is refused with
	// no quote, no synthesis and no charge, and the upsell stays withheld ("No upsell banners").
	list, err := h.service.Voices(ctx, owner)
	if err != nil || list.Entitled || list.UpsellAllowed || !list.NarrationEnabled || list.Selected != "default" {
		t.Fatalf("learn voices %+v %v", list, err)
	}
	for _, v := range list.Voices {
		if v.Curated == v.Available {
			t.Fatal("curated voices must be unavailable without the benefit", v)
		}
	}
	if _, err = h.service.QuoteVoice(ctx, owner, lesson, "fixture-warm"); !errors.Is(err, narration.ErrNotEntitled) {
		t.Fatal("unentitled quote", err)
	}
	if _, err = h.service.ApplyVoice(ctx, owner, lesson, "fixture-warm", "", list.ConfigHash); !errors.Is(err, narration.ErrNotEntitled) {
		t.Fatal("unentitled apply", err)
	}
	if _, err = h.service.QuoteVoice(ctx, owner, lesson, "not-approved"); !errors.Is(err, narration.ErrVoice) {
		t.Fatal("unknown voice", err)
	}

	// 2. Plus (from the Pricing config row) entitles curated voices; the quote discloses the new
	// characters, the credits, the pricing version and the voice config hash, and charges nothing.
	h.plan("plus")
	if list, err = h.service.Voices(ctx, owner); err != nil || !list.Entitled || list.UpsellAllowed {
		t.Fatalf("plus voices %+v %v", list, err)
	}
	quote, err := h.service.QuoteVoice(ctx, owner, lesson, "fixture-warm")
	if err != nil {
		t.Fatal(err)
	}
	if quote.Free || quote.Characters != int64(total) || quote.Credits != int64(ceilCredits(total)) || quote.QuoteID == "" || quote.PricingVersion == "" || quote.ConfigHash != list.ConfigHash {
		t.Fatalf("quote %+v want %d characters", quote, total)
	}
	if h.spent() != baseSpent || h.mock.Calls() != baseCalls || h.selected() != "default" || h.rows(lesson, "fixture-warm") != 0 {
		t.Fatal("a quote must not charge, synthesize or change the setting")
	}

	// 3. Declining (never applying) keeps everything: the old narration replays free.
	if h.effective(lesson) != "ready:default,ready:default,ready:default" || h.spent() != baseSpent {
		t.Fatal("declined quote changed narration", h.effective(lesson))
	}

	// 4. Consent must match the disclosed terms: a changed voice config or a missing quote fails closed.
	if _, err = h.service.ApplyVoice(ctx, owner, lesson, "fixture-warm", quote.QuoteID, "stale-hash"); !errors.Is(err, narration.ErrQuoteChanged) {
		t.Fatal("stale config hash", err)
	}
	if _, err = h.service.ApplyVoice(ctx, owner, lesson, "fixture-warm", "", quote.ConfigHash); !errors.Is(err, narration.ErrQuoteChanged) {
		t.Fatal("missing consent", err)
	}
	if h.rows(lesson, "fixture-warm") != 0 || h.selected() != "default" || h.spent() != baseSpent {
		t.Fatal("refused consent changed state")
	}

	// 5. Explicit consent queues synthesis but the old voice keeps playing and nothing is charged yet.
	state, err := h.service.ApplyVoice(ctx, owner, lesson, "fixture-warm", quote.QuoteID, quote.ConfigHash)
	if err != nil || state != "queued" {
		t.Fatal(state, err)
	}
	if h.spent() != baseSpent || h.effective(lesson) != "ready:default,ready:default,ready:default" || h.selected() != "fixture-warm" {
		t.Fatal("apply must not charge or break replay", h.effective(lesson), h.selected())
	}
	if err = h.service.SynthesizeVoice(ctx, owner, lesson, "fixture-warm", quote.QuoteID, true); err != nil {
		t.Fatal(err)
	}
	if h.effective(lesson) != "ready:fixture-warm,ready:fixture-warm,ready:fixture-warm" || h.spent() != baseSpent+ceilCredits(total) {
		t.Fatal("new voice", h.effective(lesson), h.spent())
	}
	var voiceIDs int
	_ = h.pool.QueryRow(ctx, `SELECT count(DISTINCT voice_id) FROM slide_narrations WHERE lesson_id=$1`, lesson).Scan(&voiceIDs)
	if voiceIDs != 2 || len(h.events.named("narration_generated")) < 6 {
		t.Fatal("both voices must be stored", voiceIDs)
	}
	// The old assets stay referenced (never queued for deletion) while the new voice plays.
	var unreferenced int
	_ = h.pool.QueryRow(ctx, `SELECT count(*) FROM narration_assets WHERE ref_count=0 OR unreferenced_at IS NOT NULL`).Scan(&unreferenced)
	if unreferenced != 0 {
		t.Fatal("old voice assets lost their references", unreferenced)
	}

	// 6. Switching back to a stored voice is a free cache hit: no quote credits, no provider call.
	spent, calls, hits := h.spent(), h.mock.Calls(), len(h.events.named("narration_cache_hit"))
	back, err := h.service.QuoteVoice(ctx, owner, lesson, "default")
	if err != nil || !back.Free || back.Characters != 0 || back.QuoteID != "" {
		t.Fatalf("switch back %+v %v", back, err)
	}
	if state, err = h.service.ApplyVoice(ctx, owner, lesson, "default", "", back.ConfigHash); err != nil || state != "ready" {
		t.Fatal(state, err)
	}
	if h.spent() != spent || h.mock.Calls() != calls || len(h.events.named("narration_cache_hit")) != hits+3 || h.selected() != "default" {
		t.Fatal("switch back charged", h.spent()-spent, h.mock.Calls()-calls)
	}
	if h.effective(lesson) != "ready:default,ready:default,ready:default" {
		t.Fatal(h.effective(lesson))
	}

	// 7. Insufficient audio credits disable only the new audio; the old voice, text and test stay usable,
	// and nothing is charged. Recovery is to review a fresh quote and consent again.
	calm, err := h.service.QuoteVoice(ctx, owner, lesson, "fixture-calm")
	if err != nil || calm.Free || calm.QuoteID == "" {
		t.Fatalf("calm quote %+v %v", calm, err)
	}
	// Drain the buckets narration can spend from, using the ledger's own bucket rule.
	if _, err = h.pool.Exec(ctx, `INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
		SELECT $1, -sum(amount), 'expire', 'drain:'||gen_random_uuid(), '2026.04', jsonb_build_object('bucket', bucket)
		FROM (SELECT amount, CASE WHEN kind='daily_grant' THEN 'daily_free' WHEN kind='subscription_grant' THEN 'subscription' WHEN kind='purchase' THEN 'purchase' ELSE COALESCE(provenance->>'bucket','purchase') END AS bucket
			FROM credit_entries WHERE learner_id=$1 AND kind!='reward') e WHERE bucket IN ('purchase','subscription') GROUP BY bucket HAVING sum(amount) > 0`, owner); err != nil {
		t.Fatal(err)
	}
	spent, calls = h.spent(), h.mock.Calls()
	if state, err = h.service.ApplyVoice(ctx, owner, lesson, "fixture-calm", calm.QuoteID, calm.ConfigHash); err != nil || state != "queued" {
		t.Fatal(state, err)
	}
	if err = h.service.SynthesizeVoice(ctx, owner, lesson, "fixture-calm", calm.QuoteID, true); err != nil {
		t.Fatal(err)
	}
	change, err := h.service.VoiceState(ctx, owner, lesson, "fixture-calm")
	if err != nil || change.State != "disabled" || change.Reason != "voice_insufficient_credits" {
		t.Fatalf("change %+v %v", change, err)
	}
	if h.effective(lesson) != "ready:default,ready:default,ready:default" && h.effective(lesson) != "ready:fixture-warm,ready:fixture-warm,ready:fixture-warm" {
		t.Fatal("old audio must stay playable", h.effective(lesson))
	}
	if h.spent() != spent || h.mock.Calls() != calls {
		t.Fatal("unfunded change charged or synthesized", h.spent()-spent, h.mock.Calls()-calls)
	}
	if _, err = h.pool.Exec(ctx, `INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
		VALUES ($1, 500, 'purchase', 'asset-topup:' || gen_random_uuid(), '2026.04', '{"bucket": "purchase"}'::jsonb)`, owner); err != nil {
		t.Fatal(err)
	}
	calm, err = h.service.QuoteVoice(ctx, owner, lesson, "fixture-calm")
	if err != nil {
		t.Fatal(err)
	}
	if state, err = h.service.ApplyVoice(ctx, owner, lesson, "fixture-calm", calm.QuoteID, calm.ConfigHash); err != nil || state != "queued" {
		t.Fatal(state, err)
	}
	if err = h.service.SynthesizeVoice(ctx, owner, lesson, "fixture-calm", calm.QuoteID, true); err != nil {
		t.Fatal(err)
	}
	if h.effective(lesson) != "ready:fixture-calm,ready:fixture-calm,ready:fixture-calm" || h.spent() != spent+ceilCredits(total) {
		t.Fatal("recovered change", h.effective(lesson), h.spent()-spent)
	}

	// 8. A lapsed entitlement removes curated voices again, even for stored assets, with no charge.
	h.plan("learn")
	if _, err = h.service.ApplyVoice(ctx, owner, lesson, "fixture-warm", "", calm.ConfigHash); !errors.Is(err, narration.ErrNotEntitled) {
		t.Fatal("lapsed entitlement", err)
	}
	if list, err = h.service.Voices(ctx, owner); err != nil || list.Selected != "default" {
		t.Fatalf("an unentitled curated choice must fall back to the default voice: %+v %v", list, err)
	}
}

func TestGlobalNarrationOffSkipsWorkAndChargesIntegration(t *testing.T) {
	h := voiceHarness(t)
	ctx := h.ctx
	h.plan("plus")
	h.enqueue(lesson)
	if err := h.service.SynthesizeLesson(ctx, owner, lesson, true); err != nil {
		t.Fatal(err)
	}
	// A pending slide exists when the Learner turns narration off.
	if _, err := h.pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,voice_key,status,narration_text) VALUES($1,$2,'late','default','pending','A late slide that is never spoken.')`, lesson, owner); err != nil {
		t.Fatal(err)
	}
	spent, calls, jobs := h.spent(), h.mock.Calls(), h.jobs()
	if _, err := h.pool.Exec(ctx, `UPDATE learner_profiles SET narration=false WHERE learner_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if err := h.service.SynthesizeLesson(ctx, owner, lesson, true); err != nil {
		t.Fatal(err)
	}
	var st, code string
	_ = h.pool.QueryRow(ctx, `SELECT status,failure_code FROM slide_narrations WHERE lesson_id=$1 AND slide_id='late'`, lesson).Scan(&st, &code)
	if st != "disabled" || code != "narration_off" || h.spent() != spent || h.mock.Calls() != calls || h.jobs() != jobs || len(h.events.named("narration_failed")) != 0 {
		t.Fatal("learner narration-off must skip the engine, quotes and charges", st, code)
	}
	if n, err := h.service.Enqueue(ctx, owner, lesson); err != nil || n != 0 {
		t.Fatal("enqueue while off", n, err)
	}
	list, err := h.service.Voices(ctx, owner)
	if err != nil || list.NarrationEnabled {
		t.Fatalf("voices while off %+v %v", list, err)
	}
	if _, err = h.service.QuoteVoice(ctx, owner, lesson, "fixture-warm"); !errors.Is(err, narration.ErrNarrationOff) {
		t.Fatal("quote while off", err)
	}
	if _, err = h.service.ApplyVoice(ctx, owner, lesson, "fixture-warm", "", list.ConfigHash); !errors.Is(err, narration.ErrNarrationOff) {
		t.Fatal("apply while off", err)
	}
	// The server flag is a second, independent switch.
	if _, err = h.pool.Exec(ctx, `UPDATE learner_profiles SET narration=true WHERE learner_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	h.opts.Off = true
	off := narration.New(h.opts)
	if _, err = off.Queue(); err != nil {
		t.Fatal(err)
	}
	if n, e := off.Enqueue(ctx, owner, lesson); e != nil || n != 0 {
		t.Fatal("enqueue while server flag off", n, e)
	}
	if _, err = off.QuoteVoice(ctx, owner, lesson, "default"); !errors.Is(err, narration.ErrNarrationOff) {
		t.Fatal("quote while server flag off", err)
	}
	if h.spent() != spent || h.mock.Calls() != calls || h.jobs() != jobs {
		t.Fatal("server flag off charged or called the engine")
	}
}
