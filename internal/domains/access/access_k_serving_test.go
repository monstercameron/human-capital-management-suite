package access_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/access"
)

func TestTodo_ACCESS_001_ServedPath(t *testing.T) {
	if access.ServingContractID == "" {
		t.Fatal("access serving contract has no stable identity")
	}
	if err := access.ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}

func TestTodo_ACCESS_002_ServedPath(t *testing.T) {
	if err := access.ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}

func TestTodo_ACCESS_003_ServedPath(t *testing.T) {
	if err := access.ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
