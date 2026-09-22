package execution

import "github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"

// WithWorkNotifications wraps a workflow's own work-item routing so that every
// routed approval or task publishes its in-app notice to the owner and a
// status notice to the requester, in the routing transaction. It is how a
// composition other than Promotion's gets the same notifications: the
// wrappers know nothing about the workflow they serve, only what
// internal/workflow/notifyplan declares for the step type.
func WithWorkNotifications(next execute.WorkItemFactory) execute.WorkItemFactory {
	return requesterStatusWorkItems{next: notifyingWorkItems{next: next}}
}

// WithRequesterStatusTerminal wraps a workflow's terminal writer so that the
// requester is told the run finished, in the transaction of the terminal
// write.
func WithRequesterStatusTerminal(next execute.TerminalWriter) execute.TerminalWriter {
	return requesterStatusTerminal{next: next}
}
