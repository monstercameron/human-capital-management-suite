package agentpersonastore

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// The seal covers the encoded claim times and the evidence table keeps
// microseconds. An evaluation signed at the wall clock's full precision was
// recorded, reported as passed, and then failed verification on every later
// read ("evaluator signature is invalid"), so the version could never be
// published after a reload.
func TestEvaluationSealSurvivesStoredTimePrecision(t *testing.T) {
	tenant := values.TenantId("tenant-evaluation-precision")
	f := newFixture(t, tenant)
	store := f.store(t, tenant)
	public, private, err := ed25519.GenerateKey(strings.NewReader(strings.Repeat("p", 128)))
	if err != nil {
		t.Fatal(err)
	}
	issued := f.when.Add(123456789 * time.Nanosecond) // 789 ns below a microsecond
	clock := issued.Add(time.Hour)
	authority, err := NewEvaluationSealAuthority(map[string]ed25519.PublicKey{"eval-v1": public}, func(key values.TenantId) uuid.UUID { return f.ids[key] }, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	claim := PersonaEvaluationClaim{TenantID: string(tenant), RunID: "run-precise", PersonaID: "persona-1", PersonaVersion: 3,
		ProfileDigest: "sha256:" + strings.Repeat("a", 64), SuiteDigest: "sha256:" + strings.Repeat("b", 64),
		RunDigest: "sha256:" + strings.Repeat("c", 64), ModelDigest: "sha256:" + strings.Repeat("d", 64),
		Passed: true, IssuedAt: issued, ExpiresAt: issued.Add(24*time.Hour + 987*time.Nanosecond), KeyID: "eval-v1"}
	evidence, err := SignPersonaEvaluationClaim(private, claim)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Claim.IssuedAt.Nanosecond()%1000 != 0 || evidence.Claim.ExpiresAt.Nanosecond()%1000 != 0 {
		t.Fatalf("sealed claim keeps sub-microsecond time: %v / %v", evidence.Claim.IssuedAt, evidence.Claim.ExpiresAt)
	}
	tx, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := authority.RecordPersonaEvaluation(context.Background(), tx, evidence); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	tx2, err := store.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback(context.Background())
	if got, err := authority.ResolvePersonaEvaluation(context.Background(), tx2, tenant, "run-precise", "persona-1", 3, claim.ProfileDigest); err != nil || !got.Passed {
		t.Fatalf("evaluation signed at nanosecond time was not readable after storage: %#v, %v", got, err)
	}

	// A claim sealed elsewhere at finer precision is refused when recorded,
	// not stored as a row no reader can verify.
	fine := claim
	fine.RunID = "run-fine"
	encoded, _ := json.Marshal(fine)
	err = authority.RecordPersonaEvaluation(context.Background(), tx2, SignedPersonaEvaluation{Claim: fine, Signature: ed25519.Sign(private, evaluationSealMessage(encoded))})
	if !errors.Is(err, errInvalidEvaluationEvidence) {
		t.Fatalf("finer-than-stored claim was recorded: %v", err)
	}
}
