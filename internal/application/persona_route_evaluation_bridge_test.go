package application

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestPersonaRouteEvaluationBridgeRejectsFailedSignedClaim(t *testing.T) {
	seed := sha256.Sum256([]byte("persona route bridge signature fixture"))
	private := ed25519.NewKeyFromSeed(seed[:])
	now := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	claim := agentpersonastore.PersonaEvaluationClaim{
		TenantID: "tenant-1", RunID: "run-1", PersonaID: "persona-1", PersonaVersion: 2,
		ProfileDigest: bridgeDigest("a"), SuiteDigest: bridgeDigest("b"), RunDigest: bridgeDigest("c"), ModelDigest: bridgeDigest("d"),
		Passed: false, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), KeyID: "eval-key",
	}
	evidence, err := agentpersonastore.SignPersonaEvaluationClaim(private, claim)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	authority, err := agentpersonastore.NewEvaluationSealAuthority(map[string]ed25519.PublicKey{"eval-key": private.Public().(ed25519.PublicKey)},
		func(values.TenantId) uuid.UUID { return tenantID }, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := NewPersonaRouteEvaluationBridge(authority)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bridge.ResolvePersonaEvaluation(context.Background(), bridgeTx{evidence: evidence}, "tenant-1", "run-1", "persona-1", 2, claim.ProfileDigest)
	if !errors.Is(err, agentpersonastore.ErrPublicationEvidenceRequired) {
		t.Fatalf("resolve signed failed claim error=%v, want ErrPublicationEvidenceRequired", err)
	}
	if _, err := bridge.ResolvePersonaEvaluation(context.Background(), bridgeTx{evidence: evidence}, "tenant-1", "run-1", "persona-1", 2, bridgeDigest("f")); err == nil {
		t.Fatal("bridge returned evidence for a substituted profile digest")
	}
}

func TestPersonaRouteEvaluationBridgeRequiresSealAuthority(t *testing.T) {
	if _, err := NewPersonaRouteEvaluationBridge(nil); err == nil {
		t.Fatal("constructed bridge without signature verifier")
	}
	var bridge *PersonaRouteEvaluationBridge
	if _, err := bridge.ResolvePersonaEvaluation(context.Background(), nil, "tenant-1", "run-1", "persona-1", 1, bridgeDigest("a")); err == nil {
		t.Fatal("nil bridge returned evaluation evidence")
	}
}

func bridgeDigest(letter string) string { return "sha256:" + strings.Repeat(letter, 64) }

type bridgeTx struct {
	evidence agentpersonastore.SignedPersonaEvaluation
}

func (bridgeTx) Exec(context.Context, string, ...any) (int64, error) {
	return 0, errors.New("unexpected Exec")
}
func (bridgeTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (tx bridgeTx) QueryRow(context.Context, string, ...any) dbport.Row {
	return bridgeRow{evidence: tx.evidence}
}
func (bridgeTx) Commit(context.Context) error   { return errors.New("unexpected Commit") }
func (bridgeTx) Rollback(context.Context) error { return nil }

type bridgeRow struct {
	evidence agentpersonastore.SignedPersonaEvaluation
}

func (row bridgeRow) Scan(dest ...any) error {
	claim := row.evidence.Claim
	*dest[0].(*string) = claim.RunID
	*dest[1].(*string) = claim.PersonaID
	*dest[2].(*int64) = claim.PersonaVersion
	*dest[3].(*string) = claim.ProfileDigest
	*dest[4].(*string) = claim.SuiteDigest
	*dest[5].(*string) = claim.RunDigest
	*dest[6].(*string) = claim.ModelDigest
	*dest[7].(*bool) = claim.Passed
	*dest[8].(*time.Time) = claim.IssuedAt
	*dest[9].(*time.Time) = claim.ExpiresAt
	*dest[10].(*string) = claim.KeyID
	*dest[11].(*[]byte) = row.evidence.Signature
	return nil
}
