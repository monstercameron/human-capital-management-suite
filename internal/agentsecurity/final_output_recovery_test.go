package agentsecurity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
)

type recoveryRehydrator struct {
	projection FinalOutputPersistence
	err        error
}

func (r recoveryRehydrator) RehydrateFinalOutput(context.Context, FinalOutputRecoveryRecord) (FinalOutputPersistence, error) {
	return r.projection, r.err
}

func signedRecoveryRecord(t *testing.T, p FinalOutputPersistence) (FinalOutputRecoveryRecord, *FinalOutputRecoveryAuthority, *FinalOutputRecoveryVerifier) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewFinalOutputRecoveryAuthority("test-key", private)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewFinalOutputRecoveryVerifier(map[string]ed25519.PublicKey{"test-key": public})
	if err != nil {
		t.Fatal(err)
	}
	record := FinalOutputRecoveryRecord{
		Identity: p.Identity(), AdmissionDigest: p.AdmissionDigest(), SemanticDigest: p.SemanticDigest(),
		PersistenceDigest: p.Digest(), Materials: []byte(`[]`), Citations: []byte(`[]`), SealedPayload: []byte(`{}`),
	}
	record.RecoveryReceipt, err = authority.IssueFinalOutputRecoveryReceipt(record)
	if err != nil {
		t.Fatal(err)
	}
	return record, authority, verifier
}

func TestTodo_AGENTP_012_RecoveryRequiresServerAuthority(t *testing.T) {
	p := testRecoveryProjection(t)
	record, _, verifier := signedRecoveryRecord(t, p)
	if _, err := RecoverFinalOutputPersistence(context.Background(), record, nil, recoveryRehydrator{projection: p}); !errors.Is(err, ErrFinalOutputRecoveryUnavailable) {
		t.Fatalf("missing verifier error = %v", err)
	}
	if _, err := RecoverFinalOutputPersistence(context.Background(), record, verifier, nil); !errors.Is(err, ErrFinalOutputRecoveryUnavailable) {
		t.Fatalf("missing rehydrator error = %v", err)
	}
}

func TestTodo_AGENTP_012_RecoveryRejectsTamperedCanonicalRow(t *testing.T) {
	p := testRecoveryProjection(t)
	record, _, verifier := signedRecoveryRecord(t, p)
	mutations := []struct {
		name   string
		change func(*FinalOutputRecoveryRecord)
	}{
		{"tenant", func(r *FinalOutputRecoveryRecord) { r.Identity.TenantID = "tenant-attacker" }},
		{"output id", func(r *FinalOutputRecoveryRecord) { r.Identity.OutputID = "output-attacker" }},
		{"admission id", func(r *FinalOutputRecoveryRecord) { r.Identity.AdmissionID = "admission-attacker" }},
		{"run id", func(r *FinalOutputRecoveryRecord) { r.Identity.RunID = "run-attacker" }},
		{"admission digest", func(r *FinalOutputRecoveryRecord) { r.AdmissionDigest = "sha256:forged" }},
		{"semantic digest", func(r *FinalOutputRecoveryRecord) { r.SemanticDigest = "sha256:forged" }},
		{"materials", func(r *FinalOutputRecoveryRecord) { r.Materials = []byte(`[{"id":"private-record","value":"forged"}]`) }},
		{"citations", func(r *FinalOutputRecoveryRecord) {
			r.Citations = []byte(`[{"source_id":"private-record","title":"secret"}]`)
		}},
		{"payload", func(r *FinalOutputRecoveryRecord) { r.SealedPayload = []byte(`{"payload":{"name":"attacker"}}`) }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			changed := record
			changed.RecoveryReceipt = append([]byte(nil), record.RecoveryReceipt...)
			changed.Materials = append([]byte(nil), record.Materials...)
			changed.Citations = append([]byte(nil), record.Citations...)
			changed.SealedPayload = append([]byte(nil), record.SealedPayload...)
			tc.change(&changed)
			if _, err := RecoverFinalOutputPersistence(context.Background(), changed, verifier, recoveryRehydrator{projection: p}); !errors.Is(err, ErrFinalOutputRecoveryUnavailable) {
				t.Fatalf("tampered row recovered: %v", err)
			}
		})
	}
}

func TestTodo_AGENTP_012_RecoverySurvivesAuthorityRestartAndCanonicalJSON(t *testing.T) {
	p := testRecoveryProjection(t)
	record, authority, verifier := signedRecoveryRecord(t, p)
	// A newly composed verifier represents a process restart. JSON whitespace and
	// object key ordering do not change the authenticated row representation.
	restarted, err := NewFinalOutputRecoveryVerifier(map[string]ed25519.PublicKey{authority.keyID: authority.key.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	record.Materials = []byte(" [ ] ")
	restored, err := RecoverFinalOutputPersistence(context.Background(), record, restarted, recoveryRehydrator{projection: p})
	if err != nil {
		t.Fatalf("recovery after restart: %v", err)
	}
	if restored.Digest() != p.Digest() || restored.Identity() != p.Identity() {
		t.Fatalf("recovered projection = %q / %+v", restored.Digest(), restored.Identity())
	}
	if _, err := RecoverFinalOutputPersistence(context.Background(), record, verifier, recoveryRehydrator{projection: p, err: errors.New("rehydration unavailable")}); !errors.Is(err, ErrFinalOutputRecoveryUnavailable) {
		t.Fatalf("rehydration error = %v", err)
	}
}

func testRecoveryProjection(t *testing.T) FinalOutputPersistence {
	t.Helper()
	g, admission, output, err := agent026FinalOutput(t, true)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := g.IssueFinalOutputPersistence(context.Background(), admission, output, agent026PersistenceIdentity(admission.Tenant))
	if err != nil {
		t.Fatal(err)
	}
	return projection
}
