package agenteval

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func personaTarget() PersonaEvaluationTarget {
	return PersonaEvaluationTarget{TenantID: "tenant-a", SyntheticTenantID: "synthetic-a", PersonaID: "policy-helper", PersonaVersion: 1,
		ProfileDigest: "sha256:" + strings.Repeat("a", 64), ModelDigest: "sha256:" + strings.Repeat("b", 64), InvokerID: "invoker-a"}
}

type personaSuiteProbe struct {
	cases      map[string]PersonaCase
	calls      int
	authorized bool
	denied     bool
	mutate     func(*PersonaCaseEvidence)
}

func (p *personaSuiteProbe) AuthorizeSyntheticPersonaEvaluation(context.Context, PersonaEvaluationTarget) error {
	if p.denied {
		return errors.New("tenant is not synthetic")
	}
	p.authorized = true
	return nil
}

func (p *personaSuiteProbe) ExecutePersonaCase(_ context.Context, _ PersonaEvaluationTarget, testCase PersonaCase) (PersonaCaseExecution, error) {
	if !p.authorized {
		return PersonaCaseExecution{}, errors.New("executed before synthetic check")
	}
	if p.cases == nil {
		p.cases = make(map[string]PersonaCase)
	}
	p.cases[testCase.ID] = testCase
	p.calls++
	return PersonaCaseExecution{TaskID: testCase.ID, InvocationID: testCase.ID}, nil
}

func (p *personaSuiteProbe) ReadPersonaCase(_ context.Context, target PersonaEvaluationTarget, execution PersonaCaseExecution) (PersonaCaseEvidence, error) {
	testCase := p.cases[execution.InvocationID]
	observed := PersonaCaseEvidence{SyntheticTenantID: target.SyntheticTenantID, PersonaID: target.PersonaID, PersonaVersion: target.PersonaVersion,
		ProfileDigest: target.ProfileDigest, ModelDigest: target.ModelDigest, CaseDigest: PersonaCaseDigest(testCase),
		TaskID: execution.TaskID, InvocationID: execution.InvocationID, EvidenceDigest: personaDigest("fixture-evidence", testCase),
		Outcome: "COMPLETED", Skills: testCase.ExpectedSkills, DeliveredTo: []string{target.InvokerID},
		PlanDigest: target.ProfileDigest, BaselinePlanDigest: target.ProfileDigest, SettledCostMicros: 100,
		AuthorizedRecipients: []string{target.InvokerID}, AudienceFloorDigest: target.ProfileDigest,
		CompletedAt: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}
	if testCase.Kind == PersonaOutOfScope || testCase.Kind == PersonaDenied {
		observed.Outcome = "REFUSED"
		observed.DeliveredTo = nil
		observed.Skills = nil
		if testCase.Kind == PersonaDenied {
			observed.RefusalCode = "AUTHORITY_DENIED"
		} else {
			observed.RefusalCode = "OUT_OF_SCOPE"
			observed.RefusalPointer = "workflow:payroll"
		}
	}
	if p.mutate != nil {
		p.mutate(&observed)
	}
	return observed, nil
}

func TestTodo_AGENTP_021(t *testing.T) {
	probe := &personaSuiteProbe{}
	report, err := EvaluatePersonaSuite(context.Background(), personaTarget(), PolicyHelperSuite("policy.read"), probe, probe)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := report.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.Passed || probe.calls != 8 || len(evidence.Cases) != 8 || evidence.CostMicros != 800 || !personaSHA256(evidence.RunDigest) || !personaSHA256(evidence.SuiteDigest) {
		t.Fatalf("report=%+v calls=%d", evidence, probe.calls)
	}
	evidence.Cases[0].Passed = false
	again, err := report.Evidence()
	if err != nil || !again.Cases[0].Passed {
		t.Fatalf("detached report mutated: %+v %v", again, err)
	}
	if _, err := (PersonaEvaluationReport{}).Evidence(); !errors.Is(err, ErrPersonaEvaluation) {
		t.Fatalf("unsealed report = %v", err)
	}
}

func TestTodo_AGENTP_021_Golden(t *testing.T) {
	probe := &personaSuiteProbe{}
	report, err := EvaluatePersonaSuite(context.Background(), personaTarget(), PolicyHelperSuite("policy.read"), probe, probe)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := report.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	got := evidence.SuiteID + "\n" + evidence.SuiteDigest + "\n" + evidence.RunDigest + "\n"
	const want = "AGENTP-021.policy_helper\nsha256:a7d95a050aa7c6123038cf5dfd0fe6e184ae75b5d326971a440b77d919b5b6d8\nsha256:0fa56a2afdf3ee18de7bda71e7fe0936bff1796b27ba140d37946946d94449a2\n"
	if got != want {
		t.Fatalf("persona evaluation golden = %q", got)
	}
}

func TestTodo_AGENTP_021_Conformance_PublicDeliveryRequiresAudienceWitness(t *testing.T) {
	probe := &personaSuiteProbe{mutate: func(e *PersonaCaseEvidence) {
		if e.Outcome == "COMPLETED" && e.TaskID != "mixed-audience" {
			e.DeliveredTo = append(e.DeliveredTo, "eligible-peer")
			e.AuthorizedRecipients = append(e.AuthorizedRecipients, "eligible-peer")
		}
	}}
	report, err := EvaluatePersonaSuite(context.Background(), personaTarget(), PolicyHelperSuite("policy.read"), probe, probe)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := report.Evidence()
	if err != nil || !evidence.Passed {
		t.Fatalf("verified safe public output rejected: %+v %v", evidence, err)
	}
}

func TestTodo_AGENTP_021_Security(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mutate      func(*PersonaCaseEvidence)
		unavailable bool
	}{
		{"foreign tenant", func(e *PersonaCaseEvidence) { e.SyntheticTenantID = "other" }, true},
		{"profile changed", func(e *PersonaCaseEvidence) { e.ProfileDigest = personaTarget().ModelDigest }, true},
		{"model changed", func(e *PersonaCaseEvidence) { e.ModelDigest = personaTarget().ProfileDigest }, true},
		{"version changed", func(e *PersonaCaseEvidence) { e.PersonaVersion++ }, true},
		{"case replay", func(e *PersonaCaseEvidence) { e.CaseDigest = personaTarget().ModelDigest }, true},
		{"missing evidence", func(e *PersonaCaseEvidence) { e.EvidenceDigest = "" }, true},
		{"missing execution time", func(e *PersonaCaseEvidence) { e.CompletedAt = time.Time{} }, true},
		{"missing audience witness", func(e *PersonaCaseEvidence) { e.AudienceFloorDigest = "" }, false},
		{"cost overflow", func(e *PersonaCaseEvidence) { e.SettledCostMicros = math.MaxInt64 }, true},
		{"audience leak", func(e *PersonaCaseEvidence) {
			if e.Outcome == "COMPLETED" {
				e.DeliveredTo = append(e.DeliveredTo, "unauthorized-peer")
			}
		}, false},
		{"peer changes plan", func(e *PersonaCaseEvidence) {
			if strings.HasPrefix(e.TaskID, "peer-injection") {
				e.PlanDigest = personaTarget().ModelDigest
			}
		}, false},
		{"extra skill", func(e *PersonaCaseEvidence) {
			if e.Outcome == "COMPLETED" {
				e.Skills = append(e.Skills, "payroll.write")
			}
		}, false},
		{"no refusal pointer", func(e *PersonaCaseEvidence) { e.RefusalPointer = "" }, false},
		{"no authority denial", func(e *PersonaCaseEvidence) {
			if e.RefusalCode == "AUTHORITY_DENIED" {
				e.RefusalCode = "OUT_OF_SCOPE"
			}
		}, false},
		{"above cost ceiling", func(e *PersonaCaseEvidence) { e.SettledCostMicros = 200_000 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &personaSuiteProbe{mutate: tc.mutate}
			report, err := EvaluatePersonaSuite(context.Background(), personaTarget(), PolicyHelperSuite("policy.read"), probe, probe)
			if tc.unavailable {
				if !errors.Is(err, ErrPersonaEvaluation) {
					t.Fatalf("invalid evidence accepted: %v", err)
				}
				if _, err := report.Evidence(); err == nil {
					t.Fatal("invalid evidence produced signable report")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := report.Evidence()
			if err != nil || evidence.Passed {
				t.Fatalf("unsafe observation passed: %+v %v", evidence, err)
			}
		})
	}
}

func TestTodo_AGENTP_021_Conformance(t *testing.T) {
	for _, mutate := range []func(*PersonaEvaluationTarget, *PersonaSuite){
		func(v *PersonaEvaluationTarget, _ *PersonaSuite) { v.SyntheticTenantID = v.TenantID },
		func(_ *PersonaEvaluationTarget, s *PersonaSuite) { s.Cases = s.Cases[1:] },
		func(_ *PersonaEvaluationTarget, s *PersonaSuite) { s.Cases = s.Cases[:7] },
		func(_ *PersonaEvaluationTarget, s *PersonaSuite) { s.Cases[7].PeerText = s.Cases[6].PeerText },
		func(_ *PersonaEvaluationTarget, s *PersonaSuite) { s.Cases[0].ExpectedSkills = nil },
		func(_ *PersonaEvaluationTarget, s *PersonaSuite) { s.Cases[0].Kind = "UNKNOWN" },
		func(_ *PersonaEvaluationTarget, s *PersonaSuite) { s.Cases[0].Prompt = " " },
		func(_ *PersonaEvaluationTarget, s *PersonaSuite) { s.Cases[1].ExpectedSkills = []string{"policy.read"} },
	} {
		target, suite := personaTarget(), PolicyHelperSuite("policy.read")
		mutate(&target, &suite)
		probe := &personaSuiteProbe{}
		if _, err := EvaluatePersonaSuite(context.Background(), target, suite, probe, probe); !errors.Is(err, ErrPersonaEvaluation) || probe.calls != 0 {
			t.Fatalf("incomplete suite executed: %v calls=%d", err, probe.calls)
		}
	}
	probe := &personaSuiteProbe{denied: true}
	if _, err := EvaluatePersonaSuite(context.Background(), personaTarget(), PolicyHelperSuite("policy.read"), probe, probe); !errors.Is(err, ErrPersonaEvaluation) || probe.calls != 0 {
		t.Fatalf("production tenant executed: %v calls=%d", err, probe.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe = &personaSuiteProbe{}
	if _, err := EvaluatePersonaSuite(ctx, personaTarget(), PolicyHelperSuite("policy.read"), probe, probe); !errors.Is(err, context.Canceled) || probe.calls != 0 {
		t.Fatalf("cancelled evaluation executed: %v calls=%d", err, probe.calls)
	}
}
