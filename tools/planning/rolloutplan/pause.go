package rolloutplan

// Pause and resume with fencing (ROLLOUT-005): a PauseGate wraps one
// activation ledger and stops new activations for paused stages. Pausing
// never moves the ledger; resuming revalidates plan, health, authority
// and cohort before the stage activates again at a fresh epoch. Running
// unsafe work drains at its declared safe points alongside (OPS-006);
// the gate owns the activation fence, the drain owns the work fence.

import (
	"errors"
	"fmt"
	"sync"
)

// Pause gate codes.
const (
	PausedStage          = "PAUSED_STAGE"
	AlreadyPaused        = "ALREADY_PAUSED"
	NotPaused            = "NOT_PAUSED"
	StaleResumeEpoch     = "STALE_RESUME_EPOCH"
	StaleResumeCohort    = "STALE_RESUME_COHORT"
	StaleResumeAuthority = "STALE_RESUME_AUTHORITY"
)

// PauseError is one exact pause-gate failure carrying its code.
type PauseError struct {
	Code   string
	Detail string
}

func (e *PauseError) Error() string { return e.Code + ": " + e.Detail }

// HasPauseCode reports whether err carries the pause code.
func HasPauseCode(err error, code string) bool {
	var pauseErr *PauseError
	if !errors.As(err, &pauseErr) {
		return false
	}
	return pauseErr.Code == code
}

// PauseReceipt proves one stage paused at one epoch with its bindings.
type PauseReceipt struct {
	Stage         string `json:"stage"`
	Epoch         uint64 `json:"epoch"`
	PlanDigest    string `json:"plan_digest"`
	ControlDigest string `json:"control_digest"`
	Reason        string `json:"reason"`
}

// PauseGate fences activations per stage around one ledger.
type PauseGate struct {
	mu     sync.Mutex
	ledger *ActivationLedger
	paused map[string]PauseReceipt
}

// NewPauseGate wraps one activation ledger.
func NewPauseGate(ledger *ActivationLedger) *PauseGate {
	return &PauseGate{ledger: ledger, paused: make(map[string]PauseReceipt)}
}

// Pause stops new activations for one activated stage. The stage must be
// active and the epoch must advance past its activation.
func (g *PauseGate) Pause(plan Plan, stage string, epoch uint64, reason string) (PauseReceipt, error) {
	compiled, err := Compile(plan)
	if err != nil {
		return PauseReceipt{}, &PauseError{Code: StalePlan, Detail: "plan is not releasable"}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, dup := g.paused[stage]; dup {
		return PauseReceipt{}, &PauseError{Code: AlreadyPaused, Detail: "stage " + stage + " is already paused"}
	}
	explanation, err := g.ledger.Explain(stage, "")
	if err != nil {
		return PauseReceipt{}, &PauseError{Code: UnknownActivationStage, Detail: "stage " + stage + " never activated"}
	}
	if epoch == 0 || epoch <= explanation.Receipt.Epoch {
		return PauseReceipt{}, &PauseError{Code: StaleEpoch, Detail: "pause epoch must advance past activation"}
	}
	if explanation.Receipt.PlanDigest != compiled.Digest {
		return PauseReceipt{}, &PauseError{Code: StalePlan, Detail: "activation binds another plan"}
	}
	receipt := PauseReceipt{
		Stage: stage, Epoch: epoch, PlanDigest: compiled.Digest,
		ControlDigest: explanation.Receipt.ControlDigest, Reason: reason,
	}
	g.paused[stage] = receipt
	return receipt, nil
}

// Paused reports whether one stage is paused.
func (g *PauseGate) Paused(stage string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.paused[stage]
	return ok
}

// Activate refuses paused stages and delegates the rest to the ledger.
func (g *PauseGate) Activate(req ActivationRequest) (ActivationReceipt, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if receipt, ok := g.paused[req.Stage]; ok {
		return ActivationReceipt{}, &PauseError{Code: PausedStage, Detail: "stage " + req.Stage +
			" paused at epoch " + fmt.Sprintf("%d", receipt.Epoch) + ": " + receipt.Reason}
	}
	return g.ledger.Activate(req)
}

// Expand refuses advancement out of a paused stage, then advances through
// the gate so a paused target is refused too.
func (g *PauseGate) Expand(req ExpansionRequest) (ActivationReceipt, error) {
	g.mu.Lock()
	_, sourcePaused := g.paused[req.Decision.Stage]
	g.mu.Unlock()
	if sourcePaused {
		return ActivationReceipt{}, &PauseError{Code: PausedStage, Detail: "stage " + req.Decision.Stage + " is paused"}
	}
	if err := req.Decision.Verify(); err != nil {
		return ActivationReceipt{}, err
	}
	if req.Decision.Verdict != VerdictExpand {
		return ActivationReceipt{}, &HealthError{Code: HealthBlocked, Detail: "verdict is not EXPAND"}
	}
	next := ""
	for i, stage := range req.Plan.Stages {
		if stage.Name == req.Decision.Stage && i+1 < len(req.Plan.Stages) {
			next = req.Plan.Stages[i+1].Name
		}
	}
	if next == "" {
		return ActivationReceipt{}, &HealthError{Code: NoFurtherStage, Detail: "no further stage"}
	}
	return g.Activate(ActivationRequest{
		Plan: req.Plan, Cohorts: req.Cohorts, Stage: next, Artifact: req.Artifact,
		PlanDigest: req.Decision.PlanDigest, CohortDigest: req.CohortDigest, Epoch: req.Epoch,
	})
}

// ResumeRequest reopens one paused stage after full revalidation.
type ResumeRequest struct {
	Plan         Plan
	Cohorts      CohortSet
	Stage        string
	Epoch        uint64
	Telemetry    Telemetry
	Thresholds   Thresholds
	Artifact     Artifact
	CohortDigest string
}

// Resume revalidates plan, health, authority and cohort, unpauses the
// stage and activates it at a fresh epoch past the pause.
func (g *PauseGate) Resume(req ResumeRequest) (ActivationReceipt, error) {
	g.mu.Lock()
	receipt, ok := g.paused[req.Stage]
	g.mu.Unlock()
	if !ok {
		return ActivationReceipt{}, &PauseError{Code: NotPaused, Detail: "stage " + req.Stage + " is not paused"}
	}
	compiled, err := Compile(req.Plan)
	if err != nil || compiled.Digest != receipt.PlanDigest {
		return ActivationReceipt{}, &PauseError{Code: StalePlan, Detail: "plan changed while paused"}
	}
	if req.Artifact != req.Plan.Artifact {
		return ActivationReceipt{}, &PauseError{Code: StaleResumeAuthority, Detail: "artifact is not the planned one"}
	}
	cohortOK := false
	for _, cohort := range req.Cohorts.Cohorts {
		if cohort.Stage == req.Stage && cohort.Digest == req.CohortDigest && cohort.PlanDigest == compiled.Digest {
			cohortOK = true
		}
	}
	if !cohortOK {
		return ActivationReceipt{}, &PauseError{Code: StaleResumeCohort, Detail: "cohort does not match the frozen one"}
	}
	decision, err := EvaluateHealth(req.Plan, req.Stage, req.Telemetry, req.Thresholds)
	if err != nil {
		return ActivationReceipt{}, err
	}
	if decision.Verdict != VerdictExpand {
		return ActivationReceipt{}, &HealthError{Code: HealthBlocked, Detail: "stage health is " + decision.Verdict}
	}
	if req.Epoch <= receipt.Epoch {
		return ActivationReceipt{}, &PauseError{Code: StaleResumeEpoch, Detail: "resume epoch must advance past the pause"}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if current, ok := g.paused[req.Stage]; !ok || current != receipt {
		return ActivationReceipt{}, &PauseError{Code: NotPaused, Detail: "stage " + req.Stage + " pause changed during resume"}
	}
	activated, err := g.ledger.Activate(ActivationRequest{
		Plan: req.Plan, Cohorts: req.Cohorts, Stage: req.Stage, Artifact: req.Artifact,
		PlanDigest: compiled.Digest, CohortDigest: req.CohortDigest, Epoch: req.Epoch,
	})
	if err != nil {
		return ActivationReceipt{}, err
	}
	delete(g.paused, req.Stage)
	return activated, nil
}
