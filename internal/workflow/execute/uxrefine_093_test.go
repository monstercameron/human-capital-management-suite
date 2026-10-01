package execute_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_UXBLIND_093_RealPG proves the production execute driver persists
// the payroll confirmation subscription as part of the real PostgreSQL
// advancement, then closes that exact subscription on a correlated provider
// signal before the downstream access wait and terminal can proceed.
func TestTodo_UXBLIND_093_RealPG(t *testing.T) {
	f := newPromotionFixture(t, "uxblind-093-real-pg")
	driver := newPromotionDriver(t, f, promotionRunner{payroll: "PASS", recon: "PASS"}, &repairRequester{})
	parked, err := driver.Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatalf("execute promotion: %v", err)
	}
	if parked.Status != execute.StatusParked {
		t.Fatalf("promotion status = %s, want PARKED at payroll confirmation", parked.Status)
	}
	var open int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM workflow_signal_subscription WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND subscription_state = 'OPEN'`, f.tenantID, parked.Start.InstanceID, promotionexec.NodeAwaitPayrollConfirmation).Scan(&open); err != nil {
		t.Fatalf("read payroll confirmation subscription: %v", err)
	}
	if open != 1 {
		t.Fatalf("open payroll confirmation subscriptions = %d, want 1", open)
	}

	afterPayroll := confirmProviderWait(t, f, driver, parked.Start.InstanceID, promotionexec.NodeAwaitPayrollConfirmation, "hcmnext.integrations.payroll")
	if afterPayroll.Status != execute.StatusParked {
		t.Fatalf("after payroll confirmation status = %s, want PARKED at access confirmation", afterPayroll.Status)
	}
	var satisfied int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM workflow_signal_subscription WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND subscription_state = 'SATISFIED'`, f.tenantID, parked.Start.InstanceID, promotionexec.NodeAwaitPayrollConfirmation).Scan(&satisfied); err != nil {
		t.Fatalf("read satisfied payroll confirmation subscription: %v", err)
	}
	if satisfied != 1 {
		t.Fatalf("satisfied payroll confirmation subscriptions = %d, want 1", satisfied)
	}
	if done := confirmProviderWait(t, f, driver, parked.Start.InstanceID, promotionexec.NodeAwaitAccessConfirmation, "hcmnext.integrations.iam"); done.Status != execute.StatusParked {
		t.Fatalf("after access confirmation status = %s, want PARKED at acknowledgement", done.Status)
	}
	if got := instance(t, f, parked.Start.InstanceID); got.RuntimeStatus == runtime.InstanceCompleted {
		t.Fatal("promotion completed before its acknowledgement obligation was discharged")
	}
}
