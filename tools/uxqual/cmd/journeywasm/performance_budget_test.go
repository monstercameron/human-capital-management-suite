//go:build !(js && wasm)

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodo_WEB_236_PerformanceBudget(t *testing.T) {
	out := t.TempDir()
	for _, budget := range [...]assetSizeBudget{
		{name: wasmFile, max: maxJourneyWasmBytes},
		{name: wasmFile + ".gz", max: maxJourneyWasmGzipBytes},
		{name: wasmExecFile, max: maxWasmExecBytes},
		{name: wasmExecFile + ".gz", max: maxWasmExecGzipBytes},
	} {
		if err := os.WriteFile(filepath.Join(out, budget.name), []byte("asset"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkJourneyAssetBudgets(out); err != nil {
		t.Fatalf("under-budget assets rejected: %v", err)
	}

	if err := os.WriteFile(filepath.Join(out, wasmFile+".gz"), make([]byte, maxJourneyWasmGzipBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	err := checkJourneyAssetBudgets(out)
	if err == nil || !strings.Contains(err.Error(), wasmFile+".gz") || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("over-budget compressed bundle error = %v", err)
	}
}

func TestTodo_WEB_236_PerformanceBudgetRejectsIncompleteArtifacts(t *testing.T) {
	for _, fixture := range []struct {
		name string
		body []byte
	}{
		{name: "missing", body: nil},
		{name: "empty", body: []byte{}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			out := t.TempDir()
			for _, name := range []string{wasmFile, wasmFile + ".gz", wasmExecFile, wasmExecFile + ".gz"} {
				if err := os.WriteFile(filepath.Join(out, name), []byte("asset"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(out, wasmExecFile)
			if fixture.name == "missing" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, fixture.body, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := checkJourneyAssetBudgets(out); err == nil {
				t.Fatal("incomplete frontend artifact set passed the budget gate")
			} else if fixture.name == "missing" && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing artifact error = %v, want not-exist cause", err)
			}
		})
	}
}
