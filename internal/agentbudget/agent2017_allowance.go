package agentbudget

// ExtensionAllowance is the most one user-accepted extension may add to a
// task's ceiling under the configured policy: the policy's maximum, held below
// what the tenant's monthly ceiling leaves above the task's own limit. It is the
// same figure the pause card offers. A task-view button that extends "by the
// allowance" asks for exactly this, so the person never has to choose numbers;
// AcceptExtensionCAS still checks it against the task and the shared ceilings.
// A zero value means the task cannot be extended.
func (l *Ledger) ExtensionAllowance(taskID string) Limits {
	if l == nil {
		return Limits{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	task, ok := l.tasks[taskID]
	if !ok {
		return Limits{}
	}
	// AcceptExtensionCAS refuses a task that is not paused at its own ceiling, so
	// nothing is offered for one that is paused for another reason or not paused.
	if task.paused == nil || task.paused.Scope != ScopeTask || !isTaskCeilingReason(task.paused.Reason) {
		return Limits{}
	}
	return l.extensionAllowanceLocked(task)
}
