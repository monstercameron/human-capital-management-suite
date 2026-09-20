package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestWedgeCLIRejectsBadInputs exercises every CLI usage and input error
// branch so the package clears the coverage floor.
func TestWedgeCLIRejectsBadInputs(t *testing.T) {
	dir := t.TempDir()
	receipt := writeTemp(t, "receipt.yaml", rev002GateAReceipt)
	key := testKeyFile(t)
	badKey := writeTemp(t, "badkey.hex", "zzzz")
	shortKey := writeTemp(t, "shortkey.hex", "00")
	badReceipt := writeTemp(t, "bad.yaml", "{{{{")
	badPartner := writeTemp(t, "badpartner.yaml", "schema_version: 99\n")
	missing := filepath.Join(dir, "does-not-exist.yaml")
	out := filepath.Join(dir, "out.yaml")

	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no gate", []string{}, "-gate must be a or b"},
		{"bad gate", []string{"-gate", "c"}, "-gate must be a or b"},
		{"no receipt", []string{"-gate", "a"}, "-receipt is required"},
		{"no key", []string{"-gate", "a", "-receipt", receipt}, "-key is required"},
		{"no signer", []string{"-gate", "a", "-receipt", receipt, "-key", key}, "-signer is required"},
		{"bad signed-at", []string{"-gate", "a", "-receipt", receipt, "-key", key, "-signer", "s", "-signed-at", "not-a-time", "-out", out}, "parsing -signed-at"},
		{"bad now", []string{"-gate", "b", "-receipt", receipt, "-key", key, "-signer", "s", "-now", "not-a-time", "-out", out}, "parsing -now"},
		{"bad key hex", []string{"-gate", "a", "-receipt", receipt, "-key", badKey, "-signer", "s", "-out", out}, "decoding hex key"},
		{"short key", []string{"-gate", "a", "-receipt", receipt, "-key", shortKey, "-signer", "s", "-out", out}, "key has 1 bytes"},
		{"missing receipt", []string{"-gate", "a", "-receipt", missing, "-key", key, "-signer", "s", "-out", out}, "reading receipt"},
		{"missing receipt b", []string{"-gate", "b", "-receipt", missing, "-key", key, "-signer", "s", "-out", out}, "reading receipt"},
		{"bad receipt yaml", []string{"-gate", "a", "-receipt", badReceipt, "-key", key, "-signer", "s", "-out", out}, "decoding receipt"},
		{"bad partner", []string{"-gate", "a", "-receipt", receipt, "-partner", badPartner, "-key", key, "-signer", "s", "-out", out}, "partner manifest invalid"},
		{"verify missing", []string{"-gate", "a", "-verify", missing}, "reading decision record"},
		{"verify bad yaml", []string{"-gate", "a", "-verify", badReceipt}, "decoding gate A record"},
		{"verify bad yaml b", []string{"-gate", "b", "-verify", badReceipt}, "decoding gate B record"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runWedge(t, tc.args...)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}

	if got := defaultOut("a"); got != filepath.Join("definitions", "planning", "gates", "wedge-gatea-decision.yaml") {
		t.Errorf("defaultOut(a) = %q", got)
	}
	if got := defaultOut("b"); got != filepath.Join("definitions", "planning", "gates", "wedge-gateb-decision.yaml") {
		t.Errorf("defaultOut(b) = %q", got)
	}

	// A cleanly written record must also verify through -verify mode.
	verifyOut := filepath.Join(dir, "verify-me.yaml")
	if _, _, err := runWedge(t, "-gate", "a", "-receipt", receipt, "-key", key, "-signer", "s", "-signed-at", "2026-09-19T12:00:00Z", "-out", verifyOut); err != nil {
		t.Fatalf("setup run failed: %v", err)
	}
	stdout, _, err := runWedge(t, "-gate", "a", "-verify", verifyOut)
	if err != nil {
		t.Fatalf("verify of a clean record failed: %v", err)
	}
	if !strings.Contains(stdout, "signature_verified: true") {
		t.Errorf("verify output missing confirmation:\n%s", stdout)
	}
}
