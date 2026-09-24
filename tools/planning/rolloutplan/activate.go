package rolloutplan

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/configbundle"
)

// KillSwitchGuard is the rollout planner's port for the authoritative CP-008
// state. The implementation lives in the data layer; the consumer owns the
// narrow interface it needs.
type KillSwitchGuard interface {
	Guard(configbundle.KillSwitchTarget, func(configbundle.KillDecision) error) error
}

// Activation finding codes.
const (
	WrongArtifact          = "WRONG_ARTIFACT"
	StalePlan              = "STALE_PLAN"
	StaleCohort            = "STALE_COHORT"
	StaleEpoch             = "STALE_EPOCH"
	UnknownActivationStage = "UNKNOWN_ACTIVATION_STAGE"
	CapabilityKilled       = "CAPABILITY_KILLED"
	KillStateRequired      = "KILL_STATE_REQUIRED"
	KillTargetMismatch     = "KILL_TARGET_MISMATCH"
)

// ActivationError is one exact activation failure carrying its code.
type ActivationError struct {
	Code   string
	Detail string
}

func (e *ActivationError) Error() string { return e.Code + ": " + e.Detail }

// HasActivationCode reports whether err carries the activation code.
func HasActivationCode(err error, code string) bool {
	var activationErr *ActivationError
	if !errors.As(err, &activationErr) {
		return false
	}
	return activationErr.Code == code
}

// ActivationRequest activates one stage cohort at one artifact version.
// Every digest is stated, never ambient: the plan, cohort and epoch must
// match the frozen inputs exactly.
type ActivationRequest struct {
	Plan         Plan
	Cohorts      CohortSet
	Stage        string
	Artifact     Artifact
	PlanDigest   string
	CohortDigest string
	Epoch        uint64
}

// ActivationReceipt proves one canary activation: the exact artifact,
// plan, cohort, epoch and control snapshot.
type ActivationReceipt struct {
	Stage         string `json:"stage"`
	Artifact      string `json:"artifact"`
	Version       string `json:"version"`
	PlanDigest    string `json:"plan_digest"`
	CohortDigest  string `json:"cohort_digest"`
	Epoch         uint64 `json:"epoch"`
	ControlDigest string `json:"control_digest"`
	Owner         string `json:"owner"`
}

// ActivationExplanation lets any subject or service explain the exact
// active version and receipt: members see their binding, everyone else
// sees the active receipt with their non-membership stated.
type ActivationExplanation struct {
	Stage   string            `json:"stage"`
	Member  bool              `json:"member"`
	Receipt ActivationReceipt `json:"receipt"`
}

// ActivationLedger records stage activations with a monotonic epoch per
// stage: replays fail, advances succeed.
type ActivationLedger struct {
	mu         sync.Mutex
	killGuard  KillSwitchGuard
	killTarget configbundle.KillSwitchTarget
	epochs     map[string]uint64
	receipts   map[string]ActivationReceipt
	members    map[string][]string
}

// NewActivationLedger returns an empty ledger.
func NewActivationLedger(killGuard KillSwitchGuard, killTarget configbundle.KillSwitchTarget) *ActivationLedger {
	return &ActivationLedger{
		killGuard: killGuard, killTarget: killTarget,
		epochs:   make(map[string]uint64),
		receipts: make(map[string]ActivationReceipt),
		members:  make(map[string][]string),
	}
}

// Activate verifies one canary activation against its frozen inputs and
// records its receipt. Wrong artifact, plan, cohort, stage or epoch
// fails with an exact code.
func (l *ActivationLedger) Activate(req ActivationRequest) (ActivationReceipt, error) {
	return l.activate(req, false)
}

// activateForRollback allows the verified rollback path to move a stage to
// its recorded safer artifact while a kill remains applied. It still holds
// the same CP-008 state lock through the ledger transition.
func (l *ActivationLedger) activateForRollback(req ActivationRequest) (ActivationReceipt, error) {
	return l.activate(req, true)
}

func (l *ActivationLedger) activate(req ActivationRequest, allowKilled bool) (ActivationReceipt, error) {
	if findings := Validate(req.Plan); len(findings) != 0 {
		return ActivationReceipt{}, &ActivationError{Code: WrongArtifact, Detail: findings[0].String()}
	}
	if req.Artifact != req.Plan.Artifact {
		return ActivationReceipt{}, &ActivationError{Code: WrongArtifact, Detail: fmt.Sprintf(
			"request pins %s@%s, plan pins %s@%s",
			req.Artifact.Type, req.Artifact.Version, req.Plan.Artifact.Type, req.Plan.Artifact.Version)}
	}
	compiled, err := Compile(req.Plan)
	if err != nil || compiled.Digest != req.PlanDigest {
		return ActivationReceipt{}, &ActivationError{Code: StalePlan, Detail: "plan digest does not match the frozen plan"}
	}
	var cohort *Cohort
	for i := range req.Cohorts.Cohorts {
		if req.Cohorts.Cohorts[i].Stage == req.Stage {
			cohort = &req.Cohorts.Cohorts[i]
			break
		}
	}
	if cohort == nil {
		return ActivationReceipt{}, &ActivationError{Code: UnknownActivationStage, Detail: fmt.Sprintf("stage %q has no cohort", req.Stage)}
	}
	if cohort.Digest != req.CohortDigest {
		return ActivationReceipt{}, &ActivationError{Code: StaleCohort, Detail: "cohort digest does not match the frozen cohort"}
	}
	if req.Epoch == 0 {
		return ActivationReceipt{}, &ActivationError{Code: StaleEpoch, Detail: "epoch zero never activates"}
	}
	var receipt ActivationReceipt
	commit := func() error {
		l.mu.Lock()
		defer l.mu.Unlock()
		if req.Epoch <= l.epochs[req.Stage] {
			return &ActivationError{Code: StaleEpoch, Detail: fmt.Sprintf(
				"epoch %d replays stage epoch %d", req.Epoch, l.epochs[req.Stage])}
		}
		receipt = ActivationReceipt{
			Stage:        req.Stage,
			Artifact:     string(req.Artifact.Type),
			Version:      req.Artifact.Version,
			PlanDigest:   req.PlanDigest,
			CohortDigest: req.CohortDigest,
			Epoch:        req.Epoch,
			ControlDigest: cohortDigest(
				compiled.Digest, req.Stage, cohort.Digest,
				string(req.Artifact.Type), req.Artifact.Version,
				fmt.Sprintf("epoch=%d", req.Epoch), req.Plan.Owner),
			Owner: req.Plan.Owner,
		}
		l.epochs[req.Stage] = req.Epoch
		l.receipts[req.Stage] = receipt
		members := append([]string(nil), cohort.Members...)
		sort.Strings(members)
		l.members[req.Stage] = members
		return nil
	}
	if l == nil || l.killGuard == nil {
		return ActivationReceipt{}, &ActivationError{Code: KillStateRequired, Detail: "CP-008 kill-switch state is required for activation"}
	}
	if strings.TrimSpace(l.killTarget.TenantID) == "" || strings.TrimSpace(l.killTarget.TenantID) != l.killTarget.TenantID ||
		strings.TrimSpace(l.killTarget.Capability) == "" || strings.TrimSpace(l.killTarget.Capability) != l.killTarget.Capability ||
		req.Plan.KillTarget != l.killTarget {
		return ActivationReceipt{}, &ActivationError{Code: KillTargetMismatch, Detail: "plan kill target does not match this ledger's trusted CP-008 subject"}
	}
	err = l.killGuard.Guard(l.killTarget, func(decision configbundle.KillDecision) error {
		if decision.Disabled && !allowKilled {
			return &ActivationError{Code: CapabilityKilled, Detail: "capability is disabled by applied CP-008 switch " + decision.SwitchID}
		}
		return commit()
	})
	if err != nil {
		return ActivationReceipt{}, err
	}
	return receipt, nil
}

// Explain returns one subject's activation explanation for one stage.
func (l *ActivationLedger) Explain(stage, subject string) (ActivationExplanation, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	receipt, ok := l.receipts[stage]
	if !ok {
		return ActivationExplanation{}, &ActivationError{Code: UnknownActivationStage, Detail: fmt.Sprintf("stage %q never activated", stage)}
	}
	explanation := ActivationExplanation{Stage: stage, Receipt: receipt}
	for _, member := range l.members[stage] {
		if member == subject {
			explanation.Member = true
			break
		}
	}
	return explanation, nil
}
