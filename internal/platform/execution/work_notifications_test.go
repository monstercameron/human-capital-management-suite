package execution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_NAAS_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	recorder := &observetest.Recorder{}
	ctx := recorder.Context(context.Background())
	tenant, instance := uuid.New(), uuid.New()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'naas-routing','cell-local','NaaS routing','ACTIVE',$2)`, tenant, at)
	db.Exec(t, `INSERT INTO workflow_instance (tenant_id,instance_id,cell_id,workflow_id,workflow_version,compiled_plan_hash,execution_mode,runtime_status,input_ref,current_node_ids,correlation_id,created_at)
	VALUES ($1,$2,'cell-local','wf.naas',1,'0000000000000000000000000000000000000000000000000000000000000000','EXECUTE','RUNNING','sha256:input',ARRAY['approval'],'corr-naas',$3)`, tenant, instance, at)
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	factory := notifyingWorkItems{next: promotionWorkItems{approver: "principal:naas-approver", plan: PLAN_PROTOTYPE}}
	item, err := factory.CreateAndRoute(ctx, tx, execute.WorkItemRequest{WorkItemID: uuid.New(),
		Continuation: runtime.ContinuationRecord{TenantID: tenant, InstanceID: instance, TargetNodeID: prototype.NodeApproval},
		Proposal:     runtime.ProposalBinding{Revision: proposalRevisionFixture()}, CorrelationID: "corr-naas", SubjectRefs: []string{"worker:jane"}, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"workflow.notification.route", "workflow.notification.publish"} {
		ops := recorder.Named(operation)
		if len(ops) != 1 || ops[0].Ended != 1 || ops[0].Outcome != observe.OutcomeSuccess || ops[0].Attrs[observe.KeyCorrelation] != "corr-naas" || ops[0].Attrs[observe.KeyTenant] != tenant.String() {
			t.Fatalf("notification telemetry %s: %+v", operation, ops)
		}
	}
	rows, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenant, item.Assignment.ChosenOwner, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].WorkItemID != item.WorkItemID || rows[0].Purpose != "APPROVAL" {
		t.Fatalf("routing notices: %+v", rows)
	}
	if err := publishWorkNotification(ctx, tx, item); err != nil {
		t.Fatalf("replay: %v", err)
	}
	for _, owner := range []string{"", "principal:not-in-resolution"} {
		invalidItem := item
		invalidItem.Assignment.ChosenOwner = owner
		if err := publishWorkNotification(ctx, tx, invalidItem); err == nil {
			t.Fatalf("published notice for unresolved owner %q", owner)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowNotificationRequiresTransaction(t *testing.T) {
	// A nil next factory would panic if routing happened before validating the
	// transactional executor. Reject it before any factory side effect.
	if _, err := (notifyingWorkItems{}).CreateAndRoute(context.Background(), nil, execute.WorkItemRequest{}); err == nil {
		t.Fatal("routing accepted a non-transactional executor")
	}
}
