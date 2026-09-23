package humanwork

import (
	"context"
	"sync"
	"testing"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// stubThresholds is the in-memory ThresholdTableSource the threshold tests
// read through. It records every call so the refusal paths can prove the
// port was never reached.
type stubThresholds struct {
	mu     sync.Mutex
	table  ThresholdTable
	calls  int
	tenant string
	err    error
}

func (s *stubThresholds) GetThresholdTable(_ context.Context, tenant, tableID string) (ThresholdTable, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return ThresholdTable{}, s.err
	}
	if tableID != "" && tableID != s.table.TableID {
		return ThresholdTable{}, ErrThresholdNotFound
	}
	out := s.table
	out.TenantID = tenant
	return out, nil
}

func (s *stubThresholds) recordedCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func referenceTestTable() ThresholdTable {
	return ThresholdTable{
		TableID: "hcmnext.rules.promotion_approval_threshold", Version: 1, VersionRef: "2026.1",
		InputNames: []string{"increase_percent", "band_position", "budget_authority", "grade_change"},
		Rows: []ThresholdRow{
			{Conditions: []ThresholdCondition{{InputName: "increase_percent", Comparison: "GREATER_THAN(20.0000)"}}, Outcome: "EXECUTIVE_REQUIRED"},
			{Outcome: "STANDARD"},
		},
		HitPolicy: ThresholdHitPolicyFirst,
	}
}

func thresholdDeps(thresholds Thresholds) Dependencies {
	return Dependencies{
		Queue:      &queueTestReader{tenant: transporttest.Tenant},
		Thresholds: thresholds,
		CursorKey:  []byte("work-queue-test-key"),
		Now:        func() time.Time { return workNow },
		Authorize: func(_ context.Context, _ *trust.Principal, action string) bool {
			return action == ActionGetThresholdTable
		},
	}
}

// TestGetThresholdTableServesTheReferenceTable is INTAPI-006's RED for the
// unimplemented read: WorkService.GetThresholdTable answered UNIMPLEMENTED
// although the contract marks it the one read P1A implements.
func TestGetThresholdTableServesTheReferenceTable(t *testing.T) {
	stub := &stubThresholds{table: referenceTestTable()}
	srv := &server{deps: thresholdDeps(stub)}

	res, err := srv.GetThresholdTable(humanworkContext(t, GetThresholdTableProcedure), &humanworkv1.GetThresholdTableRequest{})
	if err != nil {
		t.Fatalf("GetThresholdTable: %v", err)
	}
	got := res.GetTable()
	if got == nil {
		t.Fatal("GetThresholdTable returned no table")
	}
	if got.GetTableId() != "hcmnext.rules.promotion_approval_threshold" {
		t.Fatalf("table_id = %q", got.GetTableId())
	}
	if got.GetVersionRef() != "2026.1" {
		t.Fatalf("version_ref = %q, want the rules table version", got.GetVersionRef())
	}
	if got.GetTenantId() != transporttest.Tenant {
		t.Fatalf("tenant_id = %q, want the caller's tenant", got.GetTenantId())
	}
	if len(got.GetRows()) != 2 || got.GetHitPolicy() != humanworkv1.ThresholdHitPolicy_THRESHOLD_HIT_POLICY_FIRST {
		t.Fatalf("table = %v, want the stub's rows under FIRST", got)
	}
	if stub.recordedCalls() != 1 {
		t.Fatalf("port calls = %d, want exactly one read", stub.recordedCalls())
	}

	// A named table the port does not know is not found, never empty.
	if _, err := srv.GetThresholdTable(humanworkContext(t, GetThresholdTableProcedure),
		&humanworkv1.GetThresholdTableRequest{TableId: "no-such-table"}); !isNotFound(err) {
		t.Fatalf("unknown table err = %v, want NOT_FOUND", err)
	}
	// A scope naming another tenant is a hidden resource, like every other
	// read on this service.
	foreign := &commonv1.ScopeContext{TenantId: "tenant-somebody-elses"}
	if _, err := srv.GetThresholdTable(humanworkContext(t, GetThresholdTableProcedure),
		&humanworkv1.GetThresholdTableRequest{Scope: foreign}); !isNotFound(err) {
		t.Fatalf("foreign scope err = %v, want NOT_FOUND", err)
	}
	// Without the port the method is unavailable, not absent.
	bare := &server{deps: thresholdDeps(nil)}
	if _, err := bare.GetThresholdTable(humanworkContext(t, GetThresholdTableProcedure),
		&humanworkv1.GetThresholdTableRequest{}); !isUnavailable(err) {
		t.Fatalf("unwired port err = %v, want UNAVAILABLE", err)
	}
	if stub.recordedCalls() != 2 {
		t.Fatalf("refused reads reached the port: calls = %d", stub.recordedCalls())
	}
}

// TestGetThresholdTableDeniesWithoutPortAccess proves the wire-level gate
// runs before the read: a caller without the capability learns nothing,
// including whether a table exists.
func TestGetThresholdTableDeniesWithoutPortAccess(t *testing.T) {
	stub := &stubThresholds{table: referenceTestTable()}
	deps := thresholdDeps(stub)
	deps.Authorize = func(context.Context, *trust.Principal, string) bool { return false }
	srv := &server{deps: deps}
	_, err := srv.GetThresholdTable(humanworkContext(t, GetThresholdTableProcedure), &humanworkv1.GetThresholdTableRequest{})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied {
		t.Fatalf("err = %v, want PERMISSION_DENIED", err)
	}
	if stub.recordedCalls() != 0 {
		t.Fatal("a denied read reached the port")
	}
}

func isNotFound(err error) bool {
	owned, ok := envelope.As(err)
	return ok && owned.Code() == envelope.CodeNotFound
}

func isUnavailable(err error) bool {
	owned, ok := envelope.As(err)
	return ok && owned.Code() == envelope.CodeUnavailable
}
