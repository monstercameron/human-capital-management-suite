package productcorrelation_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_ALIGN_053_ServedPath pins the reachability audit closure: the
// served IntentService construction path must run this package's serving
// contract, keeping productcorrelation inside the shipped-command
// dependency closure (go list -deps ./cmd/hcmnext) instead of an orphan
// proof. NewIntentService runs validateConformanceServingContracts, and
// that table must keep the product-correlation entry.
func TestTodo_ALIGN_053_ServedPath(t *testing.T) {
	t.Run("detector rejects source without the registration", func(t *testing.T) {
		fixture := "package app\nfunc validateConformanceServingContracts() error {\n\tchecks := []struct {\n\t\tname string\n\t\tfn   func() error\n\t}{\n\t\t{\"other\", other.ValidateServingContract},\n\t}\n\t_ = checks\n\treturn nil\n}\n"
		if servedRegistrationPresent(fixture) {
			t.Fatalf("detector accepted source with no product-correlation registration")
		}
	})
	t.Run("served IntentService construction registers product-correlation", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(servedRepoRoot(t), "internal", "intent", "app", "service.go"))
		if err != nil {
			t.Fatalf("read internal/intent/app/service.go: %v", err)
		}
		src := string(raw)
		if !strings.Contains(src, "func NewIntentService") || !strings.Contains(src, "validateConformanceServingContracts(); err != nil") {
			t.Fatalf("NewIntentService no longer runs validateConformanceServingContracts")
		}
		if !servedRegistrationPresent(src) {
			t.Fatalf("service.go no longer registers productcorrelation.ValidateServingContract; ALIGN-053 is off the served path again")
		}
	})
}

// servedRegistrationPresent reports whether Go source registers this
// package's serving contract in the conformance serving table.
func servedRegistrationPresent(src string) bool {
	return strings.Contains(src, `"product-correlation"`) &&
		strings.Contains(src, "productcorrelation.ValidateServingContract")
}

// servedRepoRoot returns the repository root by walking up from the test
// working directory until go.mod is found.
func servedRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root above working directory")
		}
		dir = parent
	}
}
