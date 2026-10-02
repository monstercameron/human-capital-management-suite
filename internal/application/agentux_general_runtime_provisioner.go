package application

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// LocalPersonaRuntimeProvisioner is the local-development implementation of
// the publication runtime boundary. It shares the preparation command's
// identity, route, deployment, and policy helpers; it does not grant review,
// evaluation, publication, or placement authority.
type LocalPersonaRuntimeProvisioner struct {
	mu             sync.Mutex
	Config         LocalAgentDemoConfig
	Tenants        []string
	Core           dbport.Beginner
	Agents         *agentstore.Store
	Personas       *agentpersonastore.Store
	Evaluations    *agentpersonastore.EvaluationSealAuthority
	Signing        LocalPersonaModelSigningMaterial
	DeploymentPath string
	Now            func() time.Time
}

func (p *LocalPersonaRuntimeProvisioner) ProvisionPersonaRuntime(ctx context.Context, actor PersonaAdminCommandActor, row agentpersonastore.PersonaVersion, profile agentpersona.PersonaProfile, evidence agentpersonastore.PublicationEvidence) error {
	return p.provision(ctx, actor, row, profile, evidence, nil)
}

type localAgentDemoRecordingProvisioner struct {
	runtime *LocalPersonaRuntimeProvisioner
	changes *localAgentDemoPreparedAgent
}

func (p localAgentDemoRecordingProvisioner) ProvisionPersonaRuntime(ctx context.Context, actor PersonaAdminCommandActor, row agentpersonastore.PersonaVersion, profile agentpersona.PersonaProfile, evidence agentpersonastore.PublicationEvidence) error {
	return p.runtime.provision(ctx, actor, row, profile, evidence, p.changes)
}

func (p *LocalPersonaRuntimeProvisioner) provision(ctx context.Context, actor PersonaAdminCommandActor, row agentpersonastore.PersonaVersion, profile agentpersona.PersonaProfile, evidence agentpersonastore.PublicationEvidence, changes *localAgentDemoPreparedAgent) error {
	if p == nil || ctx == nil || p.Core == nil || p.Agents == nil || p.Personas == nil || p.Evaluations == nil || p.Now == nil || actor.Principal == nil || actor.Tenant != row.TenantID || !p.tenantAvailable(actor.Tenant) || actor.Subject != actor.Principal.Subject() || actor.Tenant != actor.Principal.Tenant() || !personaAdminActorBound(ctx, actor) || profile.PersonaID != row.PersonaID || int64(profile.Version) != row.Version || strings.TrimSpace(evidence.EvaluationRunID) == "" {
		return ErrPersonaAdminRuntimeUnavailable
	}
	now := p.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() {
		return ErrPersonaAdminRuntimeUnavailable
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	sealed, sealErr := agentpersona.Seal(profile)
	if sealErr != nil || sealed.Digest != row.ContentDigest {
		return ErrPersonaAdminRuntimeUnavailable
	}
	scoped, err := p.Personas.Scoped(actor.Tenant)
	if err != nil {
		return err
	}
	currentEvidence, err := scoped.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version)
	if err != nil || currentEvidence != evidence {
		return ErrPersonaAdminRuntimeUnavailable
	}
	config := p.Config
	config.Tenant = actor.Tenant.String()
	mapper := func(tenant values.TenantId) uuid.UUID { return pgstore.TenantID(tenant.String()) }
	manifest, err := (AgentManifestStoreAdapter{Store: p.Agents, TenantID: mapper(actor.Tenant)}).ResolveAgentManifestContext(ctx, profile.Manifest)
	if err != nil {
		return fmt.Errorf("resolve runtime manifest: %w", err)
	}
	candidate, route, err := NewLocalPersonaOpenAICandidate(manifest)
	if err != nil {
		return err
	}
	suite, err := localAgentDemoEvaluationSuiteFor(profile)
	if err != nil {
		return err
	}
	modelDigest := "sha256:" + candidate.ProfileDigest
	target := agenteval.PersonaEvaluationTarget{TenantID: string(actor.Tenant), SyntheticTenantID: localAgentDemoSynthetic, InvokerID: actor.Subject, PersonaID: row.PersonaID, PersonaVersion: row.Version, ProfileDigest: row.ContentDigest, ModelDigest: modelDigest}
	report, measuredSuite, err := localAgentDemoEvaluationReportForSuite(ctx, target, suite, now.Add(-time.Second))
	if err != nil {
		return err
	}
	qualified, err := QualifyPersonaCandidateModelProfile(report, candidate, profile.Manifest.Digest, measuredSuite)
	if err != nil {
		return err
	}
	path := p.DeploymentPath
	if strings.TrimSpace(path) == "" {
		path = filepath.FromSlash(localPersonaModelDeploymentPath)
	}
	deploymentCreated, err := ensureLocalAgentDemoDeploymentProfile(path, config.Tenant, p.Signing, qualified)
	if err != nil {
		return err
	}
	legalEntity, err := (PersonaRunLegalEntityResolver{DB: p.Core, TenantUUID: mapper, Now: func() time.Time { return now }}).Resolve(ctx, agentinvoke.RunRequest{TenantID: string(actor.Tenant), Mode: agentinvoke.OnBehalfOf, InvokerID: actor.Subject})
	if err != nil {
		return err
	}
	if _, err := ensureLocalAgentDemoRunPolicy(ctx, localAgentDemoRunPolicyDSN(config), mapper(actor.Tenant), legalEntity, now); err != nil {
		return err
	}
	routeCreated, err := ensureLocalAgentDemoModelRoute(ctx, config, p.Agents, p.Evaluations, mapper, row, profile, qualified, route, modelDigest, evidence.EvaluationRunID, legalEntity, now)
	if err != nil {
		return err
	}
	if changes != nil {
		changes.modelRouteCreated, changes.providerDeploymentCreated = routeCreated, deploymentCreated
	}
	return ensureLocalAgentDemoServicePrincipal(ctx, p.Core, p.Personas, mapper, row, profile.Handle, now)
}

func ensureLocalAgentDemoDeploymentProfile(path, tenant string, material LocalPersonaModelSigningMaterial, profile agentmodel.ModelProfile) (bool, error) {
	profiles := []agentmodel.ModelProfile{profile}
	if _, statErr := os.Stat(path); statErr == nil {
		current, err := LoadPersonaModelDeployment(path)
		if err != nil {
			return false, err
		}
		profiles = slices.Clone(current.Profiles)
		for _, existing := range profiles {
			if existing.ID != profile.ID {
				continue
			}
			if !reflect.DeepEqual(existing, profile) {
				return false, errors.New("prepared persona model profile differs from current reviewed evaluation")
			}
			return false, nil
		}
		profiles = append(profiles, profile)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}
	slices.SortFunc(profiles, func(a, b agentmodel.ModelProfile) int { return strings.Compare(a.ID, b.ID) })
	deployment, err := NewLocalPersonaOpenAIModelDeployment(tenant, profiles, material)
	if err != nil {
		return false, err
	}
	return writeLocalAgentDemoDeployment(path, deployment)
}

func localAgentDemoEvaluationAuthority(privateKey ed25519.PrivateKey, tenant string, mapper func(values.TenantId) uuid.UUID, now func() time.Time) (*agentpersonastore.EvaluationSealAuthority, error) {
	keys, err := localAgentDemoEvaluationVerificationKeys(privateKey, tenant, "harborcare-demo", "ironridge-demo")
	if err != nil {
		return nil, err
	}
	return agentpersonastore.NewEvaluationSealAuthority(keys, mapper, now)
}

// bindAdminRuntimeProvisioner attaches the required local publication step
// after the independently credentialed route authority has been composed.
func (w *personaServeWiring) bindAdminRuntimeProvisioner(provisioner PersonaAdminRuntimeProvisioner) error {
	if w == nil || provisioner == nil {
		return ErrPersonaAdminRuntimeUnavailable
	}
	factory, ok := w.adminFactory.(*PersonaAdminCommandFactory)
	if !ok || factory == nil {
		return ErrPersonaAdminRuntimeUnavailable
	}
	executor, ok := factory.executor.(*PersonaAdminLifecycleExecutor)
	if !ok || executor == nil {
		return ErrPersonaAdminRuntimeUnavailable
	}
	executor.Runtime = provisioner
	if local, ok := provisioner.(*LocalPersonaRuntimeProvisioner); ok && local != nil {
		if catalog, ok := w.adminCatalog.(readOnlyPersonaAdminCatalog); ok && catalog.service != nil {
			catalog.service.Runtime = PersonaCatalogRuntimeStatus{Store: local.Personas, Principal: PersonaRunAgentPrincipalResolver{Bindings: AgentPersonaRunPrincipalBindingReader{Store: local.Personas}, Principals: GovernancePersonaPrincipalAuthority{DB: local.Core, TenantUUID: tenantKeyMapper[values.TenantId](pgstore.TenantID)}, Now: local.Now}}
		}
	}
	return nil
}

func localAgentDemoDeploymentProfileJSON(profile agentmodel.ModelProfile) string {
	raw, _ := json.Marshal(profile)
	return string(raw)
}

func ensureLocalAgentDemoModelRoute(ctx context.Context, config LocalAgentDemoConfig, agents *agentstore.Store, evaluations *agentpersonastore.EvaluationSealAuthority, mapper func(values.TenantId) uuid.UUID, row agentpersonastore.PersonaVersion, profile agentpersona.PersonaProfile, qualified agentmodel.ModelProfile, route PersonaRunModelRoute, modelDigest, runID, legalEntity string, now time.Time) (bool, error) {
	ref, policyRaw := LocalPersonaOpenAIModelPolicyReference()
	if _, err := agents.CurrentPersonaModelRoutePolicyForAgent(ctx, mapper(values.TenantId(config.Tenant)), legalEntity, ref.ID, int64(ref.Version), int64(ref.SchemaVersion), ref.Digest, profile.Manifest.Digest, now); err == nil {
		return false, nil
	} else if !errors.Is(err, agentstore.ErrPersonaModelRouteNotFound) {
		return false, err
	}
	route.Route.TraceID = "local-dev-" + profile.Handle + "-route"
	route.Route.Pin.Primary = agentmodel.ModelSelection{ProfileID: qualified.ID, ProfileDigest: qualified.ProfileDigest, Identity: qualified.Identity}
	route.Route.BudgetRemainingMicros = qualified.MaxCostMicros
	route.Route.Task.MaxCostMicros = qualified.MaxCostMicros
	return publishLocalAgentDemoRoute(ctx, config, evaluations, mapper, row, profile, qualified, route, modelDigest, policyRaw, ref, runID, legalEntity, now)
}

func publishLocalAgentDemoRoute(ctx context.Context, config LocalAgentDemoConfig, evaluations *agentpersonastore.EvaluationSealAuthority, mapper func(values.TenantId) uuid.UUID, row agentpersonastore.PersonaVersion, profile agentpersona.PersonaProfile, qualified agentmodel.ModelProfile, route PersonaRunModelRoute, modelDigest string, policyRaw []byte, ref agentmanifest.Reference, runID, legalEntity string, now time.Time) (bool, error) {
	bridge, err := NewPersonaRouteEvaluationBridge(evaluations)
	if err != nil {
		return false, err
	}
	qualification, err := agentstore.NewPersonaRouteQualificationEvidenceAuthority(bridge, mapper)
	if err != nil {
		return false, err
	}
	authority, err := pgxadapter.NewPool(ctx, config.RouteAuthorityDatabaseURL, map[string]string{"role": "hcmnext_persona_model_route_authority"})
	if err != nil {
		return false, err
	}
	defer authority.Close()
	publisher, err := agentstore.NewPersonaModelRoutePublisher(authority, mapper, qualification)
	if err != nil {
		return false, err
	}
	raw, err := localAgentDemoRoutePayload(route, modelDigest)
	if err != nil {
		return false, err
	}
	_, err = publisher.PublishPersonaModelRoutePolicy(ctx, agentstore.PersonaModelRoutePublication{Tenant: values.TenantId(config.Tenant), LegalEntityID: legalEntity, PolicyID: ref.ID, PolicyVersion: int64(ref.Version), SchemaVersion: int64(ref.SchemaVersion), PolicyDigest: ref.Digest, Revision: 1, EffectiveFrom: now, PersonaID: row.PersonaID, PersonaVersion: row.Version, ProfileDigest: row.ContentDigest, EvaluationRunID: runID, PolicyPayload: policyRaw, RoutePayload: raw})
	return err == nil, err
}

func localAgentDemoRoutePayload(route PersonaRunModelRoute, modelDigest string) ([]byte, error) {
	return json.Marshal(struct {
		PersonaRunModelRoute
		ModelDigest string `json:"model_digest"`
	}{PersonaRunModelRoute: route, ModelDigest: modelDigest})
}

func ensureLocalAgentDemoServicePrincipal(ctx context.Context, core dbport.Beginner, personas *agentpersonastore.Store, mapper func(values.TenantId) uuid.UUID, row agentpersonastore.PersonaVersion, handle string, now time.Time) error {
	tenant := row.TenantID
	principalID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("hcmnext/local/persona/"+tenant.String()+"/"+row.PersonaID+"/"+strconv.FormatInt(row.Version, 10)))
	scoped, err := personas.Scoped(tenant)
	if err != nil {
		return err
	}
	if binding, err := scoped.ResolvePersonaAgentPrincipal(ctx, row.PersonaID, row.Version); err == nil {
		if binding.PrincipalID != principalID {
			return errors.New("persona service principal binding conflicts")
		}
		_, resolveErr := (PersonaRunAgentPrincipalResolver{Bindings: AgentPersonaRunPrincipalBindingReader{Store: personas}, Principals: GovernancePersonaPrincipalAuthority{DB: core, TenantUUID: mapper}, Now: func() time.Time { return now }}).Resolve(ctx, tenant, row.PersonaID, row.Version)
		return resolveErr
	} else if !errors.Is(err, agentpersonastore.ErrNotFound) {
		return err
	}
	tx, err := core.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tenantID := mapper(tenant)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	current, err := governance.LoadPrincipal(ctx, tx, tenantID, principalID)
	if errors.Is(err, dbport.ErrNoRows) {
		expires := now.AddDate(1, 0, 0)
		metadata, _ := json.Marshal(map[string]string{"purpose": "local-persona-runtime", "persona_id": row.PersonaID})
		current = governance.Principal{TenantID: tenantID, PrincipalID: principalID, Kind: "SERVICE", Subject: "local." + handle + ".v" + strconv.FormatInt(row.Version, 10), Assurance: "AAL2", AuthnMethod: "WORKLOAD", RevocationEpoch: 1, Lifecycle: "ACTIVE", CreatedAt: now, UpdatedAt: now, ExpiresAt: &expires, Metadata: metadata}
		if err := governance.InsertPrincipal(ctx, tx, current); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if current.Kind != "SERVICE" || current.Lifecycle != "ACTIVE" || current.ExpiresAt != nil && !current.ExpiresAt.After(now) {
		return errors.New("persona service principal is not active")
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return (PersonaAgentPrincipalProvisioner{Bindings: AgentPersonaRunPrincipalBindingWriter{Store: personas}, Principals: GovernancePersonaPrincipalAuthority{DB: core, TenantUUID: mapper}, Now: func() time.Time { return now }}).Provision(ctx, tenant, row.PersonaID, row.Version, principalID)
}

func (p *LocalPersonaRuntimeProvisioner) tenantAvailable(tenant values.TenantId) bool {
	if p == nil || !localPersonaOpenAIDemoTenant(tenant.String()) {
		return false
	}
	if len(p.Tenants) == 0 {
		return tenant.String() == p.Config.Tenant
	}
	return slices.Contains(p.Tenants, tenant.String())
}

func (p *LocalPersonaRuntimeProvisioner) PersonaRuntimeAvailable(ctx context.Context) bool {
	principal, ok := trust.FromContext(ctx)
	return ok && principal != nil && p.tenantAvailable(principal.Tenant())
}

// The serving store and the publication store need the same local public keys.
// This helper is intentionally local-demo-only and never loads a credential.
func localAgentDemoEvaluationVerificationKeys(privateKey ed25519.PrivateKey, tenants ...string) (map[string]ed25519.PublicKey, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, ErrPersonaEvaluationEvidenceUnavailable
	}
	keys := make(map[string]ed25519.PublicKey)
	for _, tenant := range tenants {
		if !localPersonaOpenAIDemoTenant(tenant) {
			continue
		}
		for _, suite := range []agenteval.PersonaSuite{agenteval.PolicyHelperSuite(personaPolicyHelperSkillID), agenteval.AssistantSuite(personaPolicyHelperSkillID, personaChatReplySkillID)} {
			keyID, err := PersonaEvaluationVerificationKeyID(tenant, suite.ID, localAgentDemoEvaluationKeyID)
			if err != nil {
				return nil, err
			}
			keys[keyID] = privateKey.Public().(ed25519.PublicKey)
		}
	}
	return keys, nil
}
