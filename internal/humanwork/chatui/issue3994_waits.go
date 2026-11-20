package chatui

// #3994: a work card says "Waiting on you" only when the resolver, which
// compared stable identities as the viewer, says so. The card never infers it
// from a display name: two people can share a name, and a renamed person keeps
// their assignment. A card without that answer is informational.

// journeyStageAwaitsApprover reports whether a journey stage is one in which
// the named approver has something to do. A journey that is blocked, waiting
// for its effective date, being revalidated, or finished does not ask its
// approver for anything, even though it is still open.
func journeyStageAwaitsApprover(stage string) bool {
	switch stage {
	case "JOURNEY_STAGE_AWAITING_APPROVAL", "JOURNEY_STAGE_FINANCE_APPROVAL", "JOURNEY_STAGE_MANAGER_APPROVAL":
		return true
	}
	return false
}

// JourneyWaitsOnSubject is the resolver's answer for a journey: the stable
// approver subject equals the viewer's subject and the stage asks for an
// approval. Both subjects must be present.
func JourneyWaitsOnSubject(stage, approverSubject, viewerSubject string) bool {
	return approverSubject != "" && approverSubject == viewerSubject && journeyStageAwaitsApprover(stage)
}

// TaskWaitsOnSubject is the resolver's answer for a project task: the stable
// assignee id equals the viewer's subject and the task is not finished.
func TaskWaitsOnSubject(statusTone, assigneeID, viewerSubject string) bool {
	return assigneeID != "" && assigneeID == viewerSubject && statusTone != "done"
}
