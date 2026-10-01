package version

import (
	"errors"
	"testing"
)

func TestTodo_WFPAGE_028(t *testing.T) {
	oldInputs := []PageBinding{{Path: "person.name", Type: "STRING"}, {Path: "person.salary", Type: "MONEY"}}
	nextInputs := []PageBinding{{Path: "person.name", Type: "STRING"}, {Path: "person.salary", Type: "DECIMAL"}, {Path: "person.start", Type: "DATE"}}
	rebased := RebasePageBindings(oldInputs, oldInputs, nextInputs)
	if len(rebased.Changes) != 2 || len(rebased.Unresolved) != 2 || rebased.Unresolved[0] != "person.salary" || rebased.Unresolved[1] != "person.start" {
		t.Fatalf("rebase = %+v, want listed retype and add conflicts", rebased)
	}
	if err := ValidatePageBindings([]PageBinding{{Path: "person.name", Type: "STRING"}}, nextInputs); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_WFPAGE_028_Security(t *testing.T) {
	err := ValidatePageBindings([]PageBinding{{Path: "person.salary", Type: "MONEY"}}, []PageBinding{{Path: "person.salary", Type: "DECIMAL"}})
	if !errors.Is(err, ErrPageBindingUnresolved) {
		t.Fatalf("retyped binding validation = %v, want ErrPageBindingUnresolved", err)
	}
	if err := ValidatePageBindings([]PageBinding{{Path: "person.name", Type: "STRING"}, {Path: "person.name", Type: "STRING"}}, []PageBinding{{Path: "person.name", Type: "STRING"}}); !errors.Is(err, ErrPageBindingInvalid) {
		t.Fatalf("duplicate binding validation = %v, want ErrPageBindingInvalid", err)
	}
}
