package pilotprovider

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProviderIdentityNeverLeaksIntoSemanticWorkflowsOrCapabilities is
// SELECT-002's REFACTOR enforcement test, not merely its assertion:
// REFACTOR requires "provider-specific facts remain configuration and
// adapter qualification inputs; semantic workflows and capabilities remain
// provider-neutral". This test loads the real checked-in topology's
// Provider.VendorID and scans the real internal/ source tree - excluding
// internal/connectivity (the connector/adapter-qualification boundary
// specs/integration-platform.md defines: "Connector Definition +
// Connection ... Canonical Human Capital Management Suite Capabilities")
// and internal/generated (compiler output, not hand-written semantic code)
// - for any .go file naming that vendor id. A hit would mean a workflow or
// capability package branches on this specific provider's identity instead
// of treating it as configuration, exactly what REFACTOR forbids.
//
// What this test can and cannot prove: it proves no *current* internal/
// package outside the adapter boundary names this placeholder's exact
// identifier. It cannot prove the stronger, general claim that no semantic
// code could ever branch on *any* provider's identity by some other means
// (e.g. an indirect lookup through a config value never compared by
// string), because that would require full data-flow analysis this package
// does not attempt. Given that nothing in the repository consumes
// SELECT-002's output yet (NEXT-002 does that later) and
// internal/connectivity/providercontract's existing PROVIDER-001 fixture is
// already fully provider-neutral (verified by this same technique with a
// broader term list below), this direct-textual-reference scan is the
// concrete, falsifiable form of REFACTOR's clause available today, and it
// will catch the regression this clause exists to prevent: a later change
// that hard-codes this vendor id into a workflow or capability package.
func TestProviderIdentityNeverLeaksIntoSemanticWorkflowsOrCapabilities(t *testing.T) {
	topology := mustLoadTopology(t)
	if topology.Provider.VendorID == "" {
		t.Fatal("checked-in topology has no provider.vendor_id to scan for")
	}

	internalRoot := filepath.FromSlash(repoRoot + "/internal")
	skipDirs := []string{
		filepath.FromSlash(repoRoot + "/internal/connectivity"),
		filepath.FromSlash(repoRoot + "/internal/generated"),
	}

	hits, err := ScanForIdentityLeak(internalRoot, skipDirs, topology.Provider.VendorID)
	if err != nil {
		t.Fatalf("ScanForIdentityLeak: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("provider identity %q leaked into semantic code outside internal/connectivity: %v", topology.Provider.VendorID, hits)
	}
}

// TestScanForIdentityLeakDetectsAPlantedReference is
// TestProviderIdentityNeverLeaksIntoSemanticWorkflowsOrCapabilities's own
// mutation-verification target made explicit and repeatable: it plants the
// needle in a real temp .go file inside a scanned root and proves
// ScanForIdentityLeak reports it, and proves a file inside a skipped
// directory is correctly ignored. Without this test,
// TestProviderIdentityNeverLeaksIntoSemanticWorkflowsOrCapabilities would be
// a check that can only ever pass silently - a scan that never fires on
// anything is indistinguishable from a scan that is broken.
func TestScanForIdentityLeakDetectsAPlantedReference(t *testing.T) {
	root := t.TempDir()
	scannedDir := filepath.Join(root, "workflow")
	skippedDir := filepath.Join(root, "connectivity")
	for _, dir := range []string{scannedDir, skippedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(scannedDir, "promotion.go"), []byte("package workflow\n\nconst vendor = \"needle-value\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skippedDir, "adapter.go"), []byte("package connectivity\n\nconst vendor = \"needle-value\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scannedDir, "unrelated.go"), []byte("package workflow\n\nconst other = \"nothing-to-see\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	hits, err := ScanForIdentityLeak(root, []string{skippedDir}, "needle-value")
	if err != nil {
		t.Fatalf("ScanForIdentityLeak: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly one hit (the scanned file), got %v", hits)
	}
	want := filepath.ToSlash(filepath.Join(scannedDir, "promotion.go"))
	if hits[0] != want {
		t.Errorf("hit = %s, want %s", hits[0], want)
	}
}
