package application

import (
	"github.com/monstercameron/human-capital-management-suite/internal/data/identityprivacystore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/proofing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ServedProofingSurface exposes identity-proofing and work-authorization
// contracts through the application boundary used by hcmnext serve. The
// proofing domain remains the semantic owner; the repository factory keeps
// its typed, tenant-aware persistence port reachable from the same product
// dependency closure as the serving command.
type ServedProofingSurface struct {
	Version                      func() int
	NewEvidenceItem              func(proofing.EvidenceKind, string, string, string, proofing.AssuranceLevel, values.Instant) (proofing.EvidenceItem, error)
	NewProofingSession           func(string, values.EntityRef, string, proofing.AssuranceLevel, []proofing.EvidenceItem, string, values.Instant) (proofing.ProofingSession, error)
	NewWorkAuthorizationEvidence func(proofing.WorkAuthorizationEvidence) (proofing.WorkAuthorizationEvidence, error)
	ConformWorkAuthorization     func(proofing.WorkAuthorizationConformanceRequest) (proofing.WorkAuthorizationConformance, error)
	ExplainSession               func(proofing.ProofingSession) (proofing.ProofingExplanation, error)
	ExplainWorkAuthorization     func(proofing.WorkAuthorizationResult) (proofing.WorkAuthorizationExplanation, error)
	NewRepository                func(identityprivacystore.DB) proofing.Repository
	NewMemoryRepository          func() *proofing.MemoryRepository
}

// NewServedProofingSurface returns the proofing capabilities reachable from
// hcmnext serve. It creates no process-wide state; persistence is constructed
// from the caller-supplied database capability.
func NewServedProofingSurface() ServedProofingSurface {
	return ServedProofingSurface{
		Version:                      proofing.Version,
		NewEvidenceItem:              proofing.NewEvidenceItem,
		NewProofingSession:           proofing.NewProofingSession,
		NewWorkAuthorizationEvidence: proofing.NewWorkAuthorizationEvidence,
		ConformWorkAuthorization:     proofing.ConformWorkAuthorization,
		ExplainSession:               proofing.Explain,
		ExplainWorkAuthorization:     proofing.ExplainWorkAuthorization,
		NewRepository:                func(db identityprivacystore.DB) proofing.Repository { return identityprivacystore.New(db) },
		NewMemoryRepository:          proofing.NewMemoryRepository,
	}
}

// Proofing returns the identity-proofing and work-authorization capabilities
// exposed by a composed application. A nil application has no served
// capabilities.
func (a *App) Proofing() ServedProofingSurface {
	if a == nil {
		return ServedProofingSurface{}
	}
	return NewServedProofingSurface()
}
