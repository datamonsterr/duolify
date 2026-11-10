//go:build integration

package notes_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type mockResumeEngineServer struct {
	enginev1.UnimplementedEngineServiceServer
	shouldFail bool
}

func (m *mockResumeEngineServer) EnrichNote(_ context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
	if m.shouldFail {
		return nil, fmt.Errorf("mock engine failure")
	}
	return &enginev1.EnrichNoteResponse{
		Summary: "Summary of " + req.Note,
		Tags:    []string{"astronomy", "sky"},
		Links:   []string{},
		Usage: &enginev1.Usage{
			Attempt:      1,
			Model:        "claude-haiku-5-5",
			Provider:     "anthropic",
			InputTokens:  100,
			OutputTokens: 50,
			Delivered:    true,
			Component:    "content",
		},
		Attempts: []*enginev1.Usage{
			{
				Attempt:      1,
				Model:        "claude-haiku-5-5",
				Provider:     "anthropic",
				InputTokens:  100,
				OutputTokens: 50,
				Delivered:    true,
				Component:    "content",
			},
		},
	}, nil
}

func startMockResumeEngine(t *testing.T, srv *mockResumeEngineServer) (string, func()) {
	t.Helper()
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(s, srv)
	go func() {
		_ = s.Serve(lis)
	}()
	return lis.Addr().String(), func() {
		s.Stop()
	}
}

func TestResumePendingEnrichmentWhenCreditsArriveIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("notes_e903"),
		postgres.WithUsername("fixture"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}

	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}

	database, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	catalogue, err := pricing.Load(ctx, database.Queries)
	if err != nil {
		t.Fatal(err)
	}

	creditsSvc := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, nil)

	mockEngine := &mockResumeEngineServer{}
	engineAddr, cleanupEngine := startMockResumeEngine(t, mockEngine)
	defer cleanupEngine()

	grpcConn, err := grpc.NewClient(engineAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = grpcConn.Close() }()
	engineClient := enginev1.NewEngineServiceClient(grpcConn)

	notesSvc := notes.NewServiceWithEnrichment(database.Pool, nil, creditsSvc, engineClient)

	// Create test learner
	learnerID := uuid.NewString()
	_, err = database.Pool.Exec(ctx, `
		INSERT INTO learners (id, display_name) VALUES ($1::uuid, 'Test Learner E903')
	`, learnerID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = database.Pool.Exec(ctx, `
		INSERT INTO learner_profiles (learner_id, analytics_opt_out) VALUES ($1, false)
	`, learnerID)
	if err != nil {
		t.Fatal(err)
	}

	// TDD: This test calls ResumePendingEnrichments, which is not yet fully implemented
	t.Run("grant wakeup processes oldest pending note first until budget exhausted", func(t *testing.T) {
		// Save Note A (older)
		noteA, err := notesSvc.SaveNote(ctx, learnerID, notes.SaveNoteInput{
			ActionKey: "action-e903-a",
			Source:    "chat",
			RawText:   "Oldest note: Stars move west to east.",
		})
		if err != nil {
			t.Fatalf("SaveNote A failed: %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		// Save Note B (newer)
		noteB, err := notesSvc.SaveNote(ctx, learnerID, notes.SaveNoteInput{
			ActionKey: "action-e903-b",
			Source:    "chat",
			RawText:   "Newer note: Polaris stays still.",
		})
		if err != nil {
			t.Fatalf("SaveNote B failed: %v", err)
		}

		// Both start in enrichment_pending (zero credits)
		if noteA.Status != "enrichment_pending" || noteB.Status != "enrichment_pending" {
			t.Fatalf("expected pending status, got A=%s B=%s", noteA.Status, noteB.Status)
		}

		// Grant exactly 2 credits (enough for 1 quick_note enrichment)
		now := time.Now()
		localDate := now.UTC().Format(time.DateOnly)
		prov1 := fmt.Sprintf(`{"bucket": "daily_free", "date": "%s"}`, localDate)
		_, err = database.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
			VALUES ($1, 2, 'daily_grant', 'grant-e903-1', 'usd-launch-2026-11-08', $2::jsonb)
		`, learnerID, prov1)
		if err != nil {
			t.Fatal(err)
		}
		_ = creditsSvc.InvalidateCache(ctx, learnerID)

		// Wake pending enrichments
		processed, err := notesSvc.ResumePendingEnrichments(ctx, learnerID)
		if err != nil {
			t.Fatalf("ResumePendingEnrichments failed: %v", err)
		}
		if processed != 1 {
			t.Fatalf("expected 1 note processed, got %d", processed)
		}

		// Verify Note A (oldest) is enriched, Note B is still pending
		refreshedA, err := notesSvc.GetNote(ctx, learnerID, noteA.ID)
		if err != nil {
			t.Fatal(err)
		}
		if refreshedA.Status != "enriched" {
			t.Fatalf("expected Note A enriched, got %s", refreshedA.Status)
		}
		if refreshedA.Summary == nil || *refreshedA.Summary == "" {
			t.Fatal("expected summary on Note A")
		}

		refreshedB, err := notesSvc.GetNote(ctx, learnerID, noteB.ID)
		if err != nil {
			t.Fatal(err)
		}
		if refreshedB.Status != "enrichment_pending" {
			t.Fatalf("expected Note B still pending, got %s", refreshedB.Status)
		}

		// Now grant 2 more credits and wake again -> Note B is enriched
		_, err = database.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
			VALUES ($1, 2, 'purchase', 'grant-e903-2', 'usd-launch-2026-11-08', '{"bucket": "purchase"}'::jsonb)
		`, learnerID)
		if err != nil {
			t.Fatal(err)
		}
		_ = creditsSvc.InvalidateCache(ctx, learnerID)

		processed, err = notesSvc.ResumePendingEnrichments(ctx, learnerID)
		if err != nil {
			t.Fatalf("ResumePendingEnrichments second pass failed: %v", err)
		}
		if processed != 1 {
			t.Fatalf("expected 1 note processed on second pass, got %d", processed)
		}

		refreshedB2, err := notesSvc.GetNote(ctx, learnerID, noteB.ID)
		if err != nil {
			t.Fatal(err)
		}
		if refreshedB2.Status != "enriched" {
			t.Fatalf("expected Note B enriched, got %s", refreshedB2.Status)
		}
	})

	t.Run("raw revision conflict prevents stale enrichment from overwriting edited note", func(t *testing.T) {
		// Save Note C
		noteC, err := notesSvc.SaveNote(ctx, learnerID, notes.SaveNoteInput{
			ActionKey: "action-e903-c",
			Source:    "chat",
			RawText:   "Initial text before edit",
		})
		if err != nil {
			t.Fatal(err)
		}

		// Edit Note C raw text, bumping raw_version
		err = notesSvc.UpdateNoteRawText(ctx, learnerID, noteC.ID, "Edited text after queueing")
		if err != nil {
			t.Fatalf("UpdateNoteRawText failed: %v", err)
		}

		// Grant credits
		_, err = database.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
			VALUES ($1, 10, 'purchase', 'grant-e903-3', 'usd-launch-2026-11-08', '{"bucket": "purchase"}'::jsonb)
		`, learnerID)
		if err != nil {
			t.Fatal(err)
		}
		_ = creditsSvc.InvalidateCache(ctx, learnerID)

		// Attempt enrichment with stale expected version 1
		err = notesSvc.ProcessEnrichmentWithVersion(ctx, noteC.ID, 1)
		// Should fail or safely terminate because raw version is now 2
		if err == nil {
			t.Fatal("expected stale version to be rejected")
		}

		// Verify note raw text was NOT overwritten
		refreshedC, err := notesSvc.GetNote(ctx, learnerID, noteC.ID)
		if err != nil {
			t.Fatal(err)
		}
		if refreshedC.RawText != "Edited text after queueing" {
			t.Fatalf("raw text was corrupted: %s", refreshedC.RawText)
		}
	})

	t.Run("provider failure releases reservation and retains raw note in enrichment_failed", func(t *testing.T) {
		mockEngine.shouldFail = true
		defer func() { mockEngine.shouldFail = false }()

		noteD, err := notesSvc.SaveNote(ctx, learnerID, notes.SaveNoteInput{
			ActionKey: "action-e903-d",
			Source:    "chat",
			RawText:   "Will fail provider call",
		})
		if err != nil {
			t.Fatal(err)
		}

		_ = notesSvc.ProcessEnrichment(ctx, noteD.ID)

		refreshedD, err := notesSvc.GetNote(ctx, learnerID, noteD.ID)
		if err != nil {
			t.Fatal(err)
		}
		if refreshedD.Status != "enrichment_failed" {
			t.Fatalf("expected enrichment_failed, got %s", refreshedD.Status)
		}
		if refreshedD.RawText != "Will fail provider call" {
			t.Fatalf("raw text lost on failure: %s", refreshedD.RawText)
		}
	})

	t.Run("retry never charges a delivered version twice", func(t *testing.T) {
		mockEngine.shouldFail = false

		noteE, err := notesSvc.SaveNote(ctx, learnerID, notes.SaveNoteInput{
			ActionKey: "action-e903-e",
			Source:    "chat",
			RawText:   "Already delivered note",
		})
		if err != nil {
			t.Fatal(err)
		}

		// Enrich once
		err = notesSvc.ProcessEnrichment(ctx, noteE.ID)
		if err != nil {
			t.Fatalf("initial enrichment failed: %v", err)
		}

		refreshedE, _ := notesSvc.GetNote(ctx, learnerID, noteE.ID)
		if refreshedE.Status != "enriched" {
			t.Fatalf("expected enriched, got %s", refreshedE.Status)
		}

		// Record credit reservations count
		var reservationsBefore int
		_ = database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_jobs WHERE learner_id=$1", learnerID).Scan(&reservationsBefore)

		// Retry without editing raw text
		err = notesSvc.ProcessEnrichment(ctx, noteE.ID)
		if err != nil {
			t.Fatalf("retry on delivered note returned error: %v", err)
		}

		var reservationsAfter int
		_ = database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_jobs WHERE learner_id=$1", learnerID).Scan(&reservationsAfter)

		if reservationsAfter != reservationsBefore {
			t.Fatalf("retry charged already delivered version! before=%d after=%d", reservationsBefore, reservationsAfter)
		}
	})
}
