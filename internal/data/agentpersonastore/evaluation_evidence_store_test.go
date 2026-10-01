package agentpersonastore

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_021_StoreRecordsAndResolvesSignedEvidence(t *testing.T) {
	tenant := values.TenantId("tenant-evaluation-write")
	f := newFixture(t, tenant)
	seed := sha256.Sum256([]byte("persona durable evidence store"))
	private := ed25519.NewKeyFromSeed(seed[:])
	now := f.when.Add(time.Hour)
	seal, err := NewEvaluationSealAuthority(map[string]ed25519.PublicKey{"eval-v1": private.Public().(ed25519.PublicKey)}, func(key values.TenantId) uuid.UUID { return f.ids[key] }, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	conn := f.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+"hcmnext_agent_app"); err != nil {
		t.Fatal(err)
	}
	root, err := NewWithPublicationAuthorities(conn, func(key values.TenantId) uuid.UUID { return f.ids[key] }, testReviewSource{}, seal)
	if err != nil {
		t.Fatal(err)
	}
	evidence := signedEvaluation(private, string(tenant), "durable-run", "persona-7", 4, "sha256:"+strings.Repeat("a", 64), true, f.when, f.when.Add(2*time.Hour))
	if err := root.RecordPersonaEvaluation(context.Background(), tenant, evidence); err != nil {
		t.Fatalf("record signed evaluation: %v", err)
	}

	scoped, err := root.ForTenant(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := scoped.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	got, err := seal.ResolvePersonaEvaluation(context.Background(), tx, tenant, "durable-run", "persona-7", 4, evidence.Claim.ProfileDigest)
	if err != nil || !got.Passed || got.RunDigest != evidence.Claim.RunDigest {
		t.Fatalf("resolve committed evidence = %#v, %v", got, err)
	}

	if err := root.RecordPersonaEvaluation(context.Background(), values.TenantId("tenant-other"), evidence); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tenant substitution error = %v", err)
	}
}
