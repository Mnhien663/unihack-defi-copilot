package avalanche

import (
	"testing"
	"time"
)

func TestAssessVaultFreshness(t *testing.T) {
	now := time.Date(2026, 11, 11, 13, 0, 0, 0, time.UTC)
	maxAge := 5 * time.Minute // Test fixture, not a production default.

	tests := []struct {
		name       string
		sourceTime time.Time
		maxAge     time.Duration
		want       VaultFreshness
	}{
		{"fresh", now.Add(-time.Minute), maxAge, VaultFreshnessFresh},
		{"exactly at limit", now.Add(-maxAge), maxAge, VaultFreshnessStale},
		{"old data fetched now", now.Add(-time.Hour), maxAge, VaultFreshnessStale},
		{"missing source time", time.Time{}, maxAge, VaultFreshnessUnknown},
		{"future source time", now.Add(time.Second), maxAge, VaultFreshnessUnknown},
		{"invalid max age", now, 0, VaultFreshnessUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AssessVaultFreshness(now, tt.sourceTime, tt.maxAge)
			if got != tt.want {
				t.Fatalf("freshness = %q; want %q", got, tt.want)
			}
		})
	}
}
