package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/ownerops"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentownerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentOperationRoles supplies fresh durable role assignments on every call.
type AgentOperationRoles interface {
	Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error)
}
type AgentOwnerOperations struct {
	store     *agentownerstore.Store
	authority agentAuthority
	now       func() time.Time
}

func NewAgentOwnerOperations(db dbport.Beginner, mapper func(values.TenantId) uuid.UUID, roles AgentOperationRoles, now func() time.Time) (*AgentOwnerOperations, error) {
	if roles == nil || now == nil {
		return nil, ownerops.ErrInvalid
	}
	store, err := agentownerstore.New(db, mapper, now)
	if err != nil {
		return nil, err
	}
	return &AgentOwnerOperations{store, agentAuthority{roles: roles, db: db, tenantUUID: mapper}, now}, nil
}
func currentAgentOperator(ctx context.Context, principal *trust.Principal, authority agentAuthority, now time.Time) error {
	if principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || !principal.Assurance().AtLeast(trust.AssuranceLow) || !now.Before(principal.ExpiresAt()) {
		return ownerops.ErrDenied
	}
	active, err := authority.workerActive(ctx, principal.Tenant(), principal.Subject())
	if err != nil {
		return err
	}
	if !active {
		return ownerops.ErrDenied
	}
	roles, err := authority.roles.Load(ctx, principal.Tenant(), agentOrgScope(principal.Tenant()))
	if err != nil {
		return err
	}
	if !hasActiveRole(roles, principal.Subject()) {
		return ownerops.ErrDenied
	}
	return nil
}
func operationPurpose(audience ownerops.Audience) string {
	if audience == ownerops.AudienceOperator {
		return ownerops.PurposeOperatorOps
	}
	return ownerops.PurposeOwnerDashboard
}

// Dashboard resolves current worker, roles and expiring operation grants before
// reading durable tasks, and returns an audience-specific projection.
func (s *AgentOwnerOperations) Dashboard(ctx context.Context, principal *trust.Principal, audience ownerops.Audience) ([]ownerops.TaskView, error) {
	return s.Trace(ctx, principal, audience, "")
}
func (s *AgentOwnerOperations) Trace(ctx context.Context, principal *trust.Principal, audience ownerops.Audience, taskID string) ([]ownerops.TaskView, error) {
	if s == nil {
		return nil, ownerops.ErrDenied
	}
	if err := currentAgentOperator(ctx, principal, s.authority, s.now().UTC()); err != nil {
		return nil, err
	}
	scope, err := s.store.ResolveScope(ctx, string(principal.Tenant()), principal.Subject(), audience, operationPurpose(audience), taskID)
	if taskID == "" && errors.Is(err, ownerops.ErrDenied) {
		ids, e := s.store.ReadTaskIDs(ctx, string(principal.Tenant()), principal.Subject(), audience, operationPurpose(audience))
		if e != nil {
			return nil, e
		}
		if len(ids) == 0 {
			return nil, ownerops.ErrDenied
		}
		views := []ownerops.TaskView{}
		for _, id := range ids {
			rows, e := s.Trace(ctx, principal, audience, id)
			if e != nil {
				return nil, e
			}
			views = append(views, rows...)
		}
		return views, nil
	}
	if err != nil {
		return nil, err
	}
	records, err := s.store.Records(ctx, string(principal.Tenant()), taskID)
	if err != nil {
		return nil, err
	}
	return ownerops.ProjectTasks(scope, records)
}

// Stop derives actor and tenant from authentication. The durable controller
// repeats grant and ownership checks inside the transaction that fences work.
func (s *AgentOwnerOperations) Stop(ctx context.Context, principal *trust.Principal, audience ownerops.Audience, request ownerops.StopRequest) (string, error) {
	if s == nil {
		return "", ownerops.ErrDenied
	}
	if err := currentAgentOperator(ctx, principal, s.authority, s.now().UTC()); err != nil {
		return "", err
	}
	if request.TenantID != "" && request.TenantID != string(principal.Tenant()) {
		return "", ownerops.ErrDenied
	}
	request.TenantID = string(principal.Tenant())
	scope, err := s.store.ResolveScope(ctx, request.TenantID, principal.Subject(), audience, operationPurpose(audience), request.TaskID)
	if err != nil {
		return "", err
	}
	return ownerops.Stop(ctx, scope, request, s.store)
}
