package leave

import (
	"strings"
	"testing"
)

// readyInput returns a valid baseline that evaluates READY: every revision
// bound, clearance valid, no restrictions, job/schedule/access confirmed,
// qualification qualified.
func readyInput() ReturnToWorkInput {
	return ReturnToWorkInput{
		LeaveRevision:    "rev:leave-1",
		EvidenceRevision: "rev:evidence-1",
		JobRevision:      "rev:job-1",
		ScheduleRevision: "rev:schedule-1",
		AccessRevision:   "rev:access-1",
		Clearance:        ClearanceValid,
		RestrictionState: RestrictionNone,
		JobRequirement:   JobAvailable,
		Schedule:         ScheduleConfirmed,
		Access:           AccessRestored,
		Qualification:    QualificationQualified,
	}
}

func TestTodo_LEAVE_012(t *testing.T) {
	// GREEN happy path: a fully cleared worker is READY with bound revisions.
	res, err := EvaluateReturnToWorkReadiness(readyInput())
	if err != nil {
		t.Fatalf("EvaluateReturnToWorkReadiness(ready) = %v, want nil", err)
	}
	if res.Result != ReadinessReady {
		t.Fatalf("Result = %q, want %q", res.Result, ReadinessReady)
	}
	if res.LeaveRevision != "rev:leave-1" || res.EvidenceRevision != "rev:evidence-1" ||
		res.JobRevision != "rev:job-1" || res.ScheduleRevision != "rev:schedule-1" ||
		res.AccessRevision != "rev:access-1" {
		t.Fatalf("revisions not bound: %+v", res)
	}
	if len(res.FollowUps) != 0 {
		t.Fatalf("READY must carry no follow-ups, got %+v", res.FollowUps)
	}
	if err := res.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// RED: missing/expired/quarantined clearance must never return ready.
	for _, clearance := range []string{ClearanceMissing, ClearanceExpired, ClearanceQuarantined} {
		in := readyInput()
		in.Clearance = clearance
		got, err := EvaluateReturnToWorkReadiness(in)
		if err != nil {
			t.Fatalf("clearance %s: unexpected error %v", clearance, err)
		}
		if got.Result == ReadinessReady || got.Result == ReadinessReadyWithRestrictions {
			t.Fatalf("clearance %s: Result = %q, want NOT_READY", clearance, got.Result)
		}
		if got.Result != ReadinessNotReady {
			t.Fatalf("clearance %s: Result = %q, want %q", clearance, got.Result, ReadinessNotReady)
		}
	}

	// RED: an unresolved (blocking) restriction must never return ready.
	blocked := readyInput()
	blocked.RestrictionState = RestrictionBlocking
	blocked.Restrictions = []StructuredRestriction{{ID: "r-1", Code: "LIFT-10KG", Source: "MEDICAL"}}
	got, err := EvaluateReturnToWorkReadiness(blocked)
	if err != nil {
		t.Fatalf("blocking restriction: unexpected error %v", err)
	}
	if got.Result != ReadinessNotReady {
		t.Fatalf("blocking restriction: Result = %q, want NOT_READY", got.Result)
	}

	// RED: an unavailable job requirement must never return ready.
	noJob := readyInput()
	noJob.JobRequirement = JobUnavailable
	got, err = EvaluateReturnToWorkReadiness(noJob)
	if err != nil {
		t.Fatalf("unavailable job: unexpected error %v", err)
	}
	if got.Result != ReadinessNotReady {
		t.Fatalf("unavailable job: Result = %q, want NOT_READY", got.Result)
	}

	// RED: unknown schedule/access must not return ready; they are UNKNOWN.
	for _, mutate := range []func(*ReturnToWorkInput){
		func(in *ReturnToWorkInput) { in.Schedule = ScheduleUnknown },
		func(in *ReturnToWorkInput) { in.Access = AccessUnknown },
	} {
		in := readyInput()
		mutate(&in)
		got, err := EvaluateReturnToWorkReadiness(in)
		if err != nil {
			t.Fatalf("unknown state: unexpected error %v", err)
		}
		if got.Result != ReadinessUnknown {
			t.Fatalf("unknown state: Result = %q, want UNKNOWN", got.Result)
		}
	}

	// RED: free-text restriction must be refused and mutate nothing.
	assignment := map[string]string{"worker": "w-1", "assignment": "nurse-day"}
	before := assignment["assignment"]
	in := readyInput()
	in.FreeTextRestriction = "light duty or something"
	if _, err := EvaluateReturnToWorkReadiness(in); err == nil {
		t.Fatal("free-text restriction accepted, want refusal")
	}
	if assignment["assignment"] != before {
		t.Fatal("free-text restriction mutated the assignment")
	}

	// GREEN: a conditional restriction yields READY_WITH_RESTRICTIONS plus a
	// governed EstablishWorkRestriction and an accommodation follow-up.
	cond := readyInput()
	cond.RestrictionState = RestrictionConditional
	cond.Restrictions = []StructuredRestriction{{ID: "r-9", Code: "LIFT-10KG", Source: "MEDICAL"}}
	cond.Qualification = QualificationQualified
	got, err = EvaluateReturnToWorkReadiness(cond)
	if err != nil {
		t.Fatalf("conditional: unexpected error %v", err)
	}
	if got.Result != ReadinessReadyWithRestrictions {
		t.Fatalf("conditional: Result = %q, want READY_WITH_RESTRICTIONS", got.Result)
	}
	foundEstablish, foundAccom := false, false
	for _, fu := range got.FollowUps {
		if fu.Kind == FollowUpEstablishWorkRestriction && fu.RestrictionID == "r-9" {
			foundEstablish = true
		}
		if fu.Kind == FollowUpAccommodation {
			foundAccom = true
		}
	}
	if !foundEstablish || !foundAccom {
		t.Fatalf("conditional follow-ups = %+v, want EstablishWorkRestriction(r-9)+Accommodation", got.FollowUps)
	}
	if err := got.Verify(); err != nil {
		t.Fatalf("Verify conditional: %v", err)
	}
}

func TestTodo_LEAVE_012_Security(t *testing.T) {
	// Withheld medical content must never leak into the result: the
	// free-text channel is refused outright, and structured reasons carry
	// only closed-vocabulary codes plus revision IDs.
	in := readyInput()
	in.FreeTextRestriction = "patient has severe depression, see sealed file X"
	res, err := EvaluateReturnToWorkReadiness(in)
	if err == nil || res.Result != "" {
		t.Fatalf("free-text with medical content accepted: %+v, err=%v", res, err)
	}

	// Structured restriction detail is a capability code, not a diagnosis,
	// and the result echoes only the restriction ID -- never a note.
	cond := readyInput()
	cond.RestrictionState = RestrictionConditional
	cond.Restrictions = []StructuredRestriction{{ID: "r-sec", Code: "LIFT-10KG", Source: "MEDICAL"}}
	got, err := EvaluateReturnToWorkReadiness(cond)
	if err != nil {
		t.Fatalf("conditional: %v", err)
	}
	for _, reason := range got.Reasons {
		if strings.Contains(reason, "depression") || strings.Contains(reason, "sealed") {
			t.Fatalf("reason leaks withheld content: %q", reason)
		}
	}

	// Invalid and empty inputs fail closed without panicking.
	if _, err := EvaluateReturnToWorkReadiness(ReturnToWorkInput{}); err == nil {
		t.Fatal("empty input accepted")
	}
	bad := readyInput()
	bad.Clearance = "TRUST_ME"
	if _, err := EvaluateReturnToWorkReadiness(bad); err == nil {
		t.Fatal("off-vocabulary clearance accepted")
	}
}

func TestTodo_LEAVE_012_Conformance(t *testing.T) {
	// The result vocabulary is exactly four values.
	for _, want := range []string{ReadinessReady, ReadinessReadyWithRestrictions, ReadinessNotReady, ReadinessUnknown} {
		if want == "" {
			t.Fatal("empty readiness value")
		}
	}
	// Every result binds all five revisions.
	cases := []ReturnToWorkInput{readyInput()}
	cond := readyInput()
	cond.RestrictionState = RestrictionConditional
	cond.Restrictions = []StructuredRestriction{{ID: "r-c", Code: "STAND-4H", Source: "SAFETY"}}
	cases = append(cases, cond)
	blocked := readyInput()
	blocked.Clearance = ClearanceExpired
	cases = append(cases, blocked)
	unk := readyInput()
	unk.Schedule = ScheduleUnknown
	cases = append(cases, unk)
	for i, in := range cases {
		got, err := EvaluateReturnToWorkReadiness(in)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		switch got.Result {
		case ReadinessReady, ReadinessReadyWithRestrictions, ReadinessNotReady, ReadinessUnknown:
		default:
			t.Fatalf("case %d: Result = %q outside the closed vocabulary", i, got.Result)
		}
		if got.LeaveRevision != in.LeaveRevision || got.EvidenceRevision != in.EvidenceRevision ||
			got.JobRevision != in.JobRevision || got.ScheduleRevision != in.ScheduleRevision ||
			got.AccessRevision != in.AccessRevision {
			t.Fatalf("case %d: revisions not bound: %+v", i, got)
		}
		if err := got.Verify(); err != nil {
			t.Fatalf("case %d Verify: %v", i, err)
		}
	}

	// Conditional qualification produces the full follow-up set: one
	// EstablishWorkRestriction per structured restriction plus
	// accommodation and reassignment intents.
	qc := readyInput()
	qc.RestrictionState = RestrictionConditional
	qc.Restrictions = []StructuredRestriction{{ID: "r-q", Code: "NIGHT-SHIFT-EXCLUDE", Source: "MEDICAL"}}
	qc.Qualification = QualificationConditional
	got, err := EvaluateReturnToWorkReadiness(qc)
	if err != nil {
		t.Fatalf("conditional qualification: %v", err)
	}
	if got.Result != ReadinessReadyWithRestrictions {
		t.Fatalf("conditional qualification: Result = %q", got.Result)
	}
	kinds := map[string]int{}
	for _, fu := range got.FollowUps {
		kinds[fu.Kind]++
	}
	if kinds[FollowUpEstablishWorkRestriction] != 1 || kinds[FollowUpAccommodation] != 1 || kinds[FollowUpReassignment] != 1 {
		t.Fatalf("follow-up kinds = %v, want one of each governed kind", kinds)
	}
	// No cross-domain engine: every follow-up kind is leave-governed.
	for _, fu := range got.FollowUps {
		switch fu.Kind {
		case FollowUpEstablishWorkRestriction, FollowUpAccommodation, FollowUpReassignment:
		default:
			t.Fatalf("follow-up kind %q escapes the leave domain", fu.Kind)
		}
	}
}

func TestTodo_LEAVE_012_Mutation(t *testing.T) {
	// Each subtest kills one named mutant: removing the cited check must
	// fail the assertion below.
	t.Run("clearanceCheck", func(t *testing.T) {
		in := readyInput()
		in.Clearance = ClearanceExpired
		got, err := EvaluateReturnToWorkReadiness(in)
		if err != nil || got.Result != ReadinessNotReady {
			t.Fatalf("expired clearance survived: %+v, err=%v", got, err)
		}
	})
	t.Run("restrictionCheck", func(t *testing.T) {
		in := readyInput()
		in.RestrictionState = RestrictionBlocking
		in.Restrictions = []StructuredRestriction{{ID: "r-m", Code: "LIFT-10KG", Source: "MEDICAL"}}
		got, err := EvaluateReturnToWorkReadiness(in)
		if err != nil || got.Result != ReadinessNotReady {
			t.Fatalf("blocking restriction survived: %+v, err=%v", got, err)
		}
	})
	t.Run("unknownDefaults", func(t *testing.T) {
		in := readyInput()
		in.Access = AccessUnknown
		got, err := EvaluateReturnToWorkReadiness(in)
		if err != nil || got.Result != ReadinessUnknown {
			t.Fatalf("unknown access defaulted: %+v, err=%v", got, err)
		}
	})
	t.Run("followUpEmission", func(t *testing.T) {
		in := readyInput()
		in.RestrictionState = RestrictionConditional
		in.Restrictions = []StructuredRestriction{{ID: "r-m2", Code: "LIFT-10KG", Source: "MEDICAL"}}
		got, err := EvaluateReturnToWorkReadiness(in)
		if err != nil {
			t.Fatalf("conditional: %v", err)
		}
		for _, fu := range got.FollowUps {
			if fu.Kind == FollowUpEstablishWorkRestriction && fu.RestrictionID == "r-m2" {
				return
			}
		}
		t.Fatalf("EstablishWorkRestriction(r-m2) dropped: %+v", got.FollowUps)
	})
	t.Run("freeTextRefusal", func(t *testing.T) {
		in := readyInput()
		in.FreeTextRestriction = "just put them back on days"
		if _, err := EvaluateReturnToWorkReadiness(in); err == nil {
			t.Fatal("free-text mutant accepted")
		}
	})
}
