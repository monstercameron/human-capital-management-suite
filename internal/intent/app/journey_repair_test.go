package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator/workflowcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// UXLIVE-006: the server half of the governed repair projection. What is
// proved here is that the four refusals are four distinct facts derived in
// the right order, that the requirements come from the operator policy
// rather than from prose in this package, and that INDETERMINATE survives
// the trip from the REPAIR mode's own verdict to the page.

type repairAuthorityStub struct {
	grant *jit.Grant
	err   error
	// asked records what the projection asked about, so a test can prove it
	// asked the door's own resolver the door's own question.
	askedKind     operator.Kind
	askedOperator string
}

func (s *repairAuthorityStub) ResolveAuthority(
	_ context.Context, _ values.TenantId, operatorID string, kind operator.Kind, _ string,
) (workflowcontrol.Authority, error) {
	s.askedKind, s.askedOperator = kind, operatorID
	if s.err != nil {
		return workflowcontrol.Authority{}, s.err
	}
	return workflowcontrol.Authority{JIT: s.grant}, nil
}

func repairEngine(door *workflowcontrol.RepairController, authority workflowcontrol.AuthorityResolver) *journeyEngine {
	return &journeyEngine{
		now:             func() time.Time { return time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC) },
		repair:          door,
		repairAuthority: authority,
	}
}

// repairDoorStub is a non-nil controller standing for "this deployment
// composed the repair door". Nothing here calls into it: the projection only
// asks whether it exists.
func repairDoorStub() *workflowcontrol.RepairController { return &workflowcontrol.RepairController{} }

// TestTodo_UXLIVE_006 is the primary red/green test: the four refusals are
// four distinct answers, taken in the order a person would ask them.
func TestTodo_UXLIVE_006(t *testing.T) {
	ctx := context.Background()
	principal := gatePrincipal(t, "promotion_operator")
	held := &repairAuthorityStub{grant: &jit.Grant{Role: jit.RoleIntegrityRepair}}

	// A journey that is not asking for a repair.
	healthy := repairEngine(repairDoorStub(), held).
		previewRepair(ctx, principal, workspace.JourneyStageProposed, 7)
	if healthy.UnavailableReasonRef != reasonRepairNotRequired {
		t.Fatalf("a healthy journey = %q, want the not-required refusal", healthy.UnavailableReasonRef)
	}

	// A deployment that cannot repair at all is not the same as "you may
	// not": it is the difference between an authority problem the reader can
	// solve and one they cannot.
	noDoor := repairEngine(nil, held).
		previewRepair(ctx, principal, workspace.JourneyStageRepairRequired, 7)
	if noDoor.UnavailableReasonRef != reasonRepairDoorUnavailable {
		t.Fatalf("a cell with no repair door = %q, want the door-unavailable refusal", noDoor.UnavailableReasonRef)
	}

	// A viewer holding no grant.
	unauthorized := repairEngine(repairDoorStub(), &repairAuthorityStub{}).
		previewRepair(ctx, principal, workspace.JourneyStageRepairRequired, 7)
	if unauthorized.UnavailableReasonRef != reasonRepairAuthorityRequired {
		t.Fatalf("a viewer with no grant = %q, want the authority refusal", unauthorized.UnavailableReasonRef)
	}

	// A viewer who does hold it is told where the repair actually runs.
	authorized := repairEngine(repairDoorStub(), held).
		previewRepair(ctx, principal, workspace.JourneyStageRepairRequired, 7)
	if authorized.UnavailableReasonRef != reasonRepairPlanRequired {
		t.Fatalf("an authorized viewer = %q, want the plan-required refusal", authorized.UnavailableReasonRef)
	}

	// The requirements are the door's own, not this package's opinion.
	policy, ok := repairPolicy()
	if !ok {
		t.Fatalf("the repair kind has no operator policy; the projection would be describing nothing")
	}
	if authorized.RequiresDualControl != policy.DualControl || authorized.RequiresSimulation != policy.SimulationRequired {
		t.Fatalf("stated requirements (dual=%v, sim=%v) disagree with the policy (dual=%v, sim=%v)",
			authorized.RequiresDualControl, authorized.RequiresSimulation, policy.DualControl, policy.SimulationRequired)
	}
	if len(authorized.AuthorityRoleRefs) != len(policy.Roles) || len(policy.Roles) == 0 {
		t.Fatalf("named roles %v do not match the policy's %v", authorized.AuthorityRoleRefs, policy.Roles)
	}
	if authorized.CurrentGovernanceVersion != 7 {
		t.Fatalf("the preview lost the journey's governance version: %d", authorized.CurrentGovernanceVersion)
	}

	// The projection asked the door's own resolver the door's own question.
	if held.askedKind != workflowcontrol.RepairKind {
		t.Fatalf("the projection resolved authority for %q, want the repair kind", held.askedKind)
	}
	if held.askedOperator != principal.Subject() {
		t.Fatalf("the projection asked about %q, want the viewer %q", held.askedOperator, principal.Subject())
	}
}

// TestTodo_UXLIVE_006_Browser is the outcome half of GREEN: the fence's
// indeterminate answer reaches the page as itself.
func TestTodo_UXLIVE_006_Browser(t *testing.T) {
	if got := repairOutcomeFromStatus(execute.RepairIndeterminate); got != workspace.InterventionIndeterminate {
		t.Fatalf("an indeterminate fence projects as %q, want INDETERMINATE", got)
	}
	for _, status := range []execute.RepairStatus{execute.RepairCompleted, execute.RepairNoLongerRequired} {
		if got := repairOutcomeFromStatus(status); got != workspace.InterventionApplied {
			t.Errorf("%s projects as %q, want APPLIED", status, got)
		}
	}
	if got := repairOutcomeFromStatus(execute.RepairReconciliationWait); got != workspace.InterventionPendingSafePoint {
		t.Fatalf("a reconciliation wait projects as %q, want PENDING_SAFE_POINT", got)
	}
	for _, status := range []execute.RepairStatus{
		execute.RepairReplanRequired, execute.RepairReapprovalRequired, execute.RepairBlocked, execute.RepairFailed,
	} {
		if got := repairOutcomeFromStatus(status); got != workspace.InterventionRepairRequired {
			t.Errorf("%s projects as %q, want REPAIR_REQUIRED", status, got)
		}
	}

	// The consequence names the fence's own limit, because a reader who is
	// told a repair "succeeded or failed" when it could not be established
	// has been told something false.
	preview := repairEngine(repairDoorStub(), &repairAuthorityStub{}).
		previewRepair(context.Background(), gatePrincipal(t, "promotion_operator"), workspace.JourneyStageRepairRequired, 1)
	if preview.LikelyOutcome != workspace.InterventionIndeterminate {
		t.Fatalf("the likely outcome is %q; a fence that may not establish the effect must not promise one", preview.LikelyOutcome)
	}
	if preview.ConsequenceSummary == "" {
		t.Fatalf("the repair preview says nothing about what a repair would do")
	}
}

// TestTodo_UXLIVE_006_Security keeps the repair a governed door. Nothing in
// this projection is ever Available, an authority question that cannot be
// answered is answered no, and the port refuses a repair submitted through
// it rather than forwarding one without a plan.
func TestTodo_UXLIVE_006_Security(t *testing.T) {
	ctx := context.Background()
	principal := gatePrincipal(t, "promotion_operator")

	for name, engine := range map[string]*journeyEngine{
		"authorized":   repairEngine(repairDoorStub(), &repairAuthorityStub{grant: &jit.Grant{Role: jit.RoleIntegrityRepair}}),
		"unauthorized": repairEngine(repairDoorStub(), &repairAuthorityStub{}),
		"no door":      repairEngine(nil, &repairAuthorityStub{grant: &jit.Grant{Role: jit.RoleIntegrityRepair}}),
	} {
		preview := engine.previewRepair(ctx, principal, workspace.JourneyStageRepairRequired, 1)
		if preview.Available {
			t.Errorf("%s: the journey port offered a runnable repair", name)
		}
		if preview.UnavailableReasonRef == "" {
			t.Errorf("%s: a refused repair names no reason", name)
		}
	}

	// A resolver that fails, or that is absent, is never read as authority.
	broken := repairEngine(repairDoorStub(), &repairAuthorityStub{err: errors.New("trust store unavailable")})
	if broken.holdsRepairAuthority(ctx, principal) {
		t.Fatalf("a failed authority read was treated as a grant")
	}
	if repairEngine(repairDoorStub(), nil).holdsRepairAuthority(ctx, principal) {
		t.Fatalf("a missing authority resolver was treated as a grant")
	}
	if repairEngine(repairDoorStub(), &repairAuthorityStub{grant: &jit.Grant{}}).holdsRepairAuthority(ctx, nil) {
		t.Fatalf("an unauthenticated viewer was treated as holding a grant")
	}

	// The refusal for a submitted repair names the authority and the door
	// rather than failing anonymously.
	err := repairNotRequestableHere()
	if err == nil {
		t.Fatalf("a repair submitted through the journey port was accepted")
	}
	for _, expected := range []string{"REPAIR", "operator repair door", "INTEGRITY_REPAIR"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("the refusal does not name %q: %v", expected, err)
		}
	}

	// An unrecognized REPAIR verdict is never read as success.
	if got := repairOutcomeFromStatus(execute.RepairStatus("SOMETHING_NEW")); got == workspace.InterventionApplied {
		t.Fatalf("an unrecognized repair verdict was reported as applied")
	}
	if got := repairOutcomeFromStatus(execute.RepairSeparationRequired); got != workspace.InterventionDenied {
		t.Fatalf("a separation-of-duties refusal projects as %q, want DENIED", got)
	}
}
