package opcmd

import (
	"os"
	"strings"
	"testing"
)

// assertGolden compares got byte-for-byte against a golden fixture the lane
// verified by hand against the recovery library semantics (fixed inputs
// yield fixed RPO/RTO/counts; digests are deterministic per the recovery
// package's own golden tests), so only an intentional evidence-surface
// change alters it.
func assertGolden(t *testing.T, path, got string) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s: %v", path, err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch for %s:\n got:\n%s\nwant:\n%s", path, got, string(want))
	}
}

// REV-017-02: RECOVERY-001..004 live only in go test. This package is the
// operator-invokable entry point: Main runs RestoreTenant/DrillPilotRestore
// against a named backup set and isolated target, and ExecuteGameDay
// against a named scenario, printing the same RPO/RTO/restored-count/
// tombstone evidence the recovery package tests assert.

const rev01702TenantRequest = `{"TenantID":"tenant-a",` +
	`"RecoveryPoint":"2026-01-01T00:00:00Z","RestoredAt":"2026-01-01T00:04:00Z",` +
	`"Rows":[{"ID":"ledger-1","TenantID":"tenant-a","Plane":"ledger","Digest":"sha256:ledger"},` +
	`{"ID":"worker-1","TenantID":"tenant-a","Plane":"people","Digest":"sha256:worker"}],` +
	`"DeletedIDs":["deleted-1"],"HeldIDs":["worker-1"],` +
	`"LedgerHead":"ledger:42","MigrationJournalDigest":"sha256:migrations","RuntimeLeaseEpoch":7,` +
	`"Conformance":{"LedgerHeadsValid":true,"ForeignKeysValid":true,"RuntimeLeasesValid":true,` +
	`"HoldsApplied":true,"DeletionsApplied":true,"HashesValid":true}}`

const rev01702Runtime = `{"timers":3,"signals":2,"frontier_nodes":5,` +
	`"outbox_entries":7,"idempotency_keys":11,"state_digest":"sha256:runtime"}`

// runMain invokes Main with argv and captures both streams.
func runMain(args []string) (code int, stdout, stderr string) {
	var out, errOut strings.Builder
	code = Main(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestTodo_REV_017_02 is the REV-017-02 primary test: an operator can run a
// measured pilot-restore drill without writing Go, and the printed evidence
// carries the same RPO/RTO/restored-count/tombstone facts the recovery
// package tests assert.
func TestTodo_REV_017_02(t *testing.T) {
	code, stdout, stderr := runMain([]string{"restore-drill",
		"--drill", "drill-operator-1",
		"--destination", "recovery-cell-1",
		"--started-at", "2026-01-01T00:02:00Z",
		"--tenant-request", rev01702TenantRequest,
		"--runtime", rev01702Runtime,
	})
	if code != 0 {
		t.Fatalf("restore-drill exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"status=READY", "rpo=4m0s", "rto=2m0s",
		"tombstones=1", "holds=1", "production_effects=false",
		"digest=sha256:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("drill output missing %q:\n%s", want, stdout)
		}
	}
}

// TestTodo_REV_017_02_Fault proves the entry point fails closed: production
// destinations, malformed envelopes and missing flags never reach the
// recovery library.
func TestTodo_REV_017_02_Fault(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"production destination refused", []string{"restore-drill",
			"--drill", "drill-operator-1",
			"--destination", "prod-us-east-1",
			"--started-at", "2026-01-01T00:02:00Z",
			"--tenant-request", rev01702TenantRequest,
			"--runtime", rev01702Runtime,
		}, "production"},
		{"malformed tenant request refused", []string{"restore-drill",
			"--drill", "drill-operator-1",
			"--destination", "recovery-cell-1",
			"--started-at", "2026-01-01T00:02:00Z",
			"--tenant-request", `{"TenantID":`,
			"--runtime", rev01702Runtime,
		}, "tenant-request"},
		{"missing drill id refused", []string{"restore-drill",
			"--destination", "recovery-cell-1",
			"--started-at", "2026-01-01T00:02:00Z",
			"--tenant-request", rev01702TenantRequest,
			"--runtime", rev01702Runtime,
		}, "drill"},
		{"production fence refused", []string{"gameday",
			"--drill", "gameday-operator-1",
			"--asset", "database",
			"--owner", "ops-commander",
			"--degraded-mode", "read-only pilot",
			"--budget-rpo", "1m", "--budget-rto", "5m",
			"--fence", "prod-us-east-1",
			"--cutover-fence", "recovery-cell-1",
			"--failback-fence", "recovery-cell-1",
			"--observed-rpo", "30s", "--observed-rto", "2m",
			"--incident", "INC-1", "--advisory", "ADV-1",
			"--repair", "REPAIR-1", "--post-review", "REVIEW-1",
		}, "production"},
		{"unknown subcommand refused", []string{"defrag"}, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := runMain(tc.args)
			if code == 0 {
				t.Fatalf("%s: exit = 0, want non-zero", tc.name)
			}
			if !strings.Contains(strings.ToLower(stderr), tc.want) {
				t.Fatalf("%s: stderr = %q, want fragment %q", tc.name, stderr, tc.want)
			}
		})
	}
}

// TestTodo_REV_017_02_Recovery proves the measured-restore path: a tenant
// restore prints its READY receipt with exact row/hold/tombstone counts and
// RPO, and a non-conforming drill stays FENCED with named findings instead
// of failing the process.
func TestTodo_REV_017_02_Recovery(t *testing.T) {
	code, stdout, stderr := runMain([]string{"restore-tenant",
		"--tenant", "tenant-a",
		"--recovery-point", "2026-01-01T00:00:00Z",
		"--restored-at", "2026-01-01T00:04:00Z",
		"--ledger-head", "ledger:42",
		"--migration-digest", "sha256:migrations",
		"--lease-epoch", "7",
		"--rows", `[{"id":"ledger-1","tenant":"tenant-a","plane":"ledger","digest":"sha256:ledger"},` +
			`{"id":"worker-1","tenant":"tenant-a","plane":"people","digest":"sha256:worker"}]`,
		"--deleted-ids", "deleted-1",
		"--held-ids", "worker-1",
		"--conform-all",
	})
	if code != 0 {
		t.Fatalf("restore-tenant exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"status=READY", "fenced=false", "rows=2", "held=1", "deleted=1", "rpo=4m0s"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("restore output missing %q:\n%s", want, stdout)
		}
	}

	// Same drill without conformance: the data plane does not conform, so
	// the drill must report FENCED with findings and still exit zero with
	// the receipt as evidence.
	nonConforming := strings.Replace(rev01702TenantRequest, `"LedgerHeadsValid":true`, `"LedgerHeadsValid":false`, 1)
	code, stdout, stderr = runMain([]string{"restore-drill",
		"--drill", "drill-operator-2",
		"--destination", "recovery-cell-1",
		"--started-at", "2026-01-01T00:02:00Z",
		"--tenant-request", nonConforming,
		"--runtime", rev01702Runtime,
	})
	if code != 0 {
		t.Fatalf("fenced restore-drill exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"status=FENCED", "DATA_PLANE_UNCONFORMING"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("fenced drill output missing %q:\n%s", want, stdout)
		}
	}
}

// TestTodo_REV_017_02_Integration drives ExecuteGameDay through the real
// operator entry point against a named scenario and asserts the gate
// verdict, observed RPO/RTO and digest evidence reach the operator.
func TestTodo_REV_017_02_Integration(t *testing.T) {
	code, stdout, stderr := runMain([]string{"gameday",
		"--drill", "gameday-operator-1",
		"--asset", "database",
		"--owner", "ops-commander",
		"--degraded-mode", "read-only pilot",
		"--budget-rpo", "1m", "--budget-rto", "5m",
		"--fence", "recovery-cell-1",
		"--cutover-fence", "recovery-cell-1",
		"--failback-fence", "recovery-cell-1",
		"--observed-rpo", "30s", "--observed-rto", "2m",
		"--incident", "INC-1", "--advisory", "ADV-1",
		"--repair", "REPAIR-1", "--post-review", "REVIEW-1",
	})
	if code != 0 {
		t.Fatalf("gameday exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"status=PASS", "observed_rpo=30s", "observed_rto=2m0s", "digest=sha256:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("gameday output missing %q:\n%s", want, stdout)
		}
	}
}

// TestTodo_REV_017_02_Golden pins the byte-exact drill receipt so a change
// to the operator evidence surface is a visible diff.
func TestTodo_REV_017_02_Golden(t *testing.T) {
	_, stdout, _ := runMain([]string{"restore-drill",
		"--drill", "drill-operator-1",
		"--destination", "recovery-cell-1",
		"--started-at", "2026-01-01T00:02:00Z",
		"--tenant-request", rev01702TenantRequest,
		"--runtime", rev01702Runtime,
	})
	assertGolden(t, "testdata/restore_drill.golden.txt", stdout)
}
