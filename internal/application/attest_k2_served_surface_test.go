package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/attest"
)

var servedAttestationTime = attest.FixedTrustedTime(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
var servedAttestationClock = attest.TrustedClockFunc(func() (attest.TrustedTime, error) { return servedAttestationTime, nil })

func servedAttestationRequest(status attest.ResponseStatus) attest.ResponseRequest {
	return attest.ResponseRequest{
		Tenant: "tenant-a", ResponseID: "response-1", StatementID: "statement-1", StatementVersion: 3,
		StatementDigest: "sha256:statement", BindingDigest: "sha256:binding", Status: status,
		Reason: func() string {
			if status == attest.ResponseAccepted {
				return ""
			}
			return "respondent declined"
		}(),
		EvidenceReceipt: "sha256:evidence", IdempotencyKey: "idem-1", TransactionID: "tx-1",
	}
}

func servedAttestationRecorder(t *testing.T) (*application.ServedAttestationSurface, *attest.MemoryResponseStore) {
	t.Helper()
	surface := application.NewServedAttestationSurface()
	store := attest.NewMemoryResponseStore()
	return &surface, store
}

func TestTodo_ATTEST_005_Served(t *testing.T) {
	surface, store := servedAttestationRecorder(t)
	refused, err := surface.RecordResponse(context.Background(), store, servedAttestationClock, servedAttestationRequest(attest.ResponseRefused))
	if err != nil {
		t.Fatal(err)
	}
	correction := servedAttestationRequest(attest.ResponseAccepted)
	correction.ResponseID, correction.IdempotencyKey = "response-correction", "idem-correction"
	correction.Kind, correction.CorrectsResponseID, correction.Authority = attest.AssertionCorrection, refused.ResponseID, "authority:hr"
	correction.Reason, correction.AffectedObligations = "corrected respondent record", []string{"obligation-1"}
	got, err := surface.RecordResponse(context.Background(), store, servedAttestationClock, correction)
	if err != nil || got.Kind != attest.AssertionCorrection || got.CorrectsResponseID != refused.ResponseID || got.Authority != "authority:hr" {
		t.Fatalf("served correction=%+v err=%v", got, err)
	}
	if refused.Status == attest.ResponseAccepted {
		t.Fatal("served correction coerced the prior refusal")
	}
}

func TestTodo_ATTEST_006_Served(t *testing.T) {
	surface, store := servedAttestationRecorder(t)
	req := servedAttestationRequest(attest.ResponseAccepted)
	req.AffectedObligations = []string{"obligation-1"}
	response, err := surface.RecordResponse(context.Background(), store, servedAttestationClock, req)
	if err != nil {
		t.Fatal(err)
	}
	requirement := attest.ExecutionRequirement{
		Tenant: response.Tenant, ObligationID: "obligation-1", StatementID: response.StatementID,
		StatementVersion: response.StatementVersion, StatementDigest: response.StatementDigest,
		BindingDigest: response.BindingDigest, ResponseID: response.ResponseID,
		ResponseRevision: response.Revision, TransactionID: response.TransactionID,
	}
	called := false
	decision, err := surface.EnforceBeforeEffect(context.Background(), store, servedAttestationClock, requirement, func(context.Context, attest.ExecutionDecision) error {
		called = true
		return nil
	})
	if err != nil || !decision.Allowed || !called {
		t.Fatalf("served execution gate decision=%+v called=%v err=%v", decision, called, err)
	}
	called = false
	requirement.StatementDigest = "sha256:stale"
	decision, err = surface.EnforceBeforeEffect(context.Background(), store, servedAttestationClock, requirement, func(context.Context, attest.ExecutionDecision) error {
		called = true
		return nil
	})
	if !errors.Is(err, attest.ErrRequiredAttestation) || decision.Allowed || called {
		t.Fatalf("served stale gate decision=%+v called=%v err=%v", decision, called, err)
	}
}

func TestTodo_ATTEST_007_Served(t *testing.T) {
	surface, store := servedAttestationRecorder(t)
	response, err := surface.RecordResponse(context.Background(), store, servedAttestationClock, servedAttestationRequest(attest.ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := surface.ExportEvidencePackage(attest.ExportRequest{
		Authorization: attest.EvidenceAuthorization{Tenant: "tenant-a", Purpose: "audit", Recipient: "auditor", RedactionProfile: "minimum-necessary/v1", DecisionDigest: "sha256:authz", Allowed: true},
		StatementID:   response.StatementID, StatementVersion: response.StatementVersion, StatementDigest: response.StatementDigest,
		BindingDigest: response.BindingDigest, Response: response,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := surface.VerifyEvidencePackage(pkg); !got.Valid || got.Status != "COMPLETE" {
		t.Fatalf("served evidence verification=%+v", got)
	}
	pkg.ResponseDigest = "sha256:tampered"
	if got := surface.VerifyEvidencePackage(pkg); got.Valid || got.Status != "ATTEST_007_REJECTED" || got.Invalid != "digest" {
		t.Fatalf("served tampered evidence verification=%+v", got)
	}
}

func TestTodo_ATTEST_008_Served(t *testing.T) {
	surface, store := servedAttestationRecorder(t)
	response, err := surface.RecordResponse(context.Background(), store, servedAttestationClock, servedAttestationRequest(attest.ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	report, err := surface.ProveConformance([]attest.ConformanceCase{
		{Domain: attest.DomainTime, Claim: attest.ClaimTimecardAccuracy, Response: response},
		{Domain: attest.DomainPayroll, Claim: attest.ClaimPayrollInputCompleteness, Response: response},
		{Domain: attest.DomainLegal, Claim: attest.ClaimLegalFact, Response: response},
	})
	if err != nil || !report.Valid || !report.SharedEvidence || !report.DistinctFromApproval || !report.DistinctFromSignature || !report.DistinctFromForm {
		t.Fatalf("served acknowledgement conformance=%+v err=%v", report, err)
	}
}

func TestServedAttestationAppSurface(t *testing.T) {
	var app application.App
	if got := app.Attestation(); got.EnforceBeforeEffect == nil || got.ExportEvidencePackage == nil || got.ProveConformance == nil {
		t.Fatal("composed application omitted attestation surface")
	}
	var nilApp *application.App
	if got := nilApp.Attestation(); got.EnforceBeforeEffect != nil {
		t.Fatal("nil application exposed attestation capabilities")
	}
}
