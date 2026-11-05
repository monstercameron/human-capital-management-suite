package simulate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

var wftest002Now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// wftest002Grants is the real scope enforcement shared by direct reads and
// test runs: the test author may read people and payroll observations, and
// nothing else.
func wftest002Grants(principal, scope string) bool {
	if principal != "principal:test-author" {
		return false
	}
	switch scope {
	case "scope:people.read", "scope:payroll.read":
		return true
	default:
		return false
	}
}

func wftest002Store() *simulate.ExpiringEvidenceStore {
	now := wftest002Now
	return simulate.NewExpiringEvidenceStore(24*time.Hour, func() time.Time { return now })
}

func wftest002TestRun(store *simulate.ExpiringEvidenceStore) *simulate.TestRunOptions {
	return &simulate.TestRunOptions{
		Principal: "principal:test-author",
		Grants:    wftest002Grants,
		Store:     store,
	}
}

// TestTodo_WF_TEST_002 proves a test run authorizes its reads as its author:
// every node scope passes the real grants with purpose workflow.test, the
// author principal is stamped on the evidence, and the invocation evidence
// lands in the expiring test-evidence store instead of permanent history.
func TestTodo_WF_TEST_002(t *testing.T) {
	invoked := false
	plan := mustCompileMutatingPlan(t, func() { invoked = true })
	contract, err := intent.ModeContractFor(intent.ModeSimulate, intent.EnvironmentTest)
	if err != nil {
		t.Fatalf("ModeContractFor: %v", err)
	}
	recording := simulate.NewRecording()
	store := wftest002Store()
	receipt, err := simulate.Run(context.Background(), plan, recordingInputs(), simulate.Options{
		Capabilities: recordingRegistry(t),
		Reads:        recordingReads{},
		Recording:    &simulate.RecordingProfile{Contract: contract, Recorder: recording},
		TestRun:      wftest002TestRun(store),
	})
	if err != nil {
		t.Fatalf("Run as author: %v", err)
	}
	if invoked {
		t.Fatal("the mutating handler was reached")
	}
	if store.Len() != 1 {
		t.Fatalf("evidence store holds %d records, want the one read invocation", store.Len())
	}
	record := store.Records()[0]
	if record.Purpose != simulate.TestPurpose || simulate.TestPurpose != "workflow.test" {
		t.Fatalf("evidence purpose = %q, want workflow.test", record.Purpose)
	}
	if record.Principal != "principal:test-author" {
		t.Fatalf("evidence principal = %q, want the author", record.Principal)
	}
	if record.CapabilityID != capReadWorker || record.Decision != "INVOKED" {
		t.Fatalf("evidence = %+v, want the invoked read", record)
	}
	if record.RetentionClass != storagedisposition.RetentionTestEvidence {
		t.Fatalf("evidence class = %q, want the registered %q", record.RetentionClass, storagedisposition.RetentionTestEvidence)
	}
	if err := receipt.Verify(); err != nil {
		t.Fatalf("receipt does not verify: %v", err)
	}
}

// TestTodo_WF_TEST_002_Security proves a test run cannot read what the author
// could not read directly: the run's allow/refuse tracks the grants function
// exactly, for granted scopes, ungranted scopes and unknown principals.
func TestTodo_WF_TEST_002_Security(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principal string
		grants    simulate.GrantsFunc
		wantErr   bool
	}{
		{"granted scope runs", "principal:test-author", wftest002Grants, false},
		{"ungranted scope refused", "principal:test-author", func(_, _ string) bool { return false }, true},
		{"unknown principal refused", "principal:intruder", wftest002Grants, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invoked := false
			plan := mustCompileMutatingPlan(t, func() { invoked = true })
			contract, err := intent.ModeContractFor(intent.ModeSimulate, intent.EnvironmentTest)
			if err != nil {
				t.Fatalf("ModeContractFor: %v", err)
			}
			store := wftest002Store()
			_, err = simulate.Run(context.Background(), plan, recordingInputs(), simulate.Options{
				Capabilities: recordingRegistry(t),
				Reads:        recordingReads{},
				Recording:    &simulate.RecordingProfile{Contract: contract, Recorder: simulate.NewRecording()},
				TestRun:      &simulate.TestRunOptions{Principal: tc.principal, Grants: tc.grants, Store: store},
			})
			// The direct-read decision is the same grants call the run makes:
			// parity between the two is the property under test.
			direct := tc.grants(tc.principal, "scope:people.read")
			if (err != nil) != tc.wantErr {
				t.Fatalf("run err = %v, wantErr %v", err, tc.wantErr)
			}
			if direct == tc.wantErr {
				t.Fatalf("direct read grants(%q, people.read) = %v but the run erred = %v: decisions diverged", tc.principal, direct, err != nil)
			}
			if invoked {
				t.Fatal("a refused run reached a handler")
			}
		})
	}
}

// TestTodo_WF_TEST_002_Golden pins the expiring retention class registration
// and the evidence lifecycle: the class is registered in the
// storage-disposition registry, the store declares it, and records purge
// exactly at expiry and read back nothing afterwards.
func TestTodo_WF_TEST_002_Golden(t *testing.T) {
	if storagedisposition.RetentionTestEvidence != "TEST_EVIDENCE" {
		t.Fatalf("registered class = %q, want TEST_EVIDENCE", storagedisposition.RetentionTestEvidence)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "definitions", "storage", "storage-disposition.yaml"))
	if err != nil {
		t.Fatalf("reading storage-disposition.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "name: TEST_EVIDENCE") {
		t.Fatal("storage-disposition.yaml does not register the TEST_EVIDENCE retention class")
	}
	entry := storagedisposition.TableEntry{
		Table: "workflow_test_evidence", Migration: "wf-test-002", OwnerPackage: "internal/workflow/simulate",
		Plane: "control", DataRole: storagedisposition.RoleRegistry,
		RetentionClass: storagedisposition.RetentionTestEvidence, EncryptionClass: storagedisposition.EncryptionPlatformManaged,
	}
	if err := storagedisposition.Validate(&storagedisposition.Registry{Version: 1, Tables: []storagedisposition.TableEntry{entry}}); err != nil {
		t.Fatalf("registry rejects the TEST_EVIDENCE class: %v", err)
	}

	now := wftest002Now
	store := simulate.NewExpiringEvidenceStore(time.Hour, func() time.Time { return now })
	if store.RetentionClass() != storagedisposition.RetentionTestEvidence {
		t.Fatalf("store class = %q, want the registered class", store.RetentionClass())
	}
	var transcript []string
	put := func(capabilityID string) {
		id, err := store.Put(simulate.EvidenceRecord{
			Purpose: simulate.TestPurpose, Principal: "principal:test-author",
			CapabilityID: capabilityID, CapabilityVersion: 1, Decision: "INVOKED",
		})
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		transcript = append(transcript, "put "+capabilityID+" -> "+id)
	}
	put("test.read")
	put("test.read2")
	transcript = append(transcript, fmt.Sprintf("purge@%s -> %d", now.Format(time.RFC3339), store.PurgeExpired(now)))
	now = now.Add(2 * time.Hour)
	transcript = append(transcript, fmt.Sprintf("purge@%s -> %d", now.Format(time.RFC3339), store.PurgeExpired(now)))
	if _, ok := store.Get("test-evidence-1"); ok {
		t.Fatal("expired evidence still reads back after purge")
	}
	transcript = append(transcript, fmt.Sprintf("len -> %d", store.Len()))
	sum := sha256.Sum256([]byte(strings.Join(transcript, "\n")))
	if got := hex.EncodeToString(sum[:]); got != "9b290857479a42522518e9e6c82c0af502c61fe34bacf826c48258d2e628fe21" {
		t.Fatalf("evidence transcript digest = %s\ntranscript:\n%s", got, strings.Join(transcript, "\n"))
	}
}
