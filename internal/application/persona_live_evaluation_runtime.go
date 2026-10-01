package application

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	modelopenai "github.com/monstercameron/human-capital-management-suite/internal/agentmodel/openai"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentbudgetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcandidateevalstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// PersonaLiveEvaluationFixtureDeployment contains previously provisioned
// synthetic owner records and verified human contexts. Definition and candidate
// authorization are separately pinned to the reviewed production candidate.
type PersonaLiveEvaluationFixtureDeployment struct {
	Definitions *PersonaCandidateDefinitionSource
	Scope       *DatabasePersonaCandidateScope
	OwnerFacts  PersonaRunOwnerFactsSource
	Authority   agentrun.Authority
	Grants      PersonaGrantTenantStoreFactory
	GrantIssuer agentinvoke.GrantIssuer
	Skills      PersonaLiveCaseSkillSource
	Placements  map[string]PersonaLiveCasePlacement
	Threads     agentinvoke.ThreadReader
	Tools       interface {
		PersonaRunT0ToolExecutionPort
		PersonaRuntimeToolGroundingSource
		PersonaRuntimeToolSourceValidator
	}
}

// PersonaLiveEvaluationRuntimeConfig is reserved for local synthetic
// evaluation. JournalDB selects the restricted evaluator writer role; the
// ordinary AgentStore retains its normal current-owner permissions.
type PersonaLiveEvaluationRuntimeConfig struct {
	AgentStore   *agentstore.Store
	CorePool     *pgxadapter.Pool
	Personas     *agentpersonastore.Store
	JournalDB    dbport.Beginner
	Chat         chatcore.ConversationService
	PrivatePosts chatcore.EphemeralStore
	Fixtures     PersonaLiveEvaluationFixtureDeployment
	Candidate    agentmodel.ModelProfile
	Route        PersonaRunModelRoute
	SkillID      string
	APIKey       string
	BaseURL      string
	HTTPClient   *http.Client
	Signing      LocalPersonaModelSigningMaterial
	TenantUUID   func(values.TenantId) uuid.UUID
	Now          func() time.Time
	RefusalLink  string
}

type PersonaLiveEvaluationRuntime struct {
	target         agenteval.PersonaEvaluationTarget
	suite          agenteval.PersonaSuite
	candidate      agentmodel.ModelProfile
	manifestDigest string
	executor       *PersonaLiveCaseExecutor
	reader         *PersonaLiveCaseEvidenceReader
	invokerContext context.Context
}

// NewPersonaLiveEvaluationRuntime composes real SchemaFlux/OpenAI transport,
// preflight token counting, single-use credential leases, durable budgets,
// admitted worker execution, protected output and committed private delivery.
// It never inserts a production route or sets evaluation/publication flags.
func NewPersonaLiveEvaluationRuntime(ctx context.Context, c PersonaLiveEvaluationRuntimeConfig) (*PersonaLiveEvaluationRuntime, error) {
	f := c.Fixtures
	if ctx == nil || c.AgentStore == nil || c.CorePool == nil || c.Personas == nil || c.JournalDB == nil || c.Chat == nil || c.PrivatePosts == nil || f.Definitions == nil || f.Scope == nil || f.OwnerFacts == nil || f.Authority == nil || f.Grants == nil || f.GrantIssuer == nil || f.Skills == nil || f.Threads == nil || f.Tools == nil || c.TenantUUID == nil || c.Now == nil || !required(c.SkillID) || !required(c.RefusalLink) || c.Candidate.Evaluation.Passed || c.Candidate.ProfileDigest != agentmodel.ModelProfileDigest(c.Candidate) {
		return nil, agenteval.ErrPersonaEvaluation
	}
	target := f.Definitions.Target
	selection := agentmodel.ModelSelection{ProfileID: c.Candidate.ID, ProfileDigest: c.Candidate.ProfileDigest, Identity: c.Candidate.Identity}
	if target != f.Scope.Target || target.ModelDigest != "sha256:"+selection.ProfileDigest || c.Route.Route.Pin.Primary != selection || len(c.Route.Route.Pin.Fallbacks) != 0 || c.Route.Purpose != LocalPersonaOpenAIPurpose || selection.Identity != (agentmodel.ModelIdentity{ProviderID: "openai", ModelID: LocalPersonaOpenAIModelID, Version: LocalPersonaOpenAIModelVersion}) {
		return nil, agenteval.ErrPersonaEvaluation
	}
	suite := agenteval.PolicyHelperSuite(c.SkillID)
	for _, testCase := range suite.Cases {
		if _, ok := f.Placements[testCase.ID]; !ok {
			return nil, agenteval.ErrPersonaEvaluation
		}
	}
	journal, err := agentcandidateevalstore.NewWithTenantUUID(c.JournalDB, func(tenant string) uuid.UUID { return c.TenantUUID(values.TenantId(tenant)) })
	if err != nil {
		return nil, err
	}
	invocations, err := agentinvocationstore.NewWithTenantUUID(c.AgentStore, func(tenant string) uuid.UUID { return c.TenantUUID(values.TenantId(tenant)) })
	if err != nil {
		return nil, err
	}
	outputs, err := c.Personas.ForTenant(ctx, values.TenantId(target.SyntheticTenantID))
	if err != nil {
		return nil, err
	}
	budgetStore, err := agentbudgetstore.New(c.CorePool, c.TenantUUID)
	if err != nil {
		return nil, err
	}
	limits := agentbudget.Limits{Steps: 2, Tokens: 10240, SpendMicros: LocalPersonaOpenAIMaxCostMicros, WallClock: LocalPersonaOpenAIMaxLatency}
	period := agentbudget.Limits{Steps: 128, Tokens: 655360, SpendMicros: suite.MaxCostMicros, WallClock: 128 * LocalPersonaOpenAIMaxLatency}
	ledger, err := agentbudget.NewWithPersistence(agentbudget.Policy{TaskDefault: limits, UserDaily: period, TenantMonthly: period, MaxRetries: 1, LoopThreshold: 2}, c.Now, budgetStore)
	if err != nil {
		return nil, err
	}
	state, err := budgetStore.Load(ctx, values.TenantId(target.SyntheticTenantID), c.Now())
	if err != nil {
		return nil, err
	}
	if err = ledger.Restore(state); err != nil {
		return nil, err
	}
	budget, err := NewPersonaLedgerModelBudget(ledger, c.AgentStore, f.Authority, c.TenantUUID, c.Now)
	if err != nil {
		return nil, err
	}
	pricing, _, err := NewLocalPersonaOpenAISignedPricing(c.Signing)
	if err != nil {
		return nil, err
	}
	const workerID = "local-persona-candidate-worker"
	const workloadID = "local-persona-candidate-model"
	credential, err := NewOpenAIModelCredentialAuthority(OpenAIModelCredentialConfig{CredentialID: "local-openai-candidate", Version: "v1", Workload: workloadID, Scopes: []OpenAIModelCredentialScope{{TenantID: target.SyntheticTenantID, Region: LocalPersonaOpenAIRegion, Purpose: LocalPersonaOpenAIPurpose, Destination: selection.ProfileID}}, MaxTTL: time.Minute, Now: c.Now})
	if err != nil {
		return nil, err
	}
	manager, err := lease.NewManager(credential, c.Now)
	if err != nil {
		return nil, err
	}
	leases, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"openai": credential}, Leases: manager, MaxTTL: time.Minute})
	if err != nil {
		return nil, err
	}
	classes := []trustdlp.DataClass{trustdlp.ClassPublic, trustdlp.ClassInternal}
	policy := PersonaModelDeployment{Destinations: []outbound.Destination{{Name: selection.ProfileID, TrustBundleRef: "openai-api:https:system-root-cas", Purposes: []string{LocalPersonaOpenAIPurpose}, DataClasses: []string{"PUBLIC", "INTERNAL"}}}, Clearances: []trustdlp.Clearance{{Destination: selection.ProfileID, Classes: classes, Decision: trustdlp.Allow}}}
	egress, err := policy.egress()
	if err != nil {
		return nil, err
	}
	provider, err := modelopenai.New(modelopenai.Config{APIKey: c.APIKey, BaseURL: c.BaseURL, HTTPClient: c.HTTPClient, ModelProfile: selection.ProfileID, Identity: selection.Identity, PreflightInputTokens: true})
	if err != nil {
		return nil, err
	}
	adapter, err := agentmodel.NewSchemaFluxAdapter(provider, selection, pricing)
	if err != nil {
		return nil, err
	}
	outputKey, workerKey, err := personaModelSigningKeys(c.Signing.OutputSeed, c.Signing.WorkloadSeed)
	if err != nil {
		return nil, err
	}
	recovery, err := agentsecurity.NewFinalOutputRecoveryAuthority("local-persona-candidate-output-v1", outputKey)
	if err != nil {
		return nil, err
	}
	verifier, err := agentsecurity.NewFinalOutputRecoveryVerifier(map[string]ed25519.PublicKey{"local-persona-candidate-output-v1": outputKey.Public().(ed25519.PublicKey)})
	if err != nil {
		return nil, err
	}
	issuer, err := workload.NewIssuer(workload.IssuerConfig{Name: "local-persona-candidate-issuer", KeyID: "candidate-worker-v1", Private: workerKey, Now: c.Now})
	if err != nil {
		return nil, err
	}
	workloadVerifier, err := workload.NewVerifier(workload.VerifierConfig{Keys: &personaModelWorkloadPublicKey{issuer: "local-persona-candidate-issuer", keyID: "candidate-worker-v1", key: workerKey.Public().(ed25519.PublicKey)}, Cell: "local-dev", Now: c.Now})
	if err != nil {
		return nil, err
	}
	worker := &VerifiedPersonaPrivateChatWorkloadIdentitySource{Verifier: workloadVerifier, Credentials: &personaModelRenewingCredential{issuer: issuer, spec: workload.IssueSpec{Subject: workerID, Role: workload.RoleWorker, Cell: "local-dev", Lifetime: time.Minute}}}
	requestSource := &PersonaCandidateRunRequestSource{Definitions: f.Definitions, OwnerFacts: f.OwnerFacts}
	builder, err := NewPersonaRunRequestBuilder(requestSource)
	if err != nil {
		return nil, err
	}
	work := &PersonaCandidateModelWorkSource{Definitions: f.Definitions, Threads: f.Threads, Leases: leases, Route: c.Route, Workload: workloadID, Now: c.Now, Ledger: ledger}
	admissions, err := agentrunstore.NewAdmissionRepository(c.AgentStore, c.TenantUUID(values.TenantId(target.SyntheticTenantID)), values.TenantId(target.SyntheticTenantID))
	if err != nil {
		return nil, err
	}
	evidence := &PersonaCandidateSourceEvidence{Definitions: f.Definitions, Admissions: admissions, Authority: f.Authority, Threads: f.Threads, Route: c.Route, ToolJournal: DatabasePersonaRuntimeToolJournal{Store: c.Personas}, ToolSources: f.Tools}
	f.Scope.Sources = evidence
	f.Definitions.Scope = f.Scope
	terms := LocalPersonaOpenAIProcessingTerms(selection.ProfileID)
	terms.SourceRules = []agentegress.ProviderSourceRule{{Class: "synthetic-fixture", Classes: classes}, {Class: "persona-untrusted-tool-result", Classes: classes}, {Class: "persona-model-tool-proposal", Classes: classes}}
	dispatcher, err := agentegress.NewProviderDispatcher(egress, manager, evidence, []agentegress.ProviderTerms{terms})
	if err != nil {
		return nil, err
	}
	model, err := NewPersonaCandidateModelGateway(PersonaCandidateModelConfig{Target: target, Selection: selection, Adapter: adapter, Egress: dispatcher, Pricing: pricing, Budget: budget, Scope: f.Scope, Journal: journal})
	if err != nil {
		return nil, err
	}
	outputAuthority, err := NewPersonaCandidateOutputAuthority(PersonaCandidateOutputAuthorityConfig{Authority: f.Authority, Definitions: f.Definitions, Threads: f.Threads, Route: c.Route, Grants: f.Grants, Worker: worker, Tools: f.Tools, Now: c.Now})
	if err != nil {
		return nil, err
	}
	output, err := NewPersonaRunOutputValidator(PersonaRunOutputValidatorConfig{Authority: outputAuthority, Persister: AgentPersonaRunFinalOutputPersister{Stores: AgentPersonaRunFinalOutputStoreFactory{Store: c.Personas}, Recovery: recovery}})
	if err != nil {
		return nil, err
	}
	committer, ok := c.Chat.(chatcore.PersonaReplyCommitter)
	if !ok {
		return nil, agenteval.ErrPersonaEvaluation
	}
	reply, err := NewPersonaReplyDelivery(c.Chat, committer, personaPrivateOnlyAudienceFloor{})
	if err != nil {
		return nil, err
	}
	rehydrator := &personaRuntimeOutputRecovery{store: c.AgentStore, tenantUUID: c.TenantUUID, authority: outputAuthority, verifier: verifier}
	base := PersonaRunStarterConfig{Builder: builder, Authority: f.Authority, Model: model, Work: work, Tools: f.Tools, Output: output, Reply: &personaRuntimeCurrentReply{next: reply, authority: rehydrator}, WorkerID: workerID, LeaseTTL: LocalPersonaOpenAIMaxLatency, Now: c.Now}
	factory, err := NewDatabasePersonaRunTenantRuntimeFactory(c.AgentStore, c.TenantUUID, base)
	if err != nil {
		return nil, err
	}
	runtime, err := factory.ForPersonaRunTenant(ctx, target.SyntheticTenantID)
	if err != nil {
		return nil, err
	}
	runtime.Model = model
	fixtures := &PersonaLiveChatFixtureSource{Definitions: f.Definitions, Chat: c.Chat, Invocations: invocations, Grants: f.GrantIssuer, Skills: f.Skills, Placements: f.Placements, Now: c.Now, NewID: func() string { return uuid.NewString() }}
	private := &PersonaLivePrivateDeliveryReader{Store: c.PrivatePosts, InvokerContext: f.Placements[suite.Cases[0].ID].InvokerContext, Outputs: outputs, Verifier: verifier, Rehydrator: rehydrator}
	executor, err := NewPersonaLiveCaseExecutor(PersonaLiveCaseExecutorConfig{Target: target, Scope: f.Scope, Fixtures: fixtures, Runtime: runtime, Model: model, Journal: journal, Outputs: outputs, Delivery: private, RefusalLink: c.RefusalLink})
	if err != nil {
		return nil, err
	}
	reader := &PersonaLiveCaseEvidenceReader{Scope: f.Scope, Journal: journal, Invocations: invocations, Admissions: admissions, Runs: runtime.ExecutionStore, Outputs: outputs, OutputVerifier: verifier, OutputRehydrator: rehydrator, Threads: f.Threads, Suite: suite, Delivery: private, Disclosure: private, Usage: budgetStore, Selection: selection}
	_, _, manifest, err := f.Definitions.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return nil, err
	}
	return &PersonaLiveEvaluationRuntime{target: target, suite: suite, candidate: c.Candidate, manifestDigest: digest, executor: executor, reader: reader, invokerContext: private.InvokerContext}, nil
}

// Evaluate returns the sealed measured report. Qualification is produced only
// when every independently read live case passes the canonical suite gates.
func (r *PersonaLiveEvaluationRuntime) Evaluate(ctx context.Context) (agenteval.PersonaEvaluationReport, agentmodel.ModelProfile, error) {
	if r == nil || ctx == nil || r.invokerContext == nil {
		return agenteval.PersonaEvaluationReport{}, agentmodel.ModelProfile{}, agenteval.ErrPersonaEvaluation
	}
	principal, ok := trust.FromContext(r.invokerContext)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != r.target.SyntheticTenantID || principal.Subject() != r.target.InvokerID {
		return agenteval.PersonaEvaluationReport{}, agentmodel.ModelProfile{}, agenteval.ErrPersonaEvaluation
	}
	ctx = trust.WithPrincipal(ctx, principal)
	report, err := agenteval.EvaluatePersonaSuite(ctx, r.target, r.suite, r.executor, r.reader)
	if err != nil {
		return report, agentmodel.ModelProfile{}, err
	}
	profile, err := QualifyPersonaCandidateModelProfile(report, r.candidate, r.manifestDigest, r.suite)
	return report, profile, err
}
