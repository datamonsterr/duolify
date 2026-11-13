package eval

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SystemOperationalUUID is the reserved system actor UUID for operational PostHog events without learner context.
const SystemOperationalUUID = "00000000-0000-4000-8000-000000000011"

// AlertConfig contains thresholds for budget, cost growth, latency, and reliability alerts.
type AlertConfig struct {
	PlatformDailyBudgetUSD       float64
	DailyBudgetAlert80Percent    bool
	DailyBudgetAlert100Percent   bool
	LearnerCostIncreaseThreshold float64
	CacheHitRateThreshold        float64
	PaidMarginThreshold          float64
	FallbackRateThreshold        float64
	ErrorRateThreshold           float64
	AlertWindowMinutes           int
}

// BudgetAlertResult indicates which budget threshold boundaries were crossed.
type BudgetAlertResult struct {
	Fire80  bool
	Fire100 bool
}

// EvaluatePlatformBudget checks whether actual spending crossed the 80% or 100% daily platform budget thresholds.
func EvaluatePlatformBudget(actualSpentUSD float64, cfg AlertConfig) BudgetAlertResult {
	res := BudgetAlertResult{}
	if cfg.PlatformDailyBudgetUSD <= 0 {
		return res
	}
	if cfg.DailyBudgetAlert80Percent && actualSpentUSD >= 0.80*cfg.PlatformDailyBudgetUSD {
		res.Fire80 = true
	}
	if cfg.DailyBudgetAlert100Percent && actualSpentUSD >= 1.00*cfg.PlatformDailyBudgetUSD {
		res.Fire100 = true
	}
	return res
}

// EvaluateLearnerCostIncrease determines whether the rolling 7-day cost per active learner increased above the threshold.
func EvaluateLearnerCostIncrease(priorAvg, currentAvg, threshold float64) bool {
	if priorAvg <= 0 {
		return false
	}
	growth := (currentAvg - priorAvg) / priorAvg
	return growth >= threshold
}

// EvaluateCacheHitRate checks whether the prompt cache hit rate fell below the configured threshold.
func EvaluateCacheHitRate(cacheHits, totalTokens int64, threshold float64) bool {
	if totalTokens <= 0 {
		return false
	}
	hitRate := float64(cacheHits) / float64(totalTokens)
	return hitRate < threshold
}

// EvaluatePaidMargin checks whether the paid AI gross margin fell below the required threshold.
func EvaluatePaidMargin(revenue, cost, threshold float64) bool {
	if revenue <= 0 {
		return false
	}
	margin := (revenue - cost) / revenue
	return margin < threshold
}

// EvaluateRate returns true if the ratio count/total is greater than or equal to threshold.
func EvaluateRate(count, total int64, threshold float64) bool {
	if total <= 0 {
		return false
	}
	rate := float64(count) / float64(total)
	return rate >= threshold
}

// DedupeKey generates a time-bucketed deduplication key for a given scope.
func DedupeKey(scope string, t time.Time, windowMinutes int) string {
	if windowMinutes <= 0 {
		windowMinutes = 60
	}
	bucket := t.UTC().Truncate(time.Duration(windowMinutes) * time.Minute)
	return fmt.Sprintf("%s:%s", scope, bucket.Format("2006-01-02T15:04"))
}

// EmitBudgetAlert captures an ai_cost_budget_alert event into analytics.
func EmitBudgetAlert(events analytics.Events, scope string, actualUSD, budgetUSD float64) error {
	if events == nil {
		return nil
	}
	props := map[string]any{
		"source":      "api",
		"platform":    "web",
		"app_version": "development",
		"scope":       scope,
		"actual_usd":  actualUSD,
		"budget_usd":  budgetUSD,
		"status":      "firing",
	}
	return events.Capture(SystemOperationalUUID, "ai_cost_budget_alert", props)
}

// TaskQualityMetric summarizes the evaluation quality metric for a given task.
type TaskQualityMetric struct {
	Task        string  `json:"task"`
	Score       float64 `json:"score"`
	Threshold   float64 `json:"threshold"`
	Passed      bool    `json:"passed"`
	SampleCount int     `json:"sample_count"`
}

// LatencySummary provides latency percentiles in milliseconds.
type LatencySummary struct {
	Mean int64 `json:"mean"`
	P50  int64 `json:"p50"`
	P95  int64 `json:"p95"`
	Max  int64 `json:"max"`
}

// ReliabilityDashboard holds operational reliability metrics without learner text.
type ReliabilityDashboard struct {
	TotalCalls    int64                        `json:"total_calls"`
	FallbackCalls int64                        `json:"fallback_calls"`
	ErrorCalls    int64                        `json:"error_calls"`
	FallbackRate  float64                      `json:"fallback_rate"`
	ErrorRate     float64                      `json:"error_rate"`
	TaskQuality   map[string]TaskQualityMetric `json:"task_quality"`
	LatencyMs     LatencySummary               `json:"latency_ms"`
}

// CostDashboard holds token and financial aggregations derived purely from operational counters and catalogue prices.
type CostDashboard struct {
	TotalInputTokens      int64   `json:"total_input_tokens"`
	TotalOutputTokens     int64   `json:"total_output_tokens"`
	CacheWriteTokens      int64   `json:"cache_write_tokens"`
	CacheReadTokens       int64   `json:"cache_read_tokens"`
	CacheHitRate          float64 `json:"cache_hit_rate"`
	TotalCostUSD          float64 `json:"total_cost_usd"`
	PlatformBudgetUSD     float64 `json:"platform_budget_usd"`
	PlatformBudgetPercent float64 `json:"platform_budget_percent"`
	LearnerCostGrowth     float64 `json:"learner_cost_growth"`
	PaidMargin            float64 `json:"paid_margin"`
	ActiveLearners        int64   `json:"active_learners"`
}

// Dashboards combines both reliability and AI cost dashboard data.
type Dashboards struct {
	Reliability ReliabilityDashboard `json:"reliability"`
	Cost        CostDashboard        `json:"cost"`
}

// CallMetrics captures operational metadata for an engine call.
type CallMetrics struct {
	Task               string
	Success            bool
	FallbackUsed       bool
	ErrorCode          string
	LatencyMs          int64
	InputTokens        int64
	OutputTokens       int64
	CacheWriteTokens   int64
	CacheReadTokens    int64
	CostNanos          int64
	CreditRevenueCents int64
}

// Report represents a golden-set eval run report.
type Report struct {
	ID              string         `json:"id,omitempty"`
	Task            string         `json:"task"`
	DatasetRevision string         `json:"dataset_revision"`
	PromptRevision  string         `json:"prompt_revision"`
	Model           string         `json:"model"`
	Score           float64        `json:"score"`
	Threshold       float64        `json:"threshold"`
	Passed          bool           `json:"passed"`
	Details         map[string]any `json:"details"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	ExpiresAt       time.Time      `json:"expires_at,omitempty"`
}

// AlertResult represents the outcome of evaluating a single alert rule.
type AlertResult struct {
	Scope        string
	AlertKey     string
	ActualUSD    float64
	BudgetUSD    float64
	Deduplicated bool
}

// Service provides dashboard aggregations, alert evaluation, and report persistence.
type Service struct {
	pool               *pgxpool.Pool
	queries            *db.Queries
	catalogue          *pricing.Catalogue
	monitor            *telemetry.Monitor
	events             analytics.Events
	mu                 sync.Mutex
	dedupe             map[string]time.Time
	reportServiceToken string
}

// NewService constructs a new eval Service.
func NewService(pool *pgxpool.Pool, catalogue *pricing.Catalogue, monitor *telemetry.Monitor, events analytics.Events) *Service {
	return &Service{
		pool:               pool,
		queries:            db.New(pool),
		catalogue:          catalogue,
		monitor:            monitor,
		events:             events,
		dedupe:             make(map[string]time.Time),
		reportServiceToken: os.Getenv("EVAL_REPORT_SERVICE_TOKEN"),
	}
}

// RecordCallMetrics increments the operational daily counters with metrics from an engine execution.
func (s *Service) RecordCallMetrics(ctx context.Context, m CallMetrics) error {
	today := time.Now().UTC()
	var fallbackCount, errorCount int64
	if m.FallbackUsed {
		fallbackCount = 1
	}
	if !m.Success {
		errorCount = 1
	}

	dateVal := pgtype.Date{Time: today, Valid: true}
	_, err := s.queries.RecordAggregateCall(ctx, db.RecordAggregateCallParams{
		Day:                dateVal,
		Task:               m.Task,
		TotalCalls:         1,
		FallbackCalls:      fallbackCount,
		ErrorCalls:         errorCount,
		TotalLatencyMs:     m.LatencyMs,
		InputTokens:        m.InputTokens,
		OutputTokens:       m.OutputTokens,
		CacheWriteTokens:   m.CacheWriteTokens,
		CacheReadTokens:    m.CacheReadTokens,
		CostNanos:          m.CostNanos,
		CreditRevenueCents: m.CreditRevenueCents,
	})
	return err
}

// SaveReport stores an evaluation report in the database with a 90-day retention expiration.
func (s *Service) SaveReport(ctx context.Context, r Report) (string, error) {
	rawDetails, err := json.Marshal(r.Details)
	if err != nil {
		rawDetails = []byte("{}")
	}

	scoreNum := floatToNumeric(r.Score)
	threshNum := floatToNumeric(r.Threshold)

	row, err := s.queries.InsertEvalReport(ctx, db.InsertEvalReportParams{
		Task:            r.Task,
		DatasetRevision: r.DatasetRevision,
		PromptRevision:  r.PromptRevision,
		Model:           r.Model,
		Score:           scoreNum,
		Threshold:       threshNum,
		Passed:          r.Passed,
		ReportJson:      rawDetails,
	})
	if err != nil {
		return "", err
	}
	return row.ID.String(), nil
}

// LoadAlertConfig loads alert configuration from the database or returns default thresholds.
func (s *Service) LoadAlertConfig(ctx context.Context) (AlertConfig, error) {
	row, err := s.queries.GetAlertConfig(ctx)
	if err != nil {
		return AlertConfig{
			PlatformDailyBudgetUSD:       50.00,
			DailyBudgetAlert80Percent:    true,
			DailyBudgetAlert100Percent:   true,
			LearnerCostIncreaseThreshold: 0.20,
			CacheHitRateThreshold:        0.60,
			PaidMarginThreshold:          0.70,
			FallbackRateThreshold:        0.15,
			ErrorRateThreshold:           0.05,
			AlertWindowMinutes:           60,
		}, nil
	}

	return AlertConfig{
		PlatformDailyBudgetUSD:       numericToFloat(row.PlatformDailyBudgetUsd),
		DailyBudgetAlert80Percent:    row.DailyBudgetAlert80Percent,
		DailyBudgetAlert100Percent:   row.DailyBudgetAlert100Percent,
		LearnerCostIncreaseThreshold: numericToFloat(row.LearnerCostIncreaseThreshold),
		CacheHitRateThreshold:        numericToFloat(row.CacheHitRateThreshold),
		PaidMarginThreshold:          numericToFloat(row.PaidMarginThreshold),
		FallbackRateThreshold:        numericToFloat(row.FallbackRateThreshold),
		ErrorRateThreshold:           numericToFloat(row.ErrorRateThreshold),
		AlertWindowMinutes:           int(row.AlertWindowMinutes),
	}, nil
}

// CheckAndTriggerAlerts evaluates all alert conditions, applies deduplication, and emits alerts when triggered.
func (s *Service) CheckAndTriggerAlerts(ctx context.Context) ([]AlertResult, error) {
	cfg, err := s.LoadAlertConfig(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	todayStr := now.Format(time.DateOnly)
	dateVal := pgtype.Date{Time: now, Valid: true}

	var results []AlertResult

	// 1. Platform daily budget check (from credit_cost_counters or operational aggregates)
	var spentNanos int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(spent_nanos, 0)
		FROM credit_cost_counters
		WHERE scope = 'platform' AND owner_reference = 'platform' AND day = $1::date
	`, todayStr).Scan(&spentNanos)

	spentUSD := float64(spentNanos) / 1e9
	budgetRes := EvaluatePlatformBudget(spentUSD, cfg)

	if budgetRes.Fire80 {
		key := fmt.Sprintf("platform_daily_80:%s", todayStr)
		res := s.fireOrDedupe(ctx, "platform_daily_80", key, spentUSD, cfg.PlatformDailyBudgetUSD)
		results = append(results, res)
	}

	if budgetRes.Fire100 {
		key := fmt.Sprintf("platform_daily_100:%s", todayStr)
		res := s.fireOrDedupe(ctx, "platform_daily_100", key, spentUSD, cfg.PlatformDailyBudgetUSD)
		results = append(results, res)
	}

	// 2. Operational rates check (fallback rate & error rate from eval_operational_aggregates)
	totals, err := s.queries.GetOverallAggregateTotals(ctx, dateVal)
	if err == nil && totals.TotalCalls > 0 {
		if EvaluateRate(totals.FallbackCalls, totals.TotalCalls, cfg.FallbackRateThreshold) {
			key := DedupeKey("fallback_rate", now, cfg.AlertWindowMinutes)
			rate := float64(totals.FallbackCalls) / float64(totals.TotalCalls)
			res := s.fireOrDedupe(ctx, "fallback_rate", key, rate, cfg.FallbackRateThreshold)
			results = append(results, res)
		}

		if EvaluateRate(totals.ErrorCalls, totals.TotalCalls, cfg.ErrorRateThreshold) {
			key := DedupeKey("error_rate", now, cfg.AlertWindowMinutes)
			rate := float64(totals.ErrorCalls) / float64(totals.TotalCalls)
			res := s.fireOrDedupe(ctx, "error_rate", key, rate, cfg.ErrorRateThreshold)
			results = append(results, res)
		}

		// 3. Cache hit rate check (<60%)
		totalTokens := totals.InputTokens + totals.CacheReadTokens + totals.CacheWriteTokens
		if totalTokens > 0 && EvaluateCacheHitRate(totals.CacheReadTokens, totalTokens, cfg.CacheHitRateThreshold) {
			key := DedupeKey("cache_hit_rate", now, cfg.AlertWindowMinutes)
			rate := float64(totals.CacheReadTokens) / float64(totalTokens)
			res := s.fireOrDedupe(ctx, "cache_hit_rate", key, rate, cfg.CacheHitRateThreshold)
			results = append(results, res)
		}

		// 4. Paid margin check (<70%)
		revUSD := float64(totals.CreditRevenueCents) / 100.0
		costUSD := float64(totals.CostNanos) / 1e9
		if revUSD > 0 && EvaluatePaidMargin(revUSD, costUSD, cfg.PaidMarginThreshold) {
			key := DedupeKey("paid_margin", now, cfg.AlertWindowMinutes)
			margin := (revUSD - costUSD) / revUSD
			res := s.fireOrDedupe(ctx, "paid_margin", key, margin, cfg.PaidMarginThreshold)
			results = append(results, res)
		}
	}

	// 5. Seven-day learner cost increase (+20%)
	var priorAvgNanos, currentAvgNanos float64
	_ = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(AVG(spent_nanos), 0)
		FROM credit_cost_counters
		WHERE scope = 'learner' AND day >= ($1::date - interval '14 days') AND day < ($1::date - interval '7 days')
	`, todayStr).Scan(&priorAvgNanos)

	_ = s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(AVG(spent_nanos), 0)
		FROM credit_cost_counters
		WHERE scope = 'learner' AND day >= ($1::date - interval '7 days') AND day <= $1::date
	`, todayStr).Scan(&currentAvgNanos)

	if priorAvgNanos > 0 {
		priorAvgUSD := priorAvgNanos / 1e9
		currentAvgUSD := currentAvgNanos / 1e9
		if EvaluateLearnerCostIncrease(priorAvgUSD, currentAvgUSD, cfg.LearnerCostIncreaseThreshold) {
			key := fmt.Sprintf("seven_day_learner_cost:%s", todayStr)
			growth := (currentAvgUSD - priorAvgUSD) / priorAvgUSD
			res := s.fireOrDedupe(ctx, "seven_day_learner_cost", key, growth, cfg.LearnerCostIncreaseThreshold)
			results = append(results, res)
		}
	}

	return results, nil
}

func (s *Service) fireOrDedupe(ctx context.Context, scope, alertKey string, actual, budget float64) AlertResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check in-process dedupe cache first
	if expiry, exists := s.dedupe[alertKey]; exists && time.Now().Before(expiry) {
		return AlertResult{
			Scope:        scope,
			AlertKey:     alertKey,
			ActualUSD:    actual,
			BudgetUSD:    budget,
			Deduplicated: true,
		}
	}

	// Check Postgres dedupe store
	var exists bool
	_ = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM eval_alert_events WHERE alert_key = $1)", alertKey).Scan(&exists)
	if exists {
		s.dedupe[alertKey] = time.Now().Add(60 * time.Minute)
		return AlertResult{
			Scope:        scope,
			AlertKey:     alertKey,
			ActualUSD:    actual,
			BudgetUSD:    budget,
			Deduplicated: true,
		}
	}

	// Insert into Postgres alert history
	actNum := floatToNumeric(actual)
	budNum := floatToNumeric(budget)
	_, _ = s.queries.InsertAlertEvent(ctx, db.InsertAlertEventParams{
		Scope:      scope,
		AlertKey:   alertKey,
		ActualUsd:  actNum,
		BudgetUsd:  budNum,
		Properties: []byte("{}"),
		Status:     "firing",
	})

	s.dedupe[alertKey] = time.Now().Add(60 * time.Minute)

	// Emit to monitor (independent server operational counter)
	if s.monitor != nil {
		s.monitor.Incident(ctx, "dependency", "ai_alert:"+scope, false)
	}

	// Emit to PostHog double
	if s.events != nil {
		_ = EmitBudgetAlert(s.events, scope, actual, budget)
	}

	return AlertResult{
		Scope:        scope,
		AlertKey:     alertKey,
		ActualUSD:    actual,
		BudgetUSD:    budget,
		Deduplicated: false,
	}
}

// GetDashboards computes and returns both the reliability and cost dashboards.
func (s *Service) GetDashboards(ctx context.Context) (Dashboards, error) {
	now := time.Now().UTC()
	todayStr := now.Format(time.DateOnly)
	dateVal := pgtype.Date{Time: now, Valid: true}

	cfg, _ := s.LoadAlertConfig(ctx)

	// Aggregate metrics
	totals, _ := s.queries.GetOverallAggregateTotals(ctx, dateVal)

	var fallbackRate, errorRate float64
	if totals.TotalCalls > 0 {
		fallbackRate = math.Round((float64(totals.FallbackCalls)/float64(totals.TotalCalls))*1000) / 1000
		errorRate = math.Round((float64(totals.ErrorCalls)/float64(totals.TotalCalls))*1000) / 1000
	}

	// Calculate latency summary
	var avgLatency int64
	if totals.TotalCalls > 0 {
		avgLatency = totals.TotalLatencyMs / totals.TotalCalls
	}

	// Query task quality from eval_reports
	taskQuality := make(map[string]TaskQualityMetric)
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (task) task, score, threshold, passed
		FROM eval_reports
		ORDER BY task, created_at DESC
	`)
	if err == nil {
		for rows.Next() {
			var t string
			var scoreNum, threshNum pgtype.Numeric
			var passed bool
			if scanErr := rows.Scan(&t, &scoreNum, &threshNum, &passed); scanErr == nil {
				taskQuality[t] = TaskQualityMetric{
					Task:        t,
					Score:       numericToFloat(scoreNum),
					Threshold:   numericToFloat(threshNum),
					Passed:      passed,
					SampleCount: 1,
				}
			}
		}
		rows.Close()
	}

	// Platform spend
	var spentNanos int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(spent_nanos, 0)
		FROM credit_cost_counters
		WHERE scope = 'platform' AND owner_reference = 'platform' AND day = $1::date
	`, todayStr).Scan(&spentNanos)

	spentUSD := float64(spentNanos) / 1e9
	if spentUSD == 0 && totals.CostNanos > 0 {
		spentUSD = float64(totals.CostNanos) / 1e9
	}

	var budgetPercent float64
	if cfg.PlatformDailyBudgetUSD > 0 {
		budgetPercent = math.Round((spentUSD/cfg.PlatformDailyBudgetUSD)*1000) / 10
	}

	// Cache hit rate
	totalTokens := totals.InputTokens + totals.CacheReadTokens + totals.CacheWriteTokens
	var cacheHitRate float64
	if totalTokens > 0 {
		cacheHitRate = math.Round((float64(totals.CacheReadTokens)/float64(totalTokens))*1000) / 1000
	}

	// Paid margin
	revUSD := float64(totals.CreditRevenueCents) / 100.0
	var paidMargin float64
	if revUSD > 0 {
		paidMargin = math.Round(((revUSD-spentUSD)/revUSD)*1000) / 1000
	}

	// Active learners
	var activeLearners int64
	_ = s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT owner_reference)
		FROM credit_cost_counters
		WHERE scope = 'learner' AND day = $1::date AND spent_nanos > 0
	`, todayStr).Scan(&activeLearners)

	return Dashboards{
		Reliability: ReliabilityDashboard{
			TotalCalls:    totals.TotalCalls,
			FallbackCalls: totals.FallbackCalls,
			ErrorCalls:    totals.ErrorCalls,
			FallbackRate:  fallbackRate,
			ErrorRate:     errorRate,
			TaskQuality:   taskQuality,
			LatencyMs: LatencySummary{
				Mean: avgLatency,
				P50:  avgLatency,
				P95:  avgLatency,
				Max:  avgLatency,
			},
		},
		Cost: CostDashboard{
			TotalInputTokens:      totals.InputTokens,
			TotalOutputTokens:     totals.OutputTokens,
			CacheWriteTokens:      totals.CacheWriteTokens,
			CacheReadTokens:       totals.CacheReadTokens,
			CacheHitRate:          cacheHitRate,
			TotalCostUSD:          spentUSD,
			PlatformBudgetUSD:     cfg.PlatformDailyBudgetUSD,
			PlatformBudgetPercent: budgetPercent,
			PaidMargin:            paidMargin,
			ActiveLearners:        activeLearners,
		},
	}, nil
}

// ServeHTTP handles HTTP routes under /v1/eval/.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	path := strings.TrimPrefix(r.URL.Path, "/v1/eval")
	path = strings.TrimPrefix(path, "/")
	if path == "reports" && r.Method == http.MethodPost {
		token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		provided := sha256.Sum256([]byte(token))
		expected := sha256.Sum256([]byte(s.reportServiceToken))
		matches := subtle.ConstantTimeCompare(provided[:], expected[:])
		if s.reportServiceToken == "" || !bearer || matches != 1 {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "service token required"})
			return
		}
	} else {
		actor := accounts.LearnerID(r.Context())
		if actor == "" {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
			return
		}
		admin, err := pricing.HasAdminAccess(r.Context(), s.pool, actor, accounts.SessionID(r.Context()))
		if err != nil {
			internalError(w, r, err)
			return
		}
		if !admin {
			respondJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access required"})
			return
		}
	}

	switch {
	case path == "dashboards" && r.Method == http.MethodGet:
		d, err := s.GetDashboards(r.Context())
		if err != nil {
			internalError(w, r, err)
			return
		}
		respondJSON(w, http.StatusOK, d)

	case path == "dashboards/reliability" && r.Method == http.MethodGet:
		d, err := s.GetDashboards(r.Context())
		if err != nil {
			internalError(w, r, err)
			return
		}
		respondJSON(w, http.StatusOK, d.Reliability)

	case path == "dashboards/cost" && r.Method == http.MethodGet:
		d, err := s.GetDashboards(r.Context())
		if err != nil {
			internalError(w, r, err)
			return
		}
		respondJSON(w, http.StatusOK, d.Cost)

	case path == "alerts" && r.Method == http.MethodGet:
		alerts, err := s.queries.ListRecentAlertEvents(r.Context(), 50)
		if err != nil {
			internalError(w, r, err)
			return
		}
		respondJSON(w, http.StatusOK, alerts)

	case path == "reports" && r.Method == http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var oversized *http.MaxBytesError
			if errors.As(err, &oversized) {
				respondJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "report body too large"})
			} else {
				respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid report json"})
			}
			return
		}
		var rep Report
		if err := json.Unmarshal(body, &rep); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid report json"})
			return
		}
		id, err := s.SaveReport(r.Context(), rep)
		if err != nil {
			internalError(w, r, err)
			return
		}
		respondJSON(w, http.StatusCreated, map[string]string{"id": id})

	case path == "reports" && r.Method == http.MethodGet:
		reports, err := s.queries.ListEvalReports(r.Context(), 50)
		if err != nil {
			internalError(w, r, err)
			return
		}
		respondJSON(w, http.StatusOK, reports)

	default:
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func internalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "eval request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

func respondJSON(w http.ResponseWriter, code int, v any) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func floatToNumeric(f float64) pgtype.Numeric {
	var num pgtype.Numeric
	_ = num.Scan(fmt.Sprintf("%.4f", f))
	return num
}

func numericToFloat(num pgtype.Numeric) float64 {
	val, err := num.Float64Value()
	if err != nil || !val.Valid {
		return 0
	}
	return val.Float64
}
