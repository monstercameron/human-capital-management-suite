package diagnosticsession_test

import (
	"errors"
	ds "github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/diagnosticsession"
	"testing"
	"time"
)

func TestTodo_ADMIN_006(t *testing.T) {
	r := ds.Registry()
	if r.ID != "ADMIN-006" || !r.RedactionRequired || !r.AuditRequired || r.MaxEvidenceBytes <= 0 || r.MaxEvidenceCPU <= 0 {
		t.Fatalf("registry missing controls: %+v", r)
	}
	now := time.Unix(100, 0).UTC()
	scope := ds.Scope{TenantID: "t", SubjectID: "s", CaseID: "c", Purpose: ds.PurposeSupport, Resources: []string{"incident:1"}, Actions: []ds.UIAction{ds.ActionViewEvidence}, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	scope.Approval = ds.MintCustomerApproval(scope, "approval-1", "customer-approver")
	s, err := ds.New(scope, acceptTestApproval())
	if err != nil || s.Validate(now.Add(30*time.Second)) != nil || !s.AllowsResource("incident:1", now.Add(30*time.Second)) {
		t.Fatalf("bounded scope rejected: %v", err)
	}
	e := ds.PrepareEvidence(ds.Evidence{ID: "e", SessionCaseID: "c", Payload: map[string]any{"token": "secret", "ok": true}})
	if !e.Redacted || e.Digest == "" {
		t.Fatalf("evidence lacks controls: %+v", e)
	}
}

func TestTodo_ADMIN_006_Security(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	base := ds.Scope{TenantID: "t", SubjectID: "s", CaseID: "c", Purpose: ds.PurposeSupport, Resources: []string{"incident:1"}, Actions: []ds.UIAction{ds.ActionViewSummary}, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	base.Approval = ds.MintCustomerApproval(base, "approval-1", "customer-approver")
	for name, s := range map[string]ds.Scope{"missing tenant": func() ds.Scope { x := base; x.TenantID = ""; return x }(), "unsafe action": func() ds.Scope {
		x := base
		x.Actions = []ds.UIAction{"delete"}
		x.Approval = ds.MintCustomerApproval(x, "approval-1", "customer-approver")
		return x
	}(), "overlong ttl": func() ds.Scope {
		x := base
		x.ExpiresAt = now.Add(ds.MaxTTL + time.Nanosecond)
		x.Approval = ds.MintCustomerApproval(x, "approval-1", "customer-approver")
		return x
	}()} {
		t.Run(name, func(t *testing.T) {
			_, err := ds.New(s, acceptTestApproval())
			if !errors.Is(err, ds.ErrInvalidScope) && !errors.Is(err, ds.ErrActionDenied) {
				t.Fatalf("New = %v", err)
			}
		})
	}
	s, err := ds.New(base, acceptTestApproval())
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.Validate(now.Add(time.Minute)), ds.ErrExpired) {
		t.Fatal("expiry boundary was not denied")
	}
}
