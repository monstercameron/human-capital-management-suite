package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/memory"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmemorystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AgentMemoryOperations binds governed raw and search copies to current durable
// policy, source, legal disposition, grants and authenticated workforce facts.
type AgentMemoryOperations struct {
	store         *agentmemorystore.Store
	manager       *memory.Manager
	authority     agentAuthority
	now           func() time.Time
	taskAuthority AgentTaskMemoryAuthority
}

func NewAgentMemoryOperations(db dbport.Beginner, mapper func(values.TenantId) uuid.UUID, roles AgentOperationRoles, now func() time.Time) (*AgentMemoryOperations, error) {
	if roles == nil || now == nil {
		return nil, memory.ErrInvalid
	}
	store, err := agentmemorystore.New(db, mapper, now)
	if err != nil {
		return nil, err
	}
	raw, err := store.Copy("raw")
	if err != nil {
		return nil, err
	}
	search, err := store.Copy("search")
	if err != nil {
		return nil, err
	}
	manager, err := memory.NewManager(memory.Config{Policies: store, Sources: store, Authorizer: store, Disposition: store, Stores: []memory.CopyStore{raw, search}}, now)
	if err != nil {
		return nil, err
	}
	return &AgentMemoryOperations{store: store, manager: manager, authority: agentAuthority{roles: roles, db: db, tenantUUID: mapper}, now: now}, nil
}

// AgentTaskMemoryAuthority reconstructs an invocation from the durable task and
// current delegation owner. Decoded caller-supplied claims are not sufficient.
type AgentTaskMemoryAuthority interface {
	ResolveTaskMemoryInvocation(context.Context, agentrun.AgentTask, agentrun.PlanStep) (agentsystem.Invocation, error)
}

// BindTaskAuthority is a startup composition operation, before serving work.
func (s *AgentMemoryOperations) BindTaskAuthority(authority AgentTaskMemoryAuthority) error {
	if s == nil || authority == nil || s.taskAuthority != nil {
		return memory.ErrInvalid
	}
	s.taskAuthority = authority
	return nil
}
func (s *AgentMemoryOperations) actor(ctx context.Context, p *trust.Principal) (memory.Actor, error) {
	if s == nil {
		return memory.Actor{}, memory.ErrDenied
	}
	if err := currentAgentOperator(ctx, p, s.authority, s.now().UTC()); err != nil {
		return memory.Actor{}, errors.Join(memory.ErrDenied, err)
	}
	return memory.Actor{TenantID: string(p.Tenant()), PrincipalID: p.Subject()}, nil
}
func (s *AgentMemoryOperations) Put(ctx context.Context, p *trust.Principal, item memory.Item) error {
	actor, err := s.actor(ctx, p)
	if err != nil {
		return err
	}
	return s.manager.Put(ctx, actor, item)
}
func (s *AgentMemoryOperations) Read(ctx context.Context, p *trust.Principal, store, item string) (memory.Item, error) {
	actor, err := s.actor(ctx, p)
	if err != nil {
		return memory.Item{}, err
	}
	return s.manager.Read(ctx, actor, store, item)
}
func (s *AgentMemoryOperations) Export(ctx context.Context, p *trust.Principal) ([]memory.Item, error) {
	actor, err := s.actor(ctx, p)
	if err != nil {
		return nil, err
	}
	if err = s.store.AuthorizeOperation(ctx, actor, memory.OperationExport); err != nil {
		return nil, err
	}
	return s.manager.ExportTenant(ctx, actor)
}
func (s *AgentMemoryOperations) Delete(ctx context.Context, p *trust.Principal, item, reason string) error {
	actor, err := s.actor(ctx, p)
	if err != nil {
		return err
	}
	return s.manager.Delete(ctx, actor, item, reason)
}
func (s *AgentMemoryOperations) Revoke(ctx context.Context, p *trust.Principal, pin memory.SourcePin, reason string) error {
	actor, err := s.actor(ctx, p)
	if err != nil {
		return err
	}
	if pin.Purpose == "" {
		return memory.ErrInvalid
	}
	return s.manager.RevokeSource(ctx, actor, pin, reason)
}
func (s *AgentMemoryOperations) Sweep(ctx context.Context, p *trust.Principal) error {
	actor, err := s.actor(ctx, p)
	if err != nil {
		return err
	}
	return s.manager.Sweep(ctx, actor)
}

func (s *AgentMemoryOperations) Inventory(ctx context.Context, p *trust.Principal) ([]memory.Metadata, error) {
	actor, err := s.actor(ctx, p)
	if err != nil {
		return nil, err
	}
	if err = s.store.AuthorizeOperation(ctx, actor, memory.OperationInventory); err != nil {
		return nil, err
	}
	return s.manager.InventoryMetadata(ctx, actor)
}
