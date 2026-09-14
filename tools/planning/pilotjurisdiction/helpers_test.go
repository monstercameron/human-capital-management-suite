package pilotjurisdiction

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

// repoRoot is the path from this package's directory to the repository
// root: tools/planning/pilotjurisdiction is three levels below root, exactly
// like tools/planning/scopeceiling.
const repoRoot = "../../.."

const profilePath = repoRoot + "/definitions/planning/gates/select-001-jurisdiction-profile.yaml"

// mustLoadProfile loads the real, checked-in SELECT-001 profile. It does not
// assert Validate() is clean: by design the checked-in profile carries
// exactly one violation (the empty reviewer.name), which
// profile_test.go's PRIMARY test asserts explicitly.
func mustLoadProfile(t *testing.T) JurisdictionProfile {
	t.Helper()
	p, err := LoadProfile(profilePath)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	return *p
}

// mustLoadPinnedCaliforniaPack loads and validates the exact rule-pack
// definition file the checked-in profile pins
// (definitions/legal/packs/states/us-ca.json), through the same
// legal.LoadPackDefinitionFile + PackDefinition.Candidate pipeline every
// other state draft goes through. It is the live cross-check target for
// TestTodo_SELECT_001_Conformance: the profile's ObligationMappings and
// OBLIGATION_KIND Exclusions must together equal exactly this pack's
// populated kinds vs. absent kinds.
func mustLoadPinnedCaliforniaPack(t *testing.T) legal.RulePack {
	t.Helper()
	def, err := legal.LoadPackDefinitionFile(repoRoot + "/definitions/legal/packs/states/us-ca.json")
	if err != nil {
		t.Fatalf("LoadPackDefinitionFile(us-ca.json): %v", err)
	}
	candidate, err := def.Candidate()
	if err != nil {
		t.Fatalf("PackDefinition.Candidate(): %v", err)
	}
	return candidate.Pack()
}
