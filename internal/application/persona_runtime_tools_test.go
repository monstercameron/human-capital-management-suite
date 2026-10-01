package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type runtimeToolAdmission struct {
	snapshot agentrun.AuthoritySnapshot
	err      error
}

type runtimeToolDocuments struct{}

func (runtimeToolDocuments) PersonaRuntimeDocumentClass(context.Context, PersonaRunT0ToolInvocation, string, string) (trustdlp.DataClass, error) {
	return trustdlp.ClassInternal, nil
}

type runtimeToolDocumentClasses map[string]trustdlp.DataClass

func (c runtimeToolDocumentClasses) PersonaRuntimeDocumentClass(_ context.Context, _ PersonaRunT0ToolInvocation, id, _ string) (trustdlp.DataClass, error) {
	return c[id], nil
}

func (a *runtimeToolAdmission) VerifyAdmission(context.Context, agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	return a.snapshot, a.err
}

type runtimeToolJournal struct {
	rows []agentpersonastore.ToolResultRecord
	err  error
}

func (j *runtimeToolJournal) PutPersonaRuntimeToolResult(_ context.Context, r agentpersonastore.ToolResultRecord) error {
	if j.err != nil {
		return j.err
	}
	j.rows = append(j.rows, r)
	return nil
}
func (j *runtimeToolJournal) ListPersonaRuntimeToolResults(_ context.Context, tenant values.TenantId, runID string) ([]agentpersonastore.ToolResultRecord, error) {
	if j.err != nil {
		return nil, j.err
	}
	return j.rows, nil
}

func runtimeToolFixture(t *testing.T) (context.Context, *PersonaRuntimeTools, *dynamicT0GrantStore, *dynamicT0PersonaStore, *personaRunT0ToolSearchFake, *runtimeToolJournal, agentrun.Record, runstate.Run) {
	t.Helper()
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	request, profile, skill, grant := dynamicT0Fixture(t, at)
	pin := profile.SkillPins[0]
	pin.ID = personaPolicyHelperSkillID
	profile.SkillPins = []agentskills.SkillPin{pin}
	request.Skills = agentinvoke.SkillScopes{pin.ID: {personaPolicySearchScope}}
	request.Grant.Skills = request.Skills.Clone()
	skill.Definition.ID = pin.ID
	skill.ResolvedOperations = []agentskills.ResolvedOperation{{Reference: agentskills.OperationRef{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: personaDocumentSearchCapabilityID, Version: 1}}, HasCapability: true, Capability: capability.Record{Definition: personaPolicySearchCapabilityDefinition(), Status: capability.StatusActive}}}
	grant.Skills = []string{pin.ID}
	grant.SkillScopes = request.Skills.Clone()
	grant.PlanSkillSetDigest = skillDigest(grant.SkillScopes)
	grant.SkillAuthorities = trust.SkillAuthorities{pin.ID: {Capabilities: []string{personaPolicySearchScope}, Resources: []string{"document:*"}, Fields: []string{"document.title"}, Purposes: []string{"persona-mention"}}}
	grant.Authority.Capabilities = []string{personaPolicySearchScope}
	grant.Authority.Resources = []string{"document:*"}
	grant.Authority.Fields = []string{"document.title"}
	grant.Authority.SkillAuthorities = trust.CloneSkillAuthorities(grant.SkillAuthorities)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	store := &dynamicT0GrantStore{grant: grant, epoch: grant.RevocationEpoch}
	personas := &dynamicT0PersonaStore{reader: dynamicT0PersonaReader{version: agentpersonastore.PersonaVersion{TenantID: values.TenantId(request.TenantID), PersonaID: request.PersonaID, Version: 2, AgentVersion: "agent-a@1", ContentDigest: sealed.Digest, Profile: profileJSON}, install: agentpersonastore.ActiveInstallation{InstallationID: request.InstallationID, PersonaID: request.PersonaID, PersonaVersion: 2, ConversationID: request.ConversationID}}}
	policy, err := NewDatabasePersonaT0SkillPolicy(PersonaT0DynamicPolicyConfig{Personas: personas, Authority: &dynamicT0Authority{admission: agentinvoke.Admission{Persona: agentinvoke.Persona{ID: request.PersonaID, Version: request.PersonaVersion, Current: true, InstallationID: request.InstallationID}, Installation: agentinvoke.Installation{ID: request.InstallationID, Current: true}, HumanMember: true, AudienceMember: true, PersonaInstalled: true, Discoverable: request.Skills.Clone()}}, InvokerAuthority: dynamicT0InvokerAuthority{value: agentdelegation.UserAuthority{UserID: request.InvokerID, Active: true, Authority: trust.AuthorityScope{Tenant: values.TenantId(request.TenantID), OrganizationScopeID: "org-a", Assurance: trust.AssuranceHigh, NotBefore: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour)}, SkillAuthorities: trust.CloneSkillAuthorities(grant.SkillAuthorities)}}, GrantStores: dynamicT0GrantFactory{store: store}, Facts: dynamicT0Facts{value: PersonaRunRequestFacts{TenantID: request.TenantID, TriggerID: request.InvocationID, PersonaDigest: sealed.Digest, Agent: agentrunVersionRef("agent-a")}}, Catalog: dynamicT0Catalog{record: skill}, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	_, _, search, _, record, run := personaRunT0ToolFixture(t)
	record.Request.Source.TenantID = request.TenantID
	record.Request.Source.Key = request.InvocationID
	record.Request.Source.Ref = request.InvokingPostID
	record.Request.Persona = &agentrun.PersonaRef{ID: request.PersonaID, Version: request.PersonaVersion, Digest: sealed.Digest}
	record.Request.InstallationID = request.InstallationID
	record.Request.Agent = agentrunVersionRef("agent-a")
	record.Request.Principal.InvokerID = request.InvokerID
	record.Request.Principal.DelegatedCredentialRef = grant.GrantID
	record.Request.Audience.ID = request.ConversationID
	record.Request.Context.ID = request.ThreadID
	record.Authority.Principal = record.Request.Principal
	record.Authority.Audience = record.Request.Audience
	record.Authority.Context = record.Request.Context
	record.Authority.Agent = record.Request.Agent
	record.Authority.GrantRef = grant.GrantID
	run.AgentDigest = record.Request.Agent.Digest
	record.RequestDigest = pin.Digest
	run.RequestDigest = pin.Digest
	journal := &runtimeToolJournal{}
	executor, err := NewPersonaRuntimeTools(PersonaRuntimeToolsConfig{Authority: &runtimeToolAdmission{snapshot: record.Authority}, Policy: policy, Gateway: search, Journal: journal, Documents: runtimeToolDocuments{}, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), foregroundPrincipal(t, at.Add(-time.Minute), at.Add(time.Hour), "persona-mention", request.InvokerID, request.TenantID))
	return ctx, executor, store, personas, search, journal, record, run
}

func TestTodo_AGENTP_008_RuntimeToolsRecheckCurrentPinGrantAndTenant(t *testing.T) {
	ctx, tools, grant, personas, search, journal, record, run := runtimeToolFixture(t)
	if schemas, err := tools.ToolSchemas(ctx, record, run); err != nil || len(schemas) != 1 || schemas[0].Name != personaDocumentSearchTool {
		t.Fatalf("schemas=%+v err=%v", schemas, err)
	}
	proposal := agentmodel.ToolProposal{ID: "call-current", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}
	output, ref, digest, err := tools.Execute(ctx, record, run, proposal)
	if err != nil || ref == "" || digest != personaRunT0ToolOutputDigest(output) || len(journal.rows) != 1 || search.calls != 1 {
		t.Fatalf("execute output=%s ref=%s digest=%s rows=%d calls=%d err=%v", output, ref, digest, len(journal.rows), search.calls, err)
	}
	grant.epoch++
	if _, _, _, err := tools.Execute(ctx, record, run, proposal); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("revoked grant allowed: %v", err)
	}
	grant.epoch--
	personas.reader.install.PersonaVersion++
	if _, _, _, err := tools.Execute(ctx, record, run, proposal); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("changed installed persona version allowed: %v", err)
	}
	personas.reader.install.PersonaVersion--
	run.TenantID = "tenant-b"
	if _, _, _, err := tools.Execute(ctx, record, run, proposal); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("cross tenant run allowed: %v", err)
	}
	if search.calls != 1 || len(journal.rows) != 1 {
		t.Fatal("denial executed or persisted another result")
	}
}

func TestTodo_AGENTP_011_RuntimeToolGroundingRechecksDocumentVersionAndJournalIdentity(t *testing.T) {
	ctx, tools, _, _, search, journal, record, run := runtimeToolFixture(t)
	proposal := agentmodel.ToolProposal{ID: "call-grounding", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}
	if _, _, _, err := tools.Execute(ctx, record, run, proposal); err != nil {
		t.Fatal(err)
	}
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		t.Fatal(err)
	}
	grounding, err := tools.ReadPersonaRunToolGrounding(ctx, record, run, gateway)
	if err != nil || len(grounding) != 1 || search.calls != 2 {
		t.Fatalf("grounding=%+v calls=%d err=%v", grounding, search.calls, err)
	}
	answer, err := gateway.BuildAnswer(grounding)
	if err != nil || len(answer.Parts) != 1 || answer.Parts[0].Trust != agentsecurity.TrustDocument || answer.Parts[0].Citations[0].SourceID != "document:doc-1/version:version-4" {
		t.Fatalf("answer=%+v err=%v", answer, err)
	}
	search.result.Hits[0].VersionID = "version-5"
	if _, err := tools.ReadPersonaRunToolGrounding(ctx, record, run, gateway); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("changed deployment used as old evidence: %v", err)
	}
	search.result.Hits[0].VersionID = "version-4"
	journal.rows[0].InvokerID = "other-user"
	calls := search.calls
	if _, err := tools.ReadPersonaRunToolGrounding(ctx, record, run, gateway); !errors.Is(err, errPersonaRuntimeTools) || search.calls != calls {
		t.Fatalf("cross-invoker row reached search: %v", err)
	}
}

func TestTodo_AGENTP_008_RuntimeToolsRejectProviderIdentityAndUnavailableJournal(t *testing.T) {
	ctx, tools, _, _, search, journal, record, run := runtimeToolFixture(t)
	proposal := agentmodel.ToolProposal{ID: "call-invalid", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave","tenant_id":"tenant-b"}`)}
	if _, _, _, err := tools.Execute(ctx, record, run, proposal); err == nil || search.calls != 0 {
		t.Fatal("provider identity override reached capability")
	}
	proposal.Arguments = json.RawMessage(`{"query":"leave"}`)
	journal.err = errors.New("journal unavailable")
	if _, _, _, err := tools.Execute(ctx, record, run, proposal); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("unpersisted result returned: %v", err)
	}
	if _, err := NewPersonaRuntimeTools(PersonaRuntimeToolsConfig{}); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("missing ports accepted: %v", err)
	}
	if _, err := (DatabasePersonaRuntimeToolJournal{}).ListPersonaRuntimeToolResults(ctx, "tenant-a", "run-a"); err == nil {
		t.Fatal("nil journal store accepted")
	}
	if err := (DatabasePersonaRuntimeToolJournal{}).PutPersonaRuntimeToolResult(ctx, agentpersonastore.ToolResultRecord{}); err == nil {
		t.Fatal("nil persistence store accepted")
	}
}

func TestTodo_AGENTP_008_RuntimeToolsProjectNoUnsupportedSkills(t *testing.T) {
	ctx, tools, grant, personas, search, journal, record, run := runtimeToolFixture(t)
	const unsupported = "skill.actual-unmapped-read"
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(personas.reader.version.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	old := profile.SkillPins[0].ID
	profile.SkillPins[0].ID = unsupported
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	personas.reader.version.Profile, err = json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	personas.reader.version.ContentDigest = sealed.Digest
	record.Request.Persona.Digest = sealed.Digest
	grant.grant.SkillScopes[unsupported] = grant.grant.SkillScopes[old]
	delete(grant.grant.SkillScopes, old)
	grant.grant.Skills = []string{unsupported}
	grant.grant.PlanSkillSetDigest = skillDigest(grant.grant.SkillScopes)
	grant.grant.SkillAuthorities[unsupported] = grant.grant.SkillAuthorities[old]
	delete(grant.grant.SkillAuthorities, old)
	grant.grant.Authority.SkillAuthorities = trust.CloneSkillAuthorities(grant.grant.SkillAuthorities)
	current := tools.cfg.Policy.authority.(*dynamicT0Authority)
	current.admission.Discoverable = agentinvoke.SkillScopes(grant.grant.SkillScopes).Clone()
	user := tools.cfg.Policy.invokerAuthority.(dynamicT0InvokerAuthority)
	user.value.SkillAuthorities = trust.CloneSkillAuthorities(grant.grant.SkillAuthorities)
	tools.cfg.Policy.invokerAuthority = user
	facts := tools.cfg.Policy.facts.(dynamicT0Facts)
	facts.value.PersonaDigest = sealed.Digest
	tools.cfg.Policy.facts = facts
	catalog := tools.cfg.Policy.catalog.(dynamicT0Catalog)
	catalog.record.Definition.ID = unsupported
	tools.cfg.Policy.catalog = catalog
	schemas, err := tools.ToolSchemas(ctx, record, run)
	if err != nil || len(schemas) != 0 {
		t.Fatalf("unsupported skills projected as tools: %+v %v", schemas, err)
	}
	if _, _, _, err := tools.Execute(ctx, record, run, agentmodel.ToolProposal{ID: "call-unavailable", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("unprojected tool invoked: %v", err)
	}
	if search.calls != 0 || len(journal.rows) != 0 {
		t.Fatal("unsupported skill caused capability effects")
	}
}

func TestTodo_AGENTP_011_RuntimeToolsUseOwnerClassificationAndRejectMixedClasses(t *testing.T) {
	ctx, tools, _, _, search, journal, record, run := runtimeToolFixture(t)
	tools.cfg.Documents = runtimeToolDocumentClasses{"doc-1": trustdlp.ClassPublic, "doc-2": trustdlp.ClassInternal}
	proposal := agentmodel.ToolProposal{ID: "call-class", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}
	if _, _, _, err := tools.Execute(ctx, record, run, proposal); err != nil {
		t.Fatal(err)
	}
	if journal.rows[0].DataClass != trustdlp.ClassPublic {
		t.Fatalf("source class replaced by route class: %s", journal.rows[0].DataClass)
	}
	second := search.result.Hits[0]
	second.DocumentID = "doc-2"
	search.result.Hits = append(search.result.Hits, second)
	search.result.Total = 2
	proposal.ID = "call-mixed"
	if _, _, _, err := tools.Execute(ctx, record, run, proposal); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("mixed source classes were silently downgraded: %v", err)
	}
	if len(journal.rows) != 1 {
		t.Fatal("unclassified output reached durable journal")
	}
}

func TestTodo_AGENTP_011_RuntimeToolJournalAdapterIntegration(t *testing.T) {
	ctx, tools, _, _, _, memory, record, run := runtimeToolFixture(t)
	record.RequestDigest = strings.TrimPrefix(record.RequestDigest, "sha256:")
	run.RequestDigest = record.RequestDigest
	if _, _, _, err := tools.Execute(ctx, record, run, agentmodel.ToolProposal{ID: "call-store", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}); err != nil {
		t.Fatal(err)
	}
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES ($1)`, tenantID)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	store, err := agentpersonastore.New(conn, func(tenant values.TenantId) uuid.UUID {
		if tenant == "tenant-a" {
			return tenantID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	journal := DatabasePersonaRuntimeToolJournal{Store: store}
	if err := journal.PutPersonaRuntimeToolResult(ctx, memory.rows[0]); err != nil {
		t.Fatal(err)
	}
	if memory.rows[0].AdmissionDigest != "sha256:"+record.RequestDigest {
		t.Fatal("actual bare admission digest was not journal-normalized")
	}
	rows, err := journal.ListPersonaRuntimeToolResults(ctx, "tenant-a", run.ID)
	if err != nil || len(rows) != 1 || rows[0].OutputDigest != memory.rows[0].OutputDigest || string(rows[0].Output) != string(memory.rows[0].Output) {
		t.Fatalf("journal rows=%+v err=%v", rows, err)
	}
	if _, err := journal.ListPersonaRuntimeToolResults(ctx, "unknown-tenant", run.ID); err == nil {
		t.Fatal("unknown tenant journal read accepted")
	}
}

func TestTodo_AGENTP_011_RuntimeToolEgressRechecksSourceCurrency(t *testing.T) {
	ctx, tools, _, _, search, journal, record, run := runtimeToolFixture(t)
	if _, _, _, err := tools.Execute(ctx, record, run, agentmodel.ToolProposal{ID: "call-egress", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}); err != nil {
		t.Fatal(err)
	}
	r := journal.rows[0]
	if err := tools.RecheckPersonaRuntimeToolResult(ctx, r); err != nil {
		t.Fatalf("current exact result rejected: %v", err)
	}
	search.err = errors.New("document grant revoked")
	if err := tools.RecheckPersonaRuntimeToolResult(ctx, r); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("revoked document source sent to provider: %v", err)
	}
	search.err = nil
	search.result.Hits[0].VersionID = "next-version"
	if err := tools.RecheckPersonaRuntimeToolResult(ctx, r); !errors.Is(err, errPersonaRuntimeTools) {
		t.Fatalf("changed source sent under stale proof: %v", err)
	}
}
