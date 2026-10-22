package agentbudget

// extensionAllowanceLocked is the most AcceptExtensionCAS would add to the task
// right now, or the zero value when it would refuse any extension. The pause
// card, the task view's Extend budget control and the one-click extension all
// ask it, so none of them offers what the ledger then refuses: a task whose own
// limits already pass the policy's extension ceiling (it has been extended once)
// and a tenant month with nothing left both used to be offered an allowance and
// answered "conflict" on the click. The caller holds l.mu.
func (l *Ledger) extensionAllowanceLocked(task *taskState) Limits {
	ceiling := l.policy.ExtensionPolicy.MaxAdditional
	if !fitsExtension(ceiling, task.spec.Limit, ceiling) {
		return Limits{}
	}
	period := l.tenantPeriodLockedAt(task.spec.TenantID, l.now().UTC())
	taken, ok := addLimitsChecked(period.used, period.reserved)
	if !ok {
		return Limits{}
	}
	allowance := minLimits(minLimits(ceiling, l.policy.TenantMonthly.sub(task.spec.Limit)), l.policy.TenantMonthly.sub(taken))
	if allowance.Steps < 0 {
		allowance.Steps = 0
	}
	if allowance.Tokens < 0 {
		allowance.Tokens = 0
	}
	if allowance.WallClock < 0 {
		allowance.WallClock = 0
	}
	if allowance.SpendMicros < 0 {
		allowance.SpendMicros = 0
	}
	return allowance
}
