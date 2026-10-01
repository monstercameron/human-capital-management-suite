package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/releaseevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// ServedAlignmentSurface exposes the cross-layer alignment contracts through
// the application boundary used by shipped commands. The release journal and
// query envelope remain owned by their packages; this value only makes their
// constructors and projections reachable from a composed application.
type ServedAlignmentSurface struct {
	NewReleaseJournal func() *releaseevidence.Journal
	EmbeddedHistory   func() ([]releaseevidence.Migration, error)
	PlanRollback      func(*releaseevidence.Journal, string, string, string, string, []releaseevidence.Migration) (releaseevidence.RollbackPlan, error)

	QueryVersion          func() int
	ExecuteQuery          func(context.Context, conformance.QueryRequest) (conformance.QueryEnvelope, error)
	ProjectSurface        func(conformance.Surface, conformance.QueryEnvelope) (conformance.SurfaceProjection, error)
	CheckNoninterference  func([]conformance.SurfaceProjection, []values.EntityRef) error
	AssertAllParity       func(conformance.QueryEnvelope, []values.EntityRef) error
	AuthorizeInvalidation func(authz.RepositoryScope, values.Instant, string, uint64, []values.EntityRef) (conformance.Invalidation, error)
}

// NewServedAlignmentSurface returns the alignment contracts reachable from a
// shipped application. It creates no process-wide state; callers own the
// journal and provide the already-evaluated authorization scope.
func NewServedAlignmentSurface() ServedAlignmentSurface {
	return ServedAlignmentSurface{
		NewReleaseJournal:     releaseevidence.NewJournal,
		EmbeddedHistory:       releaseevidence.EmbeddedHistory,
		PlanRollback:          releaseevidence.PlanRollback,
		QueryVersion:          conformance.Version,
		ExecuteQuery:          conformance.Execute,
		ProjectSurface:        conformance.ProjectSurface,
		CheckNoninterference:  conformance.CheckNoninterference,
		AssertAllParity:       conformance.AssertAllParity,
		AuthorizeInvalidation: conformance.AuthorizeInvalidation,
	}
}

// Alignment returns the cross-layer alignment contracts exposed by a
// composed application. A nil application has no served capabilities.
func (a *App) Alignment() ServedAlignmentSurface {
	if a == nil {
		return ServedAlignmentSurface{}
	}
	return NewServedAlignmentSurface()
}
