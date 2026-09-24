package achriskadapter

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/contact"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/achrisk"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
)

type contactSource struct {
	endpoint contact.ContactEndpointRevision
}

func (s contactSource) ResolveIndependentContact(string, string, string) (contact.ContactEndpointRevision, error) {
	return s.endpoint, nil
}

type dispatcher struct{}

func (dispatcher) DispatchOutOfBand(paymethod.ChangeConfirmation) error { return nil }

func TestTodo_REV_052_01(t *testing.T) {
	assessment, req, input, at := adapterFixture(t)
	decision, err := MapAssessment(assessment, req.Proposal.CanonicalDigest, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != paymethod.RiskReject || decision.RuleVersion != assessment.RuleVersion || decision.AssessmentDigest != assessment.CanonicalDigest ||
		decision.ProposalDigest != req.Proposal.CanonicalDigest || !decision.EvaluatedAt.Equal(assessment.EvaluatedAt) || !decision.ValidUntil.Equal(at.Add(time.Hour)) {
		t.Fatalf("mapped decision = %+v", decision)
	}
	input.Signals = achrisk.Signals{DestinationAge: 48 * time.Hour}
	got, evaluated, err := AuthorizeDirectDepositChange(achrisk.DefaultPolicy(), input, req, at.Add(time.Hour))
	if err != nil || evaluated.Decision != achrisk.Release || got.Status != paymethod.AuthorizationStepUpRequired || got.Activated {
		t.Fatalf("release path decision=%+v assessment=%+v err=%v", got, evaluated, err)
	}
	for _, tc := range []struct {
		name    string
		signals achrisk.Signals
		want    string
	}{
		{"alert", achrisk.Signals{AmountMinor: 20_000_000, DestinationAge: 48 * time.Hour}, paymethod.RiskAlert},
		{"hold", achrisk.Signals{AmountMinor: 20_000_000, VelocityCount: 11, DestinationAge: 48 * time.Hour}, paymethod.RiskHold},
		{"release", achrisk.Signals{DestinationAge: 48 * time.Hour}, paymethod.RiskRelease},
		{"reject", achrisk.Signals{AmountMinor: 20_000_000, VelocityCount: 11, VelocityWindow: time.Hour, DestinationAge: time.Hour, OperatorRisk: 70, DeviceRisk: 70, TimingRisk: 70, PayrollChange: true}, paymethod.RiskReject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assessment, err := achrisk.DefaultPolicy().Evaluate(achrisk.EvaluationInput{TenantID: "tenant-1", Participant: achrisk.Originator, EvaluatedAt: at, Signals: tc.signals, DestinationChangeDigest: req.BankDetailChange.CanonicalDigest})
			if err != nil {
				t.Fatal(err)
			}
			mapped, err := MapAssessment(assessment, req.Proposal.CanonicalDigest, at.Add(time.Hour))
			if err != nil || mapped.Decision != tc.want {
				t.Fatalf("mapped=%+v err=%v, want %s", mapped, err, tc.want)
			}
		})
	}
}

func TestTodo_REV_052_01_Security(t *testing.T) {
	assessment, req, _, at := adapterFixture(t)
	if _, err := MapAssessment(assessment, req.Proposal.CanonicalDigest, at); err == nil {
		t.Fatal("expired assessment accepted")
	}
	assessment.CanonicalDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := MapAssessment(assessment, req.Proposal.CanonicalDigest, at.Add(time.Hour)); err == nil {
		t.Fatal("forged assessment digest accepted")
	}
	assessment, _, _, _ = adapterFixture(t)
	if _, err := MapAssessment(assessment, "caller-selected-proposal", at.Add(time.Hour)); err == nil {
		t.Fatal("unprotected proposal binding accepted")
	}
	_, req, input, at := adapterFixture(t)
	input.TenantID = "another-tenant"
	if _, _, err := AuthorizeDirectDepositChange(achrisk.DefaultPolicy(), input, req, at.Add(time.Hour)); err == nil {
		t.Fatal("cross-tenant signal accepted")
	}
}

func TestTodo_REV_052_01_Integration(t *testing.T) {
	_, req, input, at := adapterFixture(t)
	input.Signals = achrisk.Signals{AmountMinor: 20_000_000, VelocityCount: 11, VelocityWindow: time.Hour, DestinationAge: time.Hour, OperatorRisk: 70, DeviceRisk: 70, TimingRisk: 70, PayrollChange: true}
	got, assessment, err := AuthorizeDirectDepositChange(achrisk.DefaultPolicy(), input, req, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Decision != achrisk.Reject || got.Status != paymethod.AuthorizationBlocked || got.Reason != paymethod.ReasonFraudBlocked || got.Activated {
		t.Fatalf("reject path decision=%+v assessment=%+v", got, assessment)
	}
}

func adapterFixture(t *testing.T) (achrisk.RiskAssessment, paymethod.ChangeAuthorizationRequest, achrisk.EvaluationInput, time.Time) {
	t.Helper()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2027-01-01")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "payroll", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := paymethod.NewDestination(paymethod.Destination{DestinationID: "destination-1", WorkerRef: "worker-1", Rail: paymethod.RailACH, Risk: paymethod.RiskMedium, GovernedRef: "token:old", DisplayHint: "••••1234", Currency: "USD", CountryCode: "US", Verification: paymethod.VerificationVerified, VerificationDigest: "sha256:" + strings.Repeat("a", 64), Effective: interval})
	if err != nil {
		t.Fatal(err)
	}
	proposed, err := current.NewRevision(paymethod.Destination{GovernedRef: "token:new", DisplayHint: "••••5678", Currency: "USD", CountryCode: "US", Rail: paymethod.RailACH, Risk: paymethod.RiskMedium, Verification: paymethod.VerificationVerified, VerificationDigest: "sha256:" + strings.Repeat("b", 64), Effective: interval})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := paymethod.NewDestinationChange(current, proposed, "proposal-1", "requester", "approver", interval)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := contact.NewContactEndpointRevision(values.EntityRef{Tenant: values.TenantId("tenant-1"), Kind: values.Kind("worker"), Id: "00000000-0000-0000-0000-000000000001"}, "contact-1", contact.EndpointEmail, "payroll-alerts@example.test", "payment_destination_change_confirmation", 1, "hr-contact-profile")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = endpoint.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	change, err := paymethod.StartBankDetailChange(paymethod.BankDetailChangeRequest{ID: "change-1", TenantID: "tenant-1", DestinationID: proposal.DestinationID, WorkerRef: proposal.WorkerRef, BeforeDigest: proposal.PreviousDigest, AfterDigest: proposal.ProposedDigest, RequestedBy: "requester", Approver: "approver", RequestedAt: at.Add(-48 * time.Hour), CoolingOff: time.Hour, Constraints: sod.Constraints{RuleID: "payment-destination-dual-control", RequesterMayNotApprove: true, OneApprovalPerPrincipal: true}, DecisionContext: sod.DecisionContext{Requester: sod.Actor{Subject: "requester"}, Approvers: []sod.Actor{{Subject: "approver"}}}}, contactSource{endpoint: endpoint}, dispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	change, err = change.Confirm(at.Add(-36*time.Hour), "sha256:"+strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-1"), Subject: "requester", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-1", IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), CredentialDigest: "sha256:principal"})
	if err != nil {
		t.Fatal(err)
	}
	req := paymethod.ChangeAuthorizationRequest{CurrentDestination: current, ProposedDestination: proposed, Proposal: proposal, CurrentProposal: paymethod.ProposalRevision{ID: proposal.ID, Digest: proposal.CanonicalDigest, Revision: 1}, BankDetailChange: change, Principal: principal, VerificationFreshUntil: at.Add(time.Hour), At: at}
	input := achrisk.EvaluationInput{TenantID: "tenant-1", Participant: achrisk.Originator, AnnualACHVolume2023: 7_000_000, EvaluatedAt: at, Signals: achrisk.Signals{AmountMinor: 20_000_000, VelocityCount: 11, VelocityWindow: time.Hour, DestinationAge: time.Hour, OperatorRisk: 70, DeviceRisk: 70, TimingRisk: 70, PayrollChange: true}}
	assessment, err := achrisk.DefaultPolicy().Evaluate(achrisk.EvaluationInput{TenantID: "tenant-1", Participant: achrisk.Originator, AnnualACHVolume2023: 7_000_000, EvaluatedAt: at, Signals: input.Signals, PaymentDestinationChange: &change})
	if err != nil {
		t.Fatal(err)
	}
	return assessment, req, input, at
}
