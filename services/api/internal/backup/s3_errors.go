package backup

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/aws/smithy-go"
)

// StoreError is a classified object-store failure. Its text carries only the operation and a coarse error
// class (x509, timeout, http_403 ...): never the endpoint, key, credentials, response body or learner data.
// Unwrap returns the underlying cause for errors.Is/As; do not print the cause.
type StoreError struct {
	Op     string // list, head, put, delete, get ...
	Msg    string // stable operator-facing text, e.g. "backup list failed"
	Class  string
	Status int    // HTTP status when the store answered
	Code   string // S3 error code when present
	Reason string // for x509: unknown_authority, hostname, expired, invalid
	cause  error
}

func (e *StoreError) Error() string { return e.Msg + ": " + e.Class }
func (e *StoreError) Unwrap() error { return e.cause }

// classify maps an SDK error to a safe class. It never reads the error text.
func classify(err error) *StoreError {
	out := &StoreError{Class: "unknown", cause: err}
	var verify *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var apiError smithy.APIError
	var response interface{ HTTPStatusCode() int }
	var netError net.Error
	var dns *net.DNSError
	switch {
	case errors.As(err, &unknownAuthority):
		out.Class, out.Reason = "x509", "unknown_authority"
	case errors.As(err, &hostname):
		out.Class, out.Reason = "x509", "hostname"
	case errors.As(err, &invalid):
		out.Class, out.Reason = "x509", "invalid"
		if invalid.Reason == x509.Expired {
			out.Reason = "expired"
		}
	case errors.As(err, &verify):
		out.Class, out.Reason = "x509", "invalid"
	case errors.Is(err, context.Canceled):
		out.Class = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		out.Class = "timeout"
	case errors.As(err, &dns):
		out.Class = "dns"
	case errors.As(err, &response) && response.HTTPStatusCode() != 0:
		out.Status = response.HTTPStatusCode()
		out.Class = fmt.Sprintf("http_%d", out.Status)
	case errors.As(err, &netError) && netError.Timeout():
		out.Class = "timeout"
	case errors.As(err, &netError):
		out.Class = "network"
	}
	if errors.As(err, &apiError) {
		out.Code = apiError.ErrorCode()
	}
	if out.Status == 0 && errors.As(err, &response) {
		out.Status = response.HTTPStatusCode()
	}
	return out
}

// keyPrefix is the first path segment of a key ("account-deletions/"): enough to tell which ledger or
// backup area failed, never an object name (those can embed learner references and dates).
func keyPrefix(key string) string {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i+1]
	}
	return ""
}

// fail logs the underlying cause once at error level with safe fields and returns the safe error.
func (s *S3) fail(ctx context.Context, op, msg, key string, err error) error {
	se := classify(err)
	se.Op, se.Msg = op, msg
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	attrs := []any{"op", op, "bucket", s.Bucket, "key_prefix", keyPrefix(key), "error_class", se.Class}
	if se.Reason != "" {
		attrs = append(attrs, "reason", se.Reason)
	}
	if se.Status != 0 {
		attrs = append(attrs, "http_status", se.Status)
	}
	if se.Code != "" {
		attrs = append(attrs, "s3_code", se.Code)
	}
	logger.ErrorContext(ctx, "object store request failed", attrs...)
	return se
}
