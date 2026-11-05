package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// chatbug093Root is the repository root the sweep's checks run from: the
// directory above this file that holds go.mod.
func chatbug093Root(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

// chatbug093Go runs one go command from the repository root and fails with its
// output when it does not succeed.
func chatbug093Go(t *testing.T, timeout time.Duration, env []string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, "go", args...)
	command.Dir = chatbug093Root(t)
	if env != nil {
		command.Env = env
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

// TestTodo_CHATBUG_093: the tests left red after the second wave pass, and the
// committed debug prints are gone. Each test the lane report named is run in
// its own package and must pass; the two println lines in this package's
// production sources must be gone. (Whole-package runs are the tick's evidence;
// this test pins the focused runs so a regression fails fast.)
func TestTodo_CHATBUG_093(t *testing.T) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	dir := filepath.Dir(self)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "S3DIAG") || strings.Contains(string(content), "println(") {
			t.Fatalf("%s still carries a debug print", name)
		}
	}
	for _, run := range []struct {
		pkg   string
		tests []string
	}{
		{"./internal/humanwork/chatui/", []string{"TestTodo_AGENTUX_063", "TestAgentUXChat5_LightPageAndMotion", "TestTodo_CHATUX_029", "TestTodo_CHATUX_025_Literals"}},
		{"./internal/humanwork/productui/", []string{"TestNoIsMixesElementsWithClasses", "TestEveryCSSVariableUsedIsDefined", "TestTextAlignmentIsLogical"}},
		{"./tools/uxqual/cmd/journeywasm/", []string{"TestTodo_CHATUX_026_ArrivalOrder"}},
	} {
		output := chatbug093Go(t, 15*time.Minute, nil, "test", "-count=1", "-v", "-run", "^("+strings.Join(run.tests, "|")+")$", run.pkg)
		for _, name := range run.tests {
			if !strings.Contains(output, "--- PASS: "+name+" ") {
				t.Fatalf("%s did not report %s passing:\n%s", run.pkg, name, output)
			}
		}
	}
}

// TestTodo_CHATBUG_093_Integration: the PostgreSQL-backed test left red after
// the second wave passes against a real database.
func TestTodo_CHATBUG_093_Integration(t *testing.T) {
	output := chatbug093Go(t, 15*time.Minute, nil, "test", "-count=1", "-v", "-run", "^TestDocumentSeedDemoDocuments_RoutedChat$", "./cmd/migrate/")
	if !strings.Contains(output, "--- PASS: TestDocumentSeedDemoDocuments_RoutedChat ") {
		t.Fatalf("the seed test did not pass:\n%s", output)
	}
}

// TestTodo_CHATBUG_093_Browser: go vet passes natively on the chat packages
// and for wasm on all three browser-reachable packages, and journeywasm still
// builds for wasm. (Server-only tests carry !(js && wasm) tags, so the wasm
// legs vet the browser client as the browser compiles it.)
func TestTodo_CHATBUG_093_Browser(t *testing.T) {
	chatbug093Go(t, 15*time.Minute, nil, "vet", "./internal/humanwork/chatui/", "./internal/humanwork/productui/", "./tools/uxqual/cmd/journeywasm/")
	wasm := append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	chatbug093Go(t, 15*time.Minute, wasm, "vet", "./internal/humanwork/chatui/", "./internal/humanwork/productui/", "./tools/uxqual/cmd/journeywasm/")
	chatbug093Go(t, 15*time.Minute, wasm, "build", "-o", filepath.Join(t.TempDir(), "journey.wasm"), "./tools/uxqual/cmd/journeywasm/")
}
