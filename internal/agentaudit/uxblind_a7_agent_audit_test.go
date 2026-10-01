package agentaudit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func testActor(user, task string) ActorChain {
	return ActorChain{UserID: user, AgentVersion: "agent:v3", InstallationID: "install:7", TaskID: task, PlanRevision: "plan:4", StepID: "step:2", DelegationGrantID: "grant:9"}
}

func testEntry(id, tenant, user, task string, kind EventKind) Entry {
	actor := testActor(user, task)
	return Entry{EventID: id, TenantID: tenant, Kind: kind, Actor: actor, Action: "people.lookup", ArgumentsDigest: "sha256:args", ResultDigest: "sha256:result", Fields: []Field{{Name: "subject", Value: "worker-7", Classification: ClassificationPublic}, {Name: "salary", Value: "125000", Classification: ClassificationRestricted}}, Edges: []Edge{{Kind: EdgeTask, From: task, To: id}, {Kind: EdgeIntent, From: task, To: "intent:7"}, {Kind: EdgeApproval, From: task, To: "approval:7"}, {Kind: EdgeWorkflowRun, From: task, To: "workflow:7"}, {Kind: EdgeConnectorOp, From: task, To: "connector-op:7"}}, OccurredAt: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
}

func TestTodo_AGENT2_014(t *testing.T) {
	store := NewMemoryStore()
	entry := testEntry("event:1", "tenant:a", "user:a", "task:1", EventSkillCall)
	first, err := store.Append(context.Background(), entry)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if !first.Created || first.Sequence != 1 || first.PrevHash != "" || !strings.HasPrefix(first.ChainHash, "sha256:") {
		t.Fatalf("first record = %+v, want genesis-linked created record", first)
	}
	retry, err := store.Append(context.Background(), entry)
	if err != nil {
		t.Fatalf("idempotent Append: %v", err)
	}
	if retry.Created || retry.ChainHash != first.ChainHash || retry.Sequence != first.Sequence {
		t.Fatalf("retry = %+v, want same record with Created=false", retry)
	}
	if err := store.Verify(context.Background(), "tenant:a"); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	auditor, err := store.Query(context.Background(), Query{Viewer: Viewer{TenantID: "tenant:a", UserID: "auditor", Role: ViewerAuditor}, TaskID: "task:1"})
	if err != nil {
		t.Fatalf("auditor Query: %v", err)
	}
	if len(auditor) != 1 || len(auditor[0].Edges) != 5 {
		t.Fatalf("auditor view = %+v, want one task-linked record with five graph edges", auditor)
	}
}

func TestTodo_AGENT2_014_Golden(t *testing.T) {
	entry := testEntry("event:golden", "tenant:a", "user:a", "task:golden", EventIntentOrigin)
	got := CanonicalEntry(entry)
	want := `{"EventID":"event:golden","TenantID":"tenant:a","Kind":"INTENT_ORIGIN","Actor":{"UserID":"user:a","AgentVersion":"agent:v3","InstallationID":"install:7","TaskID":"task:golden","PlanRevision":"plan:4","StepID":"step:2","SubAgentDepth":0,"DelegationGrantID":"grant:9"},"Action":"people.lookup","ArgumentsDigest":"sha256:args","ResultDigest":"sha256:result","ApprovalDigest":"","Fields":[{"Name":"salary","Value":"125000","Classification":"RESTRICTED"},{"Name":"subject","Value":"worker-7","Classification":"PUBLIC"}],"Edges":[{"Kind":"APPROVAL","From":"task:golden","To":"approval:7"},{"Kind":"CONNECTOR_OPERATION","From":"task:golden","To":"connector-op:7"},{"Kind":"INTENT","From":"task:golden","To":"intent:7"},{"Kind":"TASK","From":"task:golden","To":"event:golden"},{"Kind":"WORKFLOW_RUN","From":"task:golden","To":"workflow:7"}],"OccurredAt":"2026-09-28T12:00:00Z"}`
	if got != want {
		t.Fatalf("canonical entry changed:\n got %s\nwant %s", got, want)
	}
}

func TestTodo_AGENT2_014_Security(t *testing.T) {
	store := NewMemoryStore()
	first, err := store.Append(context.Background(), testEntry("event:a", "tenant:a", "user:a", "task:a", EventConnectorOperation))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Append(context.Background(), testEntry("event:b", "tenant:a", "user:b", "task:b", EventApproval))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), Entry{EventID: "event:a", TenantID: "tenant:a", Kind: EventConnectorOperation, Actor: testActor("user:a", "task:a"), Action: "different", Edges: []Edge{{Kind: EdgeTask, From: "task:a", To: "event:a"}, {Kind: EdgeConnectorOp, From: "task:a", To: "connector-op:7"}}, OccurredAt: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}); !errors.Is(err, ErrDuplicateConflict) {
		t.Fatalf("duplicate conflict error = %v, want ErrDuplicateConflict", err)
	}
	chain := []Record{first, second}
	if err := VerifyChain(chain); err != nil {
		t.Fatalf("VerifyChain before tamper: %v", err)
	}
	chain[1].ChainHash = "sha256:forged"
	if err := VerifyChain(chain); !errors.Is(err, ErrChainTampered) {
		t.Fatalf("VerifyChain forged error = %v, want ErrChainTampered", err)
	}
	user, err := store.Query(context.Background(), Query{Viewer: Viewer{TenantID: "tenant:a", UserID: "user:a", Role: ViewerUser}})
	if err != nil {
		t.Fatalf("user Query: %v", err)
	}
	if len(user) != 1 || user[0].Actor.UserID != "user:a" {
		t.Fatalf("user view = %+v, want only user:a", user)
	}
	if len(user[0].Fields) != 2 || user[0].Fields[1].Value != "" || !user[0].Fields[1].Redacted || user[0].Fields[1].Digest == "" {
		t.Fatalf("restricted field view = %+v, want value redacted with digest", user[0].Fields)
	}
	if _, err := store.Query(context.Background(), Query{Viewer: Viewer{TenantID: "tenant:b", UserID: "user:a", Role: ViewerAuditor}}); err != nil {
		// An empty foreign tenant is intentionally an empty result, never a
		// cross-tenant error or a disclosure of tenant:a's existence.
		t.Fatal(err)
	}
	foreign, _ := store.Query(context.Background(), Query{Viewer: Viewer{TenantID: "tenant:b", UserID: "user:a", Role: ViewerAuditor}})
	if len(foreign) != 0 {
		t.Fatalf("foreign tenant saw %d records", len(foreign))
	}
	mutated := testEntry("event:bad", "tenant:a", "user:a", "task:bad", EventSkillCall)
	mutated.Edges = nil
	if _, err := store.Append(context.Background(), mutated); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("invalid entry error = %v, want ErrInvalidEntry", err)
	}
	for _, kind := range []EventKind{EventApproval, EventIntentOrigin, EventWorkflowRun, EventConnectorOperation} {
		missingJoin := testEntry("event:missing-"+string(kind), "tenant:a", "user:a", "task:missing-"+string(kind), kind)
		missingJoin.Edges = []Edge{{Kind: EdgeTask, From: missingJoin.Actor.TaskID, To: missingJoin.EventID}}
		if _, err := store.Append(context.Background(), missingJoin); !errors.Is(err, ErrInvalidEntry) {
			t.Errorf("%s without its direct provenance edge error = %v, want ErrInvalidEntry", kind, err)
		}
	}
	duplicateField := testEntry("event:duplicate-field", "tenant:a", "user:a", "task:duplicate-field", EventSkillCall)
	duplicateField.Fields = append(duplicateField.Fields, duplicateField.Fields[0])
	if _, err := store.Append(context.Background(), duplicateField); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("duplicate field error = %v, want ErrInvalidEntry", err)
	}
}

func TestTodo_AGENT2_014_Integration(t *testing.T) {
	origin, err := intent.NewOrigin(intent.OriginClaim{TriggerRef: "chat", CorrelationID: "request:1"}, intent.TrustedOriginContext{Kind: intent.OriginAgent, Tenant: values.TenantId("tenant-a"), OrganizationScopeID: "org-a", Initiator: intent.PrincipalReference{PrincipalID: "agent:concierge", Kind: intent.InitiatorAgent, IdentityAssuranceRef: "assurance:1"}, DelegationRefs: []string{"grant:9"}, SessionRef: "session:1", Assurance: intent.AssuranceHigh, Channel: intent.ChannelUI, TrustedContextDigest: "sha256:context"})
	if err != nil {
		t.Fatalf("NewOrigin: %v", err)
	}
	chain, err := FromIntentOrigin(origin, testActor("user:a", "task:1"))
	if err != nil {
		t.Fatalf("FromIntentOrigin: %v", err)
	}
	if chain.DelegationGrantID != "grant:9" || chain.AgentVersion == "" || chain.PlanRevision == "" {
		t.Fatalf("actor chain = %+v, want grant, version and plan revision", chain)
	}

	store := NewMemoryStore()
	entry := testEntry("event:integration", "tenant-a", chain.UserID, chain.TaskID, EventConnectorOperation)
	entry.Actor = chain
	entry.Edges = append(entry.Edges, Edge{Kind: EdgeAuthorizedBy, From: entry.EventID, To: "approval:7"})
	if _, err := store.Append(context.Background(), entry); err != nil {
		t.Fatalf("connector operation Append: %v", err)
	}
	views, err := store.Query(context.Background(), Query{Viewer: Viewer{TenantID: "tenant-a", UserID: "auditor", Role: ViewerAuditor, AllowedFields: map[string]bool{"salary": true}}})
	if err != nil {
		t.Fatalf("integration Query: %v", err)
	}
	if len(views) != 1 || views[0].Fields[1].Value != "125000" || views[0].Edges[len(views[0].Edges)-1].Kind != EdgeAuthorizedBy {
		t.Fatalf("joined connector view = %+v, want approval authorization and field access", views)
	}
	admissionEntry, err := FromAdmission("event:admitted", "tenant-a", chain, agentsecurity.Admission{AgentID: "agent:concierge", Tenant: "tenant-a", Tool: "people.lookup", ArgsDigest: "sha256:args"}, "sha256:result", []Edge{{Kind: EdgeTask, From: "task:1", To: "event:admitted"}}, time.Date(2026, 9, 28, 12, 1, 0, 0, time.UTC))
	if err != nil || admissionEntry.Action != "people.lookup" || admissionEntry.ArgumentsDigest != "sha256:args" {
		t.Fatalf("FromAdmission = %+v, %v; want the existing gateway receipt fields", admissionEntry, err)
	}
}

func TestTodo_AGENT2_014_RejectsForgedActorChain(t *testing.T) {
	origin, err := intent.NewOrigin(intent.OriginClaim{TriggerRef: "chat", CorrelationID: "request:forged"}, intent.TrustedOriginContext{Kind: intent.OriginHuman, Tenant: values.TenantId("tenant-a"), OrganizationScopeID: "org-a", Initiator: intent.PrincipalReference{PrincipalID: "user:a", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance:1"}, SessionRef: "session:1", Assurance: intent.AssuranceHigh, Channel: intent.ChannelUI, TrustedContextDigest: "sha256:context"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromIntentOrigin(origin, testActor("user:a", "task:1")); err == nil {
		t.Fatal("human origin was accepted as agent provenance")
	}
	tooDeep := testActor("user:a", "task:1")
	tooDeep.SubAgentDepth = MaxSubAgentDepth + 1
	if err := tooDeep.Validate(); err == nil {
		t.Fatal("over-deep sub-agent chain was accepted")
	}
}
