package manifest

// workOrderRules is the reviewed transport and authorization contract for
// WorkOrderService. Work-order writes are revision-bound and idempotent;
// callers derive tenant and actor authority from authenticated context.
func workOrderRules() map[string]rule {
	type binding struct {
		name, action, capability       string
		behavior                       IntentBehavior
		idempotent                     bool
		revision, pagination, ordering string
	}
	rows := []binding{
		{"CreateWorkOrder", "create", "create", IntentBehaviorCreates, true, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"GetWorkOrder", "read", "read", IntentBehaviorObserves, false, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ListWorkOrders", "list", "read", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"SubmitInitiatorRequest", "requests.submit", "request", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"DecideInitiatorRequest", "requests.decide", "approve", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"AddWorkOrderNote", "notes.add", "note", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RequestPhaseTransition", "phase.transition", "phase", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RecordWorkEntry", "work.record", "inspect", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RecordProgressEntry", "progress.record", "inspect", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RecordSpendEntry", "spend.record", "record_cost", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RequestWorkOrderReport", "reports.request", "read", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RequestBillingDraft", "billing.request", "bill", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
	}
	service := "/hcmnext.workorder.v1.WorkOrderService/"
	out := make(map[string]rule, len(rows))
	for _, row := range rows {
		keyClass := IdempotencyReadSafe
		keySource := ""
		retry := "CLIENT_MAY_RETRY"
		if row.idempotent {
			keyClass = IdempotencyKey
			keySource = "request.idempotency_key"
			retry = "IDEMPOTENCY_KEY_DEDUPED"
		}
		out[service+row.name] = rule{
			owner: "WORKORDER", behavior: row.behavior,
			disposition:       DispositionServed,
			dispositionReason: "WorkOrderService operation is served by the modular field-execution workflow application",
			httpMethod:        "POST", httpPath: service + row.name, httpBody: "*",
			authzAction:       "hcmnext.workorder." + row.action,
			classificationRef: "CONFIDENTIAL_HR",
			idempotencyClass:  keyClass, idempotencyKeySrc: keySource,
			revisionPolicy: row.revision, retryPolicy: retry,
			paginationPolicy: row.pagination, orderingPolicy: row.ordering,
			compatibilityStatus: "ACTIVE", phase: "PHASE_1",
			capabilityRefs: []string{"hcmnext.workorderaccess." + row.capability},
		}
	}
	return out
}
