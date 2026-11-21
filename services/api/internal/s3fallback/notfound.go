package s3fallback

import (
	"errors"
	"net/http"

	"github.com/aws/smithy-go"
)

// ErrNotFound marks a read the store answered with "no such object". Only this error may
// send a read to the old store; auth, throttling and 5xx errors must surface.
var ErrNotFound = errors.New("object not found")

// IsNotFound reports whether an S3 error means the object (or version) does not exist.
func IsNotFound(err error) bool {
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchVersion":
			return true
		}
	}
	var status interface{ HTTPStatusCode() int }
	return errors.As(err, &status) && status.HTTPStatusCode() == http.StatusNotFound
}
