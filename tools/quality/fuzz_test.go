package main

import (
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

var windowsTestExecutableCleanup = regexp.MustCompile(`(?i)^go: unlinkat .+go-build.+\.test\.exe: access is denied\.$`)

// fuzzSubprocessPassed treats the documented Windows go-build executable
// cleanup diagnostic as post-test noise only when go test already emitted an
// ok result for the requested package. Every other nonzero subprocess result
// remains a failure.
func fuzzSubprocessPassed(err error, output, packagePath string) bool {
	if err == nil {
		return true
	}

	packagePassed := false
	cleanupSeen := false
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if windowsTestExecutableCleanup.MatchString(line) {
			if !packagePassed {
				return false
			}
			cleanupSeen = true
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "ok" && fields[1] == packagePath {
			packagePassed = true
		}
		if strings.HasPrefix(line, "FAIL") || strings.HasPrefix(line, "go:") {
			return false
		}
	}
	return cleanupSeen && packagePassed
}

func TestTodo_TOOL_013_CleanupClassification(t *testing.T) {
	packagePath := "github.com/monstercameron/human-capital-management-suite/tools/quality/fuzzkit"
	cleanup := "go: unlinkat C:\\work\\go-build123\\b001\\fuzzkit.test.exe: Access is denied."
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "package success before Windows cleanup diagnostic",
			output: "PASS\nok\t" + packagePath + "\t5s\n" + cleanup,
			want:   true,
		},
		{
			name:   "actual fuzz failure remains a failure",
			output: "--- FAIL: FuzzTodo_TOOL_013 (0.01s)\nFAIL\t" + packagePath + "\n" + cleanup,
			want:   false,
		},
		{
			name:   "cleanup diagnostic without package success remains a failure",
			output: cleanup,
			want:   false,
		},
		{
			name:   "unrecognized go command error remains a failure",
			output: "ok " + packagePath + " 5s\ngo: unexpected error",
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fuzzSubprocessPassed(errors.New("exit status 1"), tt.output, packagePath); got != tt.want {
				t.Fatalf("fuzzSubprocessPassed() = %t, want %t", got, tt.want)
			}
		})
	}
}

// TestTodo_TOOL_013 is the TOOL-013 primary test. It runs FuzzTodo_TOOL_013
// in a bounded (-fuzztime=5s), real fuzzing subprocess against two
// packages that share the same seed corpus (fuzzkit.SeedCorpus):
//
//   - tools/quality/testdata/fuzzdefect, which has one planted defect, must
//     be found by the fuzz run (RED).
//   - tools/quality/fuzzkit, the fixed reference parser, must never panic
//     under the same bounded run (GREEN).
func TestTodo_TOOL_013(t *testing.T) {
	root := repoRoot(t)

	t.Run("seeded malformed inputs find the planted defect", func(t *testing.T) {
		cmd := exec.Command("go", "test", "-run", "^$", "-fuzz", "^FuzzTodo_TOOL_013$",
			"-fuzztime", "5s", "-count=1", "./tools/quality/testdata/fuzzdefect/")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected the fuzz run to find the planted defect in fuzzdefect.ParseEnvelope, it passed:\n%s", out)
		}
		if !strings.Contains(string(out), "FAIL") {
			t.Errorf("expected fuzz output to report FAIL, got:\n%s", out)
		}
		if !strings.Contains(string(out), "index out of range") {
			t.Errorf("expected fuzz output to report the planted index-out-of-range panic, got:\n%s", out)
		}
	})

	t.Run("bounded fuzz run over the fixed parser never panics", func(t *testing.T) {
		cmd := exec.Command("go", "test", "-run", "^$", "-fuzz", "^FuzzTodo_TOOL_013$",
			"-fuzztime", "5s", "-count=1", "./tools/quality/fuzzkit/")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if !fuzzSubprocessPassed(err, string(out), "github.com/monstercameron/human-capital-management-suite/tools/quality/fuzzkit") {
			t.Fatalf("expected the bounded fuzz run over the fixed parser to pass, got:\n%s", out)
		}
	})
}
