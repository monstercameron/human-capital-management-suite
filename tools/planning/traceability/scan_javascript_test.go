package traceability

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanRepositoryTestNamesIncludesNamedJavaScriptCases(t *testing.T) {
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("pkg/sample_test.go", "package pkg\nfunc TestGoCase(t *testing.T) {}\n")
	write("src/lifecycle.test.ts", `
import { it } from "vitest";
it("TestDirectVitestTitle", () => {});
it.each(["TestTodo_CLIENT_002_Property", "TestTodo_CLIENT_002_Security"])("%s", () => {});
it.each(CHANNELS)("TestTodo_UX_008_Golden preserves the exact digest (%s)", () => {});
// it("TestCommentIsNotATest", () => {});
`)
	write("src/application.ts", `const fixture = "TestApplicationStringIsNotATest";`)

	names, err := ScanRepositoryTestNames(root)
	if err != nil {
		t.Fatalf("ScanRepositoryTestNames: %v", err)
	}
	for _, want := range []string{"TestGoCase", "TestDirectVitestTitle", "TestTodo_CLIENT_002_Property", "TestTodo_CLIENT_002_Security", "TestTodo_UX_008_Golden"} {
		if !names[want] {
			t.Errorf("ScanRepositoryTestNames omitted %q", want)
		}
	}
	for _, notTest := range []string{"TestCommentIsNotATest", "TestApplicationStringIsNotATest"} {
		if names[notTest] {
			t.Errorf("ScanRepositoryTestNames treated non-test text %q as a test", notTest)
		}
	}
}

func TestExtractEvidenceTestNamesReadsPlainTESTLabel(t *testing.T) {
	const title = "TestUniversalActionDiscoveryRejectsUnavailableOrDivergentAction"
	got := ExtractEvidenceTestNames("TypeScript proof: TEST " + title + " verified by vitest")
	if len(got) != 1 || got[0] != title {
		t.Fatalf("ExtractEvidenceTestNames() = %v, want [%s]", got, title)
	}
}
