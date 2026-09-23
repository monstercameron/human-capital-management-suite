package libfirewall_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
)

// reconciledRow finds the exact dependency-roles.yaml row for modulePath
// and requires it to carry allowed roots plus a recorded rationale
// (REV-101-05 GREEN: each row is widened with a recorded rationale).
func reconciledRow(t *testing.T, modulePath string) []string {
	t.Helper()
	m := loadRoleManifest(t)
	for _, row := range m.Modules {
		if row.Path != modulePath {
			continue
		}
		if len(row.AllowedImportRoots) == 0 {
			t.Fatalf("%s row has no allowed_import_roots", modulePath)
		}
		for field, value := range map[string]string{
			"exposure":             row.Exposure,
			"replacement_strategy": row.ReplacementStrategy,
			"semantic_owner":       row.SemanticOwner,
			"upgrade_sla":          row.UpgradeSLA,
		} {
			if strings.TrimSpace(value) == "" {
				t.Fatalf("%s row has no recorded %s", modulePath, field)
			}
		}
		return row.AllowedImportRoots
	}
	t.Fatalf("dependency-roles.yaml has no exact row for %s", modulePath)
	return nil
}

// TestTodo_REV_101_05 is the REV-101-05 PRIMARY test: the RED gaps are
// closed (every RED-named import edge is admitted by a row that records
// its rationale, and the firewall reports zero violations over the real
// import graph) and the firewall is wired into the pre-commit and CI
// gates.
func TestTodo_REV_101_05(t *testing.T) {
	m := loadRoleManifest(t)
	mod := m.Module

	t.Run("uuid row reconciled", func(t *testing.T) {
		reconciledRow(t, "github.com/google/uuid")
		for _, importer := range []string{
			mod + "/internal/application",
			mod + "/internal/domains/promotion",
			mod + "/internal/domains/leave",
			mod + "/internal/platform/execution",
			mod + "/internal/transport/cell",
		} {
			if v := libfirewall.CheckImport(m, importer, "github.com/google/uuid"); v != nil {
				t.Errorf("CheckImport(%q, uuid) = %+v, want no violation", importer, v)
			}
		}
	})

	t.Run("x/text row reconciled", func(t *testing.T) {
		reconciledRow(t, "golang.org/x/text")
		for _, importer := range []string{
			mod + "/internal/engines/wire/canonical",
			mod + "/internal/kernel/values",
			mod + "/internal/intent",
		} {
			if v := libfirewall.CheckImport(m, importer, "golang.org/x/text/unicode/norm"); v != nil {
				t.Errorf("CheckImport(%q, x/text) = %+v, want no violation", importer, v)
			}
		}
	})

	t.Run("otel rows reconciled", func(t *testing.T) {
		reconciledRow(t, "go.opentelemetry.io/otel/trace")
		for _, tc := range []struct{ importer, imported string }{
			{mod + "/internal/platform/telemetry/otel", "go.opentelemetry.io/otel/attribute"},
			{mod + "/internal/platform/telemetry/otel", "go.opentelemetry.io/otel/codes"},
			{mod + "/internal/platform/telemetry/otel", "go.opentelemetry.io/otel/trace"},
			{mod + "/internal/transport/otelmw", "go.opentelemetry.io/otel/trace"},
			{mod + "/internal/connectivity/providertelemetry", "go.opentelemetry.io/otel/attribute"},
			{mod + "/internal/connectivity/providertelemetry", "go.opentelemetry.io/otel/codes"},
			{mod + "/internal/connectivity/providertelemetry", "go.opentelemetry.io/otel/trace"},
		} {
			if v := libfirewall.CheckImport(m, tc.importer, tc.imported); v != nil {
				t.Errorf("CheckImport(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
			}
		}
	})

	t.Run("new chat importers reconciled", func(t *testing.T) {
		for _, tc := range []struct{ importer, imported string }{
			{mod + "/internal/collaboration/chat", "github.com/google/uuid"},
			{mod + "/internal/collaboration/chatroutingadapter", "github.com/google/uuid"},
			{mod + "/internal/humanwork/chatui", "github.com/monstercameron/GoWebComponents/v5/html"},
			{mod + "/internal/humanwork/chatui", "github.com/monstercameron/GoWebComponents/v5/ui"},
			{mod + "/schema/proto/gen/go/hcmnext/chat/v1", "google.golang.org/grpc"},
			{mod + "/schema/proto/gen/go/hcmnext/chat/v1", "google.golang.org/protobuf/reflect/protoreflect"},
		} {
			if v := libfirewall.CheckImport(m, tc.importer, tc.imported); v != nil {
				t.Errorf("CheckImport(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
			}
		}
	})

	t.Run("firewall runs in pre-commit and CI", func(t *testing.T) {
		root := repopath.RootDir()
		read := func(rel string) string {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatalf("reading %s: %v", rel, err)
			}
			return string(data)
		}
		pkg := read("package.json")
		if !strings.Contains(pkg, `"check:libfirewall"`) {
			t.Errorf("package.json has no check:libfirewall script")
		}
		if !strings.Contains(pkg, "check:libfirewall") || !strings.Contains(pkg, `"test:all"`) {
			t.Errorf("package.json test:all chain does not run check:libfirewall")
		}
		hook := read(".husky/pre-commit")
		if !strings.Contains(hook, "test:all") {
			t.Errorf(".husky/pre-commit does not run npm run test:all")
		}
		ci := read(".github/workflows/tests.yml")
		if !strings.Contains(ci, "libfirewall") {
			t.Errorf(".github/workflows/tests.yml has no libfirewall gate step")
		}
	})

	t.Run("real import graph reconciled", func(t *testing.T) {
		report, err := libfirewall.Evaluate(repopath.RootDir())
		if err != nil {
			t.Fatalf("evaluating firewall: %v", err)
		}
		if report.Packages == 0 {
			t.Fatalf("firewall scanned no packages")
		}
		for _, v := range report.Violations {
			t.Errorf("third-party firewall violation: %s imports %s (role %s), allowed roots: %v",
				v.Importer, v.ImportedPath, v.Role, v.AllowedRoots)
		}
		t.Logf("scanned %d packages, %d firewall violations", report.Packages, len(report.Violations))
	})
}

// TestTodo_REV_101_05_Golden pins the reconciled allowed_import_roots for
// every RED-named module family, so a silent row widening or narrowing is
// a visible diff rather than a quietly different firewall.
func TestTodo_REV_101_05_Golden(t *testing.T) {
	m := loadRoleManifest(t)

	byPath := map[string][]string{}
	for _, row := range m.Modules {
		byPath[row.Path] = row.AllowedImportRoots
	}
	var familyOTel []string
	for _, fam := range m.FamilyRules {
		if fam.Prefix == "go.opentelemetry.io/" {
			familyOTel = fam.AllowedImportRoots
		}
	}

	want := map[string][]string{
		"github.com/google/uuid": {
			"internal/kernel",
			"internal/intent",
			"internal/ledger",
			"internal/data",
			"internal/connectivity",
			"internal/transaction",
			"internal/humanwork",
			"internal/workflow",
			"internal/operations/explorer",
			"internal/resource",
			"internal/operations/reconcile",
			"internal/engines/wire/digest",
			"internal/application",
			"internal/domains/leave",
			"internal/domains/promotion",
			"internal/platform/devclock",
			"internal/platform/execution",
			"internal/transport",
			"internal/collaboration/chat",
			"internal/collaboration/chatroutingadapter",
			"tools/uxqual/journeyclient",
			"cmd",
			"test",
		},
		"golang.org/x/text": {
			"internal/engines/wire/canonical",
			"internal/kernel/values",
			"internal/intent",
			"internal/domains/people",
			"internal/experience/i18n",
			"internal/humanwork/productui",
			"internal/i18n",
			"tools/uxqual/forms",
		},
		"go.opentelemetry.io/otel/trace": {
			"internal/platform/telemetry/otel",
			"internal/transport/otelmw",
			"internal/connectivity/providertelemetry",
			"internal/intent/app",
		},
		"family:go.opentelemetry.io/": {
			"internal/platform/telemetry/otel",
			"internal/transport/otelmw",
			"internal/connectivity/providertelemetry",
		},
	}

	got := map[string][]string{
		"github.com/google/uuid":         byPath["github.com/google/uuid"],
		"golang.org/x/text":              byPath["golang.org/x/text"],
		"go.opentelemetry.io/otel/trace": byPath["go.opentelemetry.io/otel/trace"],
		"family:go.opentelemetry.io/":    familyOTel,
	}
	for path, roots := range want {
		if strings.Join(got[path], "\n") != strings.Join(roots, "\n") {
			t.Errorf("allowed_import_roots for %s = %v, want %v", path, got[path], roots)
		}
	}
}
