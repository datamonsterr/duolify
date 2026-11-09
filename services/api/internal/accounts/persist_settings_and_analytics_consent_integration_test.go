//go:build integration

package accounts_test

import (
	"context"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPersistSettingsAndConsentInPostgresBackupSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(
		ctx,
		"postgres:18",
		postgres.WithDatabase("settings"),
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
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var learnerID string
	err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('settings fixture') RETURNING id::text").Scan(&learnerID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO learner_profiles(learner_id,locale,time_zone,analytics_opt_out,appearance,motion,narration,narration_speed,captions,text_size,ui_sounds,haptics,daily_xp,reminder_enabled,reminder_time)
VALUES($1::uuid,'en-US','UTC',true,'dark','reduce',false,1.25,false,1.2,true,'off',20,false,'09:30')`, learnerID)
	if err != nil {
		t.Fatal(err)
	}
	var appearance, motion, haptics string
	var narration, captions, sounds, optedOut bool
	var speed, textSize float64
	var xp int
	err = db.Pool.QueryRow(ctx, `SELECT appearance,motion,haptics,narration,captions,ui_sounds,analytics_opt_out,narration_speed::float8,text_size::float8,daily_xp FROM learner_profiles WHERE learner_id=$1::uuid`, learnerID).
		Scan(&appearance, &motion, &haptics, &narration, &captions, &sounds, &optedOut, &speed, &textSize, &xp)
	if err != nil {
		t.Fatal(err)
	}
	if appearance != "dark" || motion != "reduce" || haptics != "off" || narration || captions || !sounds || !optedOut || speed != 1.25 || textSize != 1.2 || xp != 20 {
		t.Fatalf("settings/consent lost: %s %s %s %v %v %v %v %.2f %.2f %d", appearance, motion, haptics, narration, captions, sounds, optedOut, speed, textSize, xp)
	}
}
