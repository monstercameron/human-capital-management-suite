package topology

import (
	"path/filepath"
	"testing"
)

func svc012Manifest(t *testing.T) string {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("module root: %v", err)
	}
	return filepath.Join(root, "definitions", "architecture", "process-roles.yaml")
}

func TestTodo_SVC_012(t *testing.T) {
	cell := mustStartCell(t, svc012Manifest(t), PlacementDefault)
	ops := promotionOps(12)
	mustExecuteAll(t, cell, ops[:6])
	// Killing the worker mid-run defers its roles' work while every other
	// role keeps serving; the restart replays the journal and every
	// promotion completes exactly once.
	mustKill(t, cell, "worker")
	mustExecuteAllowing(t, cell, ops[6:], CodeCommandDown, CodeRoleDisabled)
	mustRestart(t, cell, "worker")
	report := mustReconcile(t, cell)
	if report.Committed != len(ops) {
		t.Fatalf("committed=%d want=%d (%+v)", report.Committed, len(ops), report)
	}
	if report.DuplicateEffects != 0 {
		t.Fatalf("duplicate effects=%d", report.DuplicateEffects)
	}
	receipt := cell.Receipt()
	if receipt.Digest == "" {
		t.Fatal("cell receipt must carry a digest")
	}
}
