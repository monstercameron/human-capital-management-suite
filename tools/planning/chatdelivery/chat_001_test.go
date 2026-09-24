package chatdelivery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func artifact(t *testing.T) Record {
	t.Helper()
	r, err := Load(filepath.Join(repoRoot(t), ArtifactPath))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTodo_CHAT_001(t *testing.T) {
	r := artifact(t)
	if violations := Validate(r); len(violations) != 0 {
		t.Fatalf("CHAT-001 artifact is invalid: %v", violations)
	}
	if r.Status == "APPROVED" || len(r.Approval.Signers) > 0 {
		t.Fatal("planning artifact must not claim external approval")
	}
}

func TestTodo_CHAT_001_Golden(t *testing.T) {
	r := artifact(t)
	computed, err := Digest(r)
	if err != nil {
		t.Fatal(err)
	}
	if computed != r.CanonicalDigest {
		t.Fatalf("digest changed: got %s want %s", computed, r.CanonicalDigest)
	}
	if got, want := r.CanonicalDigest, "sha256:5517e17ea55d35559f257a891c802823d78b460b36d6e16db85b7a3d9870ac61"; got != want {
		t.Fatalf("CHAT-001 golden digest changed: got %s want %s", got, want)
	}
	if got := (Violation{Field: "status", Issue: "must remain PROPOSED until approval evidence exists"}).String(); got != "CHAT-001: status: must remain PROPOSED until approval evidence exists" {
		t.Fatalf("violation formatting changed: %s", got)
	}
}

func TestChatArtifactRejectsUnapprovedMutation(t *testing.T) {
	r := artifact(t)
	r.Status = "APPROVED"
	if len(Validate(r)) == 0 {
		t.Fatal("approved mutation must be rejected")
	}
}

func TestChatArtifactRejectsIncompleteRecords(t *testing.T) {
	base := artifact(t)
	cases := []struct {
		name   string
		mutate func(*Record)
		field  string
	}{
		{"wrong todo", func(r *Record) { r.TodoID = "CHAT-002" }, "todo_id"},
		{"wrong status", func(r *Record) { r.Status = "APPROVED" }, "status"},
		{"wrong scope decision", func(r *Record) { r.ScopeDecision = "P1B" }, "scope_decision"},
		{"missing owner", func(r *Record) { r.ReleaseOwner = "" }, "release_owner"},
		{"missing displacement", func(r *Record) { r.DisplacedWork = nil }, "displaced_work"},
		{"incomplete displacement", func(r *Record) { r.DisplacedWork[0].Owner = "" }, "displaced_work[0]"},
		{"missing owners", func(r *Record) { r.Owners = nil }, "owners"},
		{"incomplete owner", func(r *Record) { r.Owners[0].Responsibility = "" }, "owners[0]"},
		{"missing pilot", func(r *Record) { r.PilotTenants = nil }, "pilot_tenants"},
		{"missing pilot criteria", func(r *Record) { r.PilotCriteria = nil }, "pilot_selection_criteria"},
		{"missing slos", func(r *Record) { r.SLOs = nil }, "slos"},
		{"incomplete slo", func(r *Record) { r.SLOs[0].Target = "" }, "slos[0]"},
		{"missing activation", func(r *Record) { r.Activation = nil }, "activation_conditions"},
		{"missing deactivation", func(r *Record) { r.Deactivation = nil }, "deactivation_conditions"},
		{"missing blockers", func(r *Record) { r.Blockers = nil }, "unmet_gates"},
		{"missing source evidence", func(r *Record) { r.Evidence = nil }, "source_evidence"},
		{"incomplete source evidence", func(r *Record) { r.Evidence[0].Path = "" }, "source_evidence[0]"},
		{"p1a not preserved", func(r *Record) { r.PreserveP1A = false }, "preserve_p1_inventory"},
		{"p1b not preserved", func(r *Record) { r.PreserveP1B = false }, "preserve_p1_inventory"},
		{"approval completed", func(r *Record) { r.Approval.Status = "APPROVED" }, "approval.status"},
		{"missing approval roles", func(r *Record) { r.Approval.RequiredRoles = nil }, "approval.required_roles"},
		{"fabricated signer", func(r *Record) { r.Approval.Signers = []string{"someone"} }, "approval.signers"},
		{"stale digest", func(r *Record) { r.CanonicalDigest = "sha256:bad" }, "canonical_digest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			r.DisplacedWork = append([]DisplacedWork(nil), base.DisplacedWork...)
			r.Owners = append([]Owner(nil), base.Owners...)
			r.SLOs = append([]SLO(nil), base.SLOs...)
			r.PilotTenants = append([]string(nil), base.PilotTenants...)
			r.PilotCriteria = append([]string(nil), base.PilotCriteria...)
			r.Activation = append([]string(nil), base.Activation...)
			r.Deactivation = append([]string(nil), base.Deactivation...)
			r.Blockers = append([]string(nil), base.Blockers...)
			r.Evidence = append([]Evidence(nil), base.Evidence...)
			r.Approval.RequiredRoles = append([]string(nil), base.Approval.RequiredRoles...)
			r.Approval.Signers = append([]string(nil), base.Approval.Signers...)
			tc.mutate(&r)
			for _, violation := range Validate(r) {
				if violation.Field == tc.field {
					return
				}
			}
			t.Fatalf("expected a %s violation, got %v", tc.field, Validate(r))
		})
	}
}

func TestChatArtifactLoadRejectsUnreadableOrMalformedSource(t *testing.T) {
	if _, err := Load(filepath.Join(repoRoot(t), "does-not-exist.json")); err == nil {
		t.Fatal("missing artifact should fail to load")
	}
	path := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("malformed artifact should fail to decode")
	}
}
