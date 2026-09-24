package hcmctl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	exit "github.com/monstercameron/human-capital-management-suite/internal/governance/exit"
)

func rehearsalFixture(t *testing.T) exit.RehearsalRequest {
	t.Helper()
	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	base := exit.Request{
		Tenant: "tenant-1", RequestedBy: "operator-1", At: at, ShutdownComplete: true,
		Export:           exit.ExportReceipt{ID: "export-1", SchemaVersion: "exit-v1", Checksum: "sha256:abc", ExpectedDigest: "sha256:abc", Recipient: "customer-1", Verified: true},
		PendingWork:      []exit.PendingWork{{ID: "work-1", Disposition: "FROZEN", Reason: "exit"}},
		HoldExceptions:   []exit.HoldException{{ID: "hold-1", CopyID: "copy-CANONICAL", Authority: "legal-1", Reason: "litigation hold"}},
		RestoreReDeletes: []exit.RestoreReDelete{{ID: "rd-1", CopyID: "copy-BACKUP", TombstoneDigest: "sha256:tombstone", Watermark: "42", Reapplied: true}},
	}
	for i, category := range exit.RequiredCategories {
		id := "copy-" + string(category)
		base.Copies = append(base.Copies, exit.Copy{
			ID: id, Tenant: base.Tenant, Category: category, Location: "store-" + string(rune('a'+i)),
			Owner: "records", Region: "us-east", KeyRef: "key-1", RetentionPolicy: "retain-v1",
			RestorePolicy: "redelete-v1", FreshAt: at.Add(-time.Hour), Known: true, DeletionSupported: true,
			ActiveAuthority: i >= 4 && i <= 7, RestoreReDeleteNeeded: category == exit.CategoryBackup,
		})
	}
	for i, kind := range []string{"provider", "credential", "webhook", "support"} {
		base.Revocations = append(base.Revocations, exit.RevocationReceipt{
			ID: "r-" + kind, Target: base.Copies[4+i].ID, Kind: kind, At: at, Success: true,
		})
	}
	categories := make([]exit.ExportCategory, 0, len(exit.RequiredCategories))
	for i, category := range exit.RequiredCategories {
		categories = append(categories, exit.ExportCategory{
			Category: category, CopyID: base.Copies[i].ID,
			Fields: []string{"id"}, Records: []json.RawMessage{json.RawMessage(`{"id":"record-1"}`)},
		})
	}
	_, pkg, err := exit.ExecuteExit(base, exit.ExportBuildRequest{
		ID: "export-1", Tenant: base.Tenant, Recipient: base.Export.Recipient,
		Copies: base.Copies, Categories: categories,
	})
	if err != nil {
		t.Fatalf("build exit export package: %v", err)
	}
	base.Export, base.ExportPackage = pkg.Receipt, &pkg
	return exit.RehearsalRequest{
		RehearsalID: "rehearsal-1", Tenant: base.Tenant, RequestedBy: "operator-1", At: at, Exit: base,
		Steps: []exit.RehearsalStep{
			{ID: "export", Title: "Produce and verify the tenant export", Observed: true, EvidenceRef: "export-1"},
			{ID: "shutdown", Title: "Shut down connectors and tenant", Observed: true, EvidenceRef: "shutdown-1"},
			{ID: "revoke", Title: "Revoke live authorities", Observed: true, EvidenceRef: "r-provider"},
			{ID: "restore-plan", Title: "Record the restore re-delete plan", Observed: true, EvidenceRef: "rd-1"},
		},
	}
}

func writeRehearsalFile(t *testing.T, request exit.RehearsalRequest) string {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal rehearsal fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "rehearsal.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write rehearsal fixture: %v", err)
	}
	return path
}

func TestTodo_REV_015_03(t *testing.T) {
	path := writeRehearsalFile(t, rehearsalFixture(t))
	cmd, err := parseArgs([]string{"tenant", "exit-rehearsal", "-tenant", "tenant-1", "-file", path})
	if err != nil {
		t.Fatalf("parse exit rehearsal: %v", err)
	}
	if cmd.runLocal == nil {
		t.Fatal("exit rehearsal did not select the local command path")
	}
	result, err := cmd.runLocal()
	if err != nil {
		t.Fatalf("run exit rehearsal: %v", err)
	}
	for _, want := range []string{"verdict: CERTIFIABLE", `"revocations": [`, `"exceptions": [`, `"restore_plan": [`} {
		if !strings.Contains(result, want) {
			t.Fatalf("rehearsal output missing %q:\n%s", want, result)
		}
	}
	if _, err := parseArgs([]string{"tenant", "exit-rehearsal", "-tenant", "tenant-2", "-file", path}); err != nil {
		t.Fatalf("parse mismatched-tenant command: %v", err)
	}
	if _, err := runTenantExitRehearsal("tenant-2", path); err == nil {
		t.Fatal("tenant mismatch in evidence was accepted")
	}
}

func TestTodo_REV_015_03_Integration(t *testing.T) {
	path := writeRehearsalFile(t, rehearsalFixture(t))
	var stdout, stderr bytes.Buffer
	code := Main([]string{"tenant", "exit-rehearsal", "-tenant", "tenant-1", "-file", path}, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"verdict: CERTIFIABLE", "export-1", "r-provider", "litigation hold", "sha256:tombstone", "\"status\": \"CERTIFIABLE\""} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("operator output missing %q:\n%s", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestTodo_REV_015_03_Recovery(t *testing.T) {
	request := rehearsalFixture(t)
	request.Exit.RestoreReDeletes = nil
	path := writeRehearsalFile(t, request)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"tenant", "exit-rehearsal", "-tenant", "tenant-1", "-file", path}, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"verdict: BLOCKED", `"code": "RESTORE_PLAN_INCOMPLETE"`, `"ready": false`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("recovery output missing %q:\n%s", want, stdout.String())
		}
	}
}
