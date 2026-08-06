package bakery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRateLimitError_Message(t *testing.T) {
	withReset := (&RateLimitError{Resets: time.Unix(1786035515, 0)}).Error()
	if !strings.Contains(withReset, "16:58 UTC") {
		t.Errorf("expected the reset time in the message, got %q", withReset)
	}
	if !strings.Contains(withReset, "GITHUB_TOKEN") {
		t.Errorf("expected the remedy in the message, got %q", withReset)
	}

	zero := (&RateLimitError{}).Error()
	if strings.Contains(zero, "resets at") {
		t.Errorf("zero reset time must not claim a reset instant, got %q", zero)
	}
	if !strings.Contains(zero, "GITHUB_TOKEN") {
		t.Errorf("expected the remedy in the message, got %q", zero)
	}
}

func TestRateLimitErrorFromResponse(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		remaining string
		reset     string
		want      bool
		wantReset int64 // 0 ⇒ expect zero time
	}{
		{
			name: "403 with exhausted quota", status: http.StatusForbidden,
			remaining: "0", reset: "1786035515", want: true, wantReset: 1786035515,
		},
		{
			name: "429 with exhausted quota", status: http.StatusTooManyRequests,
			remaining: "0", reset: "1786035515", want: true, wantReset: 1786035515,
		},
		{
			// A bare 403 is an auth/permission failure — reporting it as a rate
			// limit would send the user chasing the wrong fix.
			name: "403 with quota remaining", status: http.StatusForbidden,
			remaining: "42", want: false,
		},
		{
			name: "403 with no rate limit headers", status: http.StatusForbidden, want: false,
		},
		{
			name: "unrelated status", status: http.StatusNotFound, remaining: "0", want: false,
		},
		{
			name: "unparseable reset header", status: http.StatusForbidden,
			remaining: "0", reset: "not-a-number", want: true,
		},
		{
			name: "non-positive reset header", status: http.StatusForbidden,
			remaining: "0", reset: "0", want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tt.status, Header: http.Header{}}
			if tt.remaining != "" {
				resp.Header.Set("X-RateLimit-Remaining", tt.remaining)
			}
			if tt.reset != "" {
				resp.Header.Set("X-RateLimit-Reset", tt.reset)
			}

			got := rateLimitError(resp)
			if tt.want != (got != nil) {
				t.Fatalf("rateLimitError() = %v, want non-nil: %v", got, tt.want)
			}
			if got == nil {
				return
			}
			if tt.wantReset == 0 {
				if !got.Resets.IsZero() {
					t.Errorf("expected zero reset time, got %v", got.Resets)
				}
				return
			}
			if got.Resets.Unix() != tt.wantReset {
				t.Errorf("Resets = %d, want %d", got.Resets.Unix(), tt.wantReset)
			}
		})
	}
}

// TestFetchCatalogArch_RateLimited is the end-to-end shape the TUI relies on:
// a throttled catalog fetch must surface as a *RateLimitError, not a bare
// "status 403", so the Sysext step can explain itself.
func TestFetchCatalogArch_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1786035515")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewHTTPClientWithURL(srv.URL)
	_, err := c.FetchCatalogArch(context.Background(), "amd64")
	if err == nil {
		t.Fatal("expected an error from a rate-limited catalog fetch")
	}

	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
	}
	if rl.Resets.Unix() != 1786035515 {
		t.Errorf("Resets = %d, want 1786035515", rl.Resets.Unix())
	}
}

// TestFetchCatalogArch_ForbiddenNotRateLimited guards the discrimination: a 403
// without the quota header stays a generic status error.
func TestFetchCatalogArch_ForbiddenNotRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewHTTPClientWithURL(srv.URL)
	_, err := c.FetchCatalogArch(context.Background(), "amd64")
	if err == nil {
		t.Fatal("expected an error")
	}
	var rl *RateLimitError
	if errors.As(err, &rl) {
		t.Fatalf("a bare 403 must not be reported as a rate limit: %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("expected the status in the message, got %q", err)
	}
}
