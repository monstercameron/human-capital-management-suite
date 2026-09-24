package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHygieneFile(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestTesthygieneCommandAcceptsCleanTree(t *testing.T) {
	root := t.TempDir()
	writeHygieneFile(t, root, "clean_test.go", "package clean\n\nimport \"testing\"\n\nfunc TestReal(t *testing.T) {\n\tif 1+1 != 2 {\n\t\tt.Fatal(\"arithmetic broke\")\n\t}\n}\n")
	var out, errOut bytes.Buffer
	if code := run([]string{"testhygiene", "-root", root}, &out, &errOut); code != 0 {
		t.Fatalf("clean tree exit=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "PASS") {
		t.Fatalf("clean tree output=%q, want PASS", out.String())
	}
}

func TestTesthygieneCommandRejectsViolations(t *testing.T) {
	root := t.TempDir()
	writeHygieneFile(t, root, "alias_test.go", "package bad\n\nimport \"testing\"\n\nfunc TestAlias(t *testing.T) { TestReal(t) }\n")
	writeHygieneFile(t, root, "race_test.go", "package bad\n\nimport \"testing\"\n\nfunc TestTodo_X_Race(t *testing.T) {\n\tfor i := 0; i < 2; i++ {\n\t\tif i < 0 {\n\t\t\tt.Fatal(\"unreachable\")\n\t\t}\n\t}\n}\n")
	writeHygieneFile(t, root, "golden_test.go", "package bad\n\nimport \"testing\"\n\nfunc TestTodo_X_Golden(t *testing.T) { t.Skip(\"no oracle\") }\n")
	var out, errOut bytes.Buffer
	if code := run([]string{"testhygiene", "-root", root}, &out, &errOut); code != 1 {
		t.Fatalf("bad tree exit=%d, want 1 (stdout=%s stderr=%s)", code, out.String(), errOut.String())
	}
	for _, want := range []string{"alias-test", "race-without-concurrency", "golden-skip", "alias_test.go", "race_test.go", "golden_test.go"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report output missing %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(errOut.String(), "REJECTED") {
		t.Errorf("stderr missing REJECTED: %q", errOut.String())
	}
}

func TestTesthygieneCommandRejectsBadUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"testhygiene", "-root"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("dangling -root exit=%d stderr=%s, want 2 with usage", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"testhygiene", "-bogus", "x"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "unknown option") {
		t.Fatalf("unknown option exit=%d stderr=%s, want 2 with unknown option", code, errOut.String())
	}
}

func TestTesthygieneWiredIntoRequiredGates(t *testing.T) {
	const command = "go run ./tools/quality testhygiene -root ."
	for _, path := range []string{"../../.husky/pre-commit", "../../.github/workflows/tests.yml"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if !strings.Contains(string(content), command) {
			t.Errorf("%s does not invoke %q", path, command)
		}
	}
}
