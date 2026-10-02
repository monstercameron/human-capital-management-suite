package application

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const EnvLocalAgentPolicyAuthorityDatabaseURL = "HCMNEXT_AGENT_POLICY_AUTHORITY_DATABASE_URL"

type agentModelPolicyRegistryCompositionInput struct {
	Config            ServeConfig
	Core              dbport.Beginner
	Agents            composedAgentDatabase
	Material          LocalPersonaModelSigningMaterial
	PublisherDSN      string
	CurrentDeployment AgentModelPolicyDeploymentAuthority
	Now               func() time.Time
}

type composedAgentModelPolicyRegistry struct {
	Registry   *AgentModelPolicyRegistry
	References LocalDevPersonaStarterReferenceResolver
	Selection  AgentPolicyReferenceSelection
	Publisher  *agentmodelpolicystore.Publisher
	close      func()
}

// composeLocalAgentModelPolicyRegistry registers exact signed semantic
// contracts before candidate qualification. It creates no model profile,
// evaluation result, review, route or persona publication. Production runtime
// eligibility remains the current deployment authority's separate decision.
func composeLocalAgentModelPolicyRegistry(ctx context.Context, in agentModelPolicyRegistryCompositionInput) (composedAgentModelPolicyRegistry, error) {
	if ctx == nil || in.Config.Profile != ServeProfileLocalDev || in.Core == nil || in.Agents.store == nil || in.CurrentDeployment == nil || in.Now == nil || in.Now().IsZero() {
		return composedAgentModelPolicyRegistry{}, ErrAgentModelPolicyUnavailable
	}
	seed, err := decodeEd25519Seed(in.Material.PolicySeed)
	if err != nil {
		return composedAgentModelPolicyRegistry{}, modelConfigurationError("dedicated local policy signing material is required")
	}
	for _, other := range []string{in.Material.OutputSeed, in.Material.WorkloadSeed, in.Material.PricingSeed} {
		prior, err := decodeEd25519Seed(other)
		if err != nil || bytes.Equal(prior, seed) {
			return composedAgentModelPolicyRegistry{}, modelConfigurationError("local policy key must be independent from output, workload and pricing keys")
		}
	}
	key := ed25519.NewKeyFromSeed(seed)
	mapper := tenantKeyMapper[values.TenantId](pgstore.TenantID)
	// The local policy source is append-only: the current revision is read
	// from the store, so a cell upgraded with a new agent serves it.
	state := localAgentDemoPolicySourceState(in.Core, in.Agents.store, mapper)
	sources, err := NewLocalPersonaOpenAIPolicySources(key.Public().(ed25519.PublicKey), mapper, state)
	if err != nil {
		return composedAgentModelPolicyRegistry{}, err
	}
	store, err := agentmodelpolicystore.New(in.Agents.store)
	if err != nil {
		return composedAgentModelPolicyRegistry{}, err
	}
	registry, err := NewAgentModelPolicyRegistry(AgentModelPolicyRegistryConfig{Store: store, Sources: sources, TenantUUID: mapper, Now: in.Now, CurrentDeployment: in.CurrentDeployment, Manifests: in.Agents.store})
	if err != nil {
		return composedAgentModelPolicyRegistry{}, err
	}
	authorityPool, err := agentmodelpolicystore.NewAuthorityPool(ctx, agentmodelpolicystore.AuthorityPoolConfig{DSN: in.PublisherDSN, AgentDSN: in.Config.AgentDatabaseURL, CoreDSN: in.Config.DatabaseURL, ChatDSN: in.Config.ChatDatabaseURL, DocumentDSN: in.Config.DocumentDatabaseURL})
	if err != nil {
		return composedAgentModelPolicyRegistry{}, fmt.Errorf("compose local agent policy authority: %w", err)
	}
	ready := false
	defer func() {
		if !ready {
			authorityPool.Close()
		}
	}()
	publisher, err := agentmodelpolicystore.NewPublisher(authorityPool, sources)
	if err != nil {
		return composedAgentModelPolicyRegistry{}, err
	}
	now := in.Now().UTC()
	registered := 0
	for _, name := range in.Config.ServedTenants() {
		if !localPersonaOpenAIDemoTenant(name) {
			continue
		}
		tenant := values.TenantId(name)
		if _, revoked, err := state.CurrentAgentPolicySource(ctx, tenant, LocalPersonaOpenAIPolicySourceID); err != nil || revoked {
			return composedAgentModelPolicyRegistry{}, fmt.Errorf("%w: selected demo tenant is not seeded", ErrAgentModelPolicyUnavailable)
		}
		if err := ensureLocalAgentDemoPolicyUpgrade(ctx, in.Core, in.Agents.store, authorityPool, mapper, tenant, key, now); err != nil {
			return composedAgentModelPolicyRegistry{}, fmt.Errorf("compose immutable local agent contracts: %w", err)
		}
		registered++
	}
	if registered == 0 {
		return composedAgentModelPolicyRegistry{}, ErrAgentModelPolicyUnavailable
	}
	selection := LocalPersonaOpenAIPolicySelection()
	ready = true
	return composedAgentModelPolicyRegistry{Registry: registry, References: registry.ForPrincipal(selection), Selection: selection, Publisher: publisher, close: authorityPool.Close}, nil
}

func registeredLocalPolicyTenant(ctx context.Context, db dbport.Beginner, tenant uuid.UUID) (bool, error) {
	if ctx == nil || db == nil || tenant == uuid.Nil {
		return false, ErrAgentModelPolicyUnavailable
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return false, err
	}
	var present bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant WHERE tenant_id=$1)`, tenant).Scan(&present); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return present, nil
}
