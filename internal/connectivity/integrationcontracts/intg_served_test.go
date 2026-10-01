package integrationcontracts

import "testing"

func TestTodo_INTG_004_ServedContractIsQuarantineAware(t *testing.T) {
	contracts := Contracts()
	if err := Validate(contracts); err != nil {
		t.Fatalf("Validate(Contracts()): %v", err)
	}
	for _, contract := range contracts {
		if contract.Name != SchemaDiscovery {
			continue
		}
		if len(contract.SnapshotStates) != 3 || contract.SnapshotStates[0].String() != "QUARANTINED" {
			t.Fatalf("schema discovery states = %v, want QUARANTINED first and three total states", contract.SnapshotStates)
		}
		return
	}
	t.Fatal("schema discovery contract missing")
}

func TestTodo_INTG_005_ServedContractListsTypedImpacts(t *testing.T) {
	contracts := Contracts()
	for _, contract := range contracts {
		if contract.Name == SchemaDiff {
			if len(contract.Impacts) != 3 {
				t.Fatalf("schema diff impacts = %v, want three typed impacts", contract.Impacts)
			}
			return
		}
	}
	t.Fatal("schema diff contract missing")
}

func TestTodo_INTG_006_ServedContractNamesBoundedIdentity(t *testing.T) {
	contracts := Contracts()
	for _, contract := range contracts {
		if contract.Name == MappingValidate {
			if contract.Identity != "identity" {
				t.Fatalf("mapping identity = %q, want identity", contract.Identity)
			}
			return
		}
	}
	t.Fatal("mapping validation contract missing")
}

func TestTodo_INTG_004_ServedContractRejectsIncompleteManifest(t *testing.T) {
	if err := Validate([]Contract{{Name: SchemaDiscovery}}); err == nil {
		t.Fatal("Validate accepted an incomplete served contract manifest")
	}
	contracts := Contracts()
	contracts[0].SnapshotStates = contracts[0].SnapshotStates[1:]
	if err := Validate(contracts); err == nil {
		t.Fatal("Validate accepted a discovery contract without the quarantine state")
	}
}
