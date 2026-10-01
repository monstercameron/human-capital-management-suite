package timeclockstore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func payloadProfile(end bool) timeprofile.TimeProfile {
	profile := timeprofile.TimeProfile{
		ID: "tp-worker", Version: 2, TenantRef: values.TenantId("tenant-a"),
		EffectiveFrom: values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		Capture:       timeprofile.CapturePunch, PayBasis: timeprofile.PayHourly, Exemption: timeprofile.NonExempt,
		Category: timeprofile.CategoryEmployee, OvertimeMethod: timeprofile.OvertimeSingleRate,
		OvertimeJurisdictions: []string{"US-FED", "US-CO"}, AggregationKey: "worker:a", Destination: timeprofile.DestinationPayroll,
	}
	if end {
		profile.EffectiveTo = values.NewInstant(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	}
	return profile
}

// TestTodo_UXBLIND_123_ProfilePayloadRoundTripsOpenAndBoundedProfiles proves
// the persisted profile document decodes to the profile that was encoded, for
// the open-ended profile an unset effective-to instant would otherwise refuse
// to marshal, and for a bounded one.
func TestTodo_UXBLIND_123_ProfilePayloadRoundTripsOpenAndBoundedProfiles(t *testing.T) {
	for name, profile := range map[string]timeprofile.TimeProfile{"open-ended": payloadProfile(false), "bounded": payloadProfile(true)} {
		payload, err := EncodeProfilePayload(profile)
		if err != nil {
			t.Fatalf("%s: encode: %v", name, err)
		}
		var decoded timeprofile.TimeProfile
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if err := decoded.Validate(); err != nil {
			t.Fatalf("%s: decoded profile invalid: %v", name, err)
		}
		want, _ := profile.Digest()
		got, _ := decoded.Digest()
		if want == "" || want != got || decoded.EffectiveTo.IsSet() != profile.EffectiveTo.IsSet() {
			t.Fatalf("%s: digest %q vs %q, effective-to set %v vs %v", name, want, got, decoded.EffectiveTo.IsSet(), profile.EffectiveTo.IsSet())
		}
		if got := strings.Count(string(payload), `"EffectiveTo"`); got != map[bool]int{false: 0, true: 1}[profile.EffectiveTo.IsSet()] {
			t.Fatalf("%s: payload has %d EffectiveTo fields: %s", name, got, payload)
		}
	}
}

func TestTodo_UXBLIND_123_ProfilePayloadRefusesInvalidProfile(t *testing.T) {
	invalid := payloadProfile(false)
	invalid.AggregationKey = ""
	if payload, err := EncodeProfilePayload(invalid); err == nil || payload != nil {
		t.Fatalf("an invalid profile was encoded: %s err=%v", payload, err)
	}
}
