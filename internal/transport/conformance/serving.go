package conformance

import "fmt"

// ServingContractID identifies the transport-conformance contract composed by
// a served application cell.
const ServingContractID = "hcmnext.conformance.transport/v1"

// ValidateServingContract proves the closed release-gate and localized
// surface contracts are available on the served path. It is deliberately
// side-effect free; persistence, restart, and action-safety proofs remain
// exercised by their explicit conformance matrices.
func ValidateServingContract() error {
	gate := DefaultReleaseGate()
	items := make([]ReleaseEvidence, 0, len(gate.Required))
	for _, name := range gate.Required {
		items = append(items, ReleaseEvidence{Gate: name, Digest: "serving:" + name})
	}
	decision, err := gate.Admit(items)
	if err != nil {
		return fmt.Errorf("conformance: serving release gate: %w", err)
	}
	if !decision.Admitted {
		return fmt.Errorf("conformance: serving release gate was not admitted")
	}
	if err := decision.VerifyDecision(gate); err != nil {
		return fmt.Errorf("conformance: serving release decision: %w", err)
	}
	if err := QualifySlice(); err != nil {
		return fmt.Errorf("conformance: serving localization and accessibility: %w", err)
	}
	return nil
}
