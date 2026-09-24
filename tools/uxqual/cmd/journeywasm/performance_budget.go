//go:build !(js && wasm)

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Browser transfer and startup costs are bounded by these engineering
// ceilings. They describe generated assets, not governed business budgets.
const (
	maxJourneyWasmBytes     int64 = 60 << 20
	maxJourneyWasmGzipBytes int64 = 12 << 20
	maxWasmExecBytes        int64 = 1 << 20
	maxWasmExecGzipBytes    int64 = 256 << 10
)

type assetSizeBudget struct {
	name string
	max  int64
}

// checkJourneyAssetBudgets enforces generated browser asset sizes using
// exact file lengths. The caller invokes it after compression and before
// writing the integrity manifest, so an over-budget artifact cannot ship.
func checkJourneyAssetBudgets(outDir string) error {
	budgets := [...]assetSizeBudget{
		{name: wasmFile, max: maxJourneyWasmBytes},
		{name: wasmFile + ".gz", max: maxJourneyWasmGzipBytes},
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
		if info.Size() > budget.max {
			return fmt.Errorf("frontend performance budget: %s is %d bytes, exceeds %d-byte ceiling", budget.name, info.Size(), budget.max)
		}
	}
	return nil
}
