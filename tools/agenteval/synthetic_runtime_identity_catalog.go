package agenteval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrSyntheticRuntimeIdentity means a configured component could not prove
// that it is the physical store named by an active synthetic provision.
var ErrSyntheticRuntimeIdentity = errors.New("agenteval: synthetic runtime component identity is not attested")

// SyntheticRuntimeComponentRole names an isolated store role in a provision.
type SyntheticRuntimeComponentRole string

const (
	SyntheticGrantStoreRole SyntheticRuntimeComponentRole = "grant-store"
	SyntheticTaskStoreRole  SyntheticRuntimeComponentRole = "task-store"
	SyntheticBudgetRole     SyntheticRuntimeComponentRole = "budget-ledger"
	SyntheticAuditRole      SyntheticRuntimeComponentRole = "audit-store"
	SyntheticToolOwnerRole  SyntheticRuntimeComponentRole = "tool-owner"
)

// SyntheticRuntimePhysicalIdentity is a backend-derived identity. BackendID
// must identify the actual configured resource (for example a database,
// schema and dedicated role), rather than repeat the provision's component ID.
type SyntheticRuntimePhysicalIdentity struct {
	Role      SyntheticRuntimeComponentRole
	TenantID  uuid.UUID
	BackendID string
}

// SyntheticRuntimeComponentID derives the persisted marker ID from an
// attested physical backend identity. Provision issuers and runtime resolvers
// use the same derivation; callers never choose component IDs independently.
func SyntheticRuntimeComponentID(identity SyntheticRuntimePhysicalIdentity) (string, error) {
	if !validSyntheticRuntimeRole(identity.Role) || identity.TenantID == uuid.Nil || strings.TrimSpace(identity.BackendID) == "" {
		return "", ErrSyntheticRuntimeIdentity
	}
	canonical := string(identity.Role) + "\x00" + identity.TenantID.String() + "\x00" + strings.TrimSpace(identity.BackendID)
	digest := sha256.Sum256([]byte(canonical))
	return "synthetic-component/v1/" + hex.EncodeToString(digest[:]), nil
}

func validSyntheticRuntimeRole(role SyntheticRuntimeComponentRole) bool {
	switch role {
	case SyntheticGrantStoreRole, SyntheticTaskStoreRole, SyntheticBudgetRole, SyntheticAuditRole, SyntheticToolOwnerRole:
		return true
	default:
		return false
	}
}

// SyntheticRuntimeIdentityAttestor is implemented by a configured runtime
// component. It must attest from its underlying resource, not from a caller's
// ID string or the persisted marker. Implementations that cannot inspect their
// physical resource must fail closed.
type SyntheticRuntimeIdentityAttestor interface {
	AttestSyntheticRuntimeComponent(context.Context, values.TenantId, uuid.UUID) (SyntheticRuntimePhysicalIdentity, error)
}

// SyntheticRuntimeComponent is a configured object with an attestation
// surface. Component is the actual object later handed to runtime composition;
// it is deliberately not accompanied by a caller-provided component ID.
type SyntheticRuntimeComponent struct {
	Attestor  SyntheticRuntimeIdentityAttestor
	Component any
}

// SyntheticRuntimeResolvedComponents contains the exact components whose
// backend-derived identities matched one persisted provision.
type SyntheticRuntimeResolvedComponents struct {
	Grant  any
	Task   any
	Budget any
	Audit  any
	Owner  any
}

// SyntheticRuntimeIdentityCatalog holds configured components without
// caller-assigned IDs. Each persisted ID is matched only after the component
// attests its physical identity for the resolved tenant.
type SyntheticRuntimeIdentityCatalog struct {
	components []SyntheticRuntimeComponent
}

// NewSyntheticRuntimeIdentityCatalog copies a non-empty set of components.
// Components without a physical attestor are rejected at construction time.
func NewSyntheticRuntimeIdentityCatalog(components []SyntheticRuntimeComponent) (*SyntheticRuntimeIdentityCatalog, error) {
	if len(components) == 0 {
		return nil, ErrSyntheticRuntimeIdentity
	}
	copyOf := append([]SyntheticRuntimeComponent(nil), components...)
	for _, component := range copyOf {
		if component.Attestor == nil || component.Component == nil {
			return nil, ErrSyntheticRuntimeIdentity
		}
	}
	return &SyntheticRuntimeIdentityCatalog{components: copyOf}, nil
}

// Resolve checks that the durable marker IDs name exactly one configured
// physical component for every required role and tenant. Reusing a backend
// identity across roles or selecting an unconfigured ID fails closed.
func (c *SyntheticRuntimeIdentityCatalog) Resolve(ctx context.Context, tenant values.TenantId, tenantUUID uuid.UUID, record agentstore.SyntheticTenantProvisionRecord) (SyntheticRuntimeResolvedComponents, error) {
	if c == nil || ctx == nil || tenant.Validate() != nil || tenantUUID == uuid.Nil || record.TenantID != tenantUUID || record.SuiteTenantID != tenant.String() {
		return SyntheticRuntimeResolvedComponents{}, ErrSyntheticRuntimeIdentity
	}
	wanted := []struct {
		role SyntheticRuntimeComponentRole
		id   string
		set  func(*SyntheticRuntimeResolvedComponents, any)
	}{
		{SyntheticGrantStoreRole, record.GrantStoreID, func(r *SyntheticRuntimeResolvedComponents, v any) { r.Grant = v }},
		{SyntheticTaskStoreRole, record.TaskStoreID, func(r *SyntheticRuntimeResolvedComponents, v any) { r.Task = v }},
		{SyntheticBudgetRole, record.BudgetLedgerID, func(r *SyntheticRuntimeResolvedComponents, v any) { r.Budget = v }},
		{SyntheticAuditRole, record.AuditStoreID, func(r *SyntheticRuntimeResolvedComponents, v any) { r.Audit = v }},
		{SyntheticToolOwnerRole, record.ToolOwnerID, func(r *SyntheticRuntimeResolvedComponents, v any) { r.Owner = v }},
	}
	seenComponentIDs := make(map[string]struct{}, len(wanted))
	seenBackendIDs := make(map[string]struct{}, len(wanted))
	var resolved SyntheticRuntimeResolvedComponents
	for _, target := range wanted {
		if strings.TrimSpace(target.id) == "" {
			return SyntheticRuntimeResolvedComponents{}, ErrSyntheticRuntimeIdentity
		}
		if _, duplicate := seenComponentIDs[target.id]; duplicate {
			return SyntheticRuntimeResolvedComponents{}, ErrSyntheticRuntimeIdentity
		}
		seenComponentIDs[target.id] = struct{}{}
		matches := 0
		for _, component := range c.components {
			identity, err := component.Attestor.AttestSyntheticRuntimeComponent(ctx, tenant, tenantUUID)
			if err != nil {
				return SyntheticRuntimeResolvedComponents{}, fmt.Errorf("%w: attest %s: %v", ErrSyntheticRuntimeIdentity, target.role, err)
			}
			if identity.Role != target.role {
				continue
			}
			if identity.TenantID != tenantUUID || strings.TrimSpace(identity.BackendID) == "" {
				return SyntheticRuntimeResolvedComponents{}, fmt.Errorf("%w: invalid physical identity for %s", ErrSyntheticRuntimeIdentity, target.role)
			}
			componentID, err := SyntheticRuntimeComponentID(identity)
			if err != nil {
				return SyntheticRuntimeResolvedComponents{}, err
			}
			if componentID != target.id {
				continue
			}
			if _, duplicate := seenBackendIDs[identity.BackendID]; duplicate {
				return SyntheticRuntimeResolvedComponents{}, fmt.Errorf("%w: backend identity reused across roles", ErrSyntheticRuntimeIdentity)
			}
			seenBackendIDs[identity.BackendID] = struct{}{}
			target.set(&resolved, component.Component)
			matches++
		}
		if matches != 1 {
			return SyntheticRuntimeResolvedComponents{}, fmt.Errorf("%w: %s identity matched %d configured components", ErrSyntheticRuntimeIdentity, target.role, matches)
		}
	}
	if err := ctx.Err(); err != nil {
		return SyntheticRuntimeResolvedComponents{}, err
	}
	return resolved, nil
}
