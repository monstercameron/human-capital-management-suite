package main

// Operator-path tests for `hcmnext records-disposition` (REV-004-02): the
// command drives the served DispositionGate from an envelope file and prints
// the decision, certificate and persisted findings as JSON.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func recordsDispositionNow() time.Time {
	return time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
}

func writeRecordsDispositionEnvelope(t *testing.T, holds []recordsDispositionHold, withDeletion bool) string {
	t.Helper()
	at := "2026-09-21T12:00:00Z"
	deletion := map[string]any{
		"deletion_id":  "del-cmd-1",
		"tenant":       "tenant-a",
		"record_id":    "rec-001",
		"requested_by": "privacy-officer",
		"at":           at,
		"copies": []map[string]any{
			{"id": "copy-canonical", "kind": "CANONICAL", "tenant": "tenant-a"},
			{"id": "copy-derived", "kind": "DERIVED", "tenant": "tenant-a"},
			{"id": "copy-external", "kind": "EXTERNAL", "tenant": "tenant-a"},
			{"id": "copy-backup", "kind": "BACKUP", "tenant": "tenant-a", "re_deleted": true, "re_delete_ref": "redel-1"},
			{"id": "copy-restored", "kind": "RESTORED", "tenant": "tenant-a", "tombstone_digest": "tomb-1"},
		},
		"tombstones": []map[string]any{{"copy_id": "copy-restored", "digest": "tomb-1"}},
	}
	cutoff := "2026-01-01T00:00:00Z"
	created := "2025-01-01T00:00:00Z"
	retention := map[string]any{
		"AsOf": "2026-09-01T00:00:00Z",
		"Copies": []map[string]any{
			{"ID": "copy-payroll-1", "RecordSeries": "payroll", "Custodian": "hr", "Jurisdiction": "US-CA",
				"CreatedAt": created, "CutoffAt": cutoff, "ArchiveAcknowledged": true},
		},
		"Rules": []map[string]any{
			{"RecordSeries": "payroll", "Jurisdiction": "US-CA", "MinimumDays": 30, "AuthorityRef": "schedule-payroll-ca"},
		},
	}
	body := map[string]any{"holds": holds, "retention": retention}
	if withDeletion {
		body["deletion"] = deletion
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	path := filepath.Join(t.TempDir(), "envelope.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
	return path
}

func runRecordsDispositionCase(args []string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := runRecordsDisposition(args, &stdout, &stderr, recordsDispositionNow)
	return code, stdout.String(), stderr.String()
}

func decodeRecordsDisposition(t *testing.T, stdout string) recordsDispositionResult {
	t.Helper()
	var result recordsDispositionResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode result: %v\n%s", err, stdout)
	}
	return result
}

func TestRecordsDispositionExecuteCompletes(t *testing.T) {
	t.Parallel()
	input := writeRecordsDispositionEnvelope(t, nil, true)
	code, stdout, stderr := runRecordsDispositionCase([]string{
		"execute", "-tenant", "tenant-a", "-compartment", "hr-records",
		"-record", "rec-001", "-evidence", "evidence/cmd-1", "-input", input,
	})
	if code != 0 {
		t.Fatalf("execute = %d, stderr %q", code, stderr)
	}
	result := decodeRecordsDisposition(t, stdout)
	if result.Decision.Code != "DISPOSITION_ALLOWED" {
		t.Fatalf("decision = %+v, want DISPOSITION_ALLOWED", result.Decision)
	}
	if result.Certificate == nil || !result.Certificate.Complete || len(result.Certificate.Outcomes) != 5 {
		t.Fatalf("certificate = %+v, want a complete 5-outcome certificate", result.Certificate)
	}
	if result.Retention == nil || result.Retention.Status != "ELIGIBLE" {
		t.Fatalf("retention = %+v, want an ELIGIBLE report", result.Retention)
	}
}

func TestRecordsDispositionExecuteHeld(t *testing.T) {
	t.Parallel()
	holds := []recordsDispositionHold{{ID: "hold-1", Reason: "litigation hold", Authority: "general-counsel", Records: []string{"rec-001"}}}
	input := writeRecordsDispositionEnvelope(t, holds, true)
	code, stdout, stderr := runRecordsDispositionCase([]string{
		"execute", "-tenant", "tenant-a", "-compartment", "hr-records",
		"-record", "rec-001", "-evidence", "evidence/cmd-held", "-input", input,
	})
	if code != 3 {
		t.Fatalf("held execute = %d, stderr %q; want 3 for the governed refusal", code, stderr)
	}
	result := decodeRecordsDisposition(t, stdout)
	if result.Decision.Code != "HOLD_BLOCKED" || result.Decision.HoldID != "hold-1" {
		t.Fatalf("decision = %+v, want HOLD_BLOCKED naming hold-1", result.Decision)
	}
	if result.Certificate != nil {
		t.Fatalf("held execution printed a certificate: %+v", result.Certificate)
	}
	persisted := false
	for _, e := range result.HoldFinding {
		if e.Code == "HOLD_BLOCKED" && e.RecordRef == "rec-001" && e.HoldID == "hold-1" {
			persisted = true
		}
	}
	if !persisted {
		t.Fatalf("result carries no persisted HOLD_BLOCKED finding: %+v", result.HoldFinding)
	}
}

func TestRecordsDispositionEvaluate(t *testing.T) {
	t.Parallel()
	input := writeRecordsDispositionEnvelope(t, nil, false)
	code, stdout, stderr := runRecordsDispositionCase([]string{
		"evaluate", "-tenant", "tenant-a", "-compartment", "hr-records",
		"-record", "rec-001", "-evidence", "evidence/cmd-eval", "-input", input,
	})
	if code != 0 {
		t.Fatalf("evaluate = %d, stderr %q", code, stderr)
	}
	if got := decodeRecordsDisposition(t, stdout); !got.Decision.Allowed {
		t.Fatalf("evaluate decision = %+v, want allowed", got.Decision)
	}

	held := writeRecordsDispositionEnvelope(t, []recordsDispositionHold{{ID: "hold-1", Reason: "litigation hold", Authority: "general-counsel", Records: []string{"rec-001"}}}, false)
	code, stdout, _ = runRecordsDispositionCase([]string{
		"evaluate", "-tenant", "tenant-a", "-compartment", "hr-records",
		"-record", "rec-001", "-evidence", "evidence/cmd-eval-held", "-input", held,
	})
	if code != 3 {
		t.Fatalf("held evaluate = %d, want 3", code)
	}
	if got := decodeRecordsDisposition(t, stdout); got.Decision.Code != "HOLD_BLOCKED" {
		t.Fatalf("held evaluate decision = %+v, want HOLD_BLOCKED", got.Decision)
	}
}

func TestRecordsDispositionUsageAndFailures(t *testing.T) {
	t.Parallel()
	input := writeRecordsDispositionEnvelope(t, nil, true)

	for name, args := range map[string][]string{
		"no action":        {},
		"unknown action":   {"purge"},
		"missing flags":    {"execute", "-tenant", "tenant-a"},
		"execute no input": {"execute", "-tenant", "tenant-a", "-compartment", "hr-records", "-record", "rec-001", "-evidence", "e", "-input", filepath.Join(t.TempDir(), "absent.json")},
	} {
		args := args
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, _, _ := runRecordsDispositionCase(args)
			if code != 2 && !(name == "execute no input" && code == 1) {
				t.Fatalf("%s exit = %d", name, code)
			}
		})
	}

	noDeletion := writeRecordsDispositionEnvelope(t, nil, false)
	if code, _, _ := runRecordsDispositionCase([]string{
		"execute", "-tenant", "tenant-a", "-compartment", "hr-records",
		"-record", "rec-001", "-evidence", "e", "-input", noDeletion,
	}); code != 2 {
		t.Fatalf("execute without a deletion envelope exit = %d, want 2", code)
	}

	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write broken envelope: %v", err)
	}
	if code, _, _ := runRecordsDispositionCase([]string{
		"execute", "-tenant", "tenant-a", "-compartment", "hr-records",
		"-record", "rec-001", "-evidence", "e", "-input", broken,
	}); code != 1 {
		t.Fatalf("broken envelope exit = %d, want 1", code)
	}

	// The envelope's deletion names tenant-a; checking it against a
	// tenant-b record must fail closed, never delete.
	if code, _, _ := runRecordsDispositionCase([]string{
		"execute", "-tenant", "tenant-b", "-compartment", "hr-records",
		"-record", "rec-001", "-evidence", "e", "-input", input,
	}); code != 1 {
		t.Fatalf("cross-tenant execute exit = %d, want 1", code)
	}
}
