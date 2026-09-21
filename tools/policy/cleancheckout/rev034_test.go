// RED for REV-034-01: the clean-checkout and migration-rehearsal verifiers
// must be required gate steps in the CI workflow, not library-only tools.
package cleancheckout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_REV_034_01 pins the CI wiring: the root-module workflow must
// invoke both the clean-checkout verifier and the migration-rehearsal
// command on every push/PR so a broken tracked tree or migration manifest
// fails the build instead of merging silently.
func TestTodo_REV_034_01(t *testing.T) {
	workflow := filepath.Join("..", "..", "..", ".github", "workflows", "tests.yml")
	data, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	body := string(data)
	for _, step := range []string{
		"go run ./tools/policy/cleancheckout/cmd/cleancheckout",
		"go run ./tools/policy/migrationci/cmd/migrationci",
	} {
		if !strings.Contains(body, step) {
			t.Errorf("CI workflow %s does not run %q: the verifier is library-only", workflow, step)
		}
	}
}
