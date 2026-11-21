// Command s3-conformance checks an S3-compatible store against every operation
// the product relies on. Configuration is read from S3_CONFORMANCE_* variables
// and credentials never appear in the output.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/s3conformance"
)

func main() { os.Exit(run()) }

func run() int {
	cfg, err := s3conformance.ConfigFromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "s3-conformance:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	bucket := cfg.Bucket
	if bucket == "" {
		bucket = "(own temporary bucket)"
	}
	fmt.Printf("S3 conformance against %s, bucket %s\n", cfg.Endpoint, bucket)
	failed := 0
	results, warnings := s3conformance.Run(ctx, cfg)
	for _, r := range results {
		fmt.Println(cfg.Redact(r.String()))
		if r.Status != s3conformance.StatusPass {
			failed++
		}
	}
	for _, w := range warnings {
		fmt.Println(cfg.Redact("WARN  " + w))
	}
	if failed > 0 {
		fmt.Printf("FAILED: %d unsupported operation(s)\n", failed)
		return 1
	}
	fmt.Println("OK: all operations supported")
	return 0
}
