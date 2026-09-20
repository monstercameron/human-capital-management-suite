package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/wedge"
	"gopkg.in/yaml.v3"
)

const rev002GateAReceipt = `schema_version: 1
manifest_todo_id: WEDGE-014
manifest_digest: 9f2c4a6b8d1e3f5a7b9c0d2e4f6a8b1c3d5e7f9a1b3c5d7e9f1a3b5c7d9e1f3a5
as_of: "2026-09-19"
decision: GATE_CLEAR
findings: []
`

const rev002GateABlockedReceipt = `schema_version: 1
manifest_todo_id: WEDGE-014
manifest_digest: 9f2c4a6b8d1e3f5a7b9c0d2e4f6a8b1c3d5e7f9a1b3c5d7e9f1a3b5c7d9e1f3a5
as_of: "2026-09-19"
decision: GATE_BLOCKED
findings: []
`

const rev002GateBReceipt = `schema_version: 1
manifest_todo_id: WEDGE-015
dependencies:
- name: pilot-topology
  state: RESOLVED
scenarios:
- name: promotion-simulation
  passed: true
security:
  approver: pilot-security-owner
  approved_at: "2026-09-01T00:00:00Z"
  valid_for: 7776000000000000
rpo:
  met: true
rto:
  met: true
repair_owner: pilot-repair-owner
`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testKeyFile(t *testing.T) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub
	return writeTemp(t, "key.hex", hex.EncodeToString(priv))
}

func realPartnerYAML(t *testing.T) string {
	t.Helper()
	m := wedge.PlaceholderPartnerManifest()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	fixed := strings.ReplaceAll(string(raw), "PLACEHOLDER", "ACME")
	var m2 wedge.PartnerManifest
	if err := json.Unmarshal([]byte(fixed), &m2); err != nil {
		t.Fatal(err)
	}
	m2.SchemaVersion = wedge.SchemaVersion
	m2.Digest = wedge.DigestPartnerManifest(m2)
	if violations := wedge.ValidatePartnerManifest(m2); len(violations) != 0 {
		t.Fatalf("fixture partner manifest invalid: %v", violations)
	}
	bridged, err := json.Marshal(m2)
	if err != nil {
		t.Fatal(err)
	}
	var anyForm any
	if err := json.Unmarshal(bridged, &anyForm); err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(anyForm)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func runWedge(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

// TestTodo_REV_002_01 is the REV-002-01 primary: the wedge CLI evaluates a
// real Gate A evidence receipt plus a real partner manifest through the
// existing pure evaluators, writes a signed decision record, and re-verifies
// the signature and digest on reload.
func TestTodo_REV_002_01(t *testing.T) {
	dir := t.TempDir()
	receipt := writeTemp(t, "receipt.yaml", rev002GateAReceipt)
	partner := writeTemp(t, "partner.yaml", realPartnerYAML(t))
	key := testKeyFile(t)
	out := filepath.Join(dir, "wedge-gatea-decision.yaml")

	stdout, _, err := runWedge(t,
		"-gate", "a",
		"-receipt", receipt,
		"-partner", partner,
		"-key", key,
		"-signer", "rev-002-01-tester",
		"-signed-at", "2026-09-19T12:00:00Z",
		"-out", out,
	)
	if err != nil {
		t.Fatalf("wedge run failed: %v", err)
	}
	if !strings.Contains(stdout, "decision: PROCEED") {
		t.Errorf("expected a PROCEED decision, stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "signature_verified_on_reload: true") {
		t.Errorf("expected reload re-verification, stdout:\n%s", stdout)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var record gateevidence.GateADecisionRecord
	if err := decodeYAML(raw, &record); err != nil {
		t.Fatal(err)
	}
	if record.Decision != gateevidence.GateAProceed {
		t.Errorf("record decision = %q, want PROCEED", record.Decision)
	}
	if record.WriteAuthority {
		t.Error("gate A record must never grant write authority")
	}
	verified, err := gateevidence.VerifyGateADecision(record)
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Error("persisted decision signature did not verify")
	}
}

// TestTodo_REV_002_01_Conformance proves the CLI delegates Gate B evaluation
// to the existing pure evaluator instead of reimplementing it.
func TestTodo_REV_002_01_Conformance(t *testing.T) {
	dir := t.TempDir()
	receiptPath := writeTemp(t, "receipt.yaml", rev002GateBReceipt)
	key := testKeyFile(t)
	out := filepath.Join(dir, "wedge-gateb-decision.yaml")

	stdout, _, err := runWedge(t,
		"-gate", "b",
		"-receipt", receiptPath,
		"-key", key,
		"-signer", "rev-002-01-tester",
		"-signed-at", "2026-09-19T12:00:00Z",
		"-now", "2026-09-19T12:00:00Z",
		"-out", out,
	)
	if err != nil {
		t.Fatalf("wedge run failed: %v", err)
	}
	if !strings.Contains(stdout, "decision: PROCEED_LIMITED") {
		t.Errorf("expected a PROCEED_LIMITED decision, stdout:\n%s", stdout)
	}

	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt gateevidence.GateBPilotReceipt
	if err := decodeYAML(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	expected := gateevidence.EvaluateGateBDecision(receipt, now)

	outRaw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var record gateevidence.GateBDecisionRecord
	if err := decodeYAML(outRaw, &record); err != nil {
		t.Fatal(err)
	}
	if record.Decision != expected.Decision {
		t.Errorf("CLI decision %q != library decision %q", record.Decision, expected.Decision)
	}
	if record.EvidenceDigest != expected.EvidenceDigest {
		t.Errorf("CLI digest %q != library digest %q", record.EvidenceDigest, expected.EvidenceDigest)
	}
	verified, err := gateevidence.VerifyGateBDecision(record)
	if err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Error("persisted gate B signature did not verify")
	}
}

// TestTodo_REV_002_01_Fault proves a tampered decision record fails
// re-verification and that non-proceed evidence yields a signed non-proceed
// record instead of silent success.
func TestTodo_REV_002_01_Fault(t *testing.T) {
	dir := t.TempDir()
	receipt := writeTemp(t, "receipt.yaml", rev002GateAReceipt)
	key := testKeyFile(t)
	out := filepath.Join(dir, "wedge-gatea-decision.yaml")

	if _, _, err := runWedge(t,
		"-gate", "a", "-receipt", receipt, "-key", key,
		"-signer", "rev-002-01-tester", "-signed-at", "2026-09-19T12:00:00Z",
		"-out", out,
	); err != nil {
		t.Fatalf("wedge run failed: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	tampered := tamperDigest(t, string(raw))
	tamperedPath := filepath.Join(dir, "tampered.yaml")
	if err := os.WriteFile(tamperedPath, []byte(tampered), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runWedge(t, "-gate", "a", "-verify", tamperedPath)
	if err == nil {
		t.Error("tampered decision verified clean")
	} else {
		combined := strings.ToLower(stderr + ` ` + err.Error())
		if !strings.Contains(combined, `signature`) {
			t.Errorf(`tamper error does not name the signature: stderr=%q err=%v`, stderr, err)
		}
	}

	blocked := writeTemp(t, "blocked.yaml", rev002GateABlockedReceipt)
	blockedOut := filepath.Join(dir, "blocked-decision.yaml")
	stdout, _, err := runWedge(t,
		"-gate", "a", "-receipt", blocked, "-key", key,
		"-signer", "rev-002-01-tester", "-signed-at", "2026-09-19T12:00:00Z",
		"-out", blockedOut,
	)
	if err != nil {
		t.Fatalf("blocked-evidence run failed: %v", err)
	}
	if !strings.Contains(stdout, "decision: REMEDIATE") {
		t.Errorf("expected a REMEDIATE decision on blocked evidence, stdout:\n%s", stdout)
	}
}

// TestTodo_REV_002_01_Golden pins the exact CLI summary bytes for the
// canonical Gate A fixture.
func TestTodo_REV_002_01_Golden(t *testing.T) {
	dir := t.TempDir()
	receipt := writeTemp(t, "receipt.yaml", rev002GateAReceipt)
	key := testKeyFile(t)
	out := filepath.Join(dir, "wedge-gatea-decision.yaml")

	stdout, _, err := runWedge(t,
		"-gate", "a", "-receipt", receipt, "-key", key,
		"-signer", "rev-002-01-tester", "-signed-at", "2026-09-19T12:00:00Z",
		"-out", out,
	)
	if err != nil {
		t.Fatalf("wedge run failed: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "wedge_a_summary.golden"))
	if err != nil {
		t.Fatal(err)
	}
	// The key is random per run, so only the digest-bearing prefix lines are
	// pinned; the signature line shape is asserted, not its bytes.
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 summary lines, got %d:\n%s", len(lines), stdout)
	}
	pinned := strings.Join(lines[:3], "\n") + "\n"
	if pinned != string(want) {
		t.Errorf("golden mismatch:\n got: %q\nwant: %q", pinned, string(want))
	}
	if !strings.HasPrefix(lines[3], "signature_verified_on_reload: ") {
		t.Errorf("fourth line is not the re-verification verdict: %q", lines[3])
	}
}

// tamperDigest flips the first hex character of the evidence_digest line
// so the persisted signature no longer matches the record digest.
func tamperDigest(t *testing.T, record string) string {
	t.Helper()
	lines := strings.Split(record, "\n")
	for i, line := range lines {
		rest, ok := strings.CutPrefix(line, "evidence_digest: ")
		if !ok || len(rest) == 0 {
			continue
		}
		flipped := "0"
		if rest[:1] == "0" {
			flipped = "1"
		}
		lines[i] = "evidence_digest: " + flipped + rest[1:]
		return strings.Join(lines, "\n")
	}
	t.Fatal("record has no evidence_digest to tamper")
	return ""
}
