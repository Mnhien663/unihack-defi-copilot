package avalanche

import "time"

type VaultFreshness string

const (
	VaultFreshnessFresh   VaultFreshness = "fresh"
	VaultFreshnessStale   VaultFreshness = "stale"
	VaultFreshnessUnknown VaultFreshness = "unknown"
)

// AssessVaultFreshness evaluates the age of the source data, not the
// time at which the backend fetched it.
func AssessVaultFreshness(
	now time.Time,
	sourceTime time.Time,
	maxAge time.Duration,
) VaultFreshness {
	if now.IsZero() || sourceTime.IsZero() ||
		maxAge <= 0 || sourceTime.After(now) {
		return VaultFreshnessUnknown
	}

	if now.Sub(sourceTime) >= maxAge {
		return VaultFreshnessStale
	}

	return VaultFreshnessFresh
}
