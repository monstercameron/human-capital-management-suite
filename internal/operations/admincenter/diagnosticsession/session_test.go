package diagnosticsession_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ds "github.com/monstercameron/human-capital-management-suite/internal/operations/admincenter/diagnosticsession"
)

func scope() ds.Scope {
	now := time.Unix(100, 0).UTC()
	scope := ds.Scope{TenantID: "tenant", SubjectID: "operator", CaseID: "case-1", Purpose: ds.PurposeSupport, Resources: []string{"incident:123"}, Actions: []ds.UIAction{ds.ActionViewSummary, ds.ActionViewEvidence}, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	scope.Approval = ds.MintApproval(scope, "approval-1")
	return scope
}

func TestADMIN006RegistryExact(t *testing.T) {
	r := ds.Registry()
	if r.ID != "ADMIN-006" || r.Version != "hcmnext.admincenter.diagnostic-session/1" || r.Purpose != ds.PurposeSupport || r.MaxTTL != 15*time.Minute || !r.RedactionRequired {
		t.Fatalf("registry = %+v", r)
	}
	want := []ds.UIAction{ds.ActionViewSummary, ds.ActionViewTimeline, ds.ActionViewEvidence, ds.ActionCopyRedactedReference}
	if !reflect.DeepEqual(r.Actions, want) {
		t.Fatalf("actions = %v, want %v", r.Actions, want)
	}
}

func TestADMIN006JITScopeAndExpiry(t *testing.T) {
	s, err := ds.New(scope())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(time.Unix(101, 0)); err != nil {
		t.Fatal(err)
	}
	if !s.AllowsResource("incident:123") || s.AllowsResource("incident:999") || !s.AllowsAction(ds.ActionViewSummary) {
		t.Fatal("scope widened or denied unexpectedly")
	}
	if !s.Expired(time.Unix(400, 0)) {
		t.Fatal("expiry not enforced")
	}
	if err := s.Validate(time.Unix(400, 0)); !errors.Is(err, ds.ErrExpired) {
		t.Fatalf("Validate = %v", err)
	}
	bad := scope()
	bad.ExpiresAt = bad.IssuedAt.Add(16 * time.Minute)
	bad.Approval = ds.MintApproval(bad, "approval-1")
	if _, err := ds.New(bad); !errors.Is(err, ds.ErrInvalidScope) {
		t.Fatalf("long TTL = %v", err)
	}
	bad = scope()
	bad.Actions = []ds.UIAction{"delete_user"}
	bad.Approval = ds.MintApproval(bad, "approval-1")
	if _, err := ds.New(bad); !errors.Is(err, ds.ErrActionDenied) {
		t.Fatalf("unsafe action = %v", err)
	}
}

func TestADMIN006RedactsEvidenceWithoutMutating(t *testing.T) {
	p := map[string]any{"email": "person@example.com", "nested": map[string]any{"token": "abc", "status": "ok"}, "count": 3}
	e := ds.PrepareEvidence(ds.Evidence{ID: "ev-1", Payload: p})
	got := e.Payload.(map[string]any)
	if got["email"] != ds.RedactedValue || got["nested"].(map[string]any)["token"] != ds.RedactedValue || got["nested"].(map[string]any)["status"] != "ok" {
		t.Fatalf("payload = %#v", got)
	}
	if !e.Redacted || p["email"] != "person@example.com" {
		t.Fatal("evidence was not safely copied")
	}
}

func TestADMIN006JITScopeRequiredFieldsMatrix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ds.Scope)
	}{
		{"tenant", func(s *ds.Scope) { s.TenantID = " " }},
		{"subject", func(s *ds.Scope) { s.SubjectID = "" }},
		{"case", func(s *ds.Scope) { s.CaseID = "\t" }},
		{"purpose", func(s *ds.Scope) { s.Purpose = "other" }},
		{"resources", func(s *ds.Scope) { s.Resources = nil }},
		{"actions", func(s *ds.Scope) { s.Actions = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scope()
			tc.mutate(&got)
			if _, err := ds.New(got); !errors.Is(err, ds.ErrInvalidScope) {
				t.Fatalf("New(%s) = %v, want ErrInvalidScope", tc.name, err)
			}
		})
	}
}

func TestADMIN006TTLAndExpiryBoundaries(t *testing.T) {
	base := scope()
	base.ExpiresAt = base.IssuedAt.Add(ds.MaxTTL)
	base.Approval = ds.MintApproval(base, "approval-1")
	if _, err := ds.New(base); err != nil {
		t.Fatalf("maximum TTL rejected: %v", err)
	}

	s, err := ds.New(scope())
	if err != nil {
		t.Fatal(err)
	}
	if s.Expired(s.Scope().ExpiresAt.Add(-time.Nanosecond)) {
		t.Fatal("session expired before its deadline")
	}
	if !s.Expired(s.Scope().ExpiresAt) {
		t.Fatal("session remained valid at its deadline")
	}
}

func TestADMIN006ScopeAndAllowlistIsolation(t *testing.T) {
	in := scope()
	s, err := ds.New(in)
	if err != nil {
		t.Fatal(err)
	}
	// New must detach caller-owned slices, and Scope must return fresh copies.
	in.Resources[0] = "tenant-b:incident:123"
	in.Actions[0] = ds.ActionViewTimeline
	got := s.Scope()
	if !s.AllowsResource("incident:123") || s.AllowsResource("tenant-b:incident:123") || !s.AllowsAction(ds.ActionViewSummary) {
		t.Fatalf("caller mutation widened or changed session: %+v", got)
	}
	got.Resources[0] = "tenant-c:incident:123"
	got.Actions[0] = ds.ActionViewTimeline
	if s.AllowsResource("tenant-c:incident:123") || s.AllowsAction(ds.ActionViewTimeline) {
		t.Fatal("Scope returned aliased mutable slices")
	}
}

func TestADMIN006UIActionAndSensitiveKeyBoundaries(t *testing.T) {
	for _, action := range []ds.UIAction{ds.ActionViewSummary, ds.ActionViewTimeline, ds.ActionViewEvidence, ds.ActionCopyRedactedReference} {
		if !ds.IsSafeUIAction(action) {
			t.Fatalf("safe action %q rejected", action)
		}
	}
	if ds.IsSafeUIAction("delete") {
		t.Fatal("unsafe action accepted")
	}
	for _, key := range []string{" PASSWORD ", "secret_value", "TOKEN", "authorization", "cookie", "email", "phone", "ssn", "salary", "access_key", "private_key"} {
		if !ds.SensitiveKey(key) {
			t.Fatalf("sensitive key %q was not recognized", key)
		}
	}
	if ds.SensitiveKey("status") {
		t.Fatal("ordinary key classified as sensitive")
	}
	input := []any{map[string]any{"status": "ok", "secret": "hidden"}, "plain"}
	got := ds.Redact(input).([]any)
	if got[0].(map[string]any)["secret"] != ds.RedactedValue || got[0].(map[string]any)["status"] != "ok" || got[1] != "plain" {
		t.Fatalf("redacted array=%#v", got)
	}
}

func TestADMIN006EvidenceDigestBoundIsEnforced(t *testing.T) {
	e := ds.PrepareEvidence(ds.Evidence{ID: "large", Payload: map[string]any{"blob": strings.Repeat("x", ds.MaxEvidenceBytes)}})
	if !e.Redacted || e.Digest != "" {
		t.Fatalf("oversized evidence controls=%+v", e)
	}
	if got := ds.PrepareEvidence(ds.Evidence{ID: "scalar", Payload: "safe"}); got.Digest == "" || !got.Redacted {
		t.Fatalf("scalar evidence=%+v", got)
	}
}

func TestADMIN006NewDefaultsAndBoundaryErrors(t *testing.T) {
	bare := ds.Scope{TenantID: "tenant", SubjectID: "subject", CaseID: "case", Purpose: ds.PurposeSupport, Resources: []string{"incident:1"}, Actions: []ds.UIAction{ds.ActionViewSummary}}
	bare.Approval = ds.MintApproval(bare, "approval-1")
	s, err := ds.New(bare)
	if err != nil {
		t.Fatal(err)
	}
	if s.Scope().IssuedAt.IsZero() || !s.Scope().ExpiresAt.After(s.Scope().IssuedAt) {
		t.Fatalf("defaults not applied: %+v", s.Scope())
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ds.Scope)
	}{
		{"empty resource", func(s *ds.Scope) { s.Resources = []string{" "} }},
		{"equal expiry", func(s *ds.Scope) { s.ExpiresAt = s.IssuedAt }},
		{"expired at now", func(s *ds.Scope) { s.ExpiresAt = s.IssuedAt.Add(-time.Nanosecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := scope()
			tc.mutate(&in)
			in.Approval = ds.MintApproval(in, "approval-1")
			if _, err := ds.New(in); !errors.Is(err, ds.ErrInvalidScope) {
				t.Fatalf("New=%v", err)
			}
		})
	}
}
