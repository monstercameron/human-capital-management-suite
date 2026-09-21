package garbagedrawer

import (
	"strings"
	"testing"
)

// TestNoGarbageDrawerPackages is ARCH-GO-017's primary RED/GREEN test. The
// fixtures cover every prohibited naming family and the positive semantic
// owner cases which must remain available to a real architecture.
func TestNoGarbageDrawerPackages(t *testing.T) {
	cases := []struct {
		name  string
		pkg   Package
		kind  string
		clean bool
	}{
		{name: "services", pkg: Package{ImportPath: "example.com/app/internal/services", Name: "services"}, kind: "garbage_drawer_name"},
		{name: "utils", pkg: Package{ImportPath: "example.com/app/internal/utils", Name: "utils"}, kind: "garbage_drawer_name"},
		{name: "helpers", pkg: Package{ImportPath: "example.com/app/internal/helpers", Name: "helpers"}, kind: "garbage_drawer_name"},
		{name: "common", pkg: Package{ImportPath: "example.com/app/internal/common", Name: "common"}, kind: "garbage_drawer_name"},
		{name: "managers", pkg: Package{ImportPath: "example.com/app/internal/managers", Name: "managers"}, kind: "garbage_drawer_name"},
		{name: "central models", pkg: Package{ImportPath: "example.com/app/internal/models", Name: "models"}, kind: "central_models_or_repositories"},
		{name: "central repositories", pkg: Package{ImportPath: "example.com/app/internal/repositories", Name: "repositories"}, kind: "central_models_or_repositories"},
		{name: "impl", pkg: Package{ImportPath: "example.com/app/internal/people/payimpl", Name: "payimpl"}, kind: "impl_suffix"},
		{name: "catch all api", pkg: Package{ImportPath: "example.com/app/internal/foo", Name: "foo", ExportedSymbols: []Symbol{{Name: "New", Kind: "func"}, {Name: "Get", Kind: "func"}}}, kind: "catch_all_exported_api"},
		{name: "domain owner", pkg: Package{ImportPath: "example.com/app/internal/domains/people", Name: "people", ExportedSymbols: []Symbol{{Name: "New", Kind: "func"}, {Name: "Get", Kind: "func"}}, PackageDoc: "Package people owns the employee domain."}, clean: true},
		{name: "local model owner", pkg: Package{ImportPath: "example.com/app/internal/intent/model", Name: "model"}, clean: true},
		{name: "generated common", pkg: Package{ImportPath: "example.com/app/gen/go/common/v1", Name: "commonv1", Generated: true}, clean: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckPackage(tc.pkg)
			if tc.clean {
				if len(got) != 0 {
					t.Fatalf("CheckPackage() = %v, want clean", got)
				}
				return
			}
			found := false
			for _, v := range got {
				if v.Kind == tc.kind {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("CheckPackage() = %v, want kind %q", got, tc.kind)
			}
		})
	}
}

// TestTodo_ARCH_GO_017_Golden pins the diagnostic's stable rendering for one
// representative garbage-drawer package.
func TestTodo_ARCH_GO_017_Golden(t *testing.T) {
	got := CheckPackage(Package{ImportPath: "github.com/monstercameron/human-capital-management-suite/internal/utils", Name: "utils"})
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %v", len(got), got)
	}
	const want = "github.com/monstercameron/human-capital-management-suite/internal/utils: package path segment \"utils\" is a garbage-drawer name without a single semantic owner (garbage_drawer_name)"
	if got[0].String() != want {
		t.Fatalf("String() = %q, want %q", got[0].String(), want)
	}
}

// TestTodo_ARCH_GO_017_Integration proves the checked-in real package tree
// currently has no unowned package names or catch-all API drawers.
func TestTodo_ARCH_GO_017_Integration(t *testing.T) {
	findings, err := Scan("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		for _, v := range findings {
			t.Errorf("ARCH-GO-017 violation: %s", v)
		}
	}
}

// TestTodo_ARCH_GO_017_Conformance checks deterministic ordering, active and
// expired exception behavior, and the requirement for complete exception
// review metadata.
func TestTodo_ARCH_GO_017_Conformance(t *testing.T) {
	pkgs := []Package{
		{ImportPath: "example.com/app/internal/utils", Name: "utils"},
		{ImportPath: "example.com/app/internal/services", Name: "services"},
	}
	policy := Policy{PolicyDate: "2026-09-03", Exceptions: []Exception{{ImportPath: "example.com/app/internal/utils", Kind: "garbage_drawer_name", Owner: "platform", Rationale: "migration", ReplacementPlan: "move to owner", Expiry: "2026-10-01"}}}
	got := CheckPackages(pkgs, policy)
	if len(got) != 1 || !strings.Contains(got[0].ImportPath, "/services") {
		t.Fatalf("active exception result = %v, want services only", got)
	}
	policy.Exceptions[0].Expiry = "2026-09-02"
	if got = CheckPackages(pkgs, policy); len(got) != 2 {
		t.Fatalf("expired exception result = %v, want both findings", got)
	}
	policy.Exceptions[0].Expiry = "2026-10-01"
	policy.Exceptions[0].ReplacementPlan = ""
	if got = CheckPackages(pkgs, policy); len(got) != 2 {
		t.Fatalf("incomplete exception result = %v, want both findings", got)
	}
}
