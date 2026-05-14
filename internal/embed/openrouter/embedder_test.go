package openrouterembed

import (
	"net/http"
	"testing"
	"time"
)

func TestRequestErrorClassifiesRateLimit(t *testing.T) {
	err := requestError{
		statusCode: http.StatusTooManyRequests,
		status:     "429 Too Many Requests",
		retryAfter: 2 * time.Second,
	}

	if !err.Temporary() {
		t.Fatalf("Temporary() = false, want true")
	}
	if got := err.RetryAfter(); got != 2*time.Second {
		t.Fatalf("RetryAfter() = %s, want 2s", got)
	}
	if err.SplitBatch() {
		t.Fatalf("SplitBatch() = true, want false for rate limit")
	}
}

func TestRequestErrorClassifiesOversizedBatch(t *testing.T) {
	err := requestError{
		statusCode: http.StatusRequestEntityTooLarge,
		status:     "413 Payload Too Large",
		message:    "payload too large",
	}

	if !err.SplitBatch() {
		t.Fatalf("SplitBatch() = false, want true")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("3"); got != 3*time.Second {
		t.Fatalf("parseRetryAfter(seconds) = %s, want 3s", got)
	}

	when := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(when); got <= 0 {
		t.Fatalf("parseRetryAfter(http date) = %s, want positive duration", got)
	}
}
