package admin

import "testing"

func TestAdminCenterContracts(t *testing.T) {
	contracts := AdminCenterContracts()
	if len(contracts) != 4 {
		t.Fatalf("catalog length = %d, want 4: %+v", len(contracts), contracts)
	}
	want := map[string]struct {
		version           string
		redactionRequired bool
		readOnly          bool
	}{
		"ADMIN-002": {version: "hcmnext.admincenter.workflow-inspector/1", redactionRequired: true, readOnly: true},
		"ADMIN-005": {version: "hcmnext.admincenter.incident-repair/1", redactionRequired: true, readOnly: false},
		"ADMIN-006": {version: "hcmnext.admincenter.diagnostic-session/1", redactionRequired: true, readOnly: true},
		"ADMIN-007": {version: "hcmnext.admincenter.evidence-export/1", redactionRequired: true, readOnly: true},
	}
	for _, contract := range contracts {
		wantContract, ok := want[contract.ID]
		if !ok {
			t.Fatalf("unexpected contract %q", contract.ID)
		}
		if contract.Version != wantContract.version || contract.RedactionRequired != wantContract.redactionRequired || contract.ReadOnly != wantContract.readOnly {
			t.Errorf("contract %q = %+v, want version=%q redaction=%t read_only=%t", contract.ID, contract, wantContract.version, wantContract.redactionRequired, wantContract.readOnly)
		}
		delete(want, contract.ID)
	}
	if len(want) != 0 {
		t.Fatalf("catalog omitted contracts: %v", want)
	}
}
