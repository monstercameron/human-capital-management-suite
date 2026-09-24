package promotionexec

// Capability identities the executable graph invokes, exported for the
// composition that binds a governed invocation to each node (WF-RUN-034).
const (
	CapabilitySnapshotWorker        = capSnapshotWorker
	CapabilitySimulateCompensation  = capSimulate
	CapabilityEvaluateBand          = capEvaluateBand
	CapabilityRevalidate            = capRevalidate
	CapabilityExecutePromotion      = capExecute
	CapabilityObservePayroll        = capObservePayroll
	CapabilityObserveAccess         = capObserveAccess
	CapabilityObserveReconciliation = capObserveRecon
)
