package rolloutplan

// Rollback and emergency kill (ROLLOUT-006): one stage rolls back to an
// exact verified prior artifact version at a new epoch while its history
// stays append-only, and emergency kill switches (CP-008) propagate the
// stop within SLO. Rollback never invents a version: the target must sit
// in the recorded history with its control digest intact, and the revised
// plan must pin exactly that version.
import (
	"errors"
	"sync"
)

// Rollback codes.
const (
	NoPriorVersion    = "NO_PRIOR_VERSION"
	VersionMismatch   = "VERSION_MISMATCH"
	RollbackNotNeeded = "ROLLBACK_NOT_NEEDED"
	HistoryTampered   = "HISTORY_TAMPERED"
)

// RollbackError is one exact rollback failure carrying its code.
type RollbackError struct {
	Code   string
	Detail string
}

func (e *RollbackError) Error() string { return e.Code + ": " + e.Detail }

// HasRollbackCode reports whether err carries the rollback code.
func HasRollbackCode(err error, code string) bool {
	var rollbackErr *RollbackError
	if !errors.As(err, &rollbackErr) {
		return false
	}
	return rollbackErr.Code == code
}

// StageHistory is the append-only per-stage activation history. The live
// ledger keeps the current receipt; history keeps every receipt so
// rollback can verify its target and so nothing is ever rewritten.
type StageHistory struct {
	mu      sync.Mutex
	entries map[string][]ActivationReceipt
}

// NewStageHistory returns an empty history.
func NewStageHistory() *StageHistory {
	return &StageHistory{entries: make(map[string][]ActivationReceipt)}
}

// Record appends one activation receipt.
func (h *StageHistory) Record(receipt ActivationReceipt) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries[receipt.Stage] = append(h.entries[receipt.Stage], receipt)
}

// Lookup returns the recorded receipt for one stage at one exact version.
func (h *StageHistory) Lookup(stage, version string) (ActivationReceipt, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, receipt := range h.entries[stage] {
		if receipt.Version == version {
			return receipt, true
		}
	}
	return ActivationReceipt{}, false
}

// Entries returns the full recorded chain for one stage, oldest first.
func (h *StageHistory) Entries(stage string) []ActivationReceipt {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]ActivationReceipt(nil), h.entries[stage]...)
}

// RollbackRequest reactivates one stage at one verified prior version.
type RollbackRequest struct {
	Plan          Plan
	Cohorts       CohortSet
	Stage         string
	TargetVersion string
	Artifact      Artifact
	PlanDigest    string
	CohortDigest  string
	Epoch         uint64
}

// RollbackStage verifies the target version against history and
// reactivates the stage at a new epoch, appending the new receipt. The
// revised plan must pin exactly the verified version; rolling back to the
// live version is refused.
func RollbackStage(history *StageHistory, ledger *ActivationLedger, req RollbackRequest) (ActivationReceipt, error) {
	if findings := Validate(req.Plan); len(findings) != 0 {
		return ActivationReceipt{}, &RollbackError{Code: StalePlan, Detail: "revised plan is not releasable: " + findings[0].String()}
	}
	compiled, err := Compile(req.Plan)
	if err != nil || compiled.Digest != req.PlanDigest {
		return ActivationReceipt{}, &RollbackError{Code: StalePlan, Detail: "revised plan digest does not match"}
	}
	want, ok := history.Lookup(req.Stage, req.TargetVersion)
	if !ok {
		return ActivationReceipt{}, &RollbackError{Code: NoPriorVersion, Detail: "stage " + req.Stage + " never ran version " + req.TargetVersion}
	}
	if string(req.Artifact.Type) != want.Artifact || req.Artifact.Version != want.Version {
		return ActivationReceipt{}, &RollbackError{Code: VersionMismatch, Detail: "request does not name the verified prior version"}
	}
	if req.Plan.Artifact != req.Artifact {
		return ActivationReceipt{}, &RollbackError{Code: VersionMismatch, Detail: "revised plan does not pin the verified prior version"}
	}
	current, err := ledger.Explain(req.Stage, "")
	if err == nil && current.Receipt.Version == req.TargetVersion {
		return ActivationReceipt{}, &RollbackError{Code: RollbackNotNeeded, Detail: "version " + req.TargetVersion + " is already live"}
	}
	before := history.Entries(req.Stage)
	receipt, err := ledger.activateForRollback(ActivationRequest{
		Plan: req.Plan, Cohorts: req.Cohorts, Stage: req.Stage, Artifact: req.Artifact,
		PlanDigest: req.PlanDigest, CohortDigest: req.CohortDigest, Epoch: req.Epoch,
	})
	if err != nil {
		return ActivationReceipt{}, err
	}
	history.Record(receipt)
	after := history.Entries(req.Stage)
	if len(after) != len(before)+1 {
		return ActivationReceipt{}, &RollbackError{Code: HistoryTampered, Detail: "history did not grow by exactly one entry"}
	}
	for i, entry := range before {
		if after[i] != entry {
			return ActivationReceipt{}, &RollbackError{Code: HistoryTampered, Detail: "prior history changed under rollback"}
		}
	}
	return receipt, nil
}
