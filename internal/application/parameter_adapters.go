package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/orggraph"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/organization"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const TenantParameterCatalogID = "tenant.parameters"

var (
	ErrParameterCatalogUnavailable = errors.New("application: active tenant parameter catalog unavailable")
	ErrParameterWriteDenied        = errors.New("application: tenant parameter write denied")
	ErrParameterScopeUnavailable   = errors.New("application: authenticated organization scope unavailable")
)

// TrustedParameterPrincipal projects only the verified Principal stored by
// the authentication interceptor. Request fields never supply this identity.
type TrustedParameterPrincipal struct{}

func (TrustedParameterPrincipal) ResolveParameterIdentity(ctx context.Context) (config.ParameterIdentity, error) {
	p, err := trust.MustFromContext(ctx)
	if err != nil {
		return config.ParameterIdentity{}, err
	}
	return config.ParameterIdentity{Tenant: string(p.Tenant()), Subject: p.Subject(), OrgScope: p.OrganizationScopeID()}, nil
}

// CellParameterEnvironment is fixed at composition time for this cell. It
// deliberately does not inspect request metadata or accept a per-call value.
type CellParameterEnvironment struct{ Environment config.ParameterEnvironment }

// NewCellParameterEnvironment parses the deployment's explicit environment
// selection. An empty value cannot silently choose sandbox or production.
func NewCellParameterEnvironment(raw string) (CellParameterEnvironment, error) {
	environment := config.ParameterEnvironment(raw)
	if environment != config.EnvironmentSandbox && environment != config.EnvironmentProduction {
		return CellParameterEnvironment{}, fmt.Errorf("-%s must be explicitly set to SANDBOX or PRODUCTION", FieldParameterEnvironment)
	}
	return CellParameterEnvironment{Environment: environment}, nil
}

func (e CellParameterEnvironment) ResolveParameterEnvironment(context.Context) (config.ParameterEnvironment, error) {
	if e.Environment != config.EnvironmentSandbox && e.Environment != config.EnvironmentProduction {
		return "", fmt.Errorf("%w: cell environment is unset or invalid", config.ErrParameterValueInvalid)
	}
	return e.Environment, nil
}

// WallClockParameterClock supplies server time for parameter revision evidence.
type WallClockParameterClock struct{}

func (WallClockParameterClock) Now(ctx context.Context) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	return time.Now().UTC(), nil
}

type parameterCatalogEnvelope struct {
	Version     int                          `json:"version"`
	Definitions []config.ParameterDefinition `json:"definitions"`
}

// ActiveRegistryParameterDefinitions serves the activated immutable
// tenant.parameters REFERENCE object from the same registry the cell composes.
// The tenant UUID mapping is a bootstrap-owned fact, never request input.
type ActiveRegistryParameterDefinitions struct {
	Registry   configregistry.Store
	TenantUUID func(string) uuid.UUID
}

var _ config.ParameterDefinitionSource = ActiveRegistryParameterDefinitions{}

func (s ActiveRegistryParameterDefinitions) LoadParameterSnapshot(ctx context.Context, tenant string) (config.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return config.Snapshot{}, err
	}
	if s.Registry == nil || s.TenantUUID == nil {
		return config.Snapshot{}, ErrParameterCatalogUnavailable
	}
	tenantID := s.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		return config.Snapshot{}, ErrParameterCatalogUnavailable
	}
	scope := configregistry.Scope{TenantID: tenantID.String()}
	activation, found, err := s.Registry.GetLatestActivation(scope, configregistry.KindReference, TenantParameterCatalogID)
	if err != nil {
		return config.Snapshot{}, fmt.Errorf("application: resolve active parameter catalog: %w", err)
	}
	if !found {
		return config.Snapshot{}, ErrParameterCatalogUnavailable
	}
	ref := configregistry.ObjectRef{Scope: scope, Kind: activation.Kind, ID: activation.ID, Revision: activation.Revision}
	object, found, err := s.Registry.GetObject(ref)
	if err != nil {
		return config.Snapshot{}, fmt.Errorf("application: load active parameter catalog: %w", err)
	}
	if !found || object.Verify() != nil || object.Digest() != activation.ObjectDigest || object.SchemaRef != "hcmnext.tenant-parameters/v1" {
		return config.Snapshot{}, ErrParameterCatalogUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(object.Body))
	decoder.DisallowUnknownFields()
	var envelope parameterCatalogEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return config.Snapshot{}, fmt.Errorf("application: decode active parameter catalog: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return config.Snapshot{}, fmt.Errorf("application: parameter catalog must contain one JSON value")
	}
	if envelope.Version != 1 {
		return config.Snapshot{}, ErrParameterCatalogUnavailable
	}
	return config.NewSnapshotWithDefinitions(TenantParameterCatalogID, strconv.FormatUint(uint64(object.Revision), 10), nil, envelope.Definitions)
}

// ParameterOrganizationGraph is the verified tenant organization graph used
// to derive hierarchy scopes. Implementations return repository facts, never
// a caller-provided path.
type ParameterOrganizationGraph interface {
	LoadParameterOrganizationGraph(context.Context, string, time.Time) (ParameterOrganizationSnapshot, error)
}

// ParameterOrganizationSnapshot carries the canonical hierarchy plus the
// tenant-verified legal entity bound to each active organization unit.
type ParameterOrganizationSnapshot struct {
	Graph                     organization.Snapshot
	LegalEntityByOrganization map[string]string
}

// StoredParameterOrganizationGraph reads the canonical effective-dated org
// graph under PostgreSQL RLS for the bootstrap-mapped tenant.
type StoredParameterOrganizationGraph struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

var _ ParameterOrganizationGraph = StoredParameterOrganizationGraph{}

func (s StoredParameterOrganizationGraph) LoadParameterOrganizationGraph(ctx context.Context, tenant string, asOf time.Time) (ParameterOrganizationSnapshot, error) {
	if s.DB == nil || s.TenantUUID == nil {
		return ParameterOrganizationSnapshot{}, ErrParameterScopeUnavailable
	}
	tenantID := s.TenantUUID(values.TenantId(tenant))
	if tenantID == uuid.Nil {
		return ParameterOrganizationSnapshot{}, ErrParameterScopeUnavailable
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return ParameterOrganizationSnapshot{}, fmt.Errorf("application: begin parameter org graph read: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return ParameterOrganizationSnapshot{}, err
	}
	graph, err := orggraph.Load(ctx, tx, tenantID)
	if err != nil {
		return ParameterOrganizationSnapshot{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT ou.entity_id, ou.legal_entity_ref, le.entity_id
		FROM organization_unit ou
		LEFT JOIN legal_entity le ON le.tenant_id=ou.tenant_id AND le.entity_id=ou.legal_entity_ref
		  AND le.superseded_at IS NULL AND le.effective_from <= $2 AND (le.effective_to IS NULL OR $2 < le.effective_to)
		WHERE ou.tenant_id=$1 AND ou.superseded_at IS NULL
		  AND ou.effective_from <= $2 AND (ou.effective_to IS NULL OR $2 < ou.effective_to)
		ORDER BY ou.entity_id`, tenantID, asOf)
	if err != nil {
		return ParameterOrganizationSnapshot{}, fmt.Errorf("application: load parameter legal-entity scope facts: %w", err)
	}
	defer rows.Close()
	var links []parameterLegalEntityLink
	for rows.Next() {
		var orgID uuid.UUID
		var legalRef, legalResolved *uuid.UUID
		if err := rows.Scan(&orgID, &legalRef, &legalResolved); err != nil {
			return ParameterOrganizationSnapshot{}, err
		}
		link := parameterLegalEntityLink{OrganizationID: orgID.String()}
		if legalRef != nil {
			link.LegalEntityRef = legalRef.String()
		}
		if legalResolved != nil {
			resolved := legalResolved.String()
			link.ResolvedLegalEntity = &resolved
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return ParameterOrganizationSnapshot{}, err
	}
	legalEntities, err := parameterLegalEntityBindings(links)
	if err != nil {
		return ParameterOrganizationSnapshot{}, err
	}
	return ParameterOrganizationSnapshot{Graph: graph, LegalEntityByOrganization: legalEntities}, nil
}

type parameterLegalEntityLink struct {
	OrganizationID      string
	LegalEntityRef      string
	ResolvedLegalEntity *string
}

func parameterLegalEntityBindings(links []parameterLegalEntityLink) (map[string]string, error) {
	bindings := make(map[string]string, len(links))
	for _, link := range links {
		if link.OrganizationID == "" {
			return nil, ErrParameterScopeUnavailable
		}
		if link.LegalEntityRef == "" {
			if link.ResolvedLegalEntity != nil {
				return nil, ErrParameterScopeUnavailable
			}
			continue
		}
		if link.ResolvedLegalEntity == nil || *link.ResolvedLegalEntity != link.LegalEntityRef {
			return nil, fmt.Errorf("%w: organization %s has an absent or cross-tenant legal entity", ErrParameterScopeUnavailable, link.OrganizationID)
		}
		if prior, ok := bindings[link.OrganizationID]; ok && prior != link.LegalEntityRef {
			return nil, fmt.Errorf("%w: organization %s has ambiguous legal entity bindings", ErrParameterScopeUnavailable, link.OrganizationID)
		}
		bindings[link.OrganizationID] = link.LegalEntityRef
	}
	return bindings, nil
}

// OrganizationParameterScopes derives tenant and organization scope paths
// from the authenticated principal and canonical org hierarchy. Only hcm_admin
// may author values or hierarchy locks; lateral writes and unsupported scope
// kinds fail closed.
type OrganizationParameterScopes struct {
	Graph ParameterOrganizationGraph
	Clock config.ParameterClock
}

var _ config.ParameterScopeAuthority = OrganizationParameterScopes{}

func (s OrganizationParameterScopes) ResolveParameterPath(ctx context.Context, identity config.ParameterIdentity) (config.ParameterScopePath, error) {
	if _, err := verifiedParameterPrincipal(ctx, identity); err != nil {
		return nil, err
	}
	return s.path(ctx, identity)
}

func (s OrganizationParameterScopes) AuthorizeParameterWrite(ctx context.Context, identity config.ParameterIdentity, _ config.ParameterDefinition, target config.ParameterScope, _ bool) (config.ParameterScopePath, error) {
	p, err := verifiedParameterPrincipal(ctx, identity)
	if err != nil {
		return nil, err
	}
	if !p.HasRole("hcm_admin") {
		return nil, ErrParameterWriteDenied
	}
	path, err := s.path(ctx, identity)
	if err != nil {
		return nil, err
	}
	if target.Kind == config.ScopeTenant && path[0] == target {
		return path[:1], nil
	}
	for i, scope := range path[1:] {
		if scope == target {
			return append(config.ParameterScopePath(nil), path[:i+2]...), nil
		}
	}
	return nil, config.ErrParameterScopePathInvalid
}

func (s OrganizationParameterScopes) path(ctx context.Context, identity config.ParameterIdentity) (config.ParameterScopePath, error) {
	path := config.ParameterScopePath{{Kind: config.ScopeTenant, ID: identity.Tenant}}
	if identity.OrgScope == "" {
		return path, nil
	}
	if s.Graph == nil || s.Clock == nil {
		return nil, ErrParameterScopeUnavailable
	}
	now, err := s.Clock.Now(ctx)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.Graph.LoadParameterOrganizationGraph(ctx, identity.Tenant, now)
	if err != nil {
		return nil, err
	}
	ancestors, err := organization.Ancestry(snapshot.Graph, organization.ReadRequest{Tenant: snapshot.Graph.Tenant, Root: identity.OrgScope, AsOf: now})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParameterScopeUnavailable, err)
	}
	parents := make(map[string]string, len(ancestors.Edges))
	for _, edge := range ancestors.Edges {
		parents[edge.Target] = edge.Source
	}
	chain := []string{identity.OrgScope}
	for current := identity.OrgScope; parents[current] != ""; current = parents[current] {
		chain = append(chain, parents[current])
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	lastLegalEntity := ""
	for _, id := range chain {
		if legalEntity := snapshot.LegalEntityByOrganization[id]; legalEntity != "" && legalEntity != lastLegalEntity {
			path = append(path, config.ParameterScope{Kind: config.ScopeLegalEntity, ID: legalEntity})
			lastLegalEntity = legalEntity
		}
		path = append(path, config.ParameterScope{Kind: config.ScopeOrganization, ID: id})
	}
	return path, nil
}

func verifiedParameterPrincipal(ctx context.Context, identity config.ParameterIdentity) (*trust.Principal, error) {
	p, err := trust.MustFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if string(p.Tenant()) != identity.Tenant || p.Subject() != identity.Subject || p.OrganizationScopeID() != identity.OrgScope {
		return nil, config.ErrParameterIdentityUnavailable
	}
	return p, nil
}
