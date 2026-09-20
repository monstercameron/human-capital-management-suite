package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_REV_034_02_Usage proves every gating entry point fails closed on
// bad invocation: missing inputs, unreadable files, malformed JSON and an
// unparsable clock all exit 2 with a usage error on stderr, never with a
// decision on stdout.
func TestTodo_REV_034_02_Usage(t *testing.T) {
	dir := t.TempDir()
	badJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSON, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write bad json: %v", err)
	}
	goodJSON := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(goodJSON, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write empty json: %v", err)
	}
	missing := filepath.Join(dir, "no-such-file.json")
	cases := []struct {
		name string
		args []string
	}{
		{"admit-no-flags", []string{"admit"}},
		{"admit-missing-candidate", []string{"admit", "-candidate", missing, "-policy", goodJSON}},
		{"admit-missing-policy", []string{"admit", "-candidate", goodJSON, "-policy", missing}},
		{"admit-bad-candidate", []string{"admit", "-candidate", badJSON, "-policy", goodJSON}},
		{"admit-bad-now", []string{"admit", "-candidate", goodJSON, "-policy", goodJSON, "-now", "yesterday"}},
		{"rollout-no-flags", []string{"rollout"}},
		{"rollout-missing-health", []string{"rollout", "-id", "r", "-stages", "a", "-health", missing, "-policy", goodJSON}},
		{"rollout-missing-policy", []string{"rollout", "-id", "r", "-stages", "a", "-health", goodJSON, "-policy", missing}},
		{"rollout-bad-health", []string{"rollout", "-id", "r", "-stages", "a", "-health", badJSON, "-policy", goodJSON}},
		{"rollout-bad-now", []string{"rollout", "-id", "r", "-stages", "a", "-health", goodJSON, "-policy", goodJSON, "-now", "yesterday"}},
		{"rollout-empty-stages", []string{"rollout", "-id", "r", "-stages", " , ", "-health", goodJSON, "-policy", goodJSON}},
		{"decide-no-flags", []string{"decide"}},
		{"decide-missing-evidence", []string{"decide", "-id", "d", "-evidence", missing, "-policy", goodJSON}},
		{"decide-missing-policy", []string{"decide", "-id", "d", "-evidence", goodJSON, "-policy", missing}},
		{"decide-bad-evidence", []string{"decide", "-id", "d", "-evidence", badJSON, "-policy", goodJSON}},
		{"decide-bad-now", []string{"decide", "-id", "d", "-evidence", goodJSON, "-policy", goodJSON, "-now", "yesterday"}},
		{"decide-bad-key", []string{"decide", "-id", "d", "-evidence", goodJSON, "-policy", goodJSON, "-key", missing, "-root", dir}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != 2 {
				t.Fatalf("exit = %d, want 2 for bad invocation\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("bad invocation printed a decision to stdout:\n%s", stdout.String())
			}
			if !strings.Contains(stderr.String(), "release "+tc.args[0]+":") {
				t.Fatalf("bad invocation has no usage error on stderr:\n%s", stderr.String())
			}
		})
	}
}
