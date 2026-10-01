package projectservice

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrProjectSuspensionRepositoryUnavailable = errors.New("projectservice: project suspension repository unavailable")

// ProjectSuspensionRepository owns the durable suspension command. It is
// separate from archive/restore so the repository can enforce the scoped
// operator role before the revisioned mutation is committed.
type ProjectSuspensionRepository interface {
	SuspendProject(context.Context, string, string, uint64, string, string, string, string) (ProjectRecord, error)
}

type SuspendProjectRequest struct {
	ProjectID, Reason, IdempotencyKey string
	ExpectedProjectRevision           uint64
}

// SuspendProject blocks project writes while leaving authorized reads and
// records access available. The project store performs the final manager /
// scoped-operator check inside the same tenant transaction as the mutation.
func (s Service) SuspendProject(ctx context.Context, principal *trust.Principal, req SuspendProjectRequest) (ProjectRecord, error) {
	if err := validPrincipal(principal); err != nil {
		return ProjectRecord{}, err
	}
	if s.Auth == nil || s.Reads == nil || s.Commands == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	suspender, ok := s.Commands.(ProjectSuspensionRepository)
	if !ok {
		return ProjectRecord{}, ErrProjectSuspensionRepositoryUnavailable
	}
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.Reason) == "" || req.ExpectedProjectRevision == 0 || strings.TrimSpace(req.IdempotencyKey) == "" {
		return ProjectRecord{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.ManageProject); err != nil {
		return ProjectRecord{}, err
	}
	current, err := s.Reads.GetProject(ctx, tenant(principal), req.ProjectID)
	if err != nil {
		return ProjectRecord{}, err
	}
	if current.TenantID != tenant(principal) {
		return ProjectRecord{}, projectaccess.ErrTenantMismatch
	}
	return suspender.SuspendProject(ctx, tenant(principal), req.ProjectID, req.ExpectedProjectRevision, principal.Subject(), "HUMAN", req.Reason, req.IdempotencyKey)
}
