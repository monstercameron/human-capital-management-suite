package application

import "testing"

func TestTodo_UXFLOW_001_Conformance(t *testing.T) {
	contracts, err := NewExperienceContracts()
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts.Stages) == 0 || contracts.Owners == nil {
		t.Fatal("serve composition omitted a user-flow contract")
	}
	if owner, ok := contracts.Owners.Resolve("hcmnext.people.explain_worker_state/v1"); !ok || owner != "PEOPLE" {
		t.Fatalf("stable specification owner unresolved: %q %v", owner, ok)
	}
}

func TestTodo_UXFLOW_003_Conformance(t *testing.T) {
	contracts, err := NewExperienceContracts()
	if err != nil {
		t.Fatal(err)
	}
	if contracts.Dispositions == nil || contracts.Dispositions.Len() == 0 {
		t.Fatal("serve composition omitted the BusinessIntent flow disposition index")
	}
}
