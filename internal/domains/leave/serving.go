package leave

import "fmt"

// ServingContractID identifies the read-only Leave contract composed by the
// shipped application cell. It does not grant authority or persist state.
const ServingContractID = "hcmnext.conformance.leave/v1"

// ValidateServingContract checks the Leave vocabulary and pure projections
// that the application composition exposes. The check deliberately uses only
// ephemeral values: serving composition must not create a leave record,
// mutate a balance, or contact an external provider.
func ValidateServingContract() error {
	for _, kind := range []string{SuccessorExtend, SuccessorShorten, SuccessorCancel} {
		intent := SuccessorIntent{
			Kind: kind, LeaveRecordID: "leave:serving", PriorIntentDigest: "sha256:intent",
			PriorProposalDigest: "sha256:proposal", NewStart: 1, NewEnd: 2,
		}
		if kind == SuccessorCancel {
			intent.ConsumedHours = 1
			intent.BalanceSettlement = "settle:serving"
		} else {
			intent.ReplanDigest = "sha256:replan"
			intent.ReplanPrior = "sha256:plan"
			intent.ConflictRule = "prior-wins"
			intent.ConflictWinner = "leave:serving"
		}
		if _, err := NewSuccessorRegistry().Declare(intent, "sha256:plan"); err != nil {
			return fmt.Errorf("leave: serving contract successor %q: %w", kind, err)
		}
	}

	dimensions, err := ReconcileEffects([]ExternalEffect{
		{System: "benefits", Mandatory: true, State: EffectUnknown, Owner: "benefits-ops"},
		{System: "payroll", Mandatory: true, State: EffectPass, Observation: "observed", Owner: "payroll-ops"},
		{System: "wfm", Mandatory: false, State: EffectPass, Observation: "observed", Owner: "wfm-ops"},
	})
	if err != nil {
		return fmt.Errorf("leave: serving contract effects: %w", err)
	}
	if dimensions.Business != BusinessLeaveActive || dimensions.ConsistencyState != "DEGRADED" || dimensions.ObligationState != "PENDING" {
		return fmt.Errorf("leave: serving contract effects produced %+v", dimensions)
	}
	if err := dimensions.Verify(); err != nil {
		return fmt.Errorf("leave: serving contract effect seal: %w", err)
	}

	readiness, err := EvaluateReturnToWorkReadiness(ReturnToWorkInput{
		LeaveRevision: "rev:leave:serving", EvidenceRevision: "rev:evidence:serving",
		JobRevision: "rev:job:serving", ScheduleRevision: "rev:schedule:serving", AccessRevision: "rev:access:serving",
		Clearance: ClearanceValid, RestrictionState: RestrictionNone, JobRequirement: JobAvailable,
		Schedule: ScheduleConfirmed, Access: AccessRestored, Qualification: QualificationQualified,
	})
	if err != nil {
		return fmt.Errorf("leave: serving contract readiness: %w", err)
	}
	if readiness.Result != ReadinessReady {
		return fmt.Errorf("leave: serving contract readiness produced %q", readiness.Result)
	}
	if err := readiness.Verify(); err != nil {
		return fmt.Errorf("leave: serving contract readiness seal: %w", err)
	}

	wantEffects := []string{"payroll", "benefits", "schedule", "access"}
	if len(returnEffectSystems) != len(wantEffects) {
		return fmt.Errorf("leave: serving contract return queue is incomplete")
	}
	for i, want := range wantEffects {
		if returnEffectSystems[i] != want {
			return fmt.Errorf("leave: serving contract return effect %d is %q, want %q", i, returnEffectSystems[i], want)
		}
	}
	wantSteps := []string{
		"leave-ended-revision", "availability-restoration", "restriction-carryover",
		"ledger-events", "projections", "effect-outbox", "obligation-policy",
	}
	if len(returnCommitSteps) != len(wantSteps) {
		return fmt.Errorf("leave: serving contract return steps are incomplete")
	}
	for i, want := range wantSteps {
		if returnCommitSteps[i] != want {
			return fmt.Errorf("leave: serving contract return step %d is %q, want %q", i, returnCommitSteps[i], want)
		}
	}
	return nil
}
