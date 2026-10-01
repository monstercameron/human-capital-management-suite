package agentstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestPersonaRouteQualificationEvidenceAuthorityRejectsUnpassedAndUnverifiedBindings(t *testing.T) {
	profile := digestForTest("a")
	good := VerifiedPersonaRouteEvaluation{RunID: "run-1", TenantID: "tenant-1", PersonaID: "persona-1", PersonaVersion: 2,
		ProfileDigest: profile, SuiteDigest: digestForTest("b"), RunDigest: digestForTest("c"), Passed: false, Fresh: true}
	for _, tc := range []struct {
		name  string
		value VerifiedPersonaRouteEvaluation
		err   error
	}{
		{name: "wrong tenant", value: func() VerifiedPersonaRouteEvaluation { v := good; v.TenantID = "other"; return v }()},
		{name: "not passed", value: good},
		{name: "stale", value: func() VerifiedPersonaRouteEvaluation { v := good; v.Fresh = false; return v }()},
		{name: "resolver error", value: good, err: errors.New("signature rejected")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority, err := NewPersonaRouteQualificationEvidenceAuthority(routeEvaluationResolver{value: tc.value, err: tc.err}, func(values.TenantId) uuid.UUID { return uuid.New() })
			if err != nil {
				t.Fatal(err)
			}
			tx := &routeEvidenceTx{modelDigest: digestForTest("d"), expiresAt: time.Now().Add(time.Hour)}
			if _, err := authority.ResolvePersonaRouteQualification(context.Background(), tx, "tenant-1", "run-1", "persona-1", 2, profile); !errors.Is(err, ErrPersonaRouteQualificationRequired) {
				t.Fatalf("resolve error=%v, want ErrPersonaRouteQualificationRequired", err)
			}
			if tx.detailRead {
				t.Fatal("read unverified model/expiry details before resolving trusted claim")
			}
		})
	}
}

func TestPersonaRouteQualificationEvidenceAuthorityRequiresCompleteConfiguration(t *testing.T) {
	if _, err := NewPersonaRouteQualificationEvidenceAuthority(nil, func(values.TenantId) uuid.UUID { return uuid.New() }); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil resolver error=%v", err)
	}
	if _, err := NewPersonaRouteQualificationEvidenceAuthority(routeEvaluationResolver{}, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil mapper error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	authority, _ := NewPersonaRouteQualificationEvidenceAuthority(routeEvaluationResolver{}, func(values.TenantId) uuid.UUID { return uuid.New() })
	if _, err := authority.ResolvePersonaRouteQualification(ctx, &routeEvidenceTx{}, "tenant-1", "run-1", "persona-1", 1, digestForTest("a")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup error=%v", err)
	}
}

func digestForTest(letter string) string { return "sha256:" + strings.Repeat(letter, 64) }

type routeEvaluationResolver struct {
	value VerifiedPersonaRouteEvaluation
	err   error
}

func (r routeEvaluationResolver) ResolvePersonaEvaluation(context.Context, dbport.Tx, values.TenantId, string, string, int64, string) (VerifiedPersonaRouteEvaluation, error) {
	return r.value, r.err
}

type routeEvidenceTx struct {
	detailRead  bool
	tenantID    uuid.UUID
	runID       string
	modelDigest string
	expiresAt   time.Time
}

func (tx *routeEvidenceTx) Exec(context.Context, string, ...any) (int64, error) {
	return 0, errors.New("unexpected Exec")
}
func (tx *routeEvidenceTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (tx *routeEvidenceTx) QueryRow(_ context.Context, query string, args ...any) dbport.Row {
	if !strings.Contains(query, "FROM persona_evaluation_evidence") || len(args) != 2 {
		return routeEvidenceRow{err: errors.New("unexpected query")}
	}
	tx.detailRead = true
	tx.tenantID = args[0].(uuid.UUID)
	tx.runID = args[1].(string)
	return routeEvidenceRow{modelDigest: tx.modelDigest, expiresAt: tx.expiresAt}
}
func (*routeEvidenceTx) Commit(context.Context) error   { return errors.New("unexpected Commit") }
func (*routeEvidenceTx) Rollback(context.Context) error { return nil }

type routeEvidenceRow struct {
	modelDigest string
	expiresAt   time.Time
	err         error
}

func (r routeEvidenceRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = r.modelDigest
	*dest[1].(*time.Time) = r.expiresAt
	return nil
}
