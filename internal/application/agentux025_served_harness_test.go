package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// Served-mention composition harness (AGENTUX-025 REFACTOR). One harness
// composes a chat mention into a delivered, grounded answer exactly as the
// served cell does: the production post writer commits the human post, the
// production invocation resolves the mention against the production persona
// lookup, the production admission adapter admits through the durable
// admission store, the production executor runs a search-then-answer model
// turn against durable runstate, and the production reply delivery commits
// the sealed answer back to real chat. Only the model's words, the tool's
// search hit and the directory facts are fixtures. The published-persona
// seed is shared with AGENTUX-021; the AGENTUX-005 demo preparation composes
// the same production wiring functions with its own prep-specific setup.
const (
	agentux025Tenant      = "tenant-a"
	agentux025Admin       = "admin"
	agentux025Member      = "employee"
	agentux025AgentHandle = "policy-helper"
	agentux025PersonaID   = "persona-a"
	agentux025Channel     = "general"
	agentux025DM          = "admin-policy-helper"
	agentux025AgentID     = "agent-a"
	agentux025AgentVer    = "4"

	// agentux025Announcement is the model's first-turn narration beside its
	// tool proposal. It is dropped by the executor (G17) and must never
	// reach a delivered post.
	agentux025Announcement = "I will search the policy documents."
	// agentux025Answer is the model's second-turn answer. It names the
	// document the search hit returned, so the delivered post is grounded
	// in the placed document.
	agentux025Answer = "The Leave policy lets employees carry over unused PTO."
)

var agentux025AgentDigest = "sha256:" + strings.Repeat("d", 64)

// agentux025InstantModel is the deterministic model fixture: one document
// search proposal beside announcement text, then the grounded answer. Like a
// real model it reads the turn from the request: a request already carrying
// a tool result gets the answer, anything else gets the search proposal.
// Calls are counted per model task (one task per run) for the test's
// assertions.
type agentux025InstantModel struct {
	mu    sync.Mutex
	turns map[string][]string
}

func (m *agentux025InstantModel) Execute(_ context.Context, request AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	answered := false
	for _, message := range request.Model.Messages {
		if message.Role == agentmodel.RoleTool {
			answered = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.turns == nil {
		m.turns = map[string][]string{}
	}
	task := request.Task.TaskID
	if task == "" {
		task = "untraced"
	}
	if answered {
		m.turns[task] = append(m.turns[task], "answer")
		return AgentModelExecutorResult{Result: agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: agentux025Answer, Finish: agentmodel.FinishComplete}}, nil
	}
	m.turns[task] = append(m.turns[task], "search")
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: agentux025Announcement, Finish: agentmodel.FinishToolCalls, ToolProposals: []agentmodel.ToolProposal{{ID: "search-1", Name: personaDocumentSearchTool, Arguments: []byte(`{"query":"leave carryover"}`)}}}}, nil
}

func (m *agentux025InstantModel) turnKinds(task string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.turns[task]...)
}

// agentux025Facts resolves the builder's owner facts from the invocation, as
// the served facts source reads them from durable state: tenant and trigger
// echo the canonical invocation, audience and context pin the invocation
// conversation and thread, and agent, digest, deadline and budget are the
// harness's fixed deployment values.
type agentux025Facts struct{ personaDigest string }

func (f agentux025Facts) ResolvePersonaRun(_ context.Context, invocation agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
	return PersonaRunRequestFacts{
		TenantID:       invocation.TenantID,
		TriggerID:      invocation.InvocationID,
		LegalEntityID:  "legal-025",
		Agent:          agentrun.VersionRef{AgentID: agentux025AgentID, Version: agentux025AgentVer, Digest: agentux025AgentDigest},
		AgentPrincipal: "persona-principal",
		PersonaDigest:  f.personaDigest,
		Audience:       agentrun.AudienceScope{ID: invocation.ConversationID, SnapshotID: "aud-" + invocation.ConversationID, Digest: "sha256:" + strings.Repeat("b", 64)},
		Context:        agentrun.ContextScope{ID: invocation.ThreadID, SnapshotID: "ctx-" + invocation.ThreadID, Digest: "sha256:" + strings.Repeat("c", 64)},
		Deadline:       time.Now().UTC().Add(2 * time.Minute),
		Budget:         agentrun.Budget{MaxCostMicros: 100000, MaxInputTokens: 10000, MaxOutputTokens: 2000},
	}, nil
}

// agentux025OutputAuthority seals persona replies the way the served output
// authority does, minus the model gateway: a fresh tool gateway per
// admission, the chat-reply tool admitted for the run's own invocation, and
// the invoking post as the grounding observation. Static across runs except
// for the admission-bound call, so the harness's two mentions seal
// independently.
type agentux025OutputAuthority struct {
	personaDigest string
	questions     *agentux025Questions
}

type agentux025Questions struct {
	mu     sync.Mutex
	bodies map[string]string
}

func (q *agentux025Questions) set(postID, body string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.bodies == nil {
		q.bodies = map[string]string{}
	}
	q.bodies[postID] = body
}

func (q *agentux025Questions) get(postID string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.bodies[postID]
}

func (a agentux025OutputAuthority) ResolvePersonaRunChatReplyAuthority(ctx context.Context, admission agentrun.Record, _ runstate.Run) (PersonaRunChatReplyAuthority, error) {
	request := admission.Request
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	call := agentsecurity.ToolCall{
		Agent:      agentsecurity.AgentIdentity{Identity: "workload:persona", AgentID: request.Agent.AgentID, Tenant: request.Source.TenantID, Purpose: "persona-mention", ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 2},
		Delegation: []agentsecurity.DelegationLink{{GrantID: "agentux-025-grant", Delegator: request.Principal.InvokerID, Delegate: request.Agent.AgentID, Tenant: request.Source.TenantID, Purpose: "persona-mention", ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 2}},
		Tenant:     request.Source.TenantID, Purpose: "persona-mention", Tool: "persona.chat_reply", Capability: "persona.reply", Version: 1,
		Nonce: request.Source.Key, Args: map[string]any{"invocation": request.Source.Key}, InputTaint: []string{string(agentsecurity.TaintDerived)},
		Provenance: []string{PersonaChatReplyProvenance, "chat.current"}, CostBudget: 1, DataScope: []string{"chat.current"},
	}
	call.ArgsDigest, err = agentsecurity.DigestArguments(call.Args)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	securityAdmission, err := gateway.Admit(call)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	question := a.questions.get(request.Source.Ref)
	if question == "" {
		question = "Summarize the leave policy."
	}
	postHash := sha256.Sum256([]byte(question))
	evidence, err := gateway.Observe(agentsecurity.SourceChat, question, agentsecurity.KindObservation, agentsecurity.Citation{
		SourceID: "chat:" + request.Source.Ref, Location: "conversation:" + request.Audience.ID + "/" + request.Source.Ref, Digest: "sha256:" + hex.EncodeToString(postHash[:]),
	})
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	_ = ctx
	return PersonaRunChatReplyAuthority{
		Schema:      PersonaRunOutputSchemaRef{ID: PersonaChatReplySchema, Version: 1, Digest: PersonaChatReplySchemaDigest, PersonaDigest: a.personaDigest},
		ModelDigest: "sha256:" + strings.Repeat("f", 64), Gateway: gateway, Admission: securityAdmission, Grounding: []agentsecurity.Datum{evidence},
	}, nil
}

func (a agentux025OutputAuthority) ResolvePersonaRunOutputObservationAuthority(_ context.Context, binding PersonaRunOutputObservationBinding, admission agentrun.Record, run runstate.Run) (PersonaRunOutputObservationAuthority, error) {
	resolved, err := a.ResolvePersonaRunChatReplyAuthority(context.Background(), admission, run)
	if err != nil {
		return PersonaRunOutputObservationAuthority{}, err
	}
	return PersonaRunOutputObservationAuthority{Binding: binding, Output: resolved}, nil
}

var (
	_ PersonaRunChatReplyAuthoritySource         = agentux025OutputAuthority{}
	_ PersonaRunOutputObservationAuthoritySource = agentux025OutputAuthority{}
)

// agentux025Authority admits the harness persona for the harness admin in the
// harness tenant, bound to the installation of the conversation the mention
// was posted in. Installations are looked up from the seeded store, so an
// uninstalled conversation is refused exactly as the served authority
// refuses it.
type agentux025Authority struct {
	scoped        *agentpersonastore.TenantStore
	skills        agentinvoke.SkillScopes
	installations map[string]string
}

func (a agentux025Authority) Resolve(ctx context.Context, request agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	if request.PersonaID != agentux025PersonaID || request.InvokerID != agentux025Admin || request.TenantID != agentux025Tenant {
		return agentinvoke.Admission{}, errors.New("unexpected served-harness invocation")
	}
	installationID, ok := a.installations[request.ConversationID]
	if !ok {
		return agentinvoke.Admission{}, errors.New("persona is not installed in " + request.ConversationID)
	}
	if _, installation, err := a.scoped.ReadCurrentPersonaAuthority(ctx, request.ConversationID, request.PersonaID); err != nil || installation.InstallationID != installationID {
		return agentinvoke.Admission{}, errors.New("installed persona authority does not cover " + request.ConversationID)
	}
	version := "1"
	return agentinvoke.Admission{Persona: agentinvoke.Persona{ID: agentux025PersonaID, Version: version, PinnedSkills: a.skills.Clone(), Current: true}, Installation: agentinvoke.Installation{ID: installationID, Current: true, SkillCeiling: a.skills.Clone()}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: a.skills.Clone()}, Discoverable: a.skills.Clone(), HumanMember: true, AudienceMember: true, PersonaInstalled: true}, nil
}

// agentux025Grant issues the on-behalf-of grant the invocation binds to the
// run. The served issuer persists the grant; the harness echoes the request
// because the harness output authority re-derives admission from the run.
type agentux025Grant struct{}

func (agentux025Grant) CreateOnBehalfOfGrant(_ context.Context, request agentinvoke.GrantRequest) (agentinvoke.DelegationGrant, error) {
	return agentinvoke.DelegationGrant{ID: "agentux-025-grant", UserID: request.UserID, TenantID: request.TenantID, TaskID: request.InvocationID, TargetAgentID: request.TargetAgentID, Skills: request.Skills.Clone(), ExpiresAt: request.ExpiresAt}, nil
}

// agentux025T0Policy binds T0 runs to the harness persona, tenant, admin and
// one skill.
type agentux025T0Policy struct{}

func (agentux025T0Policy) IsBoundT0Run(_ context.Context, request agentinvoke.RunRequest) (bool, error) {
	return request.PersonaID == agentux025PersonaID && request.TenantID == agentux025Tenant && request.InvokerID == agentux025Admin && len(request.Skills) == 1, nil
}

// agentux025CapturingRuns records every started run by invocation ID so the
// test can derive the durable admission and run IDs.
type agentux025CapturingRuns struct {
	mu       sync.Mutex
	next     agentinvoke.RunStarter
	requests map[string]agentinvoke.RunRequest
}

func (r *agentux025CapturingRuns) Start(ctx context.Context, request agentinvoke.RunRequest) error {
	r.mu.Lock()
	if r.requests == nil {
		r.requests = map[string]agentinvoke.RunRequest{}
	}
	r.requests[request.InvocationID] = request
	r.mu.Unlock()
	return r.next.Start(ctx, request)
}

func (r *agentux025CapturingRuns) started(invocationID string) (agentinvoke.RunRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	request, ok := r.requests[invocationID]
	return request, ok
}

func (r *agentux025CapturingRuns) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// agentux025FailureSink captures post-commit invocation failures. A green
// composed path records none.
type agentux025FailureSink struct {
	mu       sync.Mutex
	failures []error
}

func (s *agentux025FailureSink) RecordPersonaInvocationFailure(_ context.Context, _ string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = append(s.failures, err)
}

func (s *agentux025FailureSink) list() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.failures...)
}

// agentux025Classes classifies disclosed chat bytes for the harness tenant;
// agentux025Bodies classifies generated reply text. Both are fixtures over
// fixed harness content, fail-closed outside it.
type agentux025Classes struct{}

func (agentux025Classes) PersonaPublicChatDisclosureClass(_ context.Context, tenant, conversation, postID, digest string) (trustdlp.DataClass, error) {
	if tenant != agentux025Tenant || (conversation != agentux025Channel && conversation != agentux025DM) || postID == "" || !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return "", errors.New("unclassified disclosure")
	}
	return trustdlp.ClassInternal, nil
}

type agentux025Bodies struct{}

func (agentux025Bodies) ClassifyPersonaReplyText(_ context.Context, tenant, body string) (trustdlp.DataClass, error) {
	if tenant != agentux025Tenant || strings.TrimSpace(body) == "" {
		return "", errors.New("unclassified reply text")
	}
	return trustdlp.ClassInternal, nil
}

// agentux025ReferenceDirectory answers chat's reference checks from the
// seeded installations: the harness persona is eligible exactly where it is
// installed. People and conversation autocomplete have no directory in the
// harness and match nothing; they are not on the mention path.
type agentux025ReferenceDirectory struct {
	scoped *agentpersonastore.TenantStore
}

func (d agentux025ReferenceDirectory) People(context.Context, chat.Principal, string, string, string) ([]chat.ReferenceCandidate, error) {
	return []chat.ReferenceCandidate{}, nil
}

func (d agentux025ReferenceDirectory) Agents(ctx context.Context, _ chat.Principal, tenant, conversation, query string) ([]chat.ReferenceCandidate, error) {
	if tenant != agentux025Tenant || (conversation != agentux025Channel && conversation != agentux025DM) {
		return []chat.ReferenceCandidate{}, nil
	}
	if _, _, err := d.scoped.ReadCurrentPersonaAuthority(ctx, conversation, agentux025PersonaID); err != nil {
		return []chat.ReferenceCandidate{}, nil
	}
	if query != "" && !strings.Contains(agentux025AgentHandle, query) && !strings.Contains("Policy Helper", query) {
		return []chat.ReferenceCandidate{}, nil
	}
	return []chat.ReferenceCandidate{{Reference: chat.Reference{Kind: chat.AgentMention, TenantID: tenant, ID: agentux025AgentHandle, Display: "Policy Helper", ConversationID: conversation}, Eligible: true}}, nil
}

func (d agentux025ReferenceDirectory) Conversations(context.Context, chat.Principal, string, string, string) ([]chat.ReferenceCandidate, error) {
	return []chat.ReferenceCandidate{}, nil
}

func (d agentux025ReferenceDirectory) AgentEligible(ctx context.Context, tenant, conversation, refID string) bool {
	if tenant != agentux025Tenant || refID != agentux025AgentHandle {
		return false
	}
	_, installation, err := d.scoped.ReadCurrentPersonaAuthority(ctx, conversation, agentux025PersonaID)
	if err != nil {
		return false
	}
	want := map[string]string{agentux025Channel: "install-general", agentux025DM: "install-direct"}[conversation]
	return installation.InstallationID == want
}

var (
	_ chat.ReferenceDirectory      = agentux025ReferenceDirectory{}
	_ chat.AgentReferenceDirectory = agentux025ReferenceDirectory{}
)

// agentux025Harness is the composed served mention path over two
// role-separated stores: the agent store under its least-privilege app role
// and the chat store under the harness's non-login app role.
type agentux025Harness struct {
	t             *testing.T
	now           time.Time
	clock         func() time.Time
	adminCtx      context.Context
	agentDB       *pgtest.DB
	agents        *agentstore.Store
	personas      *agentpersonastore.Store
	scoped        *agentpersonastore.TenantStore
	personaDigest string
	chatDB        *pgtest.DB
	chatRole      string
	chat          *chatstore.Store
	adapter       *chatstore.Adapter
	service       *chat.Service
	invocation    *personaChatInvocation
	floor         *PersonaRuntimeAudienceFloor
	runs          *agentux025CapturingRuns
	failures      *agentux025FailureSink
	model         *agentux025InstantModel
	persister     *personaRunFinalOutputPersisterFake
	execution     runstate.Store
	questions     *agentux025Questions
	invocations   map[string]string
}

func newAgentux025Harness(t *testing.T) *agentux025Harness {
	t.Helper()
	ctx := context.Background()
	h := &agentux025Harness{t: t, questions: &agentux025Questions{}, invocations: map[string]string{}}
	h.now = time.Now().UTC().Truncate(time.Microsecond)
	h.clock = func() time.Time { return h.now }
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: agentux025Tenant, Subject: agentux025Admin, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "agentux025", CredentialDigest: "agentux025", IssuedAt: h.now.Add(-time.Minute), ExpiresAt: h.now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	h.adminCtx = trust.WithPrincipal(ctx, principal)

	h.agentDB = pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, h.agentDB.SQL); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	h.agentDB.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	agents, err := agentstore.New(ctx, agentstore.Config{DSN: personaChatSchemaDSN(t, h.agentDB.URL, h.agentDB.Schema), CoreDSN: "postgres://unused@127.0.0.1:1/unused", MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agents.Close)
	h.agents = agents
	mapper := func(tenant values.TenantId) uuid.UUID {
		if tenant == agentux025Tenant {
			return tenantID
		}
		return uuid.Nil
	}
	// The personas store opens over the pooled agent store (AppRole-pinned),
	// not one connection: the detached run and the posting path query
	// installations concurrently.
	personas, err := agentpersonastore.New(agents, mapper)
	if err != nil {
		t.Fatal(err)
	}
	h.personas = personas
	scoped, err := personas.Scoped(agentux025Tenant)
	if err != nil {
		t.Fatal(err)
	}
	h.scoped = scoped
	h.personaDigest = agentux025SeedPublishedPersona(t, h.agentDB, tenantID, scoped, h.now, agentux025DefaultSeedConfig(t))

	h.chatDB = pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, h.chatDB)
	h.chatRole = agentux025CreateChatRole(t, h.chatDB)
	raw, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, h.chatDB.URL, h.chatDB.Schema), Role: h.chatRole})
	if err != nil {
		t.Fatal(err)
	}
	h.chat = raw
	adapter := chatstore.NewAdapter(raw)
	t.Cleanup(adapter.Close)
	h.adapter = adapter
	service := chat.NewService(adapter, h.clock)
	service.SetAuthority(servedPersonaChatAuthority{})
	service.SetEphemeralStore(adapter)
	service.SetAgentSourceAccess(agentUX070SourceAccess{})
	service.SetPersonaDMResolver(agentp011PersonaDM{conversationID: agentux025DM})
	service.SetReferenceDirectory(agentux025ReferenceDirectory{scoped: scoped})
	h.service = service
	agentux025SeedConversations(t, h)

	identities, err := newProductionPersonaChatIdentityDirectory(personas)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := newProductionPersonaReferenceLookup(identities, personas)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := newPersonaChatReferenceResolver(lookup, h.clock)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := NewPersonaRunRequestBuilder(agentux025Facts{personaDigest: h.personaDigest})
	if err != nil {
		t.Fatal(err)
	}
	h.model = &agentux025InstantModel{}
	h.persister = &personaRunFinalOutputPersisterFake{}
	validator, err := NewPersonaRunOutputValidator(PersonaRunOutputValidatorConfig{Authority: agentux025OutputAuthority{personaDigest: h.personaDigest, questions: h.questions}, Persister: h.persister})
	if err != nil {
		t.Fatal(err)
	}
	committer, err := newPersonaPublicReplyCommitter(raw, &personaPipelineCurrentReplyAuthority{runtimeReplyCurrentAuthorityFake: &runtimeReplyCurrentAuthorityFake{}, store: adapter}, privateChatGatewayVerifiedWorker(t, h.now), h.clock)
	if err != nil {
		t.Fatal(err)
	}
	audience := NewPersonaPublicAudienceAuthority(raw)
	floor := &PersonaRuntimeAudienceFloor{Chat: service, Personas: personas, Authority: NewPersonaAudienceFloorAdapter(audience, audience, audience), Classes: agentux025Classes{}, BodyClasses: agentux025Bodies{}, Documents: proactiveDocumentAuthority{placed: true}}
	h.floor = floor
	delivery, err := NewPersonaReplyDelivery(service, committer, floor)
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewDatabasePersonaRunTenantRuntimeFactory(agents, mapper, PersonaRunStarterConfig{Builder: builder, Authority: personaChatAdmissionAuthorityFake{}, Model: h.model, Work: agentUXSpeedWork{t: t}, Tools: agentUXSpeedTools{}, Output: validator, Reply: delivery, WorkerID: "agentux-025-worker", LeaseTTL: time.Minute, Now: h.clock})
	if err != nil {
		t.Fatal(err)
	}
	tenantConfig, err := factory.ForPersonaRunTenant(ctx, agentux025Tenant)
	if err != nil {
		t.Fatal(err)
	}
	starter, err := NewPersonaRunStarter(tenantConfig)
	if err != nil {
		t.Fatal(err)
	}
	h.execution = tenantConfig.ExecutionStore
	skills := agentinvoke.SkillScopes{personaPolicyHelperSkillID: {"chat.current"}}
	h.runs = &agentux025CapturingRuns{next: starter}
	h.failures = &agentux025FailureSink{}
	invocation, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: service, References: resolver,
		Authority: agentux025Authority{scoped: scoped, skills: skills, installations: map[string]string{agentux025Channel: "install-general", agentux025DM: "install-direct"}},
		Grants:    &agentux025Grant{}, Runs: h.runs, T0Skills: agentux025T0Policy{}, Repository: agentux025InvocationRepository(t, agents, tenantID),
		Failures: h.failures, DetachedAfterCommit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.invocation = invocation
	return h
}

// agentux025InvocationRepository is the durable invocation store, as served:
// the admission reader restores a mention's source key from the claimed
// invocation row, so a memory repository would break the recheck boundary.
func agentux025InvocationRepository(t *testing.T, agents *agentstore.Store, tenantID uuid.UUID) *agentinvocationstore.Store {
	t.Helper()
	repository, err := agentinvocationstore.NewWithTenantUUID(agents, func(tenant string) uuid.UUID {
		if tenant == agentux025Tenant {
			return tenantID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

// agentux025SeedInstallation is one ACTIVE installation the shared seed writes.
type agentux025SeedInstallation struct {
	ID, Conversation, Class string
	Policy                  agentpersonastore.ChannelPolicy
}

// agentux025SeedConfig is a published persona with its owner and ACTIVE
// installations. AGENTUX-021 and AGENTUX-025 seed through this one helper;
// the lifecycle (reviewed version 1) is fixed because both compose the
// served path above publication, not the publication path itself.
type agentux025SeedConfig struct {
	Persona       agentpersona.PersonaVersion
	AgentVersion  string
	Owner         string
	AssignedBy    string
	Handle        string
	Installations []agentux025SeedInstallation
}

// agentux025SeedPublishedPersona writes a reviewed version 1, its owner and
// its ACTIVE installations, and registers the persona's chat identity. It
// returns the sealed profile digest the run path pins.
func agentux025SeedPublishedPersona(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, scoped *agentpersonastore.TenantStore, now time.Time, cfg agentux025SeedConfig) string {
	t.Helper()
	ctx := context.Background()
	raw, err := json.Marshal(cfg.Persona.Profile)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO persona_versions (tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES ($1,$2,1,$3,$4,$5,$6::jsonb,$7,now())`, tenantID, cfg.Persona.Profile.PersonaID, cfg.AgentVersion, cfg.Persona.Profile.Handle, cfg.Persona.Profile.DisplayName, string(raw), cfg.Persona.Digest)
	db.Exec(t, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES ($1,$2,'BUSINESS_OWNER',$3,$4,now())`, tenantID, cfg.Persona.Profile.PersonaID, cfg.Owner, cfg.AssignedBy)
	db.Exec(t, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES ($1,'publish-v1',$2,1,'IN_REVIEW','PUBLISHED','reviewed','reviewer',now(),$3,'sha256:review','reviewer','sha256:run',$3,'sha256:suite')`, tenantID, cfg.Persona.Profile.PersonaID, cfg.Persona.Digest)
	for _, installation := range cfg.Installations {
		policy, err := json.Marshal(installation.Policy)
		if err != nil {
			t.Fatal(err)
		}
		db.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,$2,$3,1,$4,$5,'admin',$6::jsonb,'ACTIVE',1,1,now(),now())`, tenantID, installation.ID, cfg.Persona.Profile.PersonaID, installation.Conversation, installation.Class, string(policy))
	}
	if err := scoped.RegisterPersonaChatIdentity(ctx, cfg.Handle, cfg.Persona.Profile.PersonaID, now); err != nil {
		t.Fatal(err)
	}
	return cfg.Persona.Digest
}

// agentux025SeedConfig builds the AGENTUX-025 persona: Policy Helper,
// published, installed in the public channel and the administrator's direct
// conversation with it.
func agentux025DefaultSeedConfig(t *testing.T) agentux025SeedConfig {
	t.Helper()
	sealed, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: agentux025AgentID, Version: 1, Digest: agentux025AgentDigest, SchemaVersion: 1},
		PersonaID: agentux025PersonaID, Version: 1, Handle: agentux025AgentHandle, DisplayName: "Policy Helper",
		AvatarRef: "avatar:policy-helper", Purpose: "Answer approved policy questions",
		Audience:     agentpersona.Audience{Roles: []string{"worker_self"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-a"}},
		SkillPins:    []agentskills.SkillPin{{ID: personaPolicyHelperSkillID, Version: 1, Digest: "skill-digest"}},
		TierCeiling:  agentskills.TierT0,
		Instructions: "Answer approved questions from policy documents.",
		Owner:        "admin", Steward: "steward", EvalSuiteRef: "eval",
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationChannel},
		ChannelClasses:    []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic},
		EvalLimits:        agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return agentux025SeedConfig{
		Persona: sealed, AgentVersion: "agent-v1", Owner: "admin", AssignedBy: "admin", Handle: agentux025AgentHandle,
		Installations: []agentux025SeedInstallation{
			{ID: "install-general", Conversation: agentux025Channel, Class: "PUBLIC", Policy: agentpersonastore.ChannelPolicy{PlacementClass: "ANY_INTERNAL", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPublic}, ConversationSearchAllowed: true}},
			{ID: "install-direct", Conversation: agentux025DM, Class: "PRIVATE", Policy: agentpersonastore.ChannelPolicy{PlacementClass: "PRIVATE", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}}},
		},
	}
}

// agentux025CreateChatRole creates the harness's non-login chat app role with
// data-plane grants on the harness chat schema: full row access to chat
// tables, sequences and routines, and nothing else — no superuser, no schema
// ownership, no rights outside its schema. The name is unique per harness
// because the embedded server is shared by every test in the process.
func agentux025CreateChatRole(t *testing.T, db *pgtest.DB) string {
	t.Helper()
	role := "agentux025_chat_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	schema := `"` + strings.ReplaceAll(db.Schema, `"`, `""`) + `"`
	db.Exec(t, `CREATE ROLE `+role+` NOLOGIN`)
	db.Exec(t, `GRANT USAGE ON SCHEMA `+schema+` TO `+role)
	db.Exec(t, `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA `+schema+` TO `+role)
	db.Exec(t, `GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA `+schema+` TO `+role)
	db.Exec(t, `GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA `+schema+` TO `+role)
	return role
}

// agentux025SeedConversations creates the public channel with a second member
// and the administrator's direct conversation with the agent, with the
// persona channel policies the audience floor reads from chat.
func agentux025SeedConversations(t *testing.T, h *agentux025Harness) {
	t.Helper()
	asker := chat.Principal{TenantID: agentux025Tenant, SubjectID: agentux025Admin}
	if _, err := h.service.CreateConversation(h.adminCtx, chat.CreateConversationRequest{Principal: asker, TenantID: agentux025Tenant, ConversationID: agentux025Channel, Kind: chat.PublicChannel, Name: "general"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.AddMembership(h.adminCtx, chat.AddMembershipRequest{Principal: asker, Membership: chat.Membership{TenantID: agentux025Tenant, HomeTenantID: agentux025Tenant, ConversationID: agentux025Channel, SubjectID: agentux025Member, HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.CreateConversation(h.adminCtx, chat.CreateConversationRequest{Principal: asker, TenantID: agentux025Tenant, ConversationID: agentux025DM, Kind: chat.Direct, Name: "Policy Helper"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.AddMembership(h.adminCtx, chat.AddMembershipRequest{Principal: asker, Membership: chat.Membership{TenantID: agentux025Tenant, HomeTenantID: agentux025Tenant, ConversationID: agentux025DM, SubjectID: agentux025AgentHandle, HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.chat.PutPersonaChannelPolicy(h.adminCtx, agentux025Tenant, agentux025Channel, 0, chatstore.PersonaChannelPolicy{PlacementClass: "ANY_INTERNAL", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []string{"PUBLIC"}, ConversationSearchAllowed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.chat.PutPublicAudiencePolicy(h.adminCtx, agentux025Tenant, agentux025Channel, 0, chatstore.PublicAudiencePolicy{Classification: "INTERNAL", Principals: []chatstore.PublicAudiencePrincipal{{HomeTenantID: agentux025Tenant, SubjectID: agentux025Admin}, {HomeTenantID: agentux025Tenant, SubjectID: agentux025Member}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.chat.PutPersonaChannelPolicy(h.adminCtx, agentux025Tenant, agentux025DM, 0, chatstore.PersonaChannelPolicy{PlacementClass: "PRIVATE", MaxTier: "T0", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []string{"PRIVATE"}}); err != nil {
		t.Fatal(err)
	}
}

// postMention posts a typed agent mention as the administrator and records
// the question text the output authority grounds the answer in.
func (h *agentux025Harness) postMention(conversationID, body, idempotencyKey string) chat.Post {
	h.t.Helper()
	post, err := h.invocation.SendPost(h.adminCtx, chat.SendPostRequest{Principal: chat.Principal{TenantID: agentux025Tenant, SubjectID: agentux025Admin}, TenantID: agentux025Tenant, ConversationID: conversationID, Body: body, IdempotencyKey: idempotencyKey, References: []chat.Reference{{Kind: chat.AgentMention, TenantID: agentux025Tenant, ID: agentux025AgentHandle, Display: "Policy Helper"}}})
	if err != nil {
		h.t.Fatal(err)
	}
	h.questions.set(post.ID, body)
	// The harness is the trusted classifier for its fixed questions: exact
	// current bytes, INTERNAL, through the production authority API.
	sum := sha256.Sum256([]byte(body))
	if err := h.chat.PutPublicChatPostClassification(h.adminCtx, agentux025Tenant, conversationID, post.ID, "sha256:"+hex.EncodeToString(sum[:]), trustdlp.ClassInternal); err != nil {
		h.t.Fatal(err)
	}
	return post
}

// waitForReply polls the conversation until the run's thread reply lands.
func (h *agentux025Harness) waitForReply(conversationID, parentID string) chat.Post {
	h.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		listed, err := h.service.ListPosts(h.adminCtx, chat.ListPostsRequest{Principal: chat.Principal{TenantID: agentux025Tenant, SubjectID: agentux025Admin}, TenantID: agentux025Tenant, ConversationID: conversationID, Page: chat.Page{PageSize: 20}})
		if err != nil {
			h.t.Fatal(err)
		}
		for _, post := range listed.Posts {
			if post.ParentID == parentID {
				return post
			}
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("no thread reply to %s in %s after 90s (failures: %v)", parentID, conversationID, h.failures.list())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitForDMCopy polls the direct conversation until the private path's durable
// copy lands: a top-level agent post carrying the answer and the link to the
// question it answers.
func (h *agentux025Harness) waitForDMCopy(questionID string) chat.Post {
	h.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		listed, err := h.service.ListPosts(h.adminCtx, chat.ListPostsRequest{Principal: chat.Principal{TenantID: agentux025Tenant, SubjectID: agentux025Admin}, TenantID: agentux025Tenant, ConversationID: agentux025DM, Page: chat.Page{PageSize: 20}})
		if err != nil {
			h.t.Fatal(err)
		}
		for _, post := range listed.Posts {
			if post.AuthorID == agentux025AgentHandle && strings.Contains(post.Body, agentux025Answer) {
				return post
			}
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("no durable private copy for %s after 90s (failures: %v)", questionID, h.failures.list())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// admissionIDFor recomputes the durable admission ID for an invocation: the
// run's stable ID is the admission ID, and the admission ID is derived from
// the invocation's source identity (agentrun.requestID).
func agentux025AdmissionIDFor(invocationID string) string {
	key := struct {
		Tenant string              `json:"tenant"`
		Kind   agentrun.SourceKind `json:"kind"`
		Key    string              `json:"key"`
	}{agentux025Tenant, agentrun.SourcePersonaMention, invocationID}
	data, _ := json.Marshal(key)
	sum := sha256.Sum256(data)
	return "arreq_" + hex.EncodeToString(sum[:])
}

func (h *agentux025Harness) runFor(invocationID string) runstate.Run {
	h.t.Helper()
	run, err := h.execution.Get(context.Background(), agentux025AdmissionIDFor(invocationID))
	if err != nil {
		h.t.Fatalf("run for invocation %s: %v", invocationID, err)
	}
	return run
}
