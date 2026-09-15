package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func wfstep003Query(node, authority, decider string, roles ...string) app.ApprovalAuthorityQuery {
	return app.ApprovalAuthorityQuery{
		TenantID: uuid.New(), SubjectID: "worker-uuid-linh", AuthorityPrincipalID: authority,
		DeciderPrincipalID: decider, DeciderRoles: roles, At: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		Item: workitem.WorkItem{NodeID: node, Kind: workitem.KindApproval, OrganizationScopeID: "org:people"},
	}
}

// TestTodo_WF_STEP_003_PromotionApprovalAuthority pins the decision-time
// authority answer to the routing rules it shares with CreateAndRoute.
func TestTodo_WF_STEP_003_PromotionApprovalAuthority(t *testing.T) {
	ctx := context.Background()
	const finance, manager = "hc-054-thomas-baker", "hc-050-rafael-torres"

	t.Run("the routed current manager is active", func(t *testing.T) {
		managers := &fakeManagers{answer: ManagerOf{InGraph: true, SubjectKey: "hc-051-linh-tran", ManagerPrincipal: manager}}
		authority := NewPromotionApprovalAuthority(PromotionExecutionConfig{FinancePartnerPrincipalID: finance, Managers: managers, Plan: PLAN_EXECUTE})
		got, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveManager, manager, manager))
		if err != nil {
			t.Fatalf("CurrentApprovalAuthority: %v", err)
		}
		want := promotionexec.CurrentApprovalAuthority{PrincipalID: manager, Class: promotionexec.AuthorityClassCurrentManager,
			AuthorityRef: termCurrentManager, Scope: "org:people", Active: true}
		if got != want || managers.asked != "worker-uuid-linh" {
			t.Fatalf("authority = %+v (asked %q), want %+v for the subject", got, managers.asked, want)
		}
	})

	t.Run("a changed manager is not the routed authority", func(t *testing.T) {
		managers := &fakeManagers{answer: ManagerOf{InGraph: true, SubjectKey: "hc-051-linh-tran", ManagerPrincipal: "hc-004-darius-bennett"}}
		authority := NewPromotionApprovalAuthority(PromotionExecutionConfig{Managers: managers, Plan: PLAN_EXECUTE})
		got, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveManager, manager, manager))
		if err != nil || got.Active || got.PrincipalID != "hc-004-darius-bennett" {
			t.Fatalf("authority = %+v, %v; want the new manager, inactive for %s", got, err, manager)
		}
	})

	t.Run("an unresolved manager has no holder", func(t *testing.T) {
		managers := &fakeManagers{answer: ManagerOf{InGraph: true, SubjectKey: "hc-051-linh-tran"}}
		authority := NewPromotionApprovalAuthority(PromotionExecutionConfig{Managers: managers, Plan: PLAN_EXECUTE})
		got, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveManager, manager, manager))
		if err != nil || got.Active || got.PrincipalID != "" || got.Class != promotionexec.AuthorityClassCurrentManager {
			t.Fatalf("authority = %+v, %v; want no current holder", got, err)
		}
	})

	t.Run("a manager lookup failure is an error", func(t *testing.T) {
		boom := errors.New("directory offline")
		authority := NewPromotionApprovalAuthority(PromotionExecutionConfig{Managers: &fakeManagers{err: boom}, Plan: PLAN_EXECUTE})
		if _, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveManager, manager, manager)); !errors.Is(err, boom) {
			t.Fatalf("CurrentApprovalAuthority = %v, want the lookup error", err)
		}
	})

	t.Run("out-of-graph subjects fall back to the configured manager approver", func(t *testing.T) {
		authority := NewPromotionApprovalAuthority(PromotionExecutionConfig{Managers: &fakeManagers{}, Plan: PLAN_EXECUTE})
		got, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveManager, defaultManagerApproverPrincipalID, defaultManagerApproverPrincipalID))
		if err != nil || !got.Active || got.PrincipalID != defaultManagerApproverPrincipalID || got.AuthorityRef != termCurrentManager {
			t.Fatalf("authority = %+v, %v; want the configured default manager approver", got, err)
		}
	})

	t.Run("the finance partner needs the finance floor role when deciding as themselves", func(t *testing.T) {
		authority := NewPromotionApprovalAuthority(PromotionExecutionConfig{FinancePartnerPrincipalID: finance, Plan: PLAN_EXECUTE})
		held, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveFinance, finance, finance, promotionexec.FinanceApprovalAuthorityFloor))
		if err != nil || !held.Active || held.AuthorityRef != termFinancePartner {
			t.Fatalf("with the role = %+v, %v; want active", held, err)
		}
		revoked, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveFinance, finance, finance, "comp_admin"))
		if err != nil || revoked.Active {
			t.Fatalf("without the role = %+v, %v; want inactive", revoked, err)
		}
		delegated, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveFinance, finance, "principal:delegate"))
		if err != nil || !delegated.Active {
			t.Fatalf("a delegate deciding under the partner's authority = %+v, %v; want active", delegated, err)
		}
		moved := NewPromotionApprovalAuthority(PromotionExecutionConfig{FinancePartnerPrincipalID: "hc-099-new-partner", Plan: PLAN_EXECUTE})
		if got, _ := moved.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveFinance, finance, finance, promotionexec.FinanceApprovalAuthorityFloor)); got.Active {
			t.Fatalf("after the finance partner moved = %+v, want inactive", got)
		}
	})

	t.Run("the derived finance approver needs no role", func(t *testing.T) {
		authority := NewPromotionApprovalAuthority(PromotionExecutionConfig{ApproverPrincipalID: "principal:base", Plan: PLAN_EXECUTE})
		derived, err := promotionexec.FinanceApproverFor("principal:base")
		if err != nil {
			t.Fatalf("FinanceApproverFor: %v", err)
		}
		got, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query(promotionexec.NodeApproveFinance, derived, derived))
		if err != nil || !got.Active || got.AuthorityRef != termConfiguredApprover {
			t.Fatalf("authority = %+v, %v; want the derived approver active", got, err)
		}
	})

	t.Run("a non-promotion node is refused", func(t *testing.T) {
		authority := NewPromotionApprovalAuthority(PromotionExecutionConfig{})
		if _, err := authority.CurrentApprovalAuthority(ctx, nil, wfstep003Query("approval", manager, manager)); err == nil {
			t.Fatal("CurrentApprovalAuthority(prototype node) succeeded, want a refusal")
		}
	})

	t.Run("defaults mirror NewPromotionExecution", func(t *testing.T) {
		prototypePlan := NewPromotionApprovalAuthority(PromotionExecutionConfig{})
		if prototypePlan.routes.approver != defaultApproverPrincipalID || prototypePlan.routes.managerApprover != defaultApproverPrincipalID {
			t.Fatalf("prototype defaults = %+v", prototypePlan.routes)
		}
		executePlan := NewPromotionApprovalAuthority(PromotionExecutionConfig{Plan: PLAN_EXECUTE, ManagerApproverPrincipalID: ""})
		if executePlan.routes.managerApprover != defaultManagerApproverPrincipalID {
			t.Fatalf("execute defaults = %+v", executePlan.routes)
		}
		explicit := NewPromotionApprovalAuthority(PromotionExecutionConfig{ApproverPrincipalID: "a", ManagerApproverPrincipalID: "m"})
		if explicit.routes.approver != "a" || explicit.routes.managerApprover != "m" {
			t.Fatalf("explicit = %+v", explicit.routes)
		}
	})
}
