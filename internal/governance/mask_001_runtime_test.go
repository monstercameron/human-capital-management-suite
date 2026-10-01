package governance

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/masking"
)

func TestTodo_MASK_001_Served(t *testing.T) {
	source, profile := servedMaskingFixture()
	runtime := Compose(baseReq()).MaskingRuntime()

	result, err := runtime.GenerateMaskedDataset(source, profile, fixedNow)
	if err != nil {
		t.Fatalf("GenerateMaskedDataset: %v", err)
	}
	if result.Marker == "" || len(result.Rows) != len(source.Rows) {
		t.Fatalf("served masking result = %+v", result)
	}
	if result.Rows[0].Values["name"] == source.Rows[0].Values["name"] {
		t.Fatal("served masking boundary returned a direct identifier")
	}

	if err := runtime.DestroyMaskedDataset(&result); err != nil {
		t.Fatalf("DestroyMaskedDataset: %v", err)
	}
	if !result.Destroyed || len(result.Rows) != 0 {
		t.Fatalf("served masking destruction = %+v", result)
	}
}

func servedMaskingFixture() (masking.Dataset, masking.Profile) {
	source := masking.Dataset{
		SourceAuthority:   "authority:served-fixture",
		SourceEnvironment: "SANDBOX",
		TenantID:          "tenant-served",
		Purpose:           "conformance",
		ProfileVersion:    "served-profile-1",
		Rows: []masking.SourceRow{{
			ID: "worker-1",
			Values: map[string]string{
				"name": "Served Example",
			},
		}},
	}
	return source, masking.Profile{
		Version:           source.ProfileVersion,
		TenantID:          source.TenantID,
		Purpose:           source.Purpose,
		SourceAuthority:   source.SourceAuthority,
		SourceEnvironment: source.SourceEnvironment,
		Secret:            []byte("served-mask-secret-012345"),
		ExpiresAt:         fixedNow.Add(time.Hour),
		Fields:            map[string]masking.FieldRule{"name": {Kind: masking.FieldDirectID}},
	}
}
