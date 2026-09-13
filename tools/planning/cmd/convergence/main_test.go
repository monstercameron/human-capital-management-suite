package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/convergence"
)

func fixedNow() time.Time { return time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC) }

// smallSnapshot has one selected proposed gap, one ownerless gap and one
// inert fact, so every exit path has something to report.
func smallSnapshot() convergence.Snapshot {
	return convergence.Snapshot{
		Selected: []convergence.SelectedOwner{{Owner: "SELECT-X", Kind: convergence.OwnerKindSelection, Phase: "P0"}},
		Facts:    []convergence.Fact{{ID: "selection:SELECT-X", Owner: "SELECT-X", Tokens: []string{"select-x.yaml"}}},
		Observations: []convergence.Observation{
			{Compiler: convergence.CompilerSelectionBind, Code: "BINDING_NOT_READY", Owner: "SELECT-X", Contract: convergence.ContractSelectionGate, Detail: "placeholder"},
			{Compiler: convergence.CompilerClosureWitness, Code: "EDGE_ORPHAN", Contract: convergence.ContractEndpoint, Subject: "ENDPOINT|svc/M", Detail: "orphan"},
		},
		Unknowns: []convergence.Unknown{{Code: convergence.UnknownSlotUnfilled, Ref: "slot:provider"}},
	}
}

func TestEmitFormatsAndExitCodes(t *testing.T) {
	snap := smallSnapshot()
	var out, errOut bytes.Buffer
	if err := emit(snap, "summary", "", false, true, &out, &errOut); err != nil {
		t.Fatalf("summary with a stable fixed point: %v", err)
	}
	for _, want := range []string{"convergence: UNRESOLVED", "INERT FACT selection:SELECT-X", "UNKNOWN SELECTION_SLOT_UNFILLED slot:provider", "fixed point: stable=true", "scope SELECTED=2 UNKNOWN=1"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "UNKNOWN OWNERLESS_GAP") {
		t.Error("summary lists ownerless unknowns individually")
	}

	out.Reset()
	if err := emit(snap, "json", "", true, false, &out, &errOut); !errors.Is(err, errUnresolved) {
		t.Fatalf("strict over an unresolved register = %v", err)
	}
	var report convergence.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Digest != convergence.Converge(snap).Digest {
		t.Fatalf("json output is not the canonical report: %v", err)
	}

	out.Reset()
	if err := emit(snap, "proposals", "", false, false, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var proposals []convergence.ProposedTodo
	if err := json.Unmarshal(out.Bytes(), &proposals); err != nil || len(proposals) != 2 {
		t.Fatalf("proposals output = %s (%v)", out.String(), err)
	}
}

func TestEmitDiffsAgainstABaselineAndRefusesNewIdentities(t *testing.T) {
	dir := t.TempDir()
	older := smallSnapshot()
	older.Observations = older.Observations[:1]
	baseline, err := convergence.MarshalReport(convergence.Converge(older))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "baseline.json")
	if err := os.WriteFile(path, baseline, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err = emit(smallSnapshot(), "summary", path, false, true, &out, &errOut)
	if !errors.Is(err, errNewIdentity) {
		t.Fatalf("baseline gaining an identity = %v", err)
	}
	want := convergence.GapIdentity("", convergence.ContractEndpoint, "ENDPOINT|svc/M")
	if !strings.Contains(errOut.String(), "added=1 removed=0") || !strings.Contains(errOut.String(), "ADDED: "+want) {
		t.Fatalf("delta output:\n%s", errOut.String())
	}

	errOut.Reset()
	if err := emit(older, "summary", path, false, true, &out, &errOut); err != nil {
		t.Fatalf("unchanged baseline = %v", err)
	}
	if !strings.Contains(errOut.String(), "added=0 removed=0 resolution_changed=0") {
		t.Fatalf("unchanged delta output:\n%s", errOut.String())
	}

	errOut.Reset()
	if err := emit(convergence.Snapshot{}, "summary", path, false, false, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "REMOVED:") {
		t.Fatalf("removed identities not printed:\n%s", errOut.String())
	}

	if err := emit(older, "summary", filepath.Join(dir, "missing.json"), false, false, &out, &errOut); err == nil {
		t.Error("missing baseline accepted")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := emit(older, "summary", bad, false, false, &out, &errOut); err == nil {
		t.Error("malformed baseline accepted")
	}
}

func TestRunRejectsBadFlagsAndLoadsTheLiveRepository(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := run([]string{"-format", "yaml"}, &out, &errOut, fixedNow); err == nil {
		t.Error("unknown format accepted")
	}
	if err := run([]string{"-nope"}, &out, &errOut, fixedNow); err == nil {
		t.Error("unknown flag accepted")
	}
	if err := run([]string{"-root", t.TempDir()}, &out, &errOut, fixedNow); err == nil {
		t.Error("empty root accepted")
	}
	root := filepath.Join("..", "..", "..", "..")
	out.Reset()
	if err := run([]string{"-root", root, "-require-fixed-point"}, &out, &errOut, fixedNow); err != nil {
		t.Fatalf("live run: %v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "fixed point: stable=true") {
		t.Fatalf("live summary:\n%s", out.String())
	}
}
