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

func TestTodo_REV_098_02(t *testing.T) {
	scope := rev09802Scope()
	if _, err := ds.New(scope); !errors.Is(err, ds.ErrInvalidApproval) {
		t.Fatalf("consentless session = %v, want ErrInvalidApproval", err)
	}
	scope.Approval = ds.MintApproval(scope, "approval-9")
	session, err := ds.New(scope)
	if err != nil {
		t.Fatalf("approved session: %v", err)
	}
	if session.Scope().Approval.Reference != "approval-9" {
		t.Fatalf("session approval = %+v, want approval-9 carried", session.Scope().Approval)
	}
}

func TestTodo_REV_098_02_Security(t *testing.T) {
	scope := rev09802Scope()
	// Transplanted consent from another scope is forged consent: refused.
	other := rev09802Scope()
	other.CaseID = "case-10"
	other.Approval = ds.MintApproval(other, "approval-10")
	scope.Approval = other.Approval
	if _, err := ds.New(scope); !errors.Is(err, ds.ErrInvalidApproval) {
		t.Fatalf("transplanted consent session = %v, want ErrInvalidApproval", err)
	}
	// A bare reference with no digest binds nothing.
	scope.Approval = ds.CustomerApproval{Reference: "approval-9"}
	if _, err := ds.New(scope); !errors.Is(err, ds.ErrInvalidApproval) {
		t.Fatalf("digestless consent session = %v, want ErrInvalidApproval", err)
	}
	// A valid digest under a swapped reference is tampering: refused.
	scope.Approval = ds.MintApproval(scope, "approval-9")
	scope.Approval.Reference = "approval-evil"
	if _, err := ds.New(scope); !errors.Is(err, ds.ErrInvalidApproval) {
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
