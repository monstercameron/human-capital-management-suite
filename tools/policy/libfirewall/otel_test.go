package libfirewall_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
)

// TestOTelBackendQualification is the LIB-007 primary test. OpenTelemetry
// is confined to the owned adapters admitted by OBS-002. REV-017-01 deleted
// internal/operations/telemetry as a dead duplicate of the live evaluator.
func TestOTelBackendQualification(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)

	cases := []struct {
		name     string
		importer string
		imported string
		wantV    bool
	}{
		{"a domain package importing OTel trace", roles.Module + "/internal/domains/people", cfg.OTelModulePrefix + "otel/trace", true},
		{"an engine importing OTel metric", roles.Module + "/internal/engines/payband", cfg.OTelModulePrefix + "otel/metric", true},
		{"workflow importing OTel", roles.Module + "/internal/workflow/runtime", cfg.OTelModulePrefix + "otel", true},
		{"capability importing OTel", roles.Module + "/internal/capability", cfg.OTelModulePrefix + "otel/sdk/trace", true},
		{"platform adapter importing OTel", roles.Module + "/internal/platform/telemetry/otel", cfg.OTelModulePrefix + "otel/sdk/trace", false},
		{"transport middleware importing trace", roles.Module + "/internal/transport/otelmw", cfg.OTelModulePrefix + "otel/trace", false},
		{"provider adapter importing trace", roles.Module + "/internal/connectivity/providertelemetry", cfg.OTelModulePrefix + "otel/trace", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := libfirewall.CheckImport(roles, tc.importer, tc.imported)
			if tc.wantV && v == nil {
				t.Errorf("CheckImport(%q, %q) = nil, want a violation outside the owned OTel adapters", tc.importer, tc.imported)
			}
			if !tc.wantV && v != nil {
				t.Errorf("CheckImport(%q, %q) = %+v, want no violation", tc.importer, tc.imported, v)
			}
		})
	}
}

// TestTodo_LIB_007_Golden pins the OBS-002 family boundary.
func TestTodo_LIB_007_Golden(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	class := roles.Classify("go.opentelemetry.io/otel")
	if !class.Found {
		t.Fatalf("dependency-roles.yaml no longer classifies go.opentelemetry.io/*")
	}
	want := map[string]bool{
		"internal/platform/telemetry/otel":        true,
		"internal/transport/otelmw":               true,
		"internal/connectivity/providertelemetry": true,
	}
	if len(class.Row.AllowedImportRoots) != len(want) {
		t.Fatalf("OTel family roots = %v, want %v", class.Row.AllowedImportRoots, want)
	}
	for _, root := range class.Row.AllowedImportRoots {
		if !want[root] {
			t.Fatalf("unexpected OTel family root %q", root)
		}
	}
}

// TestTodo_LIB_007_Integration runs the check against the real import graph
// and reports every real direct OpenTelemetry import found in HEAD.
func TestTodo_LIB_007_Integration(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var total int
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if len(imp) < len(cfg.OTelModulePrefix) || imp[:len(cfg.OTelModulePrefix)] != cfg.OTelModulePrefix {
				continue
			}
			if v := libfirewall.CheckImport(roles, pkg.ImportPath, imp); v != nil {
				total++
				t.Errorf("LIB-007 OpenTelemetry direct-import violation: %s imports %s outside an owned adapter", v.Importer, v.ImportedPath)
			}
		}
	}
	t.Logf("scanned %d packages, %d direct OpenTelemetry imports found", len(pkgs), total)
}

// TestTodo_LIB_007_Race runs CheckImport concurrently for OTel edges.
func TestTodo_LIB_007_Race(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	done := make(chan struct{}, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_ = libfirewall.CheckImport(roles, roles.Module+"/internal/domains/people", "go.opentelemetry.io/otel")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}

// TestTodo_LIB_007_Conformance checks a representative OTel submodule
// (trace, metric, sdk) all resolve to the same family classification and
// are forbidden from domain code even though owned adapters are admitted.
func TestTodo_LIB_007_Conformance(t *testing.T) {
	_, roles := loadFirewallConfigAndRoles(t)
	submodules := []string{
		"go.opentelemetry.io/otel",
		"go.opentelemetry.io/otel/trace",
		"go.opentelemetry.io/otel/metric",
		"go.opentelemetry.io/otel/sdk/trace",
		"go.opentelemetry.io/otel/exporters/otlp/otlptrace",
	}
	for _, sub := range submodules {
		if v := libfirewall.CheckImport(roles, roles.Module+"/internal/domains/people", sub); v == nil {
			t.Errorf("%s was not flagged as forbidden", sub)
		}
	}
}

// TestTodo_LIB_007_Security checks the real package graph for direct OTel
// imports outside the three roots admitted by the dependency manifest.
func TestTodo_LIB_007_Security(t *testing.T) {
	cfg, roles := loadFirewallConfigAndRoles(t)
	pkgs, err := repopath.ListPackages(repopath.RootDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if len(imp) >= len(cfg.OTelModulePrefix) && imp[:len(cfg.OTelModulePrefix)] == cfg.OTelModulePrefix {
				if v := libfirewall.CheckImport(roles, pkg.ImportPath, imp); v != nil {
					t.Errorf("OpenTelemetry import outside admitted boundary: %s", v.Importer)
				}
			}
		}
	}
	for _, root := range []string{"internal/domains/people", "internal/engines/payband", "internal/capability"} {
		if v := libfirewall.CheckImport(roles, roles.Module+"/"+root, "go.opentelemetry.io/otel/trace"); v == nil {
			t.Errorf("sensitive root %s was admitted", root)
		}
	}
}
