package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// fakeManagers answers CurrentManagerOf from a fixed table so each routing
// rule can be exercised with exactly the relationship it names.
type fakeManagers struct {
	answer ManagerOf
	err    error
	asked  string
}

func (f *fakeManagers) CurrentManagerOf(_ context.Context, _ workitem.Executor, _ uuid.UUID, workerRef string) (ManagerOf, error) {
	f.asked = workerRef
	return f.answer, f.err
}

func routingRequest(requester string) execute.WorkItemRequest {
	revision := proposalRevisionFixture()
	revision.CreatedBy = intent.PrincipalReference{PrincipalID: requester}
	revision.Subjects = []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "worker-uuid-linh", AuthorityDomain: "PEOPLE"}}
	return execute.WorkItemRequest{
		Continuation: runtime.ContinuationRecord{TenantID: uuid.New(), TargetNodeID: promotionexec.NodeApproveFinance},
		Proposal:     runtime.ProposalBinding{Revision: revision},
		SubjectRefs:  []string{"worker-uuid-linh"},
	}
}

// TestPromotionApproverRoutingFollowsTheReferenceWorkflow pins PROMOUX-015's
// routing rules: finance to the configured finance partner, manager to
// CurrentManagerOf(worker), the corpus fallback, UNRESOLVED_MANAGER, and the
// separation refusals. Every case's fixture contains exactly the relationship
// it tests.
func TestPromotionApproverRoutingFollowsTheReferenceWorkflow(t *testing.T) {
	ctx := context.Background()
	const requester = "hc-004-darius-bennett"

	t.Run("finance partner and current manager route to real principals", func(t *testing.T) {
		managers := &fakeManagers{answer: ManagerOf{InGraph: true, SubjectKey: "hc-051-linh-tran", ManagerPrincipal: "hc-050-rafael-torres"}}
		factory := promotionWorkItems{approver: "principal:promotion-approver", financePartner: "hc-054-thomas-baker", managers: managers, plan: PLAN_EXECUTE}
		route, err := factory.resolveApprovers(ctx, nil, routingRequest(requester))
		if err != nil {
			t.Fatalf("resolveApprovers: %v", err)
		}
		if managers.asked != "worker-uuid-linh" {
			t.Fatalf("CurrentManagerOf asked for %q, want the EMPLOYMENT subject", managers.asked)
		}
		if route.finance.principal != "hc-054-thomas-baker" || route.finance.termRef != termFinancePartner {
			t.Fatalf("finance route = %+v, want the configured finance partner", route.finance)
		}
		if route.manager.principal != "hc-050-rafael-torres" || route.manager.termRef != termCurrentManager || route.manager.directoryVersion != directoryCurrentManager {
			t.Fatalf("manager route = %+v, want the worker's current manager", route.manager)
		}
	})

	t.Run("a corpus subject outside the relationship graph keeps the class-scoped configured approver", func(t *testing.T) {
		factory := promotionWorkItems{approver: "principal:base", managers: &fakeManagers{}, plan: PLAN_EXECUTE}
		route, err := factory.resolveApprovers(ctx, nil, routingRequest(requester))
		if err != nil {
			t.Fatalf("resolveApprovers: %v", err)
		}
		wantFinance, _ := promotionexec.FinanceApproverFor("principal:base")
		wantManager, _ := promotionexec.ManagerApproverFor("principal:base")
		if route.finance.principal != wantFinance || route.manager.principal != wantManager || route.manager.termRef != termConfiguredApprover {
			t.Fatalf("route = %+v, want the class-scoped derivations %q/%q", route, wantFinance, wantManager)
		}
	})

	t.Run("a graph subject whose manager does not resolve is refused, never guessed", func(t *testing.T) {
		factory := promotionWorkItems{approver: "principal:base", financePartner: "hc-054-thomas-baker",
			managers: &fakeManagers{answer: ManagerOf{InGraph: true, SubjectKey: "hc-001-amina-rahman"}}, plan: PLAN_EXECUTE}
		if _, err := factory.resolveApprovers(ctx, nil, routingRequest(requester)); !errors.Is(err, ErrUnresolvedManager) {
			t.Fatalf("resolveApprovers = %v, want ErrUnresolvedManager", err)
		}
	})

	t.Run("both approvals routed to one principal are refused", func(t *testing.T) {
		factory := promotionWorkItems{approver: "principal:base", financePartner: "hc-054-thomas-baker",
			managers: &fakeManagers{answer: ManagerOf{InGraph: true, SubjectKey: "hc-056-peter-murphy", ManagerPrincipal: "hc-054-thomas-baker"}}, plan: PLAN_EXECUTE}
		_, err := factory.resolveApprovers(ctx, nil, routingRequest(requester))
		if !errors.Is(err, ErrApproverSeparation) || !errors.Is(err, approverclass.ErrSharedOwner) {
			t.Fatalf("resolveApprovers = %v, want ErrApproverSeparation wrapping ErrSharedOwner", err)
		}
	})

	t.Run("the requester may not be routed an approval", func(t *testing.T) {
		factory := promotionWorkItems{approver: "principal:base", financePartner: "hc-054-thomas-baker",
			managers: &fakeManagers{answer: ManagerOf{InGraph: true, SubjectKey: "hc-051-linh-tran", ManagerPrincipal: "hc-050-rafael-torres"}}, plan: PLAN_EXECUTE}
		_, err := factory.resolveApprovers(ctx, nil, routingRequest("hc-050-rafael-torres"))
		if !errors.Is(err, approverclass.ErrRequesterApprover) {
			t.Fatalf("resolveApprovers = %v, want ErrRequesterApprover", err)
		}
	})

	t.Run("the subject may not be routed an approval", func(t *testing.T) {
		factory := promotionWorkItems{approver: "principal:base", financePartner: "hc-051-linh-tran",
			managers: &fakeManagers{answer: ManagerOf{InGraph: true, SubjectKey: "hc-051-linh-tran", ManagerPrincipal: "hc-050-rafael-torres"}}, plan: PLAN_EXECUTE}
		_, err := factory.resolveApprovers(ctx, nil, routingRequest(requester))
		if !errors.Is(err, approverclass.ErrSubjectApprover) {
			t.Fatalf("resolveApprovers = %v, want ErrSubjectApprover", err)
		}
	})

	t.Run("a resolver failure is reported, not routed around", func(t *testing.T) {
		boom := errors.New("directory offline")
		factory := promotionWorkItems{approver: "principal:base", financePartner: "hc-054-thomas-baker", managers: &fakeManagers{err: boom}, plan: PLAN_EXECUTE}
		if _, err := factory.resolveApprovers(ctx, nil, routingRequest(requester)); !errors.Is(err, boom) {
			t.Fatalf("resolveApprovers = %v, want the resolver's own error", err)
		}
	})

	t.Run("the subject falls back to the driver's subject reference", func(t *testing.T) {
		req := routingRequest(requester)
		req.Proposal.Revision.Subjects = nil
		req.SubjectRefs = []string{"worker-from-driver"}
		if got := employmentSubject(req); got != "worker-from-driver" {
			t.Fatalf("employmentSubject = %q, want the driver subject", got)
		}
		req.SubjectRefs = nil
		if got := employmentSubject(req); got != "" {
			t.Fatalf("employmentSubject with no subjects = %q, want empty", got)
		}
	})
}

// TestJourneyWorkerManagersResolvesTheSeededReportingLine reads the real
// demo workforce from PostgreSQL: Linh Tran reports to Rafael Torres, the CEO
// reports to a board sentinel that names no worker, and a corpus key is not a
// worker of the graph at all.
func TestJourneyWorkerManagersResolvesTheSeededReportingLine(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'promoux015-managers', 'cell-local', 'PROMOUX-015 managers', 'ACTIVE', $2)`, tenantID, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := demoworkforce.SeedOrganization(ctx, tx, tenantID); err != nil {
		t.Fatalf("SeedOrganization: %v", err)
	}
	if _, err := demoworkforce.Seed(ctx, tx, tenantID); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	var managers JourneyWorkerManagers
	linh, err := managers.CurrentManagerOf(ctx, tx, tenantID, "hc-051-linh-tran")
	if err != nil {
		t.Fatalf("CurrentManagerOf(linh): %v", err)
	}
	if !linh.InGraph || linh.SubjectKey != "hc-051-linh-tran" || linh.ManagerPrincipal != "hc-050-rafael-torres" {
		t.Fatalf("CurrentManagerOf(linh) = %+v, want Rafael Torres", linh)
	}
	ceo, err := managers.CurrentManagerOf(ctx, tx, tenantID, "hc-001-amina-rahman")
	if err != nil {
		t.Fatalf("CurrentManagerOf(ceo): %v", err)
	}
	if !ceo.InGraph || ceo.ManagerPrincipal != "" {
		t.Fatalf("CurrentManagerOf(ceo) = %+v, want in the graph with no resolvable manager", ceo)
	}
	corpus, err := managers.CurrentManagerOf(ctx, tx, tenantID, "jane-doe")
	if err != nil {
		t.Fatalf("CurrentManagerOf(corpus): %v", err)
	}
	if corpus.InGraph {
		t.Fatalf("CurrentManagerOf(corpus) = %+v, want outside the graph", corpus)
	}
}
