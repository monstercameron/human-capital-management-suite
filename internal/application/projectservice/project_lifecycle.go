package projectservice

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrProjectLifecycleRepositoryUnavailable = errors.New("projectservice: project lifecycle repository unavailable")

type ProjectLifecycleRepository interface {
	UpdateProjectSettings(context.Context, string, string, string, string, uint64, string, string) (ProjectRecord, error)
	TransitionProject(context.Context, string, string, project.Lifecycle, uint64, string, string) (ProjectRecord, error)
}

type UpdateProjectSettingsRequest struct {
	ProjectID, Name, Timezone, IdempotencyKey string
	ExpectedProjectRevision                   uint64
}

type SetProjectLifecycleRequest struct {
	ProjectID, IdempotencyKey string
	ExpectedProjectRevision   uint64
}

func (s Service) UpdateProjectSettings(ctx context.Context, principal *trust.Principal, req UpdateProjectSettingsRequest) (ProjectRecord, error) {
	if err := validPrincipal(principal); err != nil {
		return ProjectRecord{}, err
	}
	if s.Auth == nil || s.Reads == nil || s.Commands == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	lifecycle, ok := s.Commands.(ProjectLifecycleRepository)
	if !ok {
		return ProjectRecord{}, ErrProjectLifecycleRepositoryUnavailable
	}
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Timezone) == "" || req.ExpectedProjectRevision == 0 || req.IdempotencyKey == "" {
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
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.ManageProject); err != nil {
		return ProjectRecord{}, err
	}
	return lifecycle.UpdateProjectSettings(ctx, tenant(principal), req.ProjectID, req.Name, req.Timezone, req.ExpectedProjectRevision, principal.Subject(), req.IdempotencyKey)
}

func (s Service) ArchiveProject(ctx context.Context, principal *trust.Principal, req SetProjectLifecycleRequest) (ProjectRecord, error) {
	return s.setProjectLifecycle(ctx, principal, req, project.LifecycleArchived)
}

func (s Service) RestoreProject(ctx context.Context, principal *trust.Principal, req SetProjectLifecycleRequest) (ProjectRecord, error) {
	return s.setProjectLifecycle(ctx, principal, req, project.LifecycleActive)
}

func (s Service) setProjectLifecycle(ctx context.Context, principal *trust.Principal, req SetProjectLifecycleRequest, target project.Lifecycle) (ProjectRecord, error) {
	if err := validPrincipal(principal); err != nil {
		return ProjectRecord{}, err
	}
	if s.Auth == nil || s.Reads == nil || s.Commands == nil {
		return ProjectRecord{}, ErrUnavailable
	}
	lifecycle, ok := s.Commands.(ProjectLifecycleRepository)
	if !ok {
		return ProjectRecord{}, ErrProjectLifecycleRepositoryUnavailable
	}
	if strings.TrimSpace(req.ProjectID) == "" || req.ExpectedProjectRevision == 0 || req.IdempotencyKey == "" {
		return ProjectRecord{}, ErrInvalidRequest
	}
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.Archive); err != nil {
		return ProjectRecord{}, err
	}
	current, err := s.Reads.GetProject(ctx, tenant(principal), req.ProjectID)
	if err != nil {
		return ProjectRecord{}, err
	}
	if current.TenantID != tenant(principal) {
		return ProjectRecord{}, projectaccess.ErrTenantMismatch
	}
	if err := s.Auth.Authorize(ctx, principal, req.ProjectID, projectaccess.Archive); err != nil {
		return ProjectRecord{}, err
	}
	return lifecycle.TransitionProject(ctx, tenant(principal), req.ProjectID, target, req.ExpectedProjectRevision, principal.Subject(), req.IdempotencyKey)
}
