package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/audience"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func TestTodo_REV_011_01_Integration(t *testing.T) {
	testWorkflowMessageInboxIntegration(t)
}

func TestTodo_NAAS_001_Integration(t *testing.T) {
	testWorkflowMessageInboxIntegration(t)
}

func testWorkflowMessageInboxIntegration(t *testing.T) {
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
	otherRows, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenant, "principal:other-approver", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherRows) != 0 {
		t.Fatalf("notice visible to another recipient: %+v", otherRows)
	}
	if err := publishWorkNotification(ctx, tx, item, factory.next); err != nil {
		t.Fatalf("replay: %v", err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 AND recipient_message_id=$2`, tenant, rows[0].RecipientMessageID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("replayed notice rows=%d, want exactly one", count)
	}
	ownerRef := values.EntityRef{Tenant: values.TenantId(tenant.String()), Kind: "principal", Id: uuid.NewSHA1(tenant, []byte("notification-principal/v1\x00"+item.Assignment.ChosenOwner)).String()}
	unrelated := values.EntityRef{Tenant: ownerRef.Tenant, Kind: "principal", Id: uuid.NewSHA1(tenant, []byte("unrelated-audience-principal")).String()}
	unrelatedSpec := audience.AudienceSpec{ExplicitSubjects: []values.EntityRef{unrelated}}
	for name, resolution := range map[string]audience.Resolution{
		"unrelated principal": {Principals: []values.EntityRef{unrelated}, Expression: unrelatedSpec.Expression(), ResultDigest: "sha256:" + strings.Repeat("0", 64), ResolvedAt: values.NewInstant(at)},
		"empty digest":        {Principals: []values.EntityRef{ownerRef}, Expression: (audience.AudienceSpec{ExplicitSubjects: []values.EntityRef{ownerRef}}).Expression(), ResolvedAt: values.NewInstant(at)},
	} {
		if err := execute.PublishWorkItemMessage(ctx, tx, item, resolution); err == nil {
			t.Errorf("accepted %s audience resolution", name)
		}
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 AND recipient_message_id=$2`, tenant, rows[0].RecipientMessageID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rejected forged resolution changed settled inbox rows to %d", count)
	}
	staleAuthority := promotionWorkItems{approver: "principal:replacement-owner", plan: PLAN_PROTOTYPE}
	if err := publishWorkNotification(ctx, tx, item, staleAuthority); err == nil {
		t.Fatal("published notice after the current route changed away from the stored recipient")
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM inbox_record WHERE tenant_id=$1 AND recipient_message_id=$2`, tenant, rows[0].RecipientMessageID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("stale-route refusal changed the settled inbox rows to %d", count)
	}
	for _, owner := range []string{"", "principal:not-in-resolution"} {
		invalidItem := item
		invalidItem.Assignment.ChosenOwner = owner
		if err := publishWorkNotification(ctx, tx, invalidItem, factory.next); err == nil {
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
