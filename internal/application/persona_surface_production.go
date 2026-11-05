package application

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errSurfaceMentionProduction = errors.New("application: surface mention production composition unavailable")

// SurfaceMentionProductionConfig contains the production ports behind surface
// persona mentions. Personas and Documents are the durable stores; Catalog is
// the same current skill registry chat admission uses; Skills discovers the
// invoker's current skills; Grants, Runs, and Repository are the shared
// invocation backends; Peer extracts tainted peer context. Limits meters
// mentions like chat; Ephemeral and Now are optional.
type SurfaceMentionProductionConfig struct {
	Personas   *agentpersonastore.Store
	Documents  *documenthubstore.Store
	Catalog    agentgate.SkillCatalog
	Skills     AgentSkillDiscoverer
	Grants     agentinvoke.GrantIssuer
	Runs       agentinvoke.RunStarter
	Repository agentinvoke.InvocationRepository
	Ephemeral  agentinvoke.EphemeralResponder
	Peer       agentinvoke.PeerExtractor
	Limits     *PersonaMentionLimits
	Now        func() time.Time
}

// ComposeSurfaceMentionBridge builds the production surface mention bridge:
// the store-backed persona directory and segment reader, the surface
// authority projection, and the shared AGENTP-008 invocation service bound to
// surface channels. Grants and runs stay caller ports so serve binds the same
// backends chat uses; the authority adapter here resolves admission only.
func ComposeSurfaceMentionBridge(cfg SurfaceMentionProductionConfig) (*surfaceMentionBridge, error) {
	if cfg.Personas == nil || cfg.Documents == nil || isNilPersonaOutputPort(cfg.Catalog) || isNilPersonaOutputPort(cfg.Skills) ||
		isNilPersonaOutputPort(cfg.Grants) || isNilPersonaOutputPort(cfg.Runs) || isNilPersonaOutputPort(cfg.Repository) || isNilPersonaOutputPort(cfg.Peer) {
		return nil, fmt.Errorf("%w: persona and document stores, catalog, skills, grants, runs, repository and peer extractor are required", errSurfaceMentionProduction)
	}
	catalog, err := newSurfacePersonaStoreCatalog(cfg.Personas)
	if err != nil {
		return nil, err
	}
	directory, err := newStoreSurfacePersonaDirectory(catalog)
	if err != nil {
		return nil, err
	}
	content, err := newSurfaceDocumentStoreContent(cfg.Documents)
	if err != nil {
		return nil, err
	}
	segments, err := newStoreSurfaceSegmentReader(content)
	if err != nil {
		return nil, err
	}
	sources, err := NewSurfaceAuthoritySources(SurfaceAuthoritySourcesConfig{
		Personas: cfg.Personas, Documents: cfg.Documents, Catalog: cfg.Catalog, Skills: cfg.Skills,
	})
	if err != nil {
		return nil, err
	}
	authority := &PersonaAuthorityAdapter{Personas: sources.Personas, Identity: sources.Identity}
	runs := cfg.Runs
	if cfg.Limits != nil {
		if err := cfg.Limits.validate(); err != nil {
			return nil, fmt.Errorf("%w: mention limits: %v", errSurfaceMentionProduction, err)
		}
		runs = personaMentionLimitedRunStarter{next: cfg.Runs, limits: cfg.Limits}
	}
	resolver, err := agentinvoke.NewService(agentinvoke.Config{
		Authority: authority, Grants: cfg.Grants, Runs: runs, Repository: cfg.Repository,
		Ephemeral: cfg.Ephemeral, Threads: surfaceThreadReader{segments: segments}, PeerExtractor: cfg.Peer, Now: cfg.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: create invocation resolver: %v", errSurfaceMentionProduction, err)
	}
	return newSurfaceMentionBridge(surfaceMentionBridgeConfig{Directory: directory, Resolver: resolver})
}

// SurfaceServeDependencies are the served values the document surface bridge
// binds. Grants, runs, claims, and metering reuse the same durable stores,
// skill registry, and run worker as chat; only the authority projection and
// the segment reader are surface-shaped. Peer is the quarantine model chat
// does not serve; without it no surface bridge is composed.
type SurfaceServeDependencies struct {
	Personas   *agentpersonastore.Store
	Documents  *documenthubstore.Store
	AgentStore *agentstore.Store
	Pool       *pgxadapter.Pool
	TenantUUID func(values.TenantId) uuid.UUID
	Skills     *AgentSkillSource
	Catalog    agentgate.SkillCatalog
	Runs       agentinvoke.RunStarter
	Peer       agentinvoke.PeerExtractor
	Now        func() time.Time
}

// ComposeServedSurfaceMentions builds the production surface mention bridge
// from served dependencies. Every port is required; anything missing fails
// the composition so the document hook stays unbound rather than half-wired.
func ComposeServedSurfaceMentions(deps SurfaceServeDependencies) (*surfaceMentionBridge, error) {
	if deps.Personas == nil || deps.Documents == nil || deps.AgentStore == nil || deps.Pool == nil ||
		deps.TenantUUID == nil || deps.Skills == nil || deps.Skills.gate == nil || deps.Skills.current == nil ||
		isNilPersonaOutputPort(deps.Catalog) || isNilPersonaOutputPort(deps.Runs) || isNilPersonaOutputPort(deps.Peer) || deps.Now == nil {
		return nil, fmt.Errorf("%w: persona, document and agent stores, pool, tenant mapper, skill source, catalog, runs, peer extractor and clock are required", errSurfaceMentionProduction)
	}
	sources, err := NewSurfaceAuthoritySources(SurfaceAuthoritySourcesConfig{
		Personas: deps.Personas, Documents: deps.Documents, Catalog: deps.Catalog, Skills: deps.Skills,
	})
	if err != nil {
		return nil, err
	}
	projection, err := NewGateInvokerAuthoritySource(GateInvokerAuthoritySourceConfig{
		Gate: deps.Skills.gate, Skills: deps.Catalog, Current: deps.Skills.current, Purpose: personaChatReplyPurpose,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: invoker authority projection: %v", errSurfaceMentionProduction, err)
	}
	invokers, err := NewTrustedSkillInvokerAuthoritySource(sources.Identity, projection)
	if err != nil {
		return nil, fmt.Errorf("%w: per-skill invoker projection: %v", errSurfaceMentionProduction, err)
	}
	grantStores, err := agentdelegationstore.New(deps.Pool, deps.TenantUUID)
	if err != nil {
		return nil, fmt.Errorf("%w: durable grant store: %v", errSurfaceMentionProduction, err)
	}
	issuer, err := NewRequestScopedPersonaGrantIssuer(RequestScopedPersonaGrantIssuerConfig{
		Personas: sources.Personas, Identity: sources.Identity, Invokers: invokers, Stores: grantStores, Now: deps.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: surface grant issuer: %v", errSurfaceMentionProduction, err)
	}
	repository, err := agentinvocationstore.NewWithTenantUUID(deps.AgentStore, func(tenant string) uuid.UUID {
		return deps.TenantUUID(values.TenantId(tenant))
	})
	if err != nil {
		return nil, fmt.Errorf("%w: durable invocation repository: %v", errSurfaceMentionProduction, err)
	}
	limits, err := newServedPersonaMentionLimits(deps.Personas, deps.Now)
	if err != nil {
		return nil, fmt.Errorf("%w: served mention limits: %v", errSurfaceMentionProduction, err)
	}
	return ComposeSurfaceMentionBridge(SurfaceMentionProductionConfig{
		Personas: deps.Personas, Documents: deps.Documents, Catalog: deps.Catalog, Skills: deps.Skills,
		Grants: issuer, Runs: deps.Runs, Repository: repository, Peer: deps.Peer, Limits: limits, Now: deps.Now,
	})
}
