package app

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// PROMOUX-014: the WAITING_EFFECTIVE_DATE stage previously named nothing
// beyond a bare effective date -- no instant, no timezone, no owner, no
// scheduled action, no explanation (this file's RED). This file sources all
// five from durable, already-computed engine state -- the pending timer the
// engine actually promised (internal/workflow/timer.Scheduler.Pending) and
// the compiled promotion workflow's own routing
// (internal/workflow/promotionexec.Definition) -- and packages them as
// workspace.JourneyFinding rows on JourneyDetail.Findings, the one existing
// wire vessel generic enough to carry them without a schema/proto change
// (this cell's composition forbids editing schema/proto and gen/go for this
// todo). Nothing here is guessed in the browser: the client
// (tools/uxqual/journeyclient) only ever echoes the Code/Message pairs this
// file writes.

// Finding codes the WAITING_EFFECTIVE_DATE explanation carries. They are
// restated verbatim as unexported string literals in
// tools/uxqual/journeyclient/projector.go (dependency-roles.yaml keeps that
// module from importing this one), the same way that package already
// restates the wire's stage tokens.
const (
	FindingCodeWaitEffectiveInstant = "WAIT_EFFECTIVE_INSTANT"
	FindingCodeWaitOwner            = "WAIT_OWNER"
	FindingCodeWaitScheduledAction  = "WAIT_SCHEDULED_ACTION"
	FindingCodeWaitRemainingChecks  = "WAIT_REMAINING_CHECKS"
	FindingCodeWaitNotification     = "WAIT_NOTIFICATION"
	FindingCodeWaitIntervention     = "WAIT_INTERVENTION"
)

// waitFindingSeverity is the severity every PROMOUX-014 explanation finding
// carries: none of them is a simulation problem, so severityInfo
// (tools/uxqual/journeyclient's normalisation of an unrecognised severity
// word) is what a reader sees.
const waitFindingSeverity = "info"

// journeyWaitTimer loads the durable timer the engine promised for the
// promotion's effective-date wait, or (nil, nil) when the instance carries
// none -- either because it has not reached the wait yet or the wait has
// already fired and settled.
func journeyWaitTimer(ctx context.Context, tx dbport.Tx, tenantID, instanceID uuid.UUID) (*timer.Timer, error) {
	if instanceID == uuid.Nil {
		return nil, nil
	}
	pending, err := (timer.Scheduler{}).Pending(ctx, tx, tenantID, instanceID)
	if err != nil {
		return nil, fmt.Errorf("app: journey: read the instance's pending timers: %w", err)
	}
	for i := range pending {
		if pending[i].NodeID == promotionexec.NodeWaitEffectiveDate {
			t := pending[i]
			return &t, nil
		}
	}
	return nil, nil
}

// waitEffectiveDateNextNodeID names the node the compiled promotion
// workflow's own routing sends the WAIT node to once it fires -- read from
// [promotionexec.Definition], never asserted independently of it, so a
// change to the workflow's routing cannot leave this explanation describing
// a node the engine no longer visits next.
func waitEffectiveDateNextNodeID() (string, bool) {
	def := promotionexec.Definition()
	for _, e := range def.Edges {
		if e.From == promotionexec.NodeWaitEffectiveDate && e.RouteKey == "FIRED" {
			return e.To, true
		}
	}
	return "", false
}

// lastApprovalOwner names the principal who completed the most recently
// completed APPROVAL work item, or "" when none has completed -- the
// steward of record for a journey that is now waiting on nothing but the
// calendar.
func lastApprovalOwner(items []workitem.WorkItem) string {
	var owner string
	var latest time.Time
	for _, it := range items {
		if it.Kind != workitem.KindApproval || it.Status != workitem.StatusCompleted || it.CompletedBy == "" || it.CompletedAt == nil {
			continue
		}
		if it.CompletedAt.After(latest) {
			latest = *it.CompletedAt
			owner = it.CompletedBy
		}
	}
	return owner
}

// journeyWaitFindings explains one WAITING_EFFECTIVE_DATE journey. t is the
// durable timer [journeyWaitTimer] found; a nil t (no pending timer for the
// wait node) yields no findings, because there is then no promise to
// explain -- a wait-explanation reader must not manufacture one.
//
// Every fact is total and server-computed: the instant and its timezone come
// from the timer's own FiresAt and the workflow's declared zone, the
// scheduled action and remaining checks from the compiled workflow's real
// edges, the owner from the durable work-item record, and the notification
// and intervention statements from what this engine and page actually do
// (nothing, and nothing, respectively) rather than from what might sound
// plausible.
func journeyWaitFindings(t *timer.Timer, items []workitem.WorkItem) []workspace.JourneyFinding {
	if t == nil {
		return nil
	}
	instant := t.FiresAt.UTC()
	findings := make([]workspace.JourneyFinding, 0, 6)

	findings = append(findings, workspace.JourneyFinding{
		Severity: waitFindingSeverity, Code: FindingCodeWaitEffectiveInstant,
		Message: fmt.Sprintf(
			"Waits until %s -- the start of the effective date in %s, the zone the workflow's wait step is declared against.",
			instant.Format(time.RFC3339), promotionexec.EffectiveDateZoneID,
		),
	})

	owner := lastApprovalOwner(items)
	if owner == "" {
		owner = "not captured: no approval work item on this journey recorded who completed it"
	}
	findings = append(findings, workspace.JourneyFinding{
		Severity: waitFindingSeverity, Code: FindingCodeWaitOwner,
		Message: "Approved by " + owner + ", who is the steward of record while this journey waits.",
	})

	action := "the workflow resumes"
	if next, ok := waitEffectiveDateNextNodeID(); ok {
		action = fmt.Sprintf("the workflow's %q step revalidates the promotion's pinned facts before execution can proceed", next)
	}
	findings = append(findings, workspace.JourneyFinding{
		Severity: waitFindingSeverity, Code: FindingCodeWaitScheduledAction,
		Message: "At that instant, " + action + ".",
	})

	findings = append(findings, workspace.JourneyFinding{
		Severity: waitFindingSeverity, Code: FindingCodeWaitRemainingChecks,
		Message: "Remaining checks: revalidation of the pinned facts, then execution and effect observation before the promotion outcome is recorded.",
	})

	findings = append(findings, workspace.JourneyFinding{
		Severity: waitFindingSeverity, Code: FindingCodeWaitNotification,
		Message: "No notification is sent when this wait resolves. Reopen or refresh this journey to see its new stage once the effective instant has passed.",
	})

	findings = append(findings, workspace.JourneyFinding{
		Severity: waitFindingSeverity, Code: FindingCodeWaitIntervention,
		Message: "None. Production exposes no manual timer bypass; this wait resolves only at its scheduled instant, never from an action on this page.",
	})

	return findings
}

// isWaitExplanationFindingCode reports whether code is one of the six
// WAIT_* explanation codes journeyWaitFindings mints. The codes are the
// projector's routing keys for the wait-explanation section, not execution
// internals: their messages already cross the diagnostics boundary, so the
// code discloses nothing the message does not already say.
func isWaitExplanationFindingCode(code string) bool {
	switch code {
	case FindingCodeWaitEffectiveInstant, FindingCodeWaitOwner, FindingCodeWaitScheduledAction,
		FindingCodeWaitRemainingChecks, FindingCodeWaitNotification, FindingCodeWaitIntervention:
		return true
	}
	return false
}
