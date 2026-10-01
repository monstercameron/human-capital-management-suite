package application

import (
	"crypto/ed25519"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding/readiness"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/adoption"
)

// ServedCustomerReadinessSurface exposes the customer onboarding and role
// adoption gates through the application boundary used by hcmnext serve. The
// readiness and adoption packages remain the semantic owners; this value only
// makes their side-effect-free contracts reachable from a composed process.
type ServedCustomerReadinessSurface struct {
	ReadinessVersion  func() int
	ExplainReadiness  func() string
	EvaluateReadiness func(readiness.Rehearsal) (readiness.Result, error)
	SignReadiness     func(readiness.Result, string, ed25519.PrivateKey) (readiness.SignedEvidence, error)
	VerifyReadiness   func(readiness.SignedEvidence, ed25519.PublicKey) error

	ExecuteCutover   func(readiness.CutoverInput) (readiness.CutoverReport, error)
	NewCutoverCell   func() *readiness.CutoverCell
	CheckAdoption    func(adoption.Matrix, time.Time) (adoption.Report, error)
	AdoptionRegistry func() []adoption.JourneySpec
}

// NewServedCustomerReadinessSurface returns the customer readiness
// capabilities reachable from a composed serving application. It creates no
// process-wide or tenant-wide state; callers provide each rehearsal, drill,
// matrix, and signing key explicitly.
func NewServedCustomerReadinessSurface() ServedCustomerReadinessSurface {
	return ServedCustomerReadinessSurface{
		ReadinessVersion:  readiness.Version,
		ExplainReadiness:  readiness.Explain,
		EvaluateReadiness: readiness.Evaluate,
		SignReadiness:     readiness.Sign,
		VerifyReadiness:   readiness.Verify,
		ExecuteCutover:    readiness.ExecuteCutover,
		NewCutoverCell:    readiness.NewCutoverCell,
		CheckAdoption:     adoption.Check,
		AdoptionRegistry:  adoption.RegistryMatrix,
	}
}

// CustomerReadiness returns the customer readiness capabilities exposed by a
// composed application. A nil application has no served capabilities.
func (a *App) CustomerReadiness() ServedCustomerReadinessSurface {
	if a == nil {
		return ServedCustomerReadinessSurface{}
	}
	return NewServedCustomerReadinessSurface()
}
