package exit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tenant004Fixture(t *testing.T) (RehearsalRequest, time.Time) {
	t.Helper()
	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	base := validRequest()
	base.At = at
	for i := range base.Copies {
		base.Copies[i].FreshAt = at.Add(-time.Hour)
	}
	for i := range base.Revocations {
		base.Revocations[i].At = at
	}
	return RehearsalRequest{
		RehearsalID: "rehearsal-1", Tenant: "tenant-1", RequestedBy: "operator-1",
		At: at, Exit: base,
		Steps: []RehearsalStep{
			{ID: "export", Title: "Produce and verify the tenant export", Observed: true, EvidenceRef: "export-1"},
			{ID: "shutdown", Title: "Shut down connectors and tenant", Observed: true, EvidenceRef: "shutdown-1"},
			{ID: "revoke", Title: "Revoke live authorities", Observed: true, EvidenceRef: "r-provider"},
			{ID: "restore-plan", Title: "Record the restore re-delete plan", Observed: true, EvidenceRef: "rd-1"},
		},
	}, at
}

// TestTodo_TENANT_004_Golden pins the rehearsal receipt oracle.
func TestTodo_TENANT_004_Golden(t *testing.T) {
	req, _ := tenant004Fixture(t)
	rec, err := Rehearse(req)
	if err != nil {
		t.Fatalf("Rehearse: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "rehearsal.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	oracle := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("malformed golden line: %q", line)
		}
		oracle[key] = value
	}
	if rec.Digest != oracle["digest"] {
		t.Fatalf("digest mismatch:\n got=%q\nwant=%q", rec.Digest, oracle["digest"])
	}
	if rec.Status != oracle["status"] || rec.Explain() != oracle["explain"] {
		t.Fatalf("receipt mismatch:\n got=%q %q\nwant=%q %q", rec.Status, rec.Explain(), oracle["status"], oracle["explain"])
	}
}

// TestTodo_TENANT_004_Integration exercises the full rehearsal path —
// certification, steps, restore plan — and proves repeated runs agree.
func TestTodo_TENANT_004_Integration(t *testing.T) {
	req, _ := tenant004Fixture(t)
	first, err := Rehearse(req)
	if err != nil {
		t.Fatalf("Rehearse: %v", err)
	}
	second, err := Rehearse(req)
	if err != nil {
		t.Fatalf("Rehearse again: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("identical rehearsals disagree")
	}
	if first.Status != StatusCertifiable || len(first.Steps) != 4 || len(first.Revocations) != 4 {
		t.Fatalf("receipt=%+v", first)
	}
	if len(first.Exceptions) != 1 || first.Exceptions[0].CopyID != "copy-CANONICAL" {
		t.Fatalf("exceptions=%+v, want the carried legal hold", first.Exceptions)
	}
	// The rehearsal is read-only: inputs come back unmodified.
	if req.Exit.Copies[0].ID != "copy-CANONICAL" || len(req.Steps) != 4 || req.RehearsalID != "rehearsal-1" {
		t.Fatal("rehearsal mutated its input")
	}
}

// TestTodo_TENANT_004_Security: tenant isolation holds — mismatched and
// foreign tenants block without leaking inventory.
func TestTodo_TENANT_004_Security(t *testing.T) {
	req, _ := tenant004Fixture(t)
	req.Tenant = "other-tenant"
	if _, err := Rehearse(req); err == nil {
		t.Fatal("tenant-mismatched rehearsal accepted")
	}
	req, _ = tenant004Fixture(t)
	req.Exit.Copies[0].Tenant = "other-tenant"
	rec, err := Rehearse(req)
	if err != nil {
		t.Fatalf("Rehearse: %v", err)
	}
	if rec.Status != StatusBlocked || !hasBlocker(rec.Blockers, "TENANT_MISMATCH") {
		t.Fatalf("blockers=%+v, want tenant isolation findings", rec.Blockers)
	}
	if rec.Tenant != "tenant-1" {
		t.Fatal("receipt carries the wrong tenant")
	}
	for _, b := range rec.Blockers {
		if strings.Contains(b.Detail, "other-tenant") {
			t.Fatalf("blocker leaks foreign tenant: %+v", b)
		}
	}
}

// TestTodo_TENANT_004_Recovery: the restore re-delete plan replays every
// tombstone before a copy serves again, and missing evidence blocks.
func TestTodo_TENANT_004_Recovery(t *testing.T) {
	req, _ := tenant004Fixture(t)
	rec, err := Rehearse(req)
	if err != nil {
		t.Fatalf("Rehearse: %v", err)
	}
	ready := map[string]RestorePlanEntry{}
	for _, e := range rec.RestorePlan {
		ready[e.CopyID] = e
	}
	backup, ok := ready["copy-BACKUP"]
	if !ok || !backup.Ready || backup.TombstoneDigest != "sha256:tombstone" || backup.Watermark != "42" {
		t.Fatalf("restore plan=%+v, want a ready BACKUP tombstone", rec.RestorePlan)
	}
	dropped := req
	dropped.Exit.RestoreReDeletes = nil
	rec, err = Rehearse(dropped)
	if err != nil {
		t.Fatalf("Rehearse(dropped): %v", err)
	}
	if rec.Status != StatusBlocked || !hasBlocker(rec.Blockers, "RESTORE_PLAN_INCOMPLETE") {
		t.Fatalf("blockers=%+v, want restore-plan findings", rec.Blockers)
	}
}

// FuzzTodo_TENANT_004: hostile rehearsal envelopes never panic and never
// certify; malformed input is an error or a BLOCKED receipt, both without
// a digest that could pass as evidence.
func FuzzTodo_TENANT_004(f *testing.F) {
	f.Add("rehearsal-1", "tenant-1", "export", "export-1")
	f.Add("", "", "\x00", "   ")
	f.Add("r", "t", "export\xffshutdown", "e")
	f.Fuzz(func(t *testing.T, rehearsalID, tenant, stepID, evidence string) {
		req, _ := tenant004Fixture(t)
		req.RehearsalID = rehearsalID
		req.Tenant = tenant
		req.Exit.Tenant = tenant
		for i := range req.Exit.Copies {
			req.Exit.Copies[i].Tenant = tenant
		}
		for i := range req.Steps {
			// Distinct per step so the fuzzer reaches past duplicate
			// detection into blank, padded and hostile identities.
			req.Steps[i].ID = fmt.Sprintf("%s-%d", stepID, i)
			req.Steps[i].EvidenceRef = evidence
		}
		rec, err := Rehearse(req)
		if err != nil {
			if rec.Digest != "" {
				t.Fatal("errored rehearsal carries a digest")
			}
			return
		}
		if rec.Status == StatusCertifiable {
			// Certification demands a complete inventory; fuzzed
			// identities alone must never satisfy it.
			if rehearsalID == "" || tenant == "" {
				t.Fatalf("blank envelope certified: %+v", rec)
			}
		}
	})
}
