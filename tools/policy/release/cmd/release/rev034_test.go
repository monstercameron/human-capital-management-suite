// RED for REV-034-02: release admission, canary rollout and the decision
// manifest need a callable entry point wired into the deployment pipeline.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/release"
)

func writeJSON(t *testing.T, dir, name string, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func rev034Now() time.Time { return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC) }

func rev034Root(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func rev034Key(t *testing.T) (root, key string) {
	t.Helper()
	root = rev034Root(t)
	key = filepath.Join("tools", "planning", "gateevidence", "testdata", "dev-signing-key.yaml")
	if _, err := os.Stat(filepath.Join(root, key)); err != nil {
		t.Fatalf("signing fixture: %v", err)
	}
	return root, key
}

// TestTodo_REV_034_02 proves the admit entry point consults Admit: a
// candidate with incomplete admission evidence is refused with a printed
// REJECT decision and a non-zero exit, never admitted silently.
func TestTodo_REV_034_02(t *testing.T) {
	dir := t.TempDir()
	candidate := writeJSON(t, dir, "candidate.json", map[string]any{
		"bundle": "testdata/no-such-bundle",
		"scope":  "prod/pilot-cell",
	})
	policy := writeJSON(t, dir, "policy.json", map[string]any{
		"trustedPublicKeys": map[string]bool{},
		"target":            map[string]any{"manifestDigest": "aa", "scope": "prod/pilot-cell"},
		"maxConformanceAge": 86400000000000,
	})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"admit", "-candidate", candidate, "-policy", policy}, &stdout, &stderr); code != 1 {
		t.Fatalf("admit exit = %d, want 1 for incomplete evidence\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	var decision release.AdmissionDecision
	if err := json.Unmarshal(stdout.Bytes(), &decision); err != nil {
		t.Fatalf("admit stdout is not a decision document: %v\n%s", err, stdout.String())
	}
	if decision.Status != release.AdmissionRejected || decision.Admitted {
		t.Fatalf("admit decision = %+v, want REJECT without admission", decision)
	}
}

// TestTodo_REV_034_02_Security proves an unsigned, unverifiable bundle can
// never pass the admit entry point even when every other field is valid.
func TestTodo_REV_034_02_Security(t *testing.T) {
	dir := t.TempDir()
	now := rev034Now()
	candidate := writeJSON(t, dir, "candidate.json", map[string]any{
		"bundle":        filepath.Join("testdata", "no-such-bundle"),
		"scope":         "prod/pilot-cell",
		"vulnerability": map[string]any{"scanned": true, "unresolvedFindings": 0},
		"conformance":   map[string]any{"generatedAt": now.Add(-time.Hour).Format(time.RFC3339)},
	})
	policy := writeJSON(t, dir, "policy.json", map[string]any{
		"trustedPublicKeys": map[string]bool{release.DevFixturePublicKey: true},
		"target":            map[string]any{"manifestDigest": strings.Repeat("a", 64), "scope": "prod/pilot-cell"},
		"maxConformanceAge": 86400000000000,
	})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"admit", "-candidate", candidate, "-policy", policy}, &stdout, &stderr); code != 1 {
		t.Fatalf("admit exit = %d, want 1 for an unverifiable bundle\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	var decision release.AdmissionDecision
	if err := json.Unmarshal(stdout.Bytes(), &decision); err != nil {
		t.Fatalf("admit stdout is not a decision document: %v\n%s", err, stdout.String())
	}
	if decision.Admitted {
		t.Fatalf("unsigned bundle admitted: %+v", decision)
	}
}

func rev034Evidence(now time.Time, openBlockers []string) map[string]any {
	fresh := now.Add(-5 * time.Minute).Format(time.RFC3339)
	hour := now.Add(-time.Hour).Format(time.RFC3339)
	return map[string]any{
		"source":      map[string]any{"ref": map[string]any{"repository": "example.com/hcm", "ref": "main", "commit": strings.Repeat("d", 40)}},
		"toolchain":   map[string]any{"config": map[string]any{"go_version": "go1.26.3", "goos": "windows", "goarch": "arm64", "config_digest": "cfg"}},
		"artifact":    map[string]any{"verification": map[string]any{"manifestDigest": strings.Repeat("a", 64), "version": "test", "artifacts": []string{"hcmnext"}}},
		"schema":      map[string]any{"bundleSchemaVersion": 1, "validatedAt": fresh},
		"config":      map[string]any{"digest": "cfg", "valid": true, "validatedAt": fresh},
		"test":        map[string]any{"ranAt": fresh, "passed": 42, "failed": 0},
		"conformance": map[string]any{"generatedAt": hour},
		"rollout": map[string]any{"snapshot": map[string]any{
			"id":     "rev034-r1",
			"status": "IN_PROGRESS",
			"history": []any{map[string]any{
				"sequence": 1, "kind": "advance", "stage": "canary-5",
				"status": "IN_PROGRESS", "evaluated_at": hour, "digest": "hh",
			}},
		}},
		"rollback": map[string]any{"decision": map[string]any{
			"status": "ADMIT", "admitted": true, "scope": "prod/pilot-cell",
			"manifest_digest": strings.Repeat("b", 64), "evaluated_at": fresh, "digest": "dd",
		}},
		"owner":   map[string]any{"name": "cam", "confirmedAt": fresh},
		"blocker": map[string]any{"checkedAt": fresh, "open": openBlockers},
	}
}

func rev034HealthyStage(now time.Time) map[string]any {
	fresh := now.Add(-5 * time.Minute).Format(time.RFC3339)
	hour := now.Add(-time.Hour).Format(time.RFC3339)
	return map[string]any{
		"slo":            map[string]any{"withinBudget": true, "measuredAt": fresh},
		"telemetry":      map[string]any{"healthy": true, "observedAt": fresh},
		"reconciliation": map[string]any{"consistent": true, "checkedAt": fresh},
		"conformance":    map[string]any{"generatedAt": hour},
	}
}

// TestTodo_REV_034_02_Integration drives decide and rollout through the new
// entry points: full evidence yields a signed PROCEED at exit 0, an open
// blocker yields STOP at exit 1, and a healthy stage advances at exit 0
// while a stale stage refuses at exit 1.
func TestTodo_REV_034_02_Integration(t *testing.T) {
	dir := t.TempDir()
	now := rev034Now()
	nowFlag := now.Format(time.RFC3339)
	root, key := rev034Key(t)
	policy := writeJSON(t, dir, "policy.json", map[string]any{"maxConformanceAge": 86400000000000})

	proceed := writeJSON(t, dir, "proceed.json", rev034Evidence(now, nil))
	var stdout, stderr bytes.Buffer
	decide := []string{"decide", "-id", "rev034-proceed", "-evidence", proceed, "-policy", policy, "-key", key, "-root", root, "-now", nowFlag}
	if code := run(decide, &stdout, &stderr); code != 0 {
		t.Fatalf("decide exit = %d, want 0 for full evidence\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	var manifest release.DecisionManifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("decide stdout is not a manifest: %v\n%s", err, stdout.String())
	}
	if manifest.Verdict != release.DecisionProceed {
		t.Fatalf("decide verdict = %s, want PROCEED: %v", manifest.Verdict, manifest.Reasons)
	}
	if manifest.Signature == nil {
		t.Fatal("PROCEED manifest is unsigned")
	}
	if err := release.VerifyDecisionManifest(manifest, nil); err != nil {
		t.Fatalf("signed PROCEED manifest does not verify: %v", err)
	}

	blocked := writeJSON(t, dir, "blocked.json", rev034Evidence(now, []string{"SEV-1 open incident"}))
	stdout.Reset()
	stderr.Reset()
	decideBlocked := []string{"decide", "-id", "rev034-blocked", "-evidence", blocked, "-policy", policy, "-key", key, "-root", root, "-now", nowFlag}
	if code := run(decideBlocked, &stdout, &stderr); code != 1 {
		t.Fatalf("blocked decide exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	var stopped release.DecisionManifest
	if err := json.Unmarshal(stdout.Bytes(), &stopped); err != nil {
		t.Fatalf("blocked decide stdout is not a manifest: %v\n%s", err, stdout.String())
	}
	if stopped.Verdict != release.DecisionStop {
		t.Fatalf("blocked decide verdict = %s, want STOP", stopped.Verdict)
	}

	health := writeJSON(t, dir, "health.json", rev034HealthyStage(now))
	rolloutPolicy := writeJSON(t, dir, "rollout-policy.json", map[string]any{"maxConformanceAge": 86400000000000})
	stdout.Reset()
	stderr.Reset()
	advance := []string{"rollout", "-id", "rev034-r1", "-stages", "canary-5,full", "-health", health, "-policy", rolloutPolicy, "-now", nowFlag}
	if code := run(advance, &stdout, &stderr); code != 0 {
		t.Fatalf("rollout exit = %d, want 0 for healthy stage\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	var record release.StageRecord
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatalf("rollout stdout is not a stage record: %v\n%s", err, stdout.String())
	}
	if record.Stage == "" || record.Digest == "" {
		t.Fatalf("rollout record is empty: %+v", record)
	}

	staleHealth := rev034HealthyStage(now)
	staleHealth["conformance"] = map[string]any{"generatedAt": now.Add(-48 * time.Hour).Format(time.RFC3339)}
	stale := writeJSON(t, dir, "stale-health.json", staleHealth)
	stdout.Reset()
	stderr.Reset()
	advanceStale := []string{"rollout", "-id", "rev034-r2", "-stages", "canary-5,full", "-health", stale, "-policy", rolloutPolicy, "-now", nowFlag}
	if code := run(advanceStale, &stdout, &stderr); code != 1 {
		t.Fatalf("stale rollout exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
}

func TestTodo_REV_034_02_DecisionExitCodes(t *testing.T) {
	dir := t.TempDir()
	now := rev034Now()
	root, key := rev034Key(t)
	policy := writeJSON(t, dir, "policy.json", map[string]any{"maxConformanceAge": 86400000000000})
	cases := []struct {
		name        string
		evidence    map[string]any
		missing     bool
		wantCode    int
		wantVerdict release.DecisionVerdict
	}{
		{name: "proceed", evidence: rev034Evidence(now, nil), wantCode: 0, wantVerdict: release.DecisionProceed},
		{name: "remediate", evidence: func() map[string]any {
			evidence := rev034Evidence(now, nil)
			evidence["config"] = map[string]any{"digest": "cfg", "valid": false, "validatedAt": now.Add(-5 * time.Minute).Format(time.RFC3339)}
			return evidence
		}(), wantCode: 1, wantVerdict: release.DecisionRemediate},
		{name: "quarantine", evidence: func() map[string]any {
			evidence := rev034Evidence(now, nil)
			evidence["schema"] = map[string]any{"bundleSchemaVersion": release.SchemaVersion + 1, "validatedAt": now.Add(-5 * time.Minute).Format(time.RFC3339)}
			return evidence
		}(), wantCode: 1, wantVerdict: release.DecisionQuarantine},
		{name: "stop", evidence: rev034Evidence(now, []string{"SEV-1 open incident"}), wantCode: 1, wantVerdict: release.DecisionStop},
		{name: "tool failure", missing: true, wantCode: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidencePath := filepath.Join(dir, tc.name+".json")
			if !tc.missing {
				evidencePath = writeJSON(t, dir, tc.name+".json", tc.evidence)
			}
			args := []string{"decide", "-id", "rev034-" + strings.ReplaceAll(tc.name, " ", "-"), "-evidence", evidencePath, "-policy", policy, "-key", key, "-root", root, "-now", now.Format(time.RFC3339)}
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != tc.wantCode {
				t.Fatalf("decide exit = %d, want %d\nstdout: %s\nstderr: %s", code, tc.wantCode, stdout.String(), stderr.String())
			}
			if tc.missing {
				if stdout.Len() != 0 || !strings.Contains(stderr.String(), "read "+evidencePath) {
					t.Fatalf("tool failure output = stdout %q, stderr %q; want no decision and a read error", stdout.String(), stderr.String())
				}
				return
			}
			var manifest release.DecisionManifest
			if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
				t.Fatalf("decide stdout is not a manifest: %v\n%s", err, stdout.String())
			}
			if manifest.Verdict != tc.wantVerdict {
				t.Fatalf("decide verdict = %s, want %s", manifest.Verdict, tc.wantVerdict)
			}
		})
	}
}

// TestTodo_REV_034_02_Golden pins the exact signed PROCEED manifest bytes
// the decide entry point emits for fixed evidence and a fixed clock, so a
// drift in the decision document breaks the build.
func TestTodo_REV_034_02_Golden(t *testing.T) {
	dir := t.TempDir()
	now := rev034Now()
	root, key := rev034Key(t)
	policy := writeJSON(t, dir, "policy.json", map[string]any{"maxConformanceAge": 86400000000000})
	proceed := writeJSON(t, dir, "proceed.json", rev034Evidence(now, nil))
	var stdout, stderr bytes.Buffer
	decide := []string{"decide", "-id", "rev034-proceed", "-evidence", proceed, "-policy", policy, "-key", key, "-root", root, "-now", now.Format(time.RFC3339)}
	if code := run(decide, &stdout, &stderr); code != 0 {
		t.Fatalf("decide exit = %d, want 0\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	var manifest release.DecisionManifest
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("decide stdout is not a manifest: %v", err)
	}
	if manifest.Verdict != release.DecisionProceed || manifest.Signature == nil {
		t.Fatalf("golden fixture is not a signed PROCEED manifest: %+v", manifest)
	}
	for class, present := range map[string]bool{
		"source": manifest.Classes.Source, "toolchain": manifest.Classes.Toolchain,
		"artifact": manifest.Classes.Artifact, "schema": manifest.Classes.Schema,
		"config": manifest.Classes.Config, "test": manifest.Classes.Test,
		"conformance": manifest.Classes.Conformance, "rollout": manifest.Classes.Rollout,
		"rollback": manifest.Classes.Rollback, "owner": manifest.Classes.Owner,
		"blocker": manifest.Classes.Blocker,
	} {
		if !present {
			t.Fatalf("golden fixture class %s is not passing", class)
		}
	}
	goldenPath := filepath.Join("testdata", "rev034_decide_proceed_golden.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(goldenPath, stdout.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if stdout.String() != string(want) {
		t.Fatalf("decide output differs from %s:\ngot:\n%s\nwant:\n%s", goldenPath, stdout.String(), string(want))
	}
}
