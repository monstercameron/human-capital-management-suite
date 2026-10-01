package agentbudget

// SettledTaskUsage is the durable, task-scoped usage projection used by
// evaluation and audit readers. It excludes in-flight reservations.
type SettledTaskUsage struct {
	TenantID string
	TaskID   string
	Usage    Usage
}
