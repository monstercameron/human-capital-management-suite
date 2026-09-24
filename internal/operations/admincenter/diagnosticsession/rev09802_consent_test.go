package diagnosticsession_test

// REV-098-02: diagnostic work without customer consent is vendor snooping.
// Scope carries the customer approval bound to the exact grant; New refuses
// a session without consent and refuses transplanted consent.
//
// RED: Scope named tenant, subject, case, purpose, resources, actions and a
// window, but no consent field existed and New never asked for one.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ds "github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/diagnosticsession"
)

func rev09802Scope() ds.Scope {
	now := time.Unix(200, 0).UTC()
	return ds.Scope{
		TenantID: "tenant", SubjectID: "operator", CaseID: "case-9",
		Purpose: ds.PurposeSupport, Resources: []string{"incident:9"},
		Actions:  []ds.UIAction{ds.ActionViewSummary},
		IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
	}
}

func rev09802EmergencyScope() ds.Scope {
	now := time.Unix(300, 0).UTC()
	scope := ds.Scope{
		TenantID: "tenant", SubjectID: "operator", CaseID: "case-incident-9",
		Purpose: ds.PurposeEmergency, Resources: []string{"incident:9"},
		Actions: []ds.UIAction{ds.ActionViewSummary}, IssuedAt: now, ExpiresAt: now.Add(2 * time.Minute),
	}
	scope.Emergency = ds.BindEmergencyJustification(scope, "incident-9", "emergency-record-9", "incident-commander", "active payroll outage")
	return scope
}

func TestTodo_REV_098_02(t *testing.T) {
	scope := rev09802Scope()
	if _, err := ds.New(scope, acceptTestApproval()); !errors.Is(err, ds.ErrInvalidApproval) {
		t.Fatalf("consentless session = %v, want ErrInvalidApproval", err)
	}
	scope.Approval = ds.MintCustomerApproval(scope, "approval-9", "customer-approver")
	if _, err := ds.New(scope, nil); !errors.Is(err, ds.ErrApprovalUnverified) {
		t.Fatalf("approval without trusted verifier = %v, want ErrApprovalUnverified", err)
	}
	if _, err := ds.New(scope, ds.ApprovalVerifierFunc(func(ds.Scope, ds.CustomerApproval) error { return errors.New("approval not recorded by customer") })); !errors.Is(err, ds.ErrApprovalUnverified) {
		t.Fatalf("unverified customer reference = %v, want ErrApprovalUnverified", err)
	}
	session, err := ds.New(scope, acceptTestApproval())
	if err != nil {
		t.Fatalf("approved session: %v", err)
	}
	if session.Scope().Approval.Reference != "approval-9" {
		t.Fatalf("session approval = %+v, want approval-9 carried", session.Scope().Approval)
	}
}

func TestTodo_REV_098_02_Lifecycle(t *testing.T) {
	scope := rev09802Scope()
	scope.Approval = ds.MintCustomerApproval(scope, "approval-9", "customer-approver")
	verified := false
	ledger := ds.NewLedger(ds.ApprovalVerifierFunc(func(got ds.Scope, approval ds.CustomerApproval) error {
		verified = got.TenantID == "tenant" && approval.Reference == "approval-9" && approval.ApproverID == "customer-approver"
		if !verified {
			return errors.New("approval record mismatch")
		}
		return nil
	}))
	now := scope.IssuedAt
	if _, err := ledger.Open("session-9", scope, now); err != nil || !verified {
		t.Fatalf("verified consent open = %v, verified=%t", err, verified)
	}
	if got := ledger.Audit(); len(got) != 1 || got[0].Kind != ds.AuditSessionOpened || got[0].ApprovalRef != "approval-9" || got[0].TenantID != "tenant" {
		t.Fatalf("open audit = %+v", got)
	}
	if err := ledger.Revoke("session-9", "support-operator", "customer requested stop", now.Add(time.Minute)); !errors.Is(err, ds.ErrRevocationDenied) {
		t.Fatalf("unauthorized revocation = %v", err)
	}
	if _, err := ledger.Inspect("session-9", now.Add(time.Minute)); err != nil {
		t.Fatalf("unauthorized revocation changed active session: %v", err)
	}
	if err := ledger.Revoke("session-9", "customer-approver", "customer requested stop", now.Add(time.Minute)); err != nil {
		t.Fatalf("customer revocation = %v", err)
	}
	if _, err := ledger.Inspect("session-9", now.Add(2*time.Minute)); !errors.Is(err, ds.ErrSessionRevoked) {
		t.Fatalf("inspect revoked session = %v", err)
	}
	if err := ledger.Revoke("session-9", "customer-approver", "repeat", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("idempotent revocation = %v", err)
	}
	if got := ledger.Audit(); len(got) != 2 || got[1].Kind != ds.AuditSessionRevoked || got[1].ActorID != "customer-approver" || got[1].RevocationReason != "customer requested stop" {
		t.Fatalf("lifecycle audit = %+v", got)
	}
}

func TestTodo_REV_098_02_AuthorizationAndLifetime(t *testing.T) {
	scope := rev09802Scope()
	scope.Approval = ds.MintCustomerApproval(scope, "approval-9", "customer-approver")
	deny := ds.NewLedger(ds.ApprovalVerifierFunc(func(ds.Scope, ds.CustomerApproval) error { return errors.New("not in approval system") }))
	if _, err := deny.Open("session-9", scope, scope.IssuedAt); !errors.Is(err, ds.ErrApprovalUnverified) {
		t.Fatalf("unverified approval open = %v", err)
	}
	if got := deny.Audit(); len(got) != 0 {
		t.Fatalf("failed authorization was audited as open: %+v", got)
	}
	verified := ds.NewLedger(ds.ApprovalVerifierFunc(func(ds.Scope, ds.CustomerApproval) error { return nil }))
	if _, err := verified.Open("session-9", scope, scope.ExpiresAt); !errors.Is(err, ds.ErrExpired) {
		t.Fatalf("expired consent open = %v", err)
	}
	if got := verified.Audit(); len(got) != 0 {
		t.Fatalf("expired session was audited as open: %+v", got)
	}
}

func TestTodo_REV_098_02_Emergency(t *testing.T) {
	scope := rev09802EmergencyScope()
	verifier := ds.IncidentEmergencyVerifierFunc(func(got ds.Scope, justification ds.EmergencyJustification) error {
		if got.Purpose != ds.PurposeEmergency || justification.IncidentID != "incident-9" || justification.RecordReference != "emergency-record-9" || justification.DeclarerID != "incident-commander" {
			return errors.New("incident declaration mismatch")
		}
		return nil
	})
	if _, err := ds.New(scope, nil); !errors.Is(err, ds.ErrEmergencyUnverified) {
		t.Fatalf("emergency without incident verifier = %v", err)
	}
	if _, err := ds.New(scope, nil, ds.IncidentEmergencyVerifierFunc(func(ds.Scope, ds.EmergencyJustification) error { return errors.New("incident absent") })); !errors.Is(err, ds.ErrEmergencyUnverified) {
		t.Fatalf("unrecorded emergency = %v", err)
	}
	session, err := ds.New(scope, nil, verifier)
	if err != nil {
		t.Fatalf("verified incident emergency = %v", err)
	}
	if session.Scope().Approval != (ds.CustomerApproval{}) || session.Scope().Emergency.RecordReference != "emergency-record-9" || !session.AllowsAction(ds.ActionViewSummary, scope.IssuedAt.Add(time.Minute)) {
		t.Fatalf("emergency session scope/access = %+v", session.Scope())
	}
	if session.AllowsAction(ds.ActionViewSummary, scope.ExpiresAt) || session.AllowsResource("incident:9", scope.ExpiresAt) {
		t.Fatal("expired emergency session allowed access")
	}

	tooLong := rev09802EmergencyScope()
	tooLong.ExpiresAt = tooLong.IssuedAt.Add(ds.EmergencyTTL + time.Nanosecond)
	tooLong.Emergency = ds.BindEmergencyJustification(tooLong, "incident-9", "emergency-record-9", "incident-commander", "active payroll outage")
	if _, err := ds.New(tooLong, nil, verifier); !errors.Is(err, ds.ErrInvalidScope) {
		t.Fatalf("overlong emergency = %v", err)
	}

	missingReason := rev09802EmergencyScope()
	missingReason.Emergency = ds.BindEmergencyJustification(missingReason, "incident-9", "emergency-record-9", "incident-commander", " ")
	if _, err := ds.New(missingReason, nil, verifier); !errors.Is(err, ds.ErrInvalidScope) {
		t.Fatalf("emergency without recorded reason = %v", err)
	}

	ledger := ds.NewLedger(nil, verifier)
	opened, err := ledger.Open("emergency-session-9", scope, scope.IssuedAt)
	if err != nil || !opened.AllowsResource("incident:9", scope.IssuedAt.Add(time.Second)) {
		t.Fatalf("emergency ledger open/access = %v", err)
	}
	if got := ledger.Audit(); len(got) != 1 || got[0].EmergencyRef != "emergency-record-9" || got[0].IncidentID != "incident-9" || got[0].EmergencyReason != "active payroll outage" || got[0].ActorID != "incident-commander" {
		t.Fatalf("emergency opening audit = %+v", got)
	}
	if err := ledger.Revoke("emergency-session-9", "incident-commander", "incident contained", scope.IssuedAt.Add(time.Minute)); err != nil {
		t.Fatalf("emergency declarer revocation = %v", err)
	}
	if _, err := ledger.Inspect("emergency-session-9", scope.IssuedAt.Add(time.Minute)); !errors.Is(err, ds.ErrSessionRevoked) {
		t.Fatalf("inspect revoked emergency = %v", err)
	}
}

func TestTodo_REV_098_02_Security(t *testing.T) {
	scope := rev09802Scope()
	// Transplanted consent from another scope is forged consent: refused.
	other := rev09802Scope()
	other.CaseID = "case-10"
	other.Approval = ds.MintCustomerApproval(other, "approval-10", "customer-approver")
	scope.Approval = other.Approval
	if _, err := ds.New(scope, acceptTestApproval()); !errors.Is(err, ds.ErrInvalidApproval) {
		t.Fatalf("transplanted consent session = %v, want ErrInvalidApproval", err)
	}
	// A bare reference with no digest binds nothing.
	scope.Approval = ds.CustomerApproval{Reference: "approval-9"}
	if _, err := ds.New(scope, acceptTestApproval()); !errors.Is(err, ds.ErrInvalidApproval) {
		t.Fatalf("digestless consent session = %v, want ErrInvalidApproval", err)
	}
	// A valid digest under a swapped reference is tampering: refused.
	scope.Approval = ds.MintCustomerApproval(scope, "approval-9", "customer-approver")
	scope.Approval.Reference = "approval-evil"
	if _, err := ds.New(scope, acceptTestApproval()); !errors.Is(err, ds.ErrInvalidApproval) {
		t.Fatalf("retargeted consent session = %v, want ErrInvalidApproval", err)
	}
}

func TestTodo_REV_098_02_Golden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "consent_digest.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got, want := ds.ScopeDigest(rev09802Scope()), strings.TrimSpace(string(raw)); got != want {
		t.Fatalf("scope digest drifted:\n got: %q\nwant: %q", got, want)
	}
}
