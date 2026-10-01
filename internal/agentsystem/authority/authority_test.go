package authority

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type resolverFunc func(context.Context, RunContext) (CurrentGrants, error)

func (f resolverFunc) ResolveCurrent(ctx context.Context, run RunContext) (CurrentGrants, error) {
	return f(ctx, run)
}

type credentialVerifierFunc func(context.Context, agentdelegation.DelegatedCredential, CredentialRequest) error

func (f credentialVerifierFunc) VerifyDelegated(ctx context.Context, c agentdelegation.DelegatedCredential, req CredentialRequest) error {
	return f(ctx, c, req)
}

func fixture() (RunContext, CurrentGrants) {
	run := RunContext{RunID: "run-9", TenantID: values.TenantId("tenant-a"), EntityID: "entity-west", AgentID: "agent-2", AgentPrincipalID: "principal-agent-2", AgentVersion: "v3", InstallationID: "install-4", Purpose: "case.read", Audience: "private-inbox", Mode: ModeOnBehalfOf, InvokerID: "user-1", DelegationGrantID: "grant-7"}
	base := Scope{GrantRef: "grant-ref-1", TenantID: run.TenantID, EntityIDs: []string{"entity-west"}, Purposes: []string{"case.read"}, Audiences: []string{"private-inbox", "channel-7"}, Skills: []string{"case.lookup", "case.draft"}, Capabilities: []string{"case.read", "case.write"}, Sources: []string{"case:42", "case:43"}}
	installation := cloneScope(base)
	installation.GrantRef = "install-grant-1"
	contextGrant := cloneScope(base)
	contextGrant.GrantRef = "context-grant-1"
	invoker := cloneScope(base)
	invoker.GrantRef = "user-grant-1"
	delegation := cloneScope(base)
	delegation.GrantRef = "grant-7"
	current := CurrentGrants{ResolvedMode: run.Mode, AgentPrincipalID: run.AgentPrincipalID, AgentActive: true, InstallationActive: true, InvokerActive: true,
		Agent: base, Installation: installation, Context: contextGrant, Invoker: invoker,
		Delegation: DelegationGrant{GrantID: run.DelegationGrantID, SubjectID: run.InvokerID, TenantID: run.TenantID, AgentVersion: run.AgentVersion, InstallationID: run.InstallationID, RunID: run.RunID, Purpose: run.Purpose, Active: true, Scope: delegation}}
	return run, current
}

func cloneScope(in Scope) Scope {
	in.EntityIDs = append([]string(nil), in.EntityIDs...)
	in.Purposes = append([]string(nil), in.Purposes...)
	in.Audiences = append([]string(nil), in.Audiences...)
	in.Skills = append([]string(nil), in.Skills...)
	in.Capabilities = append([]string(nil), in.Capabilities...)
	in.Sources = append([]string(nil), in.Sources...)
	return in
}

func testVerifier(t *testing.T, current *CurrentGrants, verify credentialVerifierFunc, resolutions *int) *Verifier {
	t.Helper()
	v, err := NewVerifier(resolverFunc(func(_ context.Context, _ RunContext) (CurrentGrants, error) { *resolutions++; return *current, nil }), verify)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTodo_AGENT_011(t *testing.T) {
	run, current := fixture()
	count := 0
	v := testVerifier(t, &current, func(_ context.Context, credential agentdelegation.DelegatedCredential, req CredentialRequest) error {
		if credential.Raw != "signed-step" || req.SubjectID != run.InvokerID || req.RunID != run.RunID || req.GrantID != run.DelegationGrantID {
			t.Fatalf("delegated binding = %+v", req)
		}
		return nil
	}, &count)
	snapshot, err := v.VerifyAdmission(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgentPrincipalID != run.AgentPrincipalID || snapshot.InvokerID != run.InvokerID || snapshot.SponsorID != "" {
		t.Fatalf("principal chain = %+v", snapshot)
	}
	if !reflect.DeepEqual(snapshot.Capabilities, []string{"case.read", "case.write"}) {
		t.Fatalf("effective capabilities = %v", snapshot.Capabilities)
	}
	decision, err := v.VerifyTool(context.Background(), run, ToolRequest{SkillID: "case.lookup", Capabilities: []string{"case.read"}, SourceIDs: []string{"case:42"}, Tier: TierRead, Audience: run.Audience, Sender: "worker-9", Credential: agentdelegation.DelegatedCredential{Raw: "signed-step"}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Snapshot.InvokerID != run.InvokerID || count != 2 {
		t.Fatalf("decision/resolutions = %+v/%d", decision, count)
	}
}

func TestTodo_AGENT_011_Security(t *testing.T) {
	run, current := fixture()
	calls, resolves := 0, 0
	v := testVerifier(t, &current, func(context.Context, agentdelegation.DelegatedCredential, CredentialRequest) error {
		calls++
		return nil
	}, &resolves)
	bad := run
	bad.Mode = ModeSponsored
	bad.InvokerID = ""
	bad.DelegationGrantID = ""
	bad.SponsorID = "sponsor-1"
	current.ResolvedMode = ModeSponsored
	current.SponsorActive = true
	current.SponsorPrincipalID = "sponsor-1"
	current.Sponsor = cloneScope(current.Agent)
	current.Sponsor.GrantRef = "sponsor-grant-1"
	if _, err := v.VerifyAdmission(context.Background(), bad); err != nil {
		t.Fatalf("sponsored admission: %v", err)
	}
	if _, err := v.VerifyTool(context.Background(), bad, ToolRequest{SkillID: "case.lookup", Capabilities: []string{"case.write"}, Tier: TierSubmitGoverned, Audience: bad.Audience}); !errors.Is(err, ErrDenied) {
		t.Fatalf("T3 sponsored error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("delegated token verifier called for SPONSORED: %d", calls)
	}
	if _, err := v.VerifyTool(context.Background(), run, ToolRequest{SkillID: "case.lookup", Tier: TierRead, Audience: run.Audience}); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing OBO credential error = %v", err)
	}
}

func TestTodo_AGENT_011_Integration(t *testing.T) {
	run, current := fixture()
	resolves := 0
	v := testVerifier(t, &current, func(_ context.Context, _ agentdelegation.DelegatedCredential, req CredentialRequest) error {
		if req.Audience != run.Audience || req.Sender != "worker-9" {
			t.Fatalf("credential boundary = %+v", req)
		}
		return nil
	}, &resolves)
	delivery := DeliveryRequest{SkillID: "case.lookup", Capabilities: []string{"case.read"}, SourceIDs: []string{"case:42"}, Tier: TierCommunicate, Audience: run.Audience, Sender: "worker-9", Credential: agentdelegation.DelegatedCredential{Raw: "signed"}}
	if _, err := v.VerifyDelivery(context.Background(), run, delivery); err != nil {
		t.Fatal(err)
	}
	current.Context.Sources = []string{"case:43"}
	if _, err := v.VerifyDelivery(context.Background(), run, delivery); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked source delivery error = %v", err)
	}
	if resolves != 2 {
		t.Fatalf("resolver calls = %d, want fresh check at each delivery", resolves)
	}
}

func TestTodo_AGENT_011_Mutation(t *testing.T) {
	run, current := fixture()
	resolves := 0
	v := testVerifier(t, &current, func(context.Context, agentdelegation.DelegatedCredential, CredentialRequest) error { return nil }, &resolves)
	current.Delegation.Active = false
	if _, err := v.VerifyAdmission(context.Background(), run); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked delegated grant error = %v", err)
	}
	current.Delegation.Active = true
	current.AgentPrincipalID = "different-principal"
	if _, err := v.VerifyAdmission(context.Background(), run); !errors.Is(err, ErrDenied) {
		t.Fatalf("principal substitution error = %v", err)
	}
	current.AgentPrincipalID = run.AgentPrincipalID
	current.ResolvedMode = ModeSponsored
	if _, err := v.VerifyAdmission(context.Background(), run); !errors.Is(err, ErrDenied) {
		t.Fatalf("trigger-mode substitution error = %v", err)
	}
}

func TestTodo_AGENT_011_Golden(t *testing.T) {
	run, current := fixture()
	resolves := 0
	v := testVerifier(t, &current, func(context.Context, agentdelegation.DelegatedCredential, CredentialRequest) error { return nil }, &resolves)
	snapshot, err := v.VerifyAdmission(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"Mode":"ON_BEHALF_OF","AgentID":"agent-2","AgentPrincipalID":"principal-agent-2","AgentVersion":"v3","InstallationID":"install-4","DelegationGrantID":"grant-7","PolicyRefs":["grant-ref-1","install-grant-1","context-grant-1","user-grant-1","grant-7"],"InvokerID":"user-1","SponsorID":"","TenantID":"tenant-a","EntityID":"entity-west","Purpose":"case.read","Audience":"private-inbox","Skills":["case.draft","case.lookup"],"Capabilities":["case.read","case.write"],"Sources":["case:42","case:43"]}`
	if string(got) != want {
		t.Fatalf("snapshot = %s\nwant     = %s", got, want)
	}
}

func TestTodo_AGENT_011_InvalidModesAndScopes(t *testing.T) {
	run, current := fixture()
	resolves := 0
	v := testVerifier(t, &current, func(context.Context, agentdelegation.DelegatedCredential, CredentialRequest) error { return nil }, &resolves)
	if _, err := v.VerifyAdmission(context.Background(), RunContext{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty run error = %v", err)
	}
	run.Mode = "MODEL_SELECTED"
	if _, err := v.VerifyAdmission(context.Background(), run); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown mode error = %v", err)
	}
	run, current = fixture()
	current.Context.Purposes = nil
	v = testVerifier(t, &current, func(context.Context, agentdelegation.DelegatedCredential, CredentialRequest) error { return nil }, &resolves)
	if _, err := v.VerifyAdmission(context.Background(), run); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing purpose error = %v", err)
	}
}

func TestTodo_AGENT_011_SponsoredIdentity(t *testing.T) {
	run, current := fixture()
	run.Mode = ModeSponsored
	run.InvokerID = ""
	run.DelegationGrantID = ""
	run.SponsorID = "schedule:monthly"
	current.SponsorActive = true
	current.ResolvedMode = ModeSponsored
	current.SponsorPrincipalID = run.SponsorID
	current.Sponsor = cloneScope(current.Agent)
	current.Sponsor.GrantRef = "sponsor-grant-1"
	resolves := 0
	v := testVerifier(t, &current, func(context.Context, agentdelegation.DelegatedCredential, CredentialRequest) error { return nil }, &resolves)
	decision, err := v.VerifyTool(context.Background(), run, ToolRequest{SkillID: "case.lookup", Capabilities: []string{"case.read"}, SourceIDs: []string{"case:42"}, Tier: TierRead, Audience: run.Audience})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Snapshot.SponsorID != run.SponsorID || decision.Snapshot.InvokerID != "" {
		t.Fatalf("sponsored identity = %+v", decision.Snapshot)
	}
}
