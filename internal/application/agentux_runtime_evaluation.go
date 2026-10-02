package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	localAgentDemoEvaluationKeyID = "local-dev-policy-helper-evaluator-v1"
	localAgentDemoEvaluationPath  = ".artifacts/lanes/agent-dev/persona-evaluation-signing.json"
)

type localAgentDemoEvaluationMaterial struct {
	Seed string `json:"seed"`
}

// loadOrCreateLocalAgentDemoEvaluationKey persists a local-only evaluator
// authority. Its public key is intentionally absent from production trust;
// the preparation command supplies it only to its one-shot verifier.
func loadOrCreateLocalAgentDemoEvaluationKey(path string) (ed25519.PrivateKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrPersonaEvaluationEvidenceUnavailable
	}
	if raw, err := os.ReadFile(path); err == nil {
		var material localAgentDemoEvaluationMaterial
		if json.Unmarshal(raw, &material) != nil {
			return nil, ErrPersonaEvaluationEvidenceUnavailable
		}
		seed, err := base64.StdEncoding.Strict().DecodeString(material.Seed)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, ErrPersonaEvaluationEvidenceUnavailable
		}
		return ed25519.NewKeyFromSeed(seed), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrPersonaEvaluationEvidenceUnavailable
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, ErrPersonaEvaluationEvidenceUnavailable
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, ErrPersonaEvaluationEvidenceUnavailable
	}
	raw, _ := json.Marshal(localAgentDemoEvaluationMaterial{Seed: base64.StdEncoding.EncodeToString(seed)})
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateLocalAgentDemoEvaluationKey(path)
	}
	if err != nil {
		return nil, ErrPersonaEvaluationEvidenceUnavailable
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil || f.Sync() != nil {
		return nil, ErrPersonaEvaluationEvidenceUnavailable
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

type localAgentDemoModelFixture struct {
	cases map[string]agenteval.PersonaCase
	now   time.Time
}

func (f *localAgentDemoModelFixture) AuthorizeSyntheticPersonaEvaluation(context.Context, agenteval.PersonaEvaluationTarget) error {
	return nil
}

func (f *localAgentDemoModelFixture) ExecutePersonaCase(_ context.Context, _ agenteval.PersonaEvaluationTarget, testCase agenteval.PersonaCase) (agenteval.PersonaCaseExecution, error) {
	if f == nil || f.cases == nil {
		return agenteval.PersonaCaseExecution{}, agenteval.ErrPersonaEvaluation
	}
	f.cases[testCase.ID] = testCase
	return agenteval.PersonaCaseExecution{TaskID: "local-fixture-task-" + testCase.ID, InvocationID: "local-fixture-invocation-" + testCase.ID}, nil
}

func (f *localAgentDemoModelFixture) ReadPersonaCase(_ context.Context, target agenteval.PersonaEvaluationTarget, execution agenteval.PersonaCaseExecution) (agenteval.PersonaCaseEvidence, error) {
	if f == nil {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	id := strings.TrimPrefix(execution.InvocationID, "local-fixture-invocation-")
	testCase, ok := f.cases[id]
	if !ok {
		return agenteval.PersonaCaseEvidence{}, agenteval.ErrPersonaEvaluation
	}
	digest := personaRunBytesDigest([]byte("agentux-005/local-model-fixture/" + id))
	evidence := agenteval.PersonaCaseEvidence{
		SyntheticTenantID: target.SyntheticTenantID, PersonaID: target.PersonaID, PersonaVersion: target.PersonaVersion,
		ProfileDigest: target.ProfileDigest, ModelDigest: target.ModelDigest, CaseDigest: agenteval.PersonaCaseDigest(testCase),
		TaskID: execution.TaskID, InvocationID: execution.InvocationID, EvidenceDigest: digest,
		Outcome: "COMPLETED", Skills: append([]string(nil), testCase.ExpectedSkills...),
		DeliveredTo: []string{target.InvokerID}, AuthorizedRecipients: []string{target.InvokerID},
		AudienceFloorDigest: digest, PlanDigest: digest, BaselinePlanDigest: digest, CompletedAt: f.now,
	}
	switch testCase.Kind {
	case agenteval.PersonaOutOfScope:
		evidence.Outcome, evidence.RefusalCode, evidence.RefusalPointer = "REFUSED", "OUT_OF_SCOPE", "/agents/policy"
		evidence.DeliveredTo, evidence.AuthorizedRecipients, evidence.Skills = nil, nil, nil
	case agenteval.PersonaDenied:
		evidence.Outcome, evidence.RefusalCode = "REFUSED", "AUTHORITY_DENIED"
		evidence.DeliveredTo, evidence.AuthorizedRecipients, evidence.Skills = nil, nil, nil
	}
	localAgentDemoWorkspaceEvidence(testCase, &evidence)
	return evidence, nil
}

func localAgentDemoEvaluationReport(ctx context.Context, target agenteval.PersonaEvaluationTarget, at time.Time) (agenteval.PersonaEvaluationReport, agenteval.PersonaSuite, error) {
	return localAgentDemoEvaluationReportForSuite(ctx, target, agenteval.PolicyHelperSuite(personaPolicyHelperSkillID), at)
}

func localAgentDemoEvaluationReportForSuite(ctx context.Context, target agenteval.PersonaEvaluationTarget, suite agenteval.PersonaSuite, at time.Time) (agenteval.PersonaEvaluationReport, agenteval.PersonaSuite, error) {
	fixture := &localAgentDemoModelFixture{cases: make(map[string]agenteval.PersonaCase), now: at.UTC()}
	report, err := agenteval.EvaluatePersonaSuite(ctx, target, suite, fixture, fixture)
	if err != nil {
		return agenteval.PersonaEvaluationReport{}, agenteval.PersonaSuite{}, fmt.Errorf("local deterministic persona evaluation: %w", err)
	}
	return report, suite, nil
}

func localAgentDemoEvaluationSuite(id string) (agenteval.PersonaSuite, error) {
	switch id {
	case agenteval.PolicyHelperSuite(personaPolicyHelperSkillID).ID:
		return agenteval.PolicyHelperSuite(personaPolicyHelperSkillID), nil
	case localAgentDemoAssistantSuiteID:
		return agenteval.AssistantSuite(personaPolicyHelperSkillID, personaChatReplySkillID), nil
	default:
		return agenteval.PersonaSuite{}, ErrPersonaAdminEvaluationUnavailable
	}
}

// localAgentDemoEvaluationSuiteFor selects the suite version a persona version
// is evaluated against: an Assistant version that pins the workspace search
// skill is evaluated against the v3 contract, every earlier version against v2.
func localAgentDemoEvaluationSuiteFor(profile agentpersona.PersonaProfile) (agenteval.PersonaSuite, error) {
	if profile.EvalSuiteRef == localAgentDemoAssistantSuiteID {
		for _, pin := range profile.SkillPins {
			if pin.ID == personaWorkspaceSearchSkillID {
				return agenteval.AssistantWorkspaceSuite(personaPolicyHelperSkillID, personaWorkspaceSearchSkillID, personaChatReplySkillID), nil
			}
		}
	}
	return localAgentDemoEvaluationSuite(profile.EvalSuiteRef)
}

type localPersonaAdminEvaluationExecutor struct {
	now   func() time.Time
	suite agenteval.PersonaSuite
}

func (e localPersonaAdminEvaluationExecutor) EvaluatePersonaVersion(ctx context.Context, target agenteval.PersonaEvaluationTarget) (agenteval.PersonaEvaluationReport, agenteval.PersonaSuite, error) {
	if e.now == nil {
		return agenteval.PersonaEvaluationReport{}, agenteval.PersonaSuite{}, ErrPersonaAdminEvaluationUnavailable
	}
	return localAgentDemoEvaluationReportForSuite(ctx, target, e.suite, e.now().UTC())
}

// NewLocalPersonaAdminEvaluationService composes the deterministic local-dev
// evaluator with the same signed evidence store consumed by publication.
func NewLocalPersonaAdminEvaluationService(store *agentpersonastore.Store, privateKey ed25519.PrivateKey, modelDigest string, suite agenteval.PersonaSuite, now func() time.Time, newRunID func() string) (*PersonaAdminEvaluationService, error) {
	if store == nil || len(privateKey) != ed25519.PrivateKeySize || !personaRequestDigest(modelDigest) || suite.ID == "" || now == nil || newRunID == nil {
		return nil, ErrPersonaAdminEvaluationUnavailable
	}
	return &PersonaAdminEvaluationService{
		Store: personaAdminLifecycleStoreAdapter{store: store}, Versions: personaAdminInstallationVersions{store: store}, Recorder: &PersonaEvaluationEvidenceService{store: store},
		Executor: localPersonaAdminEvaluationExecutor{now: now, suite: suite}, SyntheticTenant: localAgentDemoSynthetic, KeyID: localAgentDemoEvaluationKeyID,
		PrivateKey: privateKey, ModelDigest: modelDigest, Now: now, FreshFor: localAgentDemoFreshFor, NewRunID: newRunID,
	}, nil
}

// localPersonaAdminEvaluationRunner derives the same unqualified candidate
// digest as the agent-demo preparation for the exact reviewed manifest. It is
// deliberately constructible only by the local-dev composition below.
type localPersonaAdminEvaluationRunner struct {
	store      *agentpersonastore.Store
	agents     *agentstore.Store
	tenant     values.TenantId
	tenants    []string
	tenantUUID func(values.TenantId) uuid.UUID
	privateKey ed25519.PrivateKey
	now        func() time.Time
	newRunID   func() string
}

// PersonaEvaluationAvailable reports whether the caller belongs to a trusted
// local demo tenant this runner serves.
func (r *localPersonaAdminEvaluationRunner) PersonaEvaluationAvailable(ctx context.Context) bool {
	if r == nil || ctx == nil {
		return false
	}
	principal, ok := trust.FromContext(ctx)
	return ok && principal != nil && r.tenantAvailable(principal.Tenant())
}

func (r *localPersonaAdminEvaluationRunner) RunPersonaEvaluation(ctx context.Context, actor PersonaAdminCommandActor, personaID string) (productui.PersonaAdminEvaluationResult, error) {
	if r == nil || r.store == nil || r.agents == nil || r.tenantUUID == nil || r.now == nil || r.newRunID == nil || !r.tenantAvailable(actor.Tenant) {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	tenant, err := r.store.ForTenant(ctx, actor.Tenant)
	if err != nil || tenant == nil {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	row, state, err := latestPersonaVersion(ctx, tenant, personaID, "")
	if err != nil || state != agentpersonastore.StateInReview {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(row.Profile, &profile) != nil || profile.PersonaID != personaID || int64(profile.Version) != row.Version {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	suite, err := localAgentDemoEvaluationSuiteFor(profile)
	if err != nil {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	manifest, err := (AgentManifestStoreAdapter{Store: r.agents, TenantID: r.tenantUUID(actor.Tenant)}).ResolveAgentManifestContext(ctx, profile.Manifest)
	if err != nil {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	candidate, _, err := NewLocalPersonaOpenAICandidate(manifest)
	if err != nil {
		return productui.PersonaAdminEvaluationResult{}, ErrPersonaAdminEvaluationUnavailable
	}
	service, err := NewLocalPersonaAdminEvaluationService(r.store, r.privateKey, "sha256:"+candidate.ProfileDigest, suite, r.now, r.newRunID)
	if err != nil {
		return productui.PersonaAdminEvaluationResult{}, err
	}
	return service.RunPersonaEvaluation(ctx, actor, personaID)
}

// prepareLocalAdminEvaluation replaces only the local-dev command store with
// the verifier used by agent-demo and returns its deterministic evaluator. The
// ordinary serving profile cannot load this private local authority.
func (w *personaServeWiring) prepareLocalAdminEvaluation(agentDB *agentstore.Store, profile, tenantID string, now func() time.Time, servedTenants ...string) (PersonaAdminEvaluationRunner, error) {
	if w == nil || agentDB == nil || profile != ServeProfileLocalDev || tenantID != localAgentDemoTenant || now == nil {
		return nil, ErrPersonaAdminEvaluationUnavailable
	}
	privateKey, err := loadOrCreateLocalAgentDemoEvaluationKey(filepath.FromSlash(localAgentDemoEvaluationPath))
	if err != nil {
		return nil, err
	}
	tenant := values.TenantId(tenantID)
	mapper := func(value values.TenantId) uuid.UUID { return pgstore.TenantID(value.String()) }
	reviews, err := agentpersonastore.NewDurableReviewAuthority(mapper)
	if err != nil {
		return nil, err
	}
	evaluations, err := localAgentDemoEvaluationAuthority(privateKey, tenantID, mapper, now)
	if err != nil {
		return nil, err
	}
	store, err := agentpersonastore.NewWithPublicationAuthorities(agentDB, mapper, reviews, evaluations)
	if err != nil {
		return nil, err
	}
	w.store = store
	return &localPersonaAdminEvaluationRunner{store: store, agents: agentDB, tenant: tenant, tenants: append([]string(nil), servedTenants...), tenantUUID: mapper, privateKey: privateKey, now: now, newRunID: uuid.NewString}, nil
}

func (r *localPersonaAdminEvaluationRunner) tenantAvailable(tenant values.TenantId) bool {
	if r == nil {
		return false
	}
	if len(r.tenants) == 0 {
		return tenant == r.tenant
	}
	return localPersonaOpenAIDemoTenant(tenant.String()) && slices.Contains(r.tenants, tenant.String())
}
