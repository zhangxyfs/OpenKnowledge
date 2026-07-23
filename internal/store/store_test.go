package store

import (
	"strings"
	"testing"
)

func TestTruncateToBudget(t *testing.T) {
	if got := TruncateToBudget(strings.Repeat("好", 100), 10); !strings.HasSuffix(got, "…(已截断)") {
		t.Fatalf("expected truncation marker: %q", got)
	}
	if got := TruncateToBudget("short", 100); got != "short" {
		t.Fatalf("short text should pass through, got %q", got)
	}
}
