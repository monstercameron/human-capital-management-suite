package topology

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func mustStartCell(t *testing.T, manifest string, placement Placement) *Cell {
	t.Helper()
	cell, err := StartCell(manifest, placement)
	if err != nil {
		t.Fatalf("StartCell: %v", err)
	}
	return cell
}

// promotionOps builds n promotion operations cycling all pinned roles
// with self-presented workload identity.
func promotionOps(n int) []Operation {
	ops := make([]Operation, 0, n)
	for i := range n {
		role := PinnedRoles[i%len(PinnedRoles)].Role
		ops = append(ops, Operation{Key: fmt.Sprintf("promo-%04d", i), Role: role, CredentialRole: role})
	}
	return ops
}

func mustExecuteAll(t *testing.T, cell *Cell, ops []Operation) {
	t.Helper()
	for _, op := range ops {
		if err := cell.Execute(op); err != nil {
			t.Fatalf("Execute(%s/%s): %v", op.Key, op.Role, err)
		}
	}
}

// mustExecuteAllowing executes ops, tolerating only the listed failure
// codes (bounded degradation); anything else fails the test.
func mustExecuteAllowing(t *testing.T, cell *Cell, ops []Operation, codes ...string) {
	t.Helper()
	for _, op := range ops {
		err := cell.Execute(op)
		if err == nil {
			continue
		}
		allowed := false
		for _, code := range codes {
			if strings.Contains(err.Error(), code) {
				allowed = true
			}
		}
		if !allowed {
			t.Fatalf("Execute(%s/%s): unexpected %v", op.Key, op.Role, err)
		}
	}
}

func mustKill(t *testing.T, cell *Cell, command string) {
	t.Helper()
	if err := cell.KillCommand(command); err != nil {
		t.Fatalf("KillCommand(%s): %v", command, err)
	}
}

func mustRestart(t *testing.T, cell *Cell, command string) {
	t.Helper()
	if err := cell.RestartCommand(command); err != nil {
		t.Fatalf("RestartCommand(%s): %v", command, err)
	}
}

func mustReconcile(t *testing.T, cell *Cell) ReconcileReport {
	t.Helper()
	return cell.Reconcile()
}

func requireTopologyCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s, got nil", code)
	}
	if !strings.Contains(err.Error(), code) {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

// TestTodo_SVC_012_Property: placements and concurrency vary, effects
// and the package fingerprint never do.
func TestTodo_SVC_012_Property(t *testing.T) {
	manifest := svc012Manifest(t)
	placements := []Placement{
		nil,
		{"messaging": "hcmnext"},
		{"messaging": "hcmnext", "reconciliation": "hcmnext", "repair": "hcmnext"},
		{"timer": "worker", "signal": "worker"},
	}
	for _, concurrency := range []int{1, 4, 16} {
		for pi, placement := range placements {
			cell := mustStartCell(t, manifest, placement)
			ops := promotionOps(28)
			var wg sync.WaitGroup
			errs := make([]error, len(ops))
			sem := make(chan struct{}, concurrency)
			for i, op := range ops {
				wg.Add(1)
				go func(i int, op Operation) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					errs[i] = cell.Execute(op)
				}(i, op)
			}
			wg.Wait()
			for i, err := range errs {
				if err != nil {
					t.Fatalf("placement %d concurrency %d op %d: %v", pi, concurrency, i, err)
				}
			}
			report := mustReconcile(t, cell)
			if report.Committed != len(ops) || report.DuplicateEffects != 0 || report.Deferred != 0 {
				t.Fatalf("placement %d concurrency %d: %+v", pi, concurrency, report)
			}
			if cell.Fingerprint() != PackageFingerprint() {
				t.Fatalf("placement %d: package graph moved with placement", pi)
			}
		}
	}
}

// TestTodo_SVC_012_Golden pins the cell receipt digest oracle.
func TestTodo_SVC_012_Golden(t *testing.T) {
	cell := mustStartCell(t, svc012Manifest(t), PlacementDefault)
	mustExecuteAll(t, cell, promotionOps(12))
	mustKill(t, cell, "worker")
	mustRestart(t, cell, "worker")
	mustReconcile(t, cell)
	receipt := cell.Receipt()
	raw, err := os.ReadFile(filepath.Join("testdata", "svc012.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.TrimSpace(string(raw)); receipt.Digest != want {
		t.Fatalf("cell digest mismatch:\n got=%q\nwant=%q", receipt.Digest, want)
	}
}

// TestTodo_SVC_012_Integration: a disabled role degrades boundedly —
// its operations defer while every other role serves, then complete on
// re-enable without duplicates.
func TestTodo_SVC_012_Integration(t *testing.T) {
	cell := mustStartCell(t, svc012Manifest(t), PlacementDefault)
	if err := cell.DisableRole("messaging"); err != nil {
		t.Fatalf("DisableRole: %v", err)
	}
	ops := promotionOps(14)
	mustExecuteAllowing(t, cell, ops, CodeRoleDisabled)
	report := mustReconcile(t, cell)
	if report.Deferred != 2 {
		t.Fatalf("deferred=%d want=2 (messaging ops)", report.Deferred)
	}
	if report.Committed != len(ops)-2 {
		t.Fatalf("committed=%d want=%d", report.Committed, len(ops)-2)
	}
	if err := cell.EnableRole("messaging"); err != nil {
		t.Fatalf("EnableRole: %v", err)
	}
	report = mustReconcile(t, cell)
	if report.Committed != len(ops) || report.Deferred != 0 || report.DuplicateEffects != 0 {
		t.Fatalf("post-enable: %+v", report)
	}
	// Replaying the same promotion is a no-op: no duplicate effects.
	mustExecuteAll(t, cell, ops)
	if report := mustReconcile(t, cell); report.Committed != len(ops) {
		t.Fatalf("replay: %+v", report)
	}
}

// TestTodo_SVC_012_Fault: killing each command, disabling one role,
// delaying a dependency and restarting replicas fail on the documented
// side with exact codes.
func TestTodo_SVC_012_Fault(t *testing.T) {
	manifest := svc012Manifest(t)
	for _, command := range PinnedCellCommands {
		cell := mustStartCell(t, manifest, PlacementDefault)
		mustKill(t, cell, command)
		// Double kill refuses: the command is already down.
		requireTopologyCode(t, cell.KillCommand(command), CodeCommandDown)
		// Restarting an unknown command refuses.
		requireTopologyCode(t, cell.RestartCommand("unknown-cmd"), CodeUnknownCommand)
		mustRestart(t, cell, command)
	}
	cell := mustStartCell(t, manifest, PlacementDefault)
	// Disabling an unknown role refuses.
	requireTopologyCode(t, cell.DisableRole("unknown-role"), CodeUnknownRole)
	// A delayed dependency stays bounded: work still completes and the
	// delay is visible in the receipt digest.
	cell.DelayDependency("PostgreSQL", "250ms")
	mustExecuteAll(t, cell, promotionOps(7))
	if report := mustReconcile(t, cell); report.Committed != 7 {
		t.Fatalf("delayed dependency: %+v", report)
	}
	before := mustStartCell(t, manifest, PlacementDefault)
	mustExecuteAll(t, before, promotionOps(7))
	if cell.Receipt().Digest == before.Receipt().Digest {
		t.Fatal("dependency delay invisible in receipt")
	}
	// Restarting every replica converges with no duplicate effects.
	for _, command := range PinnedCellCommands {
		mustKill(t, cell, command)
		mustRestart(t, cell, command)
	}
	if report := mustReconcile(t, cell); report.Committed != 7 || report.DuplicateEffects != 0 {
		t.Fatalf("replica restart: %+v", report)
	}
}

// TestTodo_SVC_012_Mutation: unknown commands, unknown roles, padded
// keys and cross-role credentials resolve on the documented side.
func TestTodo_SVC_012_Mutation(t *testing.T) {
	manifest := svc012Manifest(t)
	// An unpinned command in the manifest refuses the whole start.
	if _, err := StartCell(manifest, Placement{"capability": "unknown-cmd"}); err == nil {
		t.Fatal("placement on unknown command admitted")
	} else {
		requireTopologyCode(t, err, CodeUnknownCommand)
	}
	cell := mustStartCell(t, manifest, PlacementDefault)
	good := Operation{Key: "promo-0000", Role: "capability", CredentialRole: "capability"}
	// One co-located role's credential grants no other role's authority.
	for _, other := range []string{"connector", "repair", "messaging", "timer"} {
		borrowed := good
		borrowed.CredentialRole = other
		requireTopologyCode(t, cell.Execute(borrowed), CodeRoleIsolation)
	}
	// Unknown roles and padded keys refuse.
	requireTopologyCode(t, cell.Execute(Operation{Key: "k", Role: "unknown", CredentialRole: "unknown"}), CodeUnknownRole)
	requireTopologyCode(t, cell.Execute(Operation{Key: " promo-0000 ", Role: "capability", CredentialRole: "capability"}), CodeInvalidKey)
	requireTopologyCode(t, cell.Execute(Operation{Key: "", Role: "capability", CredentialRole: "capability"}), CodeInvalidKey)
	// The honest operation still commits exactly once afterwards.
	mustExecuteAll(t, cell, []Operation{good, good})
	if report := mustReconcile(t, cell); report.Committed != 1 {
		t.Fatalf("post-refusal: %+v", report)
	}
}
