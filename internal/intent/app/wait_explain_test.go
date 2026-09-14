package app

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// TestTodo_PROMOUX_014 is the PRIMARY matrix test: it proves that
// journeyWaitFindings sources every one of RED's five missing facts
// (instant, timezone, owner, scheduled action, explanation -- split here
// into the six wire codes it actually mints) from durable, already-computed
// state -- never from a guess -- and that it produces nothing at all when
// there is no pending timer to explain.
func TestTodo_PROMOUX_014(t *testing.T) {
	t.Run("no pending timer yields no findings", func(t *testing.T) {
		if got := journeyWaitFindings(nil, []workitem.WorkItem{{Kind: workitem.KindApproval, Status: workitem.StatusCompleted, CompletedBy: "principal:x", CompletedAt: ptrTime(time.Now())}}); got != nil {
			t.Fatalf("journeyWaitFindings(nil timer) = %+v, want nil", got)
		}
	})

	t.Run("a pending timer yields all six explanation findings, sourced from the timer and the compiled workflow", func(t *testing.T) {
		fireAt := time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)
		tm := &timer.Timer{NodeID: promotionexec.NodeWaitEffectiveDate, FiresAt: fireAt}
		earlier := fireAt.Add(-72 * time.Hour)
		later := fireAt.Add(-24 * time.Hour)
		items := []workitem.WorkItem{
			{Kind: workitem.KindApproval, Status: workitem.StatusCompleted, CompletedBy: "principal:finance-partner", CompletedAt: &earlier},
			{Kind: workitem.KindApproval, Status: workitem.StatusCompleted, CompletedBy: "principal:manager-approver", CompletedAt: &later},
			// A TASK work item, even completed later, must never be read as
			// the approval owner.
			{Kind: workitem.KindTask, Status: workitem.StatusCompleted, CompletedBy: "principal:someone-else", CompletedAt: ptrTime(fireAt)},
		}

		got := journeyWaitFindings(tm, items)
		if len(got) != 6 {
			t.Fatalf("journeyWaitFindings returned %d findings, want 6: %+v", len(got), got)
		}

		byCode := make(map[string]string, len(got))
		for _, f := range got {
			if f.Severity != waitFindingSeverity {
				t.Errorf("finding %s severity = %q, want %q", f.Code, f.Severity, waitFindingSeverity)
			}
			if _, dup := byCode[f.Code]; dup {
				t.Fatalf("finding code %s appears more than once", f.Code)
			}
			byCode[f.Code] = f.Message
		}

		wantCodes := []string{
			FindingCodeWaitEffectiveInstant, FindingCodeWaitOwner, FindingCodeWaitScheduledAction,
			FindingCodeWaitRemainingChecks, FindingCodeWaitNotification, FindingCodeWaitIntervention,
		}
		for _, code := range wantCodes {
			if _, ok := byCode[code]; !ok {
				t.Errorf("missing finding code %s", code)
			}
		}

		// 1. The effective instant, with its timezone -- RED's "no next
		//    timestamp, no timezone".
		instant := byCode[FindingCodeWaitEffectiveInstant]
		if !strings.Contains(instant, fireAt.Format(time.RFC3339)) {
			t.Errorf("effective-instant finding = %q, want the RFC3339 instant %s", instant, fireAt.Format(time.RFC3339))
		}
		if !strings.Contains(instant, promotionexec.EffectiveDateZoneID) {
			t.Errorf("effective-instant finding = %q, want the zone %s", instant, promotionexec.EffectiveDateZoneID)
		}

		// 2. The owner -- RED's "no owner" -- is the LATEST completed
		//    APPROVAL, not the manager-approver's completion which is a
		//    TASK, and not the finance approval which completed earlier.
		owner := byCode[FindingCodeWaitOwner]
		if !strings.Contains(owner, "principal:manager-approver") {
			t.Errorf("owner finding = %q, want the latest completed approval's owner (principal:manager-approver)", owner)
		}
		if strings.Contains(owner, "principal:someone-else") {
			t.Errorf("owner finding = %q, leaked the TASK item's completer instead of the latest APPROVAL", owner)
		}

		// 3. The scheduled action names the real next node from the
		//    compiled workflow's own routing -- RED's "no scheduled
		//    action".
		action := byCode[FindingCodeWaitScheduledAction]
		next, ok := waitEffectiveDateNextNodeID()
		if !ok {
			t.Fatal("waitEffectiveDateNextNodeID found no FIRED edge from the wait node; the fixture below cannot be meaningful")
		}
		if !strings.Contains(action, next) {
			t.Errorf("scheduled-action finding = %q, want it to name the real next node %q", action, next)
		}

		// 4, 5, 6: remaining checks, notification and intervention are all
		// non-empty explanatory text -- RED's "no explanation".
		if byCode[FindingCodeWaitRemainingChecks] == "" {
			t.Error("remaining-checks finding is empty")
		}
		if byCode[FindingCodeWaitNotification] == "" {
			t.Error("notification finding is empty")
		}
		if byCode[FindingCodeWaitIntervention] == "" {
			t.Error("intervention finding is empty")
		}
	})

	t.Run("owner is empty-but-named when no approval work item has completed", func(t *testing.T) {
		tm := &timer.Timer{NodeID: promotionexec.NodeWaitEffectiveDate, FiresAt: time.Now()}
		got := journeyWaitFindings(tm, nil)
		var owner string
		for _, f := range got {
			if f.Code == FindingCodeWaitOwner {
				owner = f.Message
			}
		}
		if !strings.Contains(owner, "not captured") {
			t.Errorf("owner finding with no completed approvals = %q, want it to say the owner was not captured rather than fabricating one", owner)
		}
	})
}

// waitEffectiveDateNextNodeID's own real-world answer, exercised directly:
// the compiled promotion workflow's WAIT node must route somewhere on
// FIRED, or journeyWaitFindings's "scheduled action" fact degrades to a
// vague default no reader could act on.
func TestTodo_PROMOUX_014_WaitEffectiveDateNextNodeIDFindsTheCompiledRoute(t *testing.T) {
	next, ok := waitEffectiveDateNextNodeID()
	if !ok || next == "" {
		t.Fatal("waitEffectiveDateNextNodeID found no FIRED edge from the effective-date wait node")
	}
	if next != promotionexec.NodeRevalidate {
		t.Errorf("FIRED edge target = %q, want %q (update this test if the workflow's own routing changed on purpose)", next, promotionexec.NodeRevalidate)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
