package application

import (
	"crypto/ed25519"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/residency"
)

// ServedResidencySurface exposes the signed residency admission contract
// through the application boundary used by hcmnext serve. The residency
// package remains the semantic owner; this projection only makes its
// value-owned policy and inventory decisions reachable from a composed
// serving process.
type ServedResidencySurface struct {
	Version   func() int
	NewPolicy func(string, string, residency.JurisdictionSet, []residency.Rule) (residency.Policy, error)
	Sign      func(residency.Policy, string, ed25519.PrivateKey) (residency.Policy, error)
	Verify    func(residency.Policy) error
	Evaluate  func(residency.Policy, residency.Inventory) (residency.Decision, error)
	Explain   func(residency.Decision) string
}

// NewServedResidencySurface returns the residency capabilities reachable from
// a composed serving application. It creates no process-wide or tenant-wide
// state; callers supply the signed policy and tenant-scoped inventory.
func NewServedResidencySurface() ServedResidencySurface {
	return ServedResidencySurface{
		Version:   residency.Version,
		NewPolicy: residency.NewPolicy,
		Sign:      residency.Policy.Sign,
		Verify:    residency.Policy.Verify,
		Evaluate:  residency.Policy.Evaluate,
		Explain:   residency.Explain,
	}
}

// Residency returns the signed residency admission surface exposed by a
// composed application. A nil application has no served capabilities.
func (a *App) Residency() ServedResidencySurface {
	if a == nil {
		return ServedResidencySurface{}
	}
	return NewServedResidencySurface()
}
