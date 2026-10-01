package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaEvaluationRunProbe struct {
	cases     map[string]agenteval.PersonaCase
	completed time.Time
	leak      bool
}

func (*personaEvaluationRunProbe) AuthorizeSyntheticPersonaEvaluation(context.Context, agenteval.PersonaEvaluationTarget) error {
	return nil
}
func (p *personaEvaluationRunProbe) ExecutePersonaCase(_ context.Context, _ agenteval.PersonaEvaluationTarget, c agenteval.PersonaCase) (agenteval.PersonaCaseExecution, error) {
	if p.cases == nil {
		p.cases = map[string]agenteval.PersonaCase{}
	}
	p.cases[c.ID] = c
	return agenteval.PersonaCaseExecution{TaskID: c.ID, InvocationID: c.ID}, nil
}
func (p *personaEvaluationRunProbe) ReadPersonaCase(_ context.Context, target agenteval.PersonaEvaluationTarget, execution agenteval.PersonaCaseExecution) (agenteval.PersonaCaseEvidence, error) {
	c := p.cases[execution.InvocationID]
	observed := agenteval.PersonaCaseEvidence{SyntheticTenantID: target.SyntheticTenantID, PersonaID: target.PersonaID, PersonaVersion: target.PersonaVersion, ProfileDigest: target.ProfileDigest, ModelDigest: target.ModelDigest, CaseDigest: agenteval.PersonaCaseDigest(c), TaskID: execution.TaskID, InvocationID: execution.InvocationID, EvidenceDigest: "sha256:" + strings.Repeat("c", 64), Outcome: "COMPLETED", Skills: c.ExpectedSkills, DeliveredTo: []string{target.InvokerID}, PlanDigest: target.ProfileDigest, BaselinePlanDigest: target.ProfileDigest, CompletedAt: p.completed, SettledCostMicros: 100}
	observed.AuthorizedRecipients = []string{target.InvokerID}
	observed.AudienceFloorDigest = "sha256:" + strings.Repeat("d", 64)
	if c.Kind == agenteval.PersonaOutOfScope || c.Kind == agenteval.PersonaDenied {
		observed.Outcome = "REFUSED"
		observed.Skills = nil
		observed.DeliveredTo = nil
		if c.Kind == agenteval.PersonaOutOfScope {
			observed.RefusalCode = "OUT_OF_SCOPE"
			observed.RefusalPointer = "workflow:payroll"
		} else {
			observed.RefusalCode = "AUTHORITY_DENIED"
		}
	} else if p.leak {
		observed.DeliveredTo = append(observed.DeliveredTo, "unapproved-peer")
	}
	return observed, nil
}

type personaEvaluationRecorderFake struct {
	calls  int
	signed agentpersonastore.SignedPersonaEvaluation
	err    error
}

func (r *personaEvaluationRecorderFake) Record(_ context.Context, _ values.TenantId, signed agentpersonastore.SignedPersonaEvaluation) error {
	r.calls++
	r.signed = signed
	return r.err
}

func personaEvaluationIssuerFixture(t *testing.T, leak bool) (*PersonaEvaluationIssuer, agenteval.PersonaEvaluationReport, *personaEvaluationRecorderFake, ed25519.PublicKey) {
	t.Helper()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	manifest := personaStarterManifest("Answer approved policy questions.")
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	profile, err := agentpersona.Seal(personaStarterProfile(starter, validPersonaStarterRequest(manifest), manifest, "Answer approved policy questions."))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(profile.Profile)
	row := agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: profile.Profile.PersonaID, Version: 1, Profile: encoded, ContentDigest: profile.Digest}
	target := agenteval.PersonaEvaluationTarget{TenantID: "tenant-a", SyntheticTenantID: "synthetic-a", PersonaID: row.PersonaID, PersonaVersion: 1, ProfileDigest: row.ContentDigest, ModelDigest: "sha256:" + strings.Repeat("b", 64), InvokerID: "invoker-a"}
	probe := &personaEvaluationRunProbe{completed: now.Add(-time.Hour), leak: leak}
	report, err := agenteval.EvaluatePersonaSuite(context.Background(), target, agenteval.PolicyHelperSuite(starter.SkillPins[0].ID), probe, probe)
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &personaEvaluationRecorderFake{}
	issuer := &PersonaEvaluationIssuer{Versions: personaAdminVersionFake{row}, Recorder: recorder, TenantID: "tenant-a", SuiteID: profile.Profile.EvalSuiteRef, SuiteDigest: agenteval.PersonaSuiteDigest(agenteval.PolicyHelperSuite(starter.SkillPins[0].ID)), KeyID: "test-key", PrivateKey: private, ModelDigest: target.ModelDigest, Now: func() time.Time { return now }, FreshFor: 24 * time.Hour, NewRunID: func() string { return "evaluation-run-1" }}
	return issuer, report, recorder, public
}

func TestTodo_AGENTP_021_EvaluationIssuerPinsMeasuredReportAndExecutionTime(t *testing.T) {
	issuer, report, recorder, public := personaEvaluationIssuerFixture(t, false)
	runID, err := issuer.Issue(context.Background(), report)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := report.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	claim := recorder.signed.Claim
	if runID != "evaluation-run-1" || recorder.calls != 1 || !claim.Passed || claim.ProfileDigest != evidence.Target.ProfileDigest || claim.RunDigest != evidence.RunDigest || claim.SuiteDigest != evidence.SuiteDigest || !claim.IssuedAt.Equal(evidence.EvaluatedAt) || !claim.ExpiresAt.Equal(evidence.EvaluatedAt.Add(24*time.Hour)) {
		t.Fatalf("claim not bound to measured report: %+v", claim)
	}
	encoded, _ := json.Marshal(claim)
	if !ed25519.Verify(public, append([]byte("hcm-next-persona-evaluation/v1\x00"), encoded...), recorder.signed.Signature) {
		t.Fatal("evaluation authority signature invalid")
	}
	issuer.Now = func() time.Time { return evidence.EvaluatedAt.Add(48 * time.Hour) }
	if _, err := issuer.Issue(context.Background(), report); !errors.Is(err, ErrPersonaEvaluationEvidenceUnavailable) || recorder.calls != 1 {
		t.Fatalf("replay renewed stale evaluation: calls=%d err=%v", recorder.calls, err)
	}
}

func TestTodo_AGENTP_021_EvaluationIssuerRetainsFailedMeasuredRun(t *testing.T) {
	issuer, report, recorder, _ := personaEvaluationIssuerFixture(t, true)
	if _, err := issuer.Issue(context.Background(), report); err != nil {
		t.Fatal(err)
	}
	if recorder.calls != 1 || recorder.signed.Claim.Passed {
		t.Fatalf("failed audience fixture promoted: %+v", recorder.signed.Claim)
	}
}

func TestTodo_AGENTP_021_EvaluationIssuerRejectsUnsealedOrForeignReports(t *testing.T) {
	issuer, report, recorder, _ := personaEvaluationIssuerFixture(t, false)
	if _, err := issuer.Issue(context.Background(), agenteval.PersonaEvaluationReport{}); !errors.Is(err, ErrPersonaEvaluationEvidenceUnavailable) {
		t.Fatal(err)
	}
	issuer.TenantID = "tenant-b"
	if _, err := issuer.Issue(context.Background(), report); !errors.Is(err, ErrPersonaEvaluationEvidenceUnavailable) {
		t.Fatal(err)
	}
	if recorder.calls != 0 {
		t.Fatal("foreign or unsealed evaluation reached recorder")
	}
}
