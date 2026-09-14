package pilotblueprint

import (
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
)

// TestInstantiateNeverMutatesTheBlueprintTemplate is REFACTOR's enforcement
// mechanism: "instantiate the reusable blueprint per tenant; customer-
// specific facts never alter the platform's semantic contracts." It calls
// Instantiate three times against the SAME Blueprint value with three
// different, contradictory sets of tenant facts (blocked, stale, ready) and
// proves the Blueprint's own CanonicalDigest - and therefore every
// workstream's Kind, Owners, Prerequisites, artifacts, AcceptanceOracle,
// DataProcessingBoundary, Escalation and Fallback - is byte-identical
// before and after every call. If Instantiate ever wrote a tenant fact back
// into the Blueprint it was given, this digest would move.
func TestInstantiateNeverMutatesTheBlueprintTemplate(t *testing.T) {
	bp := validFixture()
	before, err := bp.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest(before): %v", err)
	}
	beforeWorkstreams := deepCopy(bp).Workstreams

	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	scenarios := []struct {
		name     string
		customer CustomerFacts
		provider pilotprovider.ProviderTopology
	}{
		{"no customer at all", CustomerFacts{}, pilotprovider.ProviderTopology{}},
		{"customer named but unconfirmed", CustomerFacts{DesignPartnerName: "Acme Corp"}, pilotprovider.ProviderTopology{}},
		{"customer fully confirmed and fresh", fullyConfirmedCustomer(now), realSelectionFixtureProvider()},
	}

	for _, sc := range scenarios {
		_ = Instantiate(bp, sc.customer, sc.provider, reviewedFixtureJurisdiction(), now)

		after, err := bp.CanonicalDigest()
		if err != nil {
			t.Fatalf("CanonicalDigest(after %q): %v", sc.name, err)
		}
		if after != before {
			t.Errorf("scenario %q: Blueprint.CanonicalDigest moved from %s to %s - Instantiate mutated the template", sc.name, before, after)
		}
		if !reflect.DeepEqual(beforeWorkstreams, bp.Workstreams) {
			t.Errorf("scenario %q: bp.Workstreams changed after Instantiate", sc.name)
		}
	}
}

func fullyConfirmedCustomer(now time.Time) CustomerFacts {
	owners := map[WorkstreamKind]OwnerConfirmation{}
	for _, k := range AllWorkstreamKinds() {
		owners[k] = OwnerConfirmation{Name: "Fixture Owner", Contact: "fixture@example.test", ConfirmedAt: now.Add(-time.Hour).Format(time.RFC3339)}
	}
	return CustomerFacts{DesignPartnerName: "Acme Corp", WorkstreamOwners: owners}
}

// TestReadinessReportCarriesNoSemanticContractField is the second half of
// REFACTOR's proof: a ReadinessReport - the only thing tenant facts can ever
// produce - is structurally incapable of carrying a workstream's semantic
// contract (its AcceptanceOracle, DataProcessingBoundary, Escalation or
// Fallback text). This is checked by reflection against the compiled type,
// not by inspecting one instance, so it holds for every possible
// instantiation, not just the ones this test happens to construct.
func TestReadinessReportCarriesNoSemanticContractField(t *testing.T) {
	forbidden := []string{"AcceptanceOracle", "DataProcessingBoundary", "Escalation", "Fallback", "Owners", "Prerequisites", "InputArtifacts", "OutputArtifacts", "Timing"}

	typ := reflect.TypeOf(WorkstreamReadiness{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		for _, f := range forbidden {
			if name == f {
				t.Errorf("WorkstreamReadiness carries semantic-contract field %q - tenant facts must never be able to smuggle contract text back out as if it were an assessment", name)
			}
		}
	}
}
