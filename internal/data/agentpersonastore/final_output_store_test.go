package agentpersonastore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

type persistenceDraft struct{ Name string }

func (p persistenceDraft) DraftFields() []string     { return nil }
func (p persistenceDraft) DraftReferences() []string { return []string{"record-1"} }
func (p persistenceDraft) DraftClaims() []string     { return nil }
func (p persistenceDraft) DraftCanonicalBytes() ([]byte, error) {
	return []byte(`{"name":"Ada"}`), nil
}
func (p persistenceDraft) DetachDraft() (agentsecurity.DraftValue, error) { return p, nil }

type persistenceRefs map[string]bool

func (r persistenceRefs) Exists(_ context.Context, id string) (bool, error) { return r[id], nil }

type persistenceFields struct{}

func (persistenceFields) AuthorizeFields(context.Context, string, string, []string) error { return nil }

type persistenceClaims struct{}

func (persistenceClaims) Supports(context.Context, string) (bool, error) { return true, nil }

type testRecoveryRehydrator struct {
	projection agentsecurity.FinalOutputPersistence
}

func (r testRecoveryRehydrator) RehydrateFinalOutput(context.Context, agentsecurity.FinalOutputRecoveryRecord) (agentsecurity.FinalOutputPersistence, error) {
	return r.projection, nil
}

func finalOutputProjection(t *testing.T, tenant string) agentsecurity.FinalOutputPersistence {
	t.Helper()
	g, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{{
		Name: "people.lookup", Capability: "people.read", Version: 1, Class: agentsecurity.ToolDraft,
		DataScope: []string{"people.basic"}, Cost: 1, Schema: "people.v1",
		Validate: func(value any) (agentsecurity.TypedResult, error) {
			return agentsecurity.TypedResult{Schema: "people.v1", Value: value, Validated: true, Taint: []string{"DERIVED"}, Provenance: []string{"validator"}}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"id": "record-1"}
	digest, err := agentsecurity.DigestArguments(args)
	if err != nil {
		t.Fatal(err)
	}
	call := agentsecurity.ToolCall{Agent: agentsecurity.AgentIdentity{Identity: "identity", AgentID: "agent", Tenant: tenant, Purpose: "purpose", ToolSet: []string{"people.lookup"}, DataScope: []string{"people.basic"}, Budget: 2}, Delegation: []agentsecurity.DelegationLink{{GrantID: "grant", Delegator: "root", Delegate: "agent", Tenant: tenant, Purpose: "purpose", ToolSet: []string{"people.lookup"}, DataScope: []string{"people.basic"}, Budget: 2}}, Tenant: tenant, Purpose: "purpose", Tool: "people.lookup", Capability: "people.read", Version: 1, Nonce: "nonce", Args: args, ArgsDigest: digest, InputTaint: []string{"DERIVED"}, Provenance: []string{"validator"}, CostBudget: 1, DataScope: []string{"people.basic"}}
	admission, err := g.Admit(call)
	if err != nil {
		t.Fatal(err)
	}
	text := "Employee start date is 2026-05-01."
	sum := sha256.Sum256([]byte(text))
	datum, err := g.Observe(agentsecurity.SourceDocument, text, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: "record-1", Location: "record:1", Digest: "sha256:" + hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	output, err := g.ValidateFinalOutput(context.Background(), admission, "people.lookup", agentsecurity.FinalOutputCandidate{Complete: true, Draft: agentsecurity.AgentOutput{Schema: "people.v1", Value: persistenceDraft{Name: "Ada"}, References: []string{"record-1"}, Narrative: text}, Answer: []agentsecurity.Datum{datum}}, persistenceRefs{"record-1": true}, persistenceFields{}, persistenceClaims{})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := g.IssueFinalOutputPersistence(context.Background(), admission, output, agentsecurity.FinalOutputIdentity{TenantID: tenant, OutputID: "output-1", InvocationID: "invocation-1", AdmissionID: "admission-1", RunID: "run-1", InvokerID: "invoker-1", ConversationID: "conversation-1", ThreadID: "thread-1", PostID: "post-1", PersonaID: "persona-1", PersonaVersion: "persona-v1", InstallationID: "install-1"})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func TestTodo_AGENTP_012_PutAndGetProjectionIsIdempotent(t *testing.T) {
	f := newFixture(t, "final-output-valid")
	s := f.store(t, "final-output-valid")
	projection := finalOutputProjection(t, "final-output-valid")
	authority, verifier := finalOutputRecoveryKeys(t)
	if err := s.PutFinalOutput(context.Background(), projection, authority); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetFinalOutput(context.Background(), "output-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "final-output-valid" || got.InvocationID != "invocation-1" || got.AdmissionID != "admission-1" || got.RunID != "run-1" || got.OutputID != "output-1" || got.AdmissionDigest != projection.AdmissionDigest() || got.Agent026Digest != projection.SemanticDigest() || got.PersistenceDigest != projection.Digest() || len(got.RecoveryReceipt) == 0 {
		t.Fatalf("stored projection identity/evidence = %+v", got)
	}
	recovered, err := s.RecoverFinalOutput(context.Background(), "output-1", verifier, testRecoveryRehydrator{projection: projection})
	if err != nil || recovered.Digest() != projection.Digest() {
		t.Fatalf("verified recovery digest = %q, %v", recovered.Digest(), err)
	}
	if err := s.PutFinalOutput(context.Background(), projection, authority); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate projection = %v, want ErrConflict", err)
	}
	identity := projection.Identity()
	recoveredBound, err := s.RecoverFinalOutputForIdentity(context.Background(), identity, verifier, testRecoveryRehydrator{projection: projection})
	if err != nil || recoveredBound.Digest() != projection.Digest() {
		t.Fatalf("identity-bound recovery = %q, %v", recoveredBound.Digest(), err)
	}
	identity.RunID = "different-run"
	if _, err := s.RecoverFinalOutputForIdentity(context.Background(), identity, verifier, testRecoveryRehydrator{projection: projection}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong run recovery = %v, want ErrNotFound", err)
	}
	byInvocation, err := s.GetFinalOutputByInvocation(context.Background(), "invocation-1")
	if err != nil || byInvocation.OutputID != "output-1" {
		t.Fatalf("lookup by invocation = %+v, %v", byInvocation, err)
	}
}

func TestTodo_AGENTP_012_RecoveryRejectsMissingServerReceipt(t *testing.T) {
	f := newFixture(t, "final-output-no-receipt")
	projection := finalOutputProjection(t, "final-output-no-receipt")
	if err := f.store(t, "final-output-no-receipt").PutFinalOutput(context.Background(), projection, nil); !errors.Is(err, agentsecurity.ErrFinalOutputRecoveryUnavailable) {
		t.Fatalf("write without server authority = %v, want fail closed", err)
	}
}

func finalOutputRecoveryKeys(t *testing.T) (*agentsecurity.FinalOutputRecoveryAuthority, *agentsecurity.FinalOutputRecoveryVerifier) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := agentsecurity.NewFinalOutputRecoveryAuthority("store-test-key", private)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := agentsecurity.NewFinalOutputRecoveryVerifier(map[string]ed25519.PublicKey{"store-test-key": public})
	if err != nil {
		t.Fatal(err)
	}
	return authority, verifier
}

func TestTodo_AGENTP_012_PutRejectsUnsealedProjection(t *testing.T) {
	f := newFixture(t, "final-output-invalid")
	authority, _ := finalOutputRecoveryKeys(t)
	if err := f.store(t, "final-output-invalid").PutFinalOutput(context.Background(), agentsecurity.FinalOutputPersistence{}, authority); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero projection = %v, want ErrInvalid", err)
	}
}

func TestTodo_AGENTP_012_PutRejectsTenantMismatch(t *testing.T) {
	f := newFixture(t, "final-output-a", "final-output-b")
	projection := finalOutputProjection(t, "final-output-a")
	authority, _ := finalOutputRecoveryKeys(t)
	if err := f.store(t, "final-output-b").PutFinalOutput(context.Background(), projection, authority); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign projection = %v, want ErrInvalid", err)
	}
}

func TestTodo_AGENTP_012_ImmutableRowGuardRemainsEnabled(t *testing.T) {
	f := newFixture(t, "final-output-immutable")
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, f.ids["final-output-immutable"]); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO persona_final_outputs (tenant_id,invocation_id,output_id,invoker_id,conversation_id,thread_id,parent_post_id,persona_id,persona_version,installation_id,admission_digest,agent026_digest,persistence_digest,recovery_receipt,materials,citations,sealed_payload,created_at) VALUES ($1,'i','o','u','c','t','p','persona','v','install','sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef','sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef','sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef','server-signed','[]','[]','{}',CURRENT_TIMESTAMP)`, f.ids["final-output-immutable"]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	tx, err = conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, f.ids["final-output-immutable"]); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE persona_final_outputs SET invoker_id='attacker' WHERE output_id='o'`); err == nil {
		t.Fatal("immutable final output accepted an update")
	}
	_ = tx.Rollback(context.Background())

	tx, err = conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, f.ids["final-output-immutable"]); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `DELETE FROM persona_final_outputs WHERE output_id='o'`); err == nil {
		t.Fatal("immutable final output accepted a delete")
	}
	_ = tx.Rollback(context.Background())
}
