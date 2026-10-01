package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/attest"
)

// ServedAttestationSurface exposes the attestation contracts reachable from a
// composed hcmnext application. It owns no response, tenant or authorization
// state; callers supply the current stores and request evidence explicitly.
// The attestation package remains the semantic owner of every decision.
type ServedAttestationSurface struct {
	Bind                  func(attest.Request) (attest.Binding, error)
	VerifyBinding         func(attest.Request, attest.Binding) error
	VerifyStatement       func(*attest.Directory, attest.Statement, attest.Requirement, *trust.Principal, time.Time) (attest.Decision, error)
	RecordResponse        func(context.Context, attest.ResponseStore, attest.TrustedClock, attest.ResponseRequest) (attest.Response, error)
	RevalidateAtExecution func(context.Context, attest.ResponseStore, attest.TrustedClock, attest.ExecutionRequirement) (attest.ExecutionDecision, error)
	EnforceBeforeEffect   func(context.Context, attest.ResponseStore, attest.TrustedClock, attest.ExecutionRequirement, func(context.Context, attest.ExecutionDecision) error) (attest.ExecutionDecision, error)
	ExportEvidencePackage func(attest.ExportRequest) (attest.EvidencePackage, error)
	VerifyEvidencePackage func(attest.EvidencePackage) attest.Verification
	ProveConformance      func([]attest.ConformanceCase) (attest.ConformanceReport, error)
}

// NewServedAttestationSurface returns the attestation capabilities available
// through the served application boundary. All functions delegate directly to
// the trust/attest semantic owner and retain its fail-closed behavior.
func NewServedAttestationSurface() ServedAttestationSurface {
	return ServedAttestationSurface{
		Bind:                  attest.Bind,
		VerifyBinding:         attest.VerifyBinding,
		VerifyStatement:       attest.Verify,
		RecordResponse:        attest.RecordResponse,
		RevalidateAtExecution: attest.RevalidateAtExecution,
		EnforceBeforeEffect:   attest.EnforceBeforeEffect,
		ExportEvidencePackage: attest.ExportEvidencePackage,
		VerifyEvidencePackage: attest.VerifyEvidencePackage,
		ProveConformance:      attest.ProveConformance,
	}
}

// Attestation returns the immutable attestation surface exposed by a
// composed application. A nil application has no served capabilities.
func (a *App) Attestation() ServedAttestationSurface {
	if a == nil {
		return ServedAttestationSurface{}
	}
	return NewServedAttestationSurface()
}
