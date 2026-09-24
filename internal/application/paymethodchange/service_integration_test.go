package paymethodchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/paymethodstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/contact"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/achrisk"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/paymethod/achriskadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type testContactSource struct {
	endpoint contact.ContactEndpointRevision
}

func (s testContactSource) ResolveIndependentContact(string, string, string) (contact.ContactEndpointRevision, error) {
	return s.endpoint, nil
}

type testConfirmationDispatcher struct{}

func (testConfirmationDispatcher) DispatchOutOfBand(paymethod.ChangeConfirmation) error { return nil }

type successfulStepUp struct{}

func (successfulStepUp) Present(context.Context, stepup.Proof, stepup.Operation, *trust.Principal, stepup.Requirement) (stepup.Outcome, error) {
	return stepup.OutcomeExecuted, nil
}

func TestTodo_REV_052_01_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 106); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, "rev052-change", "rev052-change")
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatal(err)
	}
	store := paymethodstore.New()
	service := New(store)
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	current, request, input := changeFixture(t, tenantID, at)
	withTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		_, err := store.PutDestination(context.Background(), tx, tenantID, current)
		return err
	})

	// A real high-risk signal reaches the policy and authorization gate. The
	// blocked result must leave the current destination revision as the head.
	input.Signals = achrisk.Signals{AmountMinor: 20_000_000, VelocityCount: 11, VelocityWindow: time.Hour, DestinationAge: time.Hour, OperatorRisk: 70, DeviceRisk: 70, TimingRisk: 70, PayrollChange: true}
	blocked := withTenantTxResult(t, conn, tenantID, func(tx dbport.Tx) (Result, error) {
		return service.AuthorizeAndActivate(context.Background(), tx, tenantID, achrisk.DefaultPolicy(), input, request, at.Add(time.Hour))
	})
	if blocked.Assessment.Decision != achrisk.Reject || blocked.Authorization.Status != paymethod.AuthorizationBlocked || blocked.Authorization.Activated {
		t.Fatalf("blocked result = %+v", blocked)
	}
	assertDestinationHead(t, conn, tenantID, store, current, 1)

	// A low-risk signal completes step-up, authorizes, and persists revision 2.
	input.Signals = achrisk.Signals{DestinationAge: 48 * time.Hour, PayrollChange: true}
	request.StepUpPresenter = successfulStepUp{}
	op := paymethod.StepUpOperation(request)
	request.StepUpProof = stepup.Proof{Tenant: values.TenantId(tenantID.String()), Subject: "requester", SessionRef: "session-rev052", Assurance: trust.AssuranceHigh, Action: stepup.ActionApprove, ProposalID: request.Proposal.CanonicalDigest, Scopes: op.Scopes}
	accepted := withTenantTxResult(t, conn, tenantID, func(tx dbport.Tx) (Result, error) {
		return service.AuthorizeAndActivate(context.Background(), tx, tenantID, achrisk.DefaultPolicy(), input, request, at.Add(time.Hour))
	})
	if accepted.Assessment.Decision != achrisk.Release || accepted.Authorization.Status != paymethod.AuthorizationAuthorized || !accepted.Authorization.Activated || accepted.Destination.CanonicalDigest != request.ProposedDestination.CanonicalDigest {
		t.Fatalf("accepted result = %+v", accepted)
	}
	assertDestinationHead(t, conn, tenantID, store, accepted.Destination, 2)
}

func TestTodo_REV_052_01_Security(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 106); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenantID, "rev052-security", "rev052-security")
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatal(err)
	}
	store := paymethodstore.New()
	service := New(store)
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	current, request, input := changeFixture(t, tenantID, at)
	withTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		_, err := store.PutDestination(context.Background(), tx, tenantID, current)
		return err
	})
	request.StepUpPresenter = successfulStepUp{}
	op := paymethod.StepUpOperation(request)
	request.StepUpProof = stepup.Proof{Tenant: values.TenantId(tenantID.String()), Subject: "requester", SessionRef: "session-rev052", Assurance: trust.AssuranceHigh, Action: stepup.ActionApprove, ProposalID: request.Proposal.CanonicalDigest, Scopes: op.Scopes}
	input.Signals = achrisk.Signals{DestinationAge: 48 * time.Hour}
	request.Proposal.CanonicalDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	_, err := service.AuthorizeAndActivate(context.Background(), conn, tenantID, achrisk.DefaultPolicy(), input, request, at.Add(time.Hour))
	if err == nil {
		t.Fatal("mismatched proposal binding was accepted")
	}
	assertDestinationHead(t, conn, tenantID, store, current, 1)

	otherTenant := uuid.New()
	if _, err := service.AuthorizeAndActivate(context.Background(), conn, otherTenant, achrisk.DefaultPolicy(), input, request, at.Add(time.Hour)); err == nil {
		t.Fatal("tenant mismatch was accepted")
	}
	if _, err := achriskadapterMapTamper(request, at); err == nil {
		t.Fatal("tampered assessment digest was accepted")
	}
}

func achriskadapterMapTamper(request paymethod.ChangeAuthorizationRequest, at time.Time) (paymethod.RiskDecision, error) {
	assessment, err := achrisk.DefaultPolicy().Evaluate(achrisk.EvaluationInput{TenantID: request.BankDetailChange.TenantID, Participant: achrisk.Originator, EvaluatedAt: at, Signals: achrisk.Signals{DestinationAge: 48 * time.Hour}})
	if err != nil {
		return paymethod.RiskDecision{}, err
	}
	assessment.CanonicalDigest = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"
	return achriskadapter.MapAssessment(assessment, request.Proposal.CanonicalDigest, at.Add(time.Hour))
}

func changeFixture(t *testing.T, tenantID uuid.UUID, at time.Time) (paymethod.Destination, paymethod.ChangeAuthorizationRequest, achrisk.EvaluationInput) {
	t.Helper()
	worker := uuid.New().String()
	effective, err := values.NewOpenInstantInterval(values.NewInstant(at.Add(-365 * 24 * time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	current, err := paymethod.NewDestination(paymethod.Destination{DestinationID: "destination-rev052", WorkerRef: worker, Rail: paymethod.RailACH, Risk: paymethod.RiskMedium, GovernedRef: "vault-token:old", DisplayHint: "••••1234", Currency: "USD", CountryCode: "US", Verification: paymethod.VerificationVerified, VerificationDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Effective: effective})
	if err != nil {
		t.Fatal(err)
	}
	proposed, err := current.NewRevision(paymethod.Destination{GovernedRef: "vault-token:new", DisplayHint: "••••5678", Currency: "USD", CountryCode: "US", Rail: paymethod.RailACH, Risk: paymethod.RiskMedium, Verification: paymethod.VerificationVerified, VerificationDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Effective: effective})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := paymethod.NewDestinationChange(current, proposed, "proposal-rev052", "requester", "approver", effective)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := contact.NewContactEndpointRevision(values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: values.Kind("worker"), Id: worker}, "contact-rev052", contact.EndpointEmail, "payroll@example.test", "payment_destination_change_confirmation", 1, "hr-contact-profile")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = endpoint.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	change, err := paymethod.StartBankDetailChange(paymethod.BankDetailChangeRequest{ID: "change-rev052", TenantID: tenantID.String(), DestinationID: proposal.DestinationID, WorkerRef: worker, BeforeDigest: current.CanonicalDigest, AfterDigest: proposed.CanonicalDigest, RequestedBy: "requester", Approver: "approver", RequestedAt: at.Add(-48 * time.Hour), CoolingOff: time.Hour, Constraints: sod.Constraints{RuleID: "payment-destination-dual-control", RequesterMayNotApprove: true, OneApprovalPerPrincipal: true}, DecisionContext: sod.DecisionContext{Requester: sod.Actor{Subject: "requester"}, Approvers: []sod.Actor{{Subject: "approver"}}}}, testContactSource{endpoint: endpoint}, testConfirmationDispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	change, err = change.Confirm(at.Add(-36*time.Hour), "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenantID.String()), Subject: "requester", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-rev052", IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour), CredentialDigest: "sha256:principal-rev052"})
	if err != nil {
		t.Fatal(err)
	}
	request := paymethod.ChangeAuthorizationRequest{CurrentDestination: current, ProposedDestination: proposed, Proposal: proposal, CurrentProposal: paymethod.ProposalRevision{ID: proposal.ID, Digest: proposal.CanonicalDigest, Revision: 1}, BankDetailChange: change, Principal: principal, VerificationFreshUntil: at.Add(time.Hour), At: at}
	return current, request, achrisk.EvaluationInput{TenantID: tenantID.String(), Participant: achrisk.Originator, AnnualACHVolume2023: 1, EvaluatedAt: at}
}

func withTenantTx(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func withTenantTxResult(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) (Result, error)) Result {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		t.Fatal(err)
	}
	result, err := fn(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertDestinationHead(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, store paymethodstore.Store, want paymethod.Destination, revision uint64) {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadDestination(context.Background(), tx, tenantID, want.DestinationID, 0)
	if revision == 1 && errors.Is(err, paymethodstore.ErrNotFound) {
		t.Fatal("expected current destination to remain persisted")
	}
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != revision || got.CanonicalDigest != want.CanonicalDigest {
		t.Fatalf("destination head = %d/%s, want %d/%s", got.Revision, got.CanonicalDigest, revision, want.CanonicalDigest)
	}
}
