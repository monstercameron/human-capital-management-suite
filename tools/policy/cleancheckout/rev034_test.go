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
// invoke the clean-checkout verifier and validate the checked-in migration
// manifest against the migration bytes on every push/PR.
func TestTodo_REV_034_01(t *testing.T) {
	workflow := filepath.Join("..", "..", "..", ".github", "workflows", "tests.yml")
	data, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	body := string(data)
	for _, trigger := range []string{
		"  push:\n    branches:\n      - main",
		"  pull_request:",
	} {
		if !strings.Contains(body, trigger) {
			t.Errorf("CI workflow %s does not include required trigger %q", workflow, strings.TrimSpace(trigger))
		}
	}
	for _, step := range []string{
		"go run ./tools/policy/cleancheckout/cmd/cleancheckout -root .",
		"go run ./tools/policy/migrationci/cmd/migrationci -root . -manifest migrations/manifest.json -rehearse -database-url \"$HCMNEXT_TEST_DATABASE_URL\"",
	} {
		if !strings.Contains(body, step) {
			t.Errorf("CI workflow %s does not run %q: the verifier is library-only", workflow, step)
		}
	}
}
