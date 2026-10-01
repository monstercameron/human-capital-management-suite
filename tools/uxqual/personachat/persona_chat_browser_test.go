// Package personachat runs persona acceptance against a served, hydrated app.
package personachat

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTodo_AGENTP_019_Browser(t *testing.T) {
	runPersonaBrowser(t, "TestTodo_AGENTP_019_Browser")
}

func TestTodo_AGENTP_020_Browser(t *testing.T) {
	runPersonaBrowser(t, "TestTodo_AGENTP_020_Browser")
}

func TestTodo_AGENTP_019_I18n(t *testing.T) {
	runPersonaBrowser(t, "TestTodo_AGENTP_019_I18n")
}

// A document GET cannot prove keyboard selection, hydration, a streamed
// invocation, or a model answer. Delegate to the actual browser and preserve
// its nonzero exit and assertions instead of checking static HTML snippets.
func runPersonaBrowser(t *testing.T, name string) {
	t.Helper()
	if os.Getenv("HCMNEXT_DEV_URL") == "" {
		t.Skip("real persona browser acceptance is unverified: set HCMNEXT_DEV_URL to a served app with installed personas and an actual model provider")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository for browser acceptance")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	args := []string{"playwright", "test", "--config=tools/uxqual/browser/playwright.config.mjs", "persona_chat_runtime.spec.mjs", "--grep", name}
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(t.Context(), "cmd", append([]string{"/c", "npx"}, args...)...)
	} else {
		command = exec.CommandContext(t.Context(), "npx", args...)
	}
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real persona browser acceptance failed: %v\n%s", err, output)
	}
	t.Logf("%s", output)
}
