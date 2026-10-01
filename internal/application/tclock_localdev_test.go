package application

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application/timeclockstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// TestTodo_UXBLIND_123_SeededProfilesPersistForEveryIronridgeWorker proves the
// demo seed can be stored and read back for every seeded Ironridge worker: the
// open-ended profile has no effective-to instant, which an unset Instant
// cannot marshal, and the seed once failed the whole server boot on it.
func TestTodo_UXBLIND_123_SeededProfilesPersistForEveryIronridgeWorker(t *testing.T) {
	pack, ok := demoworkforce.PackFor(demoworkforce.IronridgeKey)
	if !ok {
		t.Fatal("the Ironridge demo pack is not registered")
	}
	employees, err := pack.Plan(pgstore.TenantID(demoworkforce.IronridgeKey))
	if err != nil {
		t.Fatal(err)
	}
	if len(employees) == 0 {
		t.Fatal("the Ironridge plan seeded nobody")
	}
	var punch, exempt int
	for _, employee := range employees {
		row := employee.Row
		profile := demoTimeProfile(demoworkforce.IronridgeKey, row)
		payload, err := timeclockstore.EncodeProfilePayload(profile)
		if err != nil {
			t.Fatalf("%s: encode: %v", row.WorkerKey, err)
		}
		var decoded timeprofile.TimeProfile
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("%s: decode: %v", row.WorkerKey, err)
		}
		if err := decoded.Validate(); err != nil {
			t.Fatalf("%s: decoded profile invalid: %v", row.WorkerKey, err)
		}
		want, wantErr := profile.Digest()
		got, gotErr := decoded.Digest()
		if wantErr != nil || gotErr != nil || want != got {
			t.Fatalf("%s: digest changed across persistence: %q vs %q (%v, %v)", row.WorkerKey, want, got, wantErr, gotErr)
		}
		hourly := row.PayBasis == demoworkforce.PayBasisHourly
		if hourly != (decoded.Capture == timeprofile.CapturePunch) {
			t.Fatalf("%s: pay basis %q got capture %q", row.WorkerKey, row.PayBasis, decoded.Capture)
		}
		if hourly {
			punch++
			if decoded.Exemption != timeprofile.NonExempt {
				t.Fatalf("%s: hourly worker is %q, want NON_EXEMPT", row.WorkerKey, decoded.Exemption)
			}
		} else {
			exempt++
			if decoded.Exemption != timeprofile.Exempt {
				t.Fatalf("%s: salaried worker is %q, want EXEMPT", row.WorkerKey, decoded.Exemption)
			}
		}
	}
	if punch == 0 || exempt == 0 {
		t.Fatalf("seed produced %d punch and %d exempt profiles, want both", punch, exempt)
	}
}

func TestTodo_UXBLIND_123_EncodeProfilePayloadRefusesInvalidProfile(t *testing.T) {
	if _, err := timeclockstore.EncodeProfilePayload(timeprofile.TimeProfile{}); err == nil {
		t.Fatal("an invalid profile was encoded for persistence")
	}
}
