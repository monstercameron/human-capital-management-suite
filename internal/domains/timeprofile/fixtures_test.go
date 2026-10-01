package timeprofile

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func valuesTenant(s string) values.TenantId { return values.TenantId(s) }

func instant(t *testing.T, rfc3339 string) values.Instant {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatalf("parse fixture instant %q: %v", rfc3339, err)
	}
	return values.NewInstant(tm)
}

// baseProfile is a valid, punch-capture, non-exempt hourly employee profile.
// Every test that needs a valid profile starts here and overrides only the
// fields that matter to it, so a change to an unrelated field never breaks
// an unrelated test.
func baseProfile(t *testing.T) TimeProfile {
	t.Helper()
	return TimeProfile{
		ID:                    "tp-001",
		Version:               1,
		TenantRef:             values.TenantId("acme-co"),
		EffectiveFrom:         instant(t, "2026-01-01T00:00:00Z"),
		Capture:               CapturePunch,
		PayBasis:              PayHourly,
		Exemption:             NonExempt,
		Category:              CategoryEmployee,
		OvertimeMethod:        OvertimeSingleRate,
		AggregationKey:        "worker:w-1001",
		OvertimeJurisdictions: []string{"US-FED", "US-CA"},
		Destination:           DestinationPayroll,
	}
}

func mustValid(t *testing.T, p TimeProfile) TimeProfile {
	t.Helper()
	if err := p.Validate(); err != nil {
		t.Fatalf("expected a valid fixture profile, got: %v", err)
	}
	return p
}
