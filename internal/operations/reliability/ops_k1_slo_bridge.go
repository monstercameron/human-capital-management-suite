package reliability

import "github.com/monstercameron/human-capital-management-suite/internal/operations/slo"

// SLOContractVersion exposes the versioned SLO evaluator used by the served
// reliability package. Keeping this bridge here makes the production
// reliability dependency closure include the evaluator rather than leaving
// it as a test-only contract.
func SLOContractVersion() int { return slo.Version() }
