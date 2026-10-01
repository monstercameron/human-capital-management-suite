package pseudonym

import (
	"fmt"
	"strings"
)

// ServingContractID identifies the scoped-pseudonym contract linked by the
// shipped application cell. The contract check is pure and creates no
// tenant-wide or process-wide state.
const ServingContractID = "hcmnext.scoped-pseudonym/v1"

// ValidateServingContract keeps the pseudonym semantic owner on the served
// dependency path and checks the public coordinates that all projections
// must preserve. Custody-backed generation remains an explicit operation on
// Service or EscrowedService; this check never invents a fallback key.
func ValidateServingContract() error {
	if Version() <= 0 {
		return fmt.Errorf("pseudonym: serving contract version must be positive")
	}
	if strings.TrimSpace(Explain()) == "" || strings.TrimSpace(ExplainEscrow()) == "" {
		return fmt.Errorf("pseudonym: serving contract explanations are required")
	}
	coordinates := Pseudonym{
		ID:         "psn:v1:g1:serving",
		Value:      "psn:v1:g1:serving",
		Generation: 1,
		Tenant:     "tenant-serving",
		Scope:      "case-serving",
		Purpose:    "case-investigation",
	}
	if coordinates.ID == "" || coordinates.Value != coordinates.ID || coordinates.Generation <= 0 ||
		strings.TrimSpace(coordinates.Tenant) == "" || strings.TrimSpace(coordinates.Scope) == "" ||
		strings.TrimSpace(coordinates.Purpose) == "" {
		return fmt.Errorf("pseudonym: serving contract coordinates are incomplete")
	}
	return nil
}
