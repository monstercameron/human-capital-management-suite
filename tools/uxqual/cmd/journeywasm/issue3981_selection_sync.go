package main

import "strings"

// agentSelectionOutOfSync (#3981) reports whether the task detail drawn on the
// Agents page differs from the task the address names. The router changes the
// address on Close, Back and Forward but leaves the task region the page drew
// itself, so the two parted: the address said no task while the detail stayed.
// The address is the truth; the region is redrawn from it.
func agentSelectionOutOfSync(renderedTaskID, rawQuery string) bool {
	return strings.TrimSpace(renderedTaskID) != selectedAgentTaskID(rawQuery)
}

// agentSelectionReconcileDelays are the moments after a navigation at which the
// region is compared with the address again. The router commits the new address
// a little after the click or popstate that caused it, so one look may come too
// early; each look is a no-op once the two agree.
var agentSelectionReconcileDelays = []int{0, 60, 250, 700}
