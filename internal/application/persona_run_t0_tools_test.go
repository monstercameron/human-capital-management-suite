package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportdocument "github.com/monstercameron/human-capital-management-suite/internal/transport/document"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaRunT0ToolPolicyFake struct {
	binding PersonaT0SkillPin
	err     error
	calls   int
	seen    PersonaRunT0ToolInvocation
}

func (f *personaRunT0ToolPolicyFake) ResolveAndAuthorize(_ context.Context, _ agentrun.Record, _ runstate.Run, invocation PersonaRunT0ToolInvocation, skill string) (PersonaT0SkillPin, error) {
	f.calls++
	f.seen = invocation
	if skill != personaPolicyHelperSkillID {
		return PersonaT0SkillPin{}, errPersonaRunT0Tool
	}
	return f.binding, f.err
}

type personaRunT0ToolSearchFake struct {
	result  transportdocument.SearchResult
	err     error
	calls   int
	args    []string
	request capability.InvokeRequest
}

func (f *personaRunT0ToolSearchFake) Invoke(_ context.Context, request capability.InvokeRequest) (capability.InvokeResult, error) {
	f.calls++
	f.request = request
	call, ok := request.Payload.(personaDocumentSearchCall)
	if !ok {
		return capability.InvokeResult{}, errPersonaRunT0Tool
	}
	f.args = []string{call.TenantID.String(), call.ConversationID, call.AgentID, call.InvokerID, call.Query, call.Filters.TeamID, call.Filters.ChannelID}
	if f.err != nil {
		return capability.InvokeResult{}, f.err
	}
	return capability.InvokeResult{Response: f.result, EvidenceID: "evidence-1"}, nil
}

type personaRunT0ToolJournalFake struct {
	ref, digest string
	bytes       []byte
	calls       int
}

func (f *personaRunT0ToolJournalFake) PersistPersonaRunT0ToolResult(_ context.Context, _ PersonaRunT0ToolInvocation, effectID, callID string, value []byte) (string, string, error) {
	f.calls++
	if effectID == "" || callID == "" {
		return "", "", errPersonaRunT0Tool
	}
	f.bytes = append([]byte(nil), value...)
	return f.ref, f.digest, nil
}

func personaRunT0ToolFixture(t *testing.T) (*PersonaRunT0ToolExecutor, *personaRunT0ToolPolicyFake, *personaRunT0ToolSearchFake, *personaRunT0ToolJournalFake, agentrun.Record, runstate.Run) {
	t.Helper()
	invocation := PersonaT0Invocation{TenantID: "tenant-a", PersonaID: "policy-helper", PersonaVersion: "3", InstallationID: "install-a", InvocationID: "mention-9"}
	encoded, err := json.Marshal(transportdocument.SearchResult{Hits: []transportdocument.SearchHit{{DocumentID: "doc-1", VersionID: "version-4", Title: "Leave policy"}}, Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	journal := &personaRunT0ToolJournalFake{ref: "tool-result-1", digest: personaRunT0ToolOutputDigest(encoded)}
	binding := PersonaT0SkillPin{Invocation: invocation, Pin: agentskills.SkillPin{ID: personaPolicyHelperSkillID, Version: 1, Digest: "pin-digest"}, Scopes: []string{"documents:search"}}
	policy := &personaRunT0ToolPolicyFake{binding: binding}
	skillRecord := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: binding.Pin.ID, Version: binding.Pin.Version, SideEffectTier: agentskills.TierT0}, Digest: binding.Pin.Digest, Status: agentskills.StatusActive, HighestCapabilityTier: agentskills.TierT0, ResolvedOperations: []agentskills.ResolvedOperation{{Reference: agentskills.OperationRef{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion}}, HasCapability: true, Capability: capability.Record{Definition: capability.Definition{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion, AuthZScopeRef: "documents:search", EffectClass: capability.EffectReadOnly}, Status: capability.StatusActive}}}}
	t0, err := NewPersonaT0SkillPolicy(personaT0CatalogFake{record: skillRecord}, &personaT0RevocationFake{}, []PersonaT0SkillPin{binding})
	if err != nil {
		t.Fatal(err)
	}
	search := &personaRunT0ToolSearchFake{result: transportdocument.SearchResult{Hits: []transportdocument.SearchHit{{DocumentID: "doc-1", VersionID: "version-4", Title: "Leave policy"}}, Total: 1}}
	executor, err := NewPersonaRunT0ToolExecutor(policy, t0, search, journal)
	if err != nil {
		t.Fatal(err)
	}
	agent := agentrun.VersionRef{AgentID: "persona-worker", Version: "5", Digest: "agent-digest"}
	principal := agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "workload-1", InvokerID: "alice", DelegatedCredentialRef: "grant-1"}
	audience := agentrun.AudienceScope{ID: "conversation-a"}
	contextScope := agentrun.ContextScope{ID: "thread-a", SnapshotID: "snapshot-a", Digest: "context-digest"}
	record := agentrun.Record{ID: "run-a", Decision: agentrun.DecisionAccepted, RequestDigest: "request-digest", Request: agentrun.Request{
		Source:  agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourcePersonaMention, Key: "mention-9", Ref: "post-9"},
		Persona: &agentrun.PersonaRef{ID: "policy-helper", Version: "3", Digest: "persona-digest"}, Agent: agent,
		InstallationID: "install-a", Principal: principal, Audience: audience, Context: contextScope,
	}, Authority: agentrun.AuthoritySnapshot{Agent: agent, InstallationID: "install-a", Principal: principal, Audience: audience, Context: contextScope, GrantRef: "grant-1", PolicyDigest: "policy-digest"}}
	run := runstate.Run{ID: "run-a", AdmissionID: "run-a", RequestDigest: "request-digest", TenantID: "tenant-a", AgentDigest: agent.Digest}
	return executor, policy, search, journal, record, run
}

func TestPersonaRunT0ToolExecutor_UsesDurableIdentityAndPersistsResult(t *testing.T) {
	executor, policy, search, journal, admission, run := personaRunT0ToolFixture(t)
	proposal := agentmodel.ToolProposal{ID: "call-1", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave","team_id":"team-hr"}`)}
	got, ref, digest, err := executor.Execute(personaRunT0ToolHumanContext(t), admission, run, proposal)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(got) != string(journal.bytes) || ref != journal.ref || digest != journal.digest || search.calls != 1 || journal.calls != 1 || policy.calls != 1 {
		t.Fatalf("output=%s ref=%q digest=%q calls policy/search/journal=%d/%d/%d", got, ref, digest, policy.calls, search.calls, journal.calls)
	}
	if search.request.Capability != (capability.Key{ID: personaDocumentSearchCapabilityID, Version: personaDocumentSearchCapabilityVersion}) || search.request.Authorization.Decision != capability.Allow || search.request.Authorization.Tenant != "tenant-a" || search.request.Authorization.SubjectRef != "alice" || len(search.request.Authorization.Scopes) != 1 || search.request.Authorization.Scopes[0] != "documents:search" {
		t.Fatalf("gateway request was not bound to the current invocation grant: %+v", search.request)
	}
	// The team and channel filters a model supplies are guesses (it cannot
	// know those identifiers), so they are accepted and ignored: the search
	// scope is the run's own conversation.
	want := []string{"tenant-a", "conversation-a", "persona-worker", "alice", "leave", "", ""}
	for i := range want {
		if search.args[i] != want[i] {
			t.Fatalf("search arg[%d]=%q, want %q", i, search.args[i], want[i])
		}
	}
	if policy.seen.InvocationID != "mention-9" || policy.seen.ThreadID != "thread-a" || policy.seen.PersonaVersion != "3" {
		t.Fatalf("policy saw non-durable invocation: %+v", policy.seen)
	}
}

func TestPersonaRunT0ToolExecutor_FailsClosedOnForgedProposalOrAdmission(t *testing.T) {
	for _, tc := range []struct {
		name     string
		proposal agentmodel.ToolProposal
		mutate   func(*agentrun.Record)
	}{
		{name: "unknown tool", proposal: agentmodel.ToolProposal{ID: "c", Name: "people.read", Arguments: json.RawMessage(`{"query":"leave"}`)}},
		{name: "unknown argument", proposal: agentmodel.ToolProposal{ID: "c", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave","tenant_id":"tenant-b"}`)}},
		{name: "empty query", proposal: agentmodel.ToolProposal{ID: "c", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":" "}`)}},
		{name: "foreign durable tenant", proposal: agentmodel.ToolProposal{ID: "c", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}, mutate: func(r *agentrun.Record) { r.Request.Source.TenantID = "tenant-b" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor, policy, search, journal, admission, run := personaRunT0ToolFixture(t)
			if tc.mutate != nil {
				tc.mutate(&admission)
			}
			if _, _, _, err := executor.Execute(personaRunT0ToolHumanContext(t), admission, run, tc.proposal); !errors.Is(err, errPersonaRunT0Tool) {
				t.Fatalf("Execute() error=%v, want tool refusal", err)
			}
			if search.calls != 0 || journal.calls != 0 {
				t.Fatalf("denied proposal reached effect ports search=%d journal=%d", search.calls, journal.calls)
			}
			if tc.name == "unknown tool" && policy.calls != 0 {
				t.Fatalf("unknown tool consulted policy %d times", policy.calls)
			}
		})
	}
}

func TestPersonaRunT0ToolExecutor_OnlyProjectsWhenCurrentGrantResolves(t *testing.T) {
	executor, policy, _, _, admission, run := personaRunT0ToolFixture(t)
	policy.err = errors.New("revoked grant")
	if _, err := executor.ToolSchemas(personaRunT0ToolHumanContext(t), admission, run); !errors.Is(err, errPersonaRunT0Tool) {
		t.Fatalf("ToolSchemas() error=%v, want fail-closed refusal", err)
	}
	if policy.calls != 1 {
		t.Fatalf("current grant checks=%d, want one", policy.calls)
	}
}

func TestPersonaRunT0ToolExecutor_ProjectsOnlyExactRegisteredPolicySearchPin(t *testing.T) {
	executor, _, _, _, admission, run := personaRunT0ToolFixture(t)
	schemas, err := executor.ToolSchemas(personaRunT0ToolHumanContext(t), admission, run)
	if err != nil || len(schemas) != 1 || schemas[0].Name != personaDocumentSearchTool {
		t.Fatalf("ToolSchemas()=%+v err=%v, want one exact document-search projection", schemas, err)
	}
	record := executor.t0.catalog.(personaT0CatalogFake).record
	record.ResolvedOperations = append(record.ResolvedOperations, agentskills.ResolvedOperation{Reference: agentskills.OperationRef{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: "unrelated", Version: 1}}, HasCapability: true})
	executor.t0.catalog = personaT0CatalogFake{record: record}
	if schemas, err = executor.ToolSchemas(personaRunT0ToolHumanContext(t), admission, run); !errors.Is(err, errPersonaRunT0Tool) || len(schemas) != 0 {
		t.Fatalf("ToolSchemas() exposed a pin with an extra operation: schemas=%+v err=%v", schemas, err)
	}
}

func personaRunT0ToolHumanContext(t *testing.T) context.Context {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-a", Purposes: []string{"agent.inference"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0), CredentialDigest: "credential-digest"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}
