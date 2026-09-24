package archrules_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func TestTodo_ARCH_GO_005_Golden(t *testing.T) {
	path := filepath.Join(repopath.RootDir(), "definitions", "architecture", "architecture-rules.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%X", sha256.Sum256(content))
	const want = "8F2BD24D1CF578AA8F43D0664BDBB228B704D6E2ACEFAEC91868C2C4EA37899A"
	if got != want {
		t.Fatalf("architecture-rules.yaml SHA256 = %s, want %s", got, want)
	}
}

func TestTodo_ARCH_GO_005_Mutation(t *testing.T) {
	cfg := loadArchConfig(t)
	markers := append([]string(nil), cfg.IntentCapability.CapabilityForbiddenContent...)
	path := "internal/capability/discovery/cache"
	if archrules.MatchesAnyGlob(markers, path) {
		t.Fatalf("baseline markers unexpectedly forbid %q", path)
	}
	mutated := append(markers, "internal/capability/*/cache")
	if !archrules.MatchesAnyGlob(mutated, path) {
		t.Fatalf("adding cache marker did not forbid %q", path)
	}
	if archrules.MatchesAnyGlob(mutated, "internal/domains/discovery/cache") {
		t.Fatal("capability marker escaped its root")
	}
}
