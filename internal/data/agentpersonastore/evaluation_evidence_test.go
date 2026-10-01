package agentpersonastore

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_021(t *testing.T) {
	tenant := values.TenantId("tenant-evaluation")
	f := newFixture(t, tenant)
	store := f.store(t, tenant)
	public, private, err := ed25519.GenerateKey(strings.NewReader(strings.Repeat("e", 128)))
	if err != nil {
		t.Fatal(err)
	}
	clock := f.when.Add(time.Hour)
	authority, err := NewEvaluationSealAuthority(map[string]ed25519.PublicKey{"eval-v1": public}, func(key values.TenantId) uuid.UUID { return f.ids[key] }, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	evidence := signedEvaluation(private, string(tenant), "run-1", "persona-1", 3, "sha256:"+strings.Repeat("a", 64), true, f.when, f.when.Add(2*time.Hour))
	tx, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := authority.RecordPersonaEvaluation(context.Background(), tx, evidence); err != nil {
		t.Fatalf("record trusted result: %v", err)
	}
	got, err := authority.ResolvePersonaEvaluation(context.Background(), tx, tenant, "run-1", "persona-1", 3, evidence.Claim.ProfileDigest)
	if err != nil || !got.Passed || !got.Fresh || got.RunDigest != evidence.Claim.RunDigest {
		t.Fatalf("resolve durable signed result = %#v, %v", got, err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The same source resolves the durable evidence after a new transaction,
	// which models publication after a process restart.
	tx2, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback(context.Background())
	got, err = authority.ResolvePersonaEvaluation(context.Background(), tx2, tenant, "run-1", "persona-1", 3, evidence.Claim.ProfileDigest)
	if err != nil || got.RunID != "run-1" || got.SuiteDigest != evidence.Claim.SuiteDigest {
		t.Fatalf("resolve after transaction restart = %#v, %v", got, err)
	}
}

func TestTodo_AGENTP_021_SigningClaim(t *testing.T) {
	seed := sha256.Sum256([]byte("persona claim signer"))
	private := ed25519.NewKeyFromSeed(seed[:])
	issued := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	claim := PersonaEvaluationClaim{TenantID: "tenant-a", RunID: "run-a", PersonaID: "persona-a", PersonaVersion: 2,
		ProfileDigest: "sha256:" + strings.Repeat("a", 64), SuiteDigest: "sha256:" + strings.Repeat("b", 64),
		RunDigest: "sha256:" + strings.Repeat("c", 64), ModelDigest: "sha256:" + strings.Repeat("d", 64), Passed: true,
		IssuedAt: issued, ExpiresAt: issued.Add(time.Hour), KeyID: "evaluation-v1"}
	signed, err := SignPersonaEvaluationClaim(private, claim)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(signed.Claim)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(private.Public().(ed25519.PublicKey), evaluationSealMessage(encoded), signed.Signature) {
		t.Fatal("signed claim did not verify")
	}
	for _, mutate := range []func(*PersonaEvaluationClaim){
		func(c *PersonaEvaluationClaim) { c.ProfileDigest = "sha256:bad" },
		func(c *PersonaEvaluationClaim) { c.ExpiresAt = c.IssuedAt },
		func(c *PersonaEvaluationClaim) { c.RunID = " " },
	} {
		invalid := claim
		mutate(&invalid)
		if _, err := SignPersonaEvaluationClaim(private, invalid); !errors.Is(err, errInvalidEvaluationEvidence) {
			t.Fatalf("invalid claim signing error=%v", err)
		}
	}
	if _, err := SignPersonaEvaluationClaim(private[:len(private)-1], claim); !errors.Is(err, errInvalidEvaluationEvidence) {
		t.Fatalf("invalid key signing error=%v", err)
	}
}

func TestTodo_AGENTP_021_Security(t *testing.T) {
	tenant := values.TenantId("tenant-evaluation-security")
	f := newFixture(t, tenant)
	store := f.store(t, tenant)
	seed := sha256.Sum256([]byte("evaluation authority test key"))
	private := ed25519.NewKeyFromSeed(seed[:])
	public := private.Public().(ed25519.PublicKey)
	now := f.when.Add(time.Hour)
	authority, err := NewEvaluationSealAuthority(map[string]ed25519.PublicKey{"eval-v1": public}, func(key values.TenantId) uuid.UUID { return f.ids[key] }, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		edit func(*SignedPersonaEvaluation)
	}{
		{name: "caller flips pass flag", edit: func(e *SignedPersonaEvaluation) { e.Claim.Passed = true }},
		{name: "profile digest substituted", edit: func(e *SignedPersonaEvaluation) { e.Claim.ProfileDigest = "sha256:" + strings.Repeat("f", 64) }},
		{name: "tenant substituted", edit: func(e *SignedPersonaEvaluation) { e.Claim.TenantID = "other-tenant" }},
		{name: "expired claim", edit: func(e *SignedPersonaEvaluation) { e.Claim.ExpiresAt = now.Add(-time.Second) }},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence := signedEvaluation(private, string(tenant), "forged-"+string(rune('a'+i)), "persona-1", 3, "sha256:"+strings.Repeat("a", 64), false, f.when, f.when.Add(2*time.Hour))
			tc.edit(&evidence)
			tx, err := store.begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if err := authority.RecordPersonaEvaluation(context.Background(), tx, evidence); !errors.Is(err, errInvalidEvaluationEvidence) {
				t.Fatalf("forged evidence error = %v, want invalid evidence", err)
			}
		})
	}

	failed := signedEvaluation(private, string(tenant), "failed-run", "persona-1", 3, "sha256:"+strings.Repeat("a", 64), false, f.when, f.when.Add(2*time.Hour))
	tx, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RecordPersonaEvaluation(context.Background(), tx, failed); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.ResolvePersonaEvaluation(context.Background(), tx, tenant, "failed-run", "persona-1", 3, failed.Claim.ProfileDigest); !errors.Is(err, ErrPublicationEvidenceRequired) {
		t.Fatalf("failed evidence resolution = %v, want publication refusal", err)
	}
	_ = tx.Rollback(context.Background())
}

func signedEvaluation(private ed25519.PrivateKey, tenant, runID, personaID string, version int64, profileDigest string, passed bool, issuedAt, expiresAt time.Time) SignedPersonaEvaluation {
	claim := PersonaEvaluationClaim{TenantID: tenant, RunID: runID, PersonaID: personaID, PersonaVersion: version,
		ProfileDigest: profileDigest, SuiteDigest: "sha256:" + strings.Repeat("b", 64),
		RunDigest: "sha256:" + strings.Repeat("c", 64), ModelDigest: "sha256:" + strings.Repeat("d", 64),
		Passed: passed, IssuedAt: issuedAt, ExpiresAt: expiresAt, KeyID: "eval-v1"}
	encoded, _ := json.Marshal(claim)
	return SignedPersonaEvaluation{Claim: claim, Signature: ed25519.Sign(private, evaluationSealMessage(encoded))}
}
