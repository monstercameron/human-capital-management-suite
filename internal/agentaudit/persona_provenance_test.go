package agentaudit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
)

func personaDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func personaRunRequest() agentinvoke.RunRequest {
	actor := agentinvoke.ActorChain{
		UserID: "manager-1", PersonaID: "comp-analyst", PersonaVersion: "v7",
		InstallationID: "installation-room-4", ConversationID: "conversation-4",
		InvokingPostID: "post-invoke", InvocationID: "invocation-88",
	}
	return agentinvoke.RunRequest{
		InvocationID: actor.InvocationID, TenantID: "tenant-a", ConversationID: actor.ConversationID,
		ThreadID: "thread-4", InvokingPostID: actor.InvokingPostID, InvokerID: actor.UserID,
		PersonaID: actor.PersonaID, PersonaVersion: actor.PersonaVersion, InstallationID: actor.InstallationID,
		Mode: agentinvoke.OnBehalfOf, Actor: actor,
		Grant:  agentinvoke.DelegationGrant{ID: "grant-1", UserID: actor.UserID, TenantID: "tenant-a", Skills: agentinvoke.SkillScopes{"compensation.read": {"people.compensation", "bands.read"}}},
		Skills: agentinvoke.SkillScopes{"compensation.read": {"people.compensation", "bands.read"}},
		Context: agentinvoke.BoundContext{
			Goal: "private prompt text must not be stored",
			Entries: []agentinvoke.ContextEntry{
				{PostID: "post-invoke", AuthorID: "manager-1", Digest: personaDigest("invoke text"), Taint: agentinvoke.TaintInvokerInstruction},
				{PostID: "post-peer", AuthorID: "manager-2", Digest: personaDigest("peer text"), Taint: agentinvoke.TaintUntrustedPeer},
				{PostID: "post-peer", AuthorID: "manager-2", Digest: personaDigest("attachment"), Taint: agentinvoke.TaintUntrustedPeer, Attachment: true, AttachmentID: "file-77"},
			},
		},
	}
}

func personaExecution() PersonaSkillExecution {
	return PersonaSkillExecution{TaskID: "task-88", PlanRevision: "plan-r3", StepID: "step-2", SkillID: "compensation.read", SkillVersion: "4.2.1"}
}

func personaEntry(t *testing.T) Entry {
	t.Helper()
	entry, err := NewPersonaAuditEntry("event-persona-1", "tenant-a", "compensation.read", personaDigest("args"), personaDigest("result"), personaExecution(), personaRunRequest(), time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestTodo_AGENTP_017(t *testing.T) {
	entry := personaEntry(t)
	if entry.Actor.UserID != "manager-1" || entry.Actor.AgentVersion != "v7" || entry.Actor.InstallationID != "installation-room-4" || entry.Actor.TaskID != "task-88" || entry.Actor.PlanRevision != "plan-r3" || entry.Actor.StepID != "step-2" || entry.Actor.DelegationGrantID != "grant-1" {
		t.Fatalf("generic actor chain lost invocation execution identity: %+v", entry.Actor)
	}
	if len(entry.Fields) != 6+4+3*3+1 {
		t.Fatalf("persona fields = %d, want 20 fields including all context evidence", len(entry.Fields))
	}
	canonical := CanonicalEntry(entry)
	if strings.Contains(canonical, "private prompt text") || strings.Contains(canonical, "invoke text") || strings.Contains(canonical, "peer text") {
		t.Fatal("raw prompt or context content entered the audit provenance")
	}
	if !strings.Contains(canonical, "post-peer") || !strings.Contains(canonical, "UNTRUSTED_PEER") || !strings.Contains(canonical, "file-77") {
		t.Fatal("audit provenance omitted peer post, taint, or attachment lineage")
	}
}

func TestTodo_AGENTP_017_Golden(t *testing.T) {
	entry := personaEntry(t)
	got := personaDigest(CanonicalEntry(entry))
	const want = "sha256:2356d01e684c46c3353981616d5de0f94509e5b637a806ec13e7037e668d500d"
	if got != want {
		t.Fatalf("persona provenance digest = %s, want %s", got, want)
	}
}

func TestTodo_AGENTP_017_Security(t *testing.T) {
	base := personaRunRequest()
	tests := []struct {
		name   string
		mutate func(*agentinvoke.RunRequest, *PersonaSkillExecution, *string)
	}{
		{"forged invocation", func(r *agentinvoke.RunRequest, _ *PersonaSkillExecution, _ *string) {
			r.InvocationID = "invocation-forged"
		}},
		{"forged persona version", func(r *agentinvoke.RunRequest, _ *PersonaSkillExecution, _ *string) { r.PersonaVersion = "v8" }},
		{"wrong grant principal", func(r *agentinvoke.RunRequest, _ *PersonaSkillExecution, _ *string) { r.Grant.UserID = "other-user" }},
		{"missing pinned skill", func(r *agentinvoke.RunRequest, e *PersonaSkillExecution, _ *string) { e.SkillID = "admin.write" }},
		{"skill omitted from grant", func(r *agentinvoke.RunRequest, _ *PersonaSkillExecution, _ *string) { r.Grant.Skills = nil }},
		{"peer taint for invocation", func(r *agentinvoke.RunRequest, _ *PersonaSkillExecution, _ *string) {
			r.Context.Entries[0].Taint = agentinvoke.TaintUntrustedPeer
		}},
		{"unknown taint", func(r *agentinvoke.RunRequest, _ *PersonaSkillExecution, _ *string) {
			r.Context.Entries[1].Taint = "TRUSTED_SYSTEM"
		}},
		{"invalid context digest", func(r *agentinvoke.RunRequest, _ *PersonaSkillExecution, _ *string) {
			r.Context.Entries[1].Digest = "sha256:forged"
		}},
		{"missing invoking post", func(r *agentinvoke.RunRequest, _ *PersonaSkillExecution, _ *string) {
			r.Context.Entries = r.Context.Entries[1:]
		}},
		{"cross tenant", func(_ *agentinvoke.RunRequest, _ *PersonaSkillExecution, tenant *string) { *tenant = "tenant-b" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := base
			run.Context.Entries = append([]agentinvoke.ContextEntry(nil), base.Context.Entries...)
			execution := personaExecution()
			tenant := "tenant-a"
			tc.mutate(&run, &execution, &tenant)
			if _, err := NewPersonaAuditEntry("event-security", tenant, "compensation.read", personaDigest("args"), personaDigest("result"), execution, run, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrInvalidEntry) && !errors.Is(err, ErrTenantBoundary) {
				t.Fatalf("tampered persona request error = %v", err)
			}
		})
	}

	store := NewMemoryStore()
	record, err := store.Append(context.Background(), personaEntry(t))
	if err != nil {
		t.Fatal(err)
	}
	record.Fields[1].Value = "v-forged"
	if err := VerifyChain([]Record{record}); !errors.Is(err, ErrChainTampered) {
		t.Fatalf("tampered persona version error = %v, want ErrChainTampered", err)
	}
}

func TestTodo_AGENTP_017_Integration(t *testing.T) {
	store := NewMemoryStore()
	first := personaEntry(t)
	second := first
	second.EventID = "event-persona-2"
	second.Action = "compensation.read.summary"
	second.Fields = append([]Field(nil), first.Fields...)
	second.Edges = []Edge{{Kind: EdgeTask, From: second.Actor.TaskID, To: second.EventID}, {Kind: EdgeCaused, From: second.EventID, To: "invocation-88"}}
	for _, entry := range []Entry{first, second} {
		if _, err := store.Append(context.Background(), entry); err != nil {
			t.Fatalf("Append(%s): %v", entry.EventID, err)
		}
	}
	allowed := make(map[string]bool)
	for _, field := range first.Fields {
		allowed[field.Name] = true
	}
	views, err := store.Query(context.Background(), Query{Viewer: Viewer{TenantID: "tenant-a", UserID: "auditor-1", Role: ViewerAuditor, AllowedFields: allowed}, TaskID: "task-88"})
	if err != nil {
		t.Fatal(err)
	}
	trace, err := ResolvePersonaTrace(views, "invocation-88")
	if err != nil {
		t.Fatalf("ResolvePersonaTrace: %v", err)
	}
	if trace.PersonaID != "comp-analyst" || trace.PersonaVersion != "v7" || trace.InstallationID != "installation-room-4" || trace.ConversationID != "conversation-4" || trace.InvokingPostID != "post-invoke" || len(trace.Events) != 2 || len(trace.Context) != 3 {
		t.Fatalf("resolved trace = %+v, want both skill events and three content-free context entries", trace)
	}
	if trace.Context[1].PostID != "post-peer" || trace.Context[1].Taint != agentinvoke.TaintUntrustedPeer || trace.Context[2].AttachmentID != "file-77" {
		t.Fatalf("resolved context lineage = %+v", trace.Context)
	}
	if err := store.Verify(context.Background(), "tenant-a"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	redacted, err := store.Query(context.Background(), Query{Viewer: Viewer{TenantID: "tenant-a", UserID: "auditor-1", Role: ViewerAuditor}, TaskID: "task-88"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolvePersonaTrace(redacted, "invocation-88"); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("trace resolution without invocation-field access = %v, want no matching authorized trace", err)
	}
}
