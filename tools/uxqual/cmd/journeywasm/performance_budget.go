//go:build !(js && wasm)

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// The runtime shim is bounded by these engineering ceilings. The browser
// client itself (journey.wasm and its compressed form) carries no size
// ceiling: the owner removed it on 2026-11-20, so the gate only requires that
// both files exist and are not empty. Its size is still reported by the build
// and attributed per package by -size-report.
const (
	maxWasmExecBytes     int64 = 1 << 20
	maxWasmExecGzipBytes int64 = 256 << 10
)

// assetSizeBudget names one generated asset. A max of zero means the asset
// must be present and non-empty but has no size ceiling.
type assetSizeBudget struct {
	name string
	max  int64
}

// checkJourneyAssetBudgets checks the generated browser assets using exact
// file lengths: every asset is present and non-empty, and the runtime shim is
// within its ceilings. The caller invokes it after compression and before
// writing the integrity manifest, so an incomplete artifact set cannot ship.
func checkJourneyAssetBudgets(outDir string) error {
	budgets := [...]assetSizeBudget{
		{name: wasmFile},
		{name: wasmFile + ".gz"},
		{name: wasmExecFile, max: maxWasmExecBytes},
		{name: wasmExecFile + ".gz", max: maxWasmExecGzipBytes},
	}
	for _, budget := range budgets {
		info, err := os.Stat(filepath.Join(outDir, budget.name))
		if err != nil {
			return fmt.Errorf("frontend performance budget: stat %s: %w", budget.name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("frontend performance budget: %s is not a regular file", budget.name)
		}
		if info.Size() <= 0 {
			return fmt.Errorf("frontend performance budget: %s is empty", budget.name)
		}
		if budget.max > 0 && info.Size() > budget.max {
			return fmt.Errorf("frontend performance budget: %s is %d bytes, exceeds %d-byte ceiling", budget.name, info.Size(), budget.max)
		}
	}
	return nil
}
