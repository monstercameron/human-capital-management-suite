package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type recordedTerminal struct{ err error }

func (w recordedTerminal) Write(context.Context, dbport.Tx, execute.TerminalWriteRequest) (idempotency.ResultIdentity, error) {
	return idempotency.ResultIdentity{}, w.err
}

// TestTodo_WF_NOTIFY_001_Integration routes an approval and ends a run through the
// composed wrappers, against PostgreSQL: the requester hears that the request
// is with someone and that it finished, the approver still gets only the
// approval notice, and a replay adds nothing.
func TestTodo_WF_NOTIFY_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	recorder := &observetest.Recorder{}
	ctx := recorder.Context(context.Background())
	tenant, instance := uuid.New(), uuid.New()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,'naas-status','cell-local','NaaS status','ACTIVE',$2)`, tenant, at)
	db.Exec(t, `INSERT INTO workflow_instance (tenant_id,instance_id,cell_id,workflow_id,workflow_version,compiled_plan_hash,execution_mode,runtime_status,input_ref,current_node_ids,correlation_id,created_at)
	VALUES ($1,$2,'cell-local','wf.naas',1,'0000000000000000000000000000000000000000000000000000000000000000','EXECUTE','RUNNING','sha256:input',ARRAY['approval'],'corr-status',$3)`, tenant, instance, at)
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	proposal := runtime.ProposalBinding{Revision: proposalRevisionFixture()}
	requester := proposal.Revision.CreatedBy.PrincipalID
	factory := requesterStatusWorkItems{next: notifyingWorkItems{next: promotionWorkItems{approver: "principal:naas-approver", plan: PLAN_PROTOTYPE}}}
	request := execute.WorkItemRequest{WorkItemID: uuid.New(),
		Continuation: runtime.ContinuationRecord{TenantID: tenant, InstanceID: instance, TargetNodeID: prototype.NodeApproval},
		Proposal:     proposal, CorrelationID: "corr-status", SubjectRefs: []string{"worker:jane"}, CreatedAt: at}
	item, err := factory.CreateAndRoute(ctx, tx, request)
	if err != nil {
		t.Fatal(err)
	}
	owner := item.Assignment.ChosenOwner
	routing := recorder.Named("workflow.notification.requester.route")
	if len(routing) != 1 || routing[0].Ended != 1 || routing[0].Outcome != observe.OutcomeSuccess || routing[0].Attrs[observe.KeyInstance] != instance.String() {
		t.Fatalf("requester routing telemetry: %+v", routing)
	}

	statuses, err := (inbox.Store{}).WorkflowStatusNotices(ctx, tx, tenant, requester, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Event != inbox.StatusInReview || statuses[0].EventRef != item.WorkItemID.String() ||
		statuses[0].InstanceID != instance || statuses[0].CorrelationID != "corr-status" || statuses[0].ReadState != inbox.Unread {
		t.Fatalf("requester status after routing: %+v", statuses)
	}
	if ownerStatuses, err := (inbox.Store{}).WorkflowStatusNotices(ctx, tx, tenant, owner, 20); err != nil || len(ownerStatuses) != 0 {
		t.Fatalf("approver received a requester status: %+v, %v", ownerStatuses, err)
	}
	// The approval queue reader selects its own template, so a status notice
	// never appears there with a missing work item.
	if approvals, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenant, requester, 20); err != nil || len(approvals) != 0 {
		t.Fatalf("status notice leaked into the approval feed: %+v, %v", approvals, err)
	}
	if approvals, err := (inbox.Store{}).WorkflowNotices(ctx, tx, tenant, owner, 20); err != nil || len(approvals) != 1 || approvals[0].Purpose != notifyplan.PurposeApproval {
		t.Fatalf("approver notice: %+v, %v", approvals, err)
	}

	// Routing stamps the work item with the composition clock, so the end of
	// the run is placed relative to it rather than to the fixture instant.
	terminal := requesterStatusTerminal{next: recordedTerminal{}}
	end := execute.TerminalWriteRequest{TenantID: tenant, InstanceID: instance, WorkflowID: "wf.naas", Proposal: proposal,
		TerminalCode: "PROMOTION_APPROVED", CorrelationID: "corr-status", RecordedAt: item.RecordedAt.Add(time.Hour), EndNodeID: "end_approved"}
	for range 2 {
		if _, err := terminal.Write(ctx, tx, end); err != nil {
			t.Fatal(err)
		}
	}
	finished := recorder.Named("workflow.notification.requester.terminal")
	if len(finished) != 2 {
		t.Fatalf("terminal telemetry count = %d, want 2", len(finished))
	}
	for _, op := range finished {
		if op.Ended != 1 || op.Outcome != observe.OutcomeSuccess || op.Attrs[observe.KeyCorrelation] != "corr-status" || op.Attrs[observe.KeyTenant] != tenant.String() {
			t.Fatalf("requester terminal telemetry: %+v", op)
		}
	}
	statuses, err = (inbox.Store{}).WorkflowStatusNotices(ctx, tx, tenant, requester, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].Event != inbox.StatusFinished || statuses[0].EventRef != "end_approved" || statuses[1].Event != inbox.StatusInReview {
		t.Fatalf("requester statuses after the terminal write, newest first: %+v", statuses)
	}
}

// TestTodo_WF_NOTIFY_001 pins who is and is not told, without a database: a failed
// terminal write, a requester who owns the routed work and a request with no
// recorded requester all publish nothing, and so never need a transaction.
func TestTodo_WF_NOTIFY_001(t *testing.T) {
	ctx := context.Background()
	refused := errors.New("terminal refused")
	if _, err := (requesterStatusTerminal{next: recordedTerminal{err: refused}}).Write(ctx, nil, execute.TerminalWriteRequest{}); !errors.Is(err, refused) {
		t.Fatalf("terminal error was replaced: %v", err)
	}
	proposal := runtime.ProposalBinding{Revision: proposalRevisionFixture()}
	requester := proposal.Revision.CreatedBy.PrincipalID
	base := requesterStatus{stepType: "APPROVAL", moment: notifyplan.MomentRouted, event: inbox.StatusInReview, eventRef: "item",
		tenant: uuid.New(), instance: uuid.New(), proposal: proposal, correlation: "corr", at: time.Now()}

	sameOwner := base
	sameOwner.skip = requester
	anonymous := base
	anonymous.proposal = runtime.ProposalBinding{}
	undeclared := base
	undeclared.stepType = "WAIT"
	for name, status := range map[string]requesterStatus{"requester owns the work": sameOwner, "no recorded requester": anonymous, "step declares no requester notice": undeclared} {
		if err := publishRequesterStatus(ctx, nil, status); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := publishRequesterStatus(ctx, nil, base); err == nil {
		t.Fatal("a declared status notice was accepted without a transaction")
	}
}

// The exported wrappers are what a composition other than Promotion's uses;
// they must compose the same two layers the served composition does.
func TestWithWorkNotificationsComposesOwnerAndRequesterNotices(t *testing.T) {
	outer, ok := WithWorkNotifications(promotionWorkItems{}).(requesterStatusWorkItems)
	if !ok {
		t.Fatalf("outer wrapper = %T, want the requester status layer", WithWorkNotifications(promotionWorkItems{}))
	}
	if _, ok := outer.next.(notifyingWorkItems); !ok {
		t.Fatalf("inner wrapper = %T, want the owner notification layer", outer.next)
	}
	if _, ok := WithRequesterStatusTerminal(recordedTerminal{}).(requesterStatusTerminal); !ok {
		t.Fatal("terminal wrapper is not the requester status layer")
	}
}
