// Command s3-copy-verify copies all object versions and delete markers between two
// buckets and verifies them. It never writes to the source (ADR 0014, E18-06).
//
// Source: COPY_SRC_ENDPOINT, _REGION, _BUCKET, _ACCESS_KEY_ID, _SECRET_ACCESS_KEY.
// Target: the same with COPY_DST_. COPY_VERIFY_SAMPLE caps checksummed versions (default 200; 0 = all).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/datamonsterr/duolify/services/api/internal/s3copy"
)

type side struct{ endpoint, region, bucket, accessKey, secretKey string }

func load(prefix string) (side, error) {
	s := side{os.Getenv(prefix + "_ENDPOINT"), os.Getenv(prefix + "_REGION"), os.Getenv(prefix + "_BUCKET"), os.Getenv(prefix + "_ACCESS_KEY_ID"), os.Getenv(prefix + "_SECRET_ACCESS_KEY")}
	for name, v := range map[string]string{"_ENDPOINT": s.endpoint, "_REGION": s.region, "_BUCKET": s.bucket, "_ACCESS_KEY_ID": s.accessKey, "_SECRET_ACCESS_KEY": s.secretKey} {
		if v == "" {
			return s, errors.New(prefix + name + " is required")
		}
	}
	return s, nil
}

func (s side) client() *s3.Client {
	return s3.New(s3.Options{Region: s.region, BaseEndpoint: aws.String(s.endpoint), UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(s.accessKey, s.secretKey, ""), RetryMaxAttempts: 3,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired})
}

func run(ctx context.Context) error {
	src, err := load("COPY_SRC")
	if err != nil {
		return err
	}
	dst, err := load("COPY_DST")
	if err != nil {
		return err
	}
	if src.endpoint == dst.endpoint && src.bucket == dst.bucket {
		return errors.New("source and target must differ")
	}
	sample := 200
	if v := os.Getenv("COPY_VERIFY_SAMPLE"); v != "" {
		if sample, err = strconv.Atoi(v); err != nil || sample < 0 {
			return errors.New("invalid COPY_VERIFY_SAMPLE")
		}
	}
	// Credentials are never printed.
	fmt.Printf("copy %s/%s -> %s/%s (credentials redacted, checksum sample %d)\n", src.endpoint, src.bucket, dst.endpoint, dst.bucket, sample)
	report, err := s3copy.Run(ctx, s3copy.Options{Src: src.client(), SrcBucket: src.bucket, Dst: dst.client(), DstBucket: dst.bucket, SampleSize: sample})
	if report != nil {
		fmt.Print(report.String())
	}
	if err != nil {
		return err
	}
	if err = report.Err(); err != nil {
		return err
	}
	fmt.Println("verified: count, bytes, checksum sample and version history match")
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "s3-copy-verify:", err)
		os.Exit(1)
	}
}
