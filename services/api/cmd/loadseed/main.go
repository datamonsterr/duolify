// Command loadseed prepares a throwaway compose stack for the load baseline (E18-02).
// It never runs against a real environment: it refuses anything but DUOLIFY_ENV=development.
//
//	loadseed pricing <tutor-deadline-seconds>   before the API starts: long Tutor deadline
//	loadseed fixtures <learner-id>              after sign-in: course, lesson, manifest, credits
//
// Fixtures reuse playerfixture.Seed (the player e2e course) and the e2e SQL for credits.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
)

const loadVersion = "usd-load-baseline"

func main() {
	if os.Getenv("DUOLIFY_ENV") != "development" || len(os.Args) < 3 {
		log.Fatal("usage: DUOLIFY_ENV=development loadseed pricing <seconds> | fixtures <learner-id>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := store.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	switch os.Args[1] {
	case "pricing":
		secs, err := strconv.Atoi(os.Args[2])
		if err != nil || secs < 30 {
			log.Fatal("tutor deadline seconds must be >= 30")
		}
		// The launch catalogue caps a Tutor reply at 30 s (tutor_note_deadline_seconds), which makes a
		// 10-minute stream impossible. Same pattern as the pricing e2e tests: add an approved version
		// that only raises that deadline; it exists only in this throwaway database.
		_, err = db.Pool.Exec(ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config)
			SELECT $1, t, true, jsonb_set(jsonb_set(jsonb_set(config,'{version}',to_jsonb($1::text)),'{effective_from}',to_jsonb(to_char(t AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'))),'{limits,tutor_note_deadline_seconds}',to_jsonb($2::int))
			FROM (SELECT date_trunc('second', now()) - interval '1 minute' AS t) s, (SELECT config FROM pricing_versions WHERE approved AND effective_from<=now() ORDER BY effective_from DESC LIMIT 1) c`, loadVersion, secs)
		if err != nil {
			log.Fatalf("pricing seed: %v", err)
		}
		// Quotes need the quote policy of the version they price from; copy the one this version was cloned from.
		if _, err = db.Pool.Exec(ctx, `INSERT INTO pricing_quote_policies(version,policy)
			SELECT $1, p.policy FROM pricing_quote_policies p JOIN (SELECT version FROM pricing_versions WHERE approved AND version<>$1 ORDER BY effective_from DESC LIMIT 1) v USING(version)`, loadVersion); err != nil {
			log.Fatalf("pricing seed: %v", err)
		}
		// The runtime catalogue only prices from versions with an activation event.
		if _, err = db.Pool.Exec(ctx, `INSERT INTO pricing_activation_events(version) VALUES($1)`, loadVersion); err != nil {
			log.Fatalf("pricing seed: %v", err)
		}
	case "fixtures":
		owner := os.Args[2]
		cat, err := pricing.Load(ctx, db.Queries)
		if err != nil {
			log.Fatalf("fixtures seed: %v", err)
		}
		lesson, err := playerfixture.Seed(ctx, db.Pool, player.NewS3(os.Getenv), owner)
		if err != nil {
			log.Fatalf("fixtures seed: %v", err)
		}
		var course string
		if err := db.Pool.QueryRow(ctx, `SELECT u.course_id::text FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1::uuid`, lesson).Scan(&course); err != nil {
			log.Fatalf("fixtures seed: %v", err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,100000,'purchase','load-baseline-fund',$2) ON CONFLICT DO NOTHING`, owner, cat.Config().Version); err != nil {
			log.Fatalf("fixtures seed: %v", err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"course": course, "lesson": lesson, "pricing_version": cat.Config().Version})
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	}
}
