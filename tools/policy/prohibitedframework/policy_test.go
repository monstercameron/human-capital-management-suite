package prohibitedframework

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func TestToolchainWasmBridgeAdmitsOnlyExactGeneratedShim(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "wasm_exec.js")
	if err := os.WriteFile(path, []byte("customer rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isToolchainWasmBridge(path, "internal/humanwork/workspace/assets/wasm_exec.js") {
		t.Fatal("arbitrary script at generated path was admitted")
	}
	if isToolchainWasmBridge(path, "internal/humanwork/workspace/assets/other.js") {
		t.Fatal("arbitrary script path was admitted")
	}
}

func testPolicy() Policy {
	return Policy{
		Version:      1,
		Module:       "github.com/monstercameron/human-capital-management-suite",
		PolicyDate:   "2026-09-03",
		RuntimeRoots: []string{"cmd", "internal", "gen/go", "migrations"},
		SemanticRoots: []string{
			"internal/capability", "internal/domains", "internal/governance",
			"internal/intent", "internal/kernel", "internal/workflow",
		},
		Rules: []Rule{
			{ID: "orm", Category: "ORM", Disposition: DispositionProhibited, ImportPrefixes: []string{"gorm.io/gorm", "entgo.io/ent"}},
			{ID: "workflow", Category: "WORKFLOW_ENGINE", Disposition: DispositionProhibited, ImportPrefixes: []string{"go.temporal.io/sdk", "github.com/camunda/camunda-platform"}},
			{ID: "rules", Category: "CUSTOMER_RULE_RUNTIME", Disposition: DispositionProhibited, ImportPrefixes: []string{"go.starlark.net", "github.com/yuin/gopher-lua"}},
			{ID: "broker", Category: "PHASE1_BROKER", Disposition: DispositionPhaseOneProhibited, ImportPrefixes: []string{"github.com/segmentio/kafka-go", "github.com/IBM/sarama"}},
			{ID: "provider", Category: "PROVIDER_SDK", Disposition: DispositionAdapterOnly, ImportPrefixes: []string{"github.com/aws/aws-sdk-go-v2", "github.com/Azure/azure-sdk-for-go"}, AllowedRoots: []string{"internal/connectivity/adapters", "tools"}},
		},
	}
}

// TestProhibitedFrameworkPolicy is the primary LIB-013 proof. Each seeded
// framework captures semantics or Phase 1 correctness unless confined to an
// explicitly replaceable mechanical adapter.
func TestProhibitedFrameworkPolicy(t *testing.T) {
	p := testPolicy()
	cases := []struct {
		name string
		pkg  Package
		code string
	}{
		{"ORM owns persistence", Package{ImportPath: p.Module + "/internal/data/store", Imports: []string{"gorm.io/gorm"}}, CodeSemanticFramework},
		{"workflow engine owns lifecycle", Package{ImportPath: p.Module + "/internal/workflow", Imports: []string{"go.temporal.io/sdk/workflow"}}, CodeSemanticFramework},
		{"customer rule interpreter", Package{ImportPath: p.Module + "/internal/engines/rules", Imports: []string{"go.starlark.net/starlark"}}, CodeSemanticFramework},
		{"mandatory Phase 1 broker", Package{ImportPath: p.Module + "/internal/data/outbox", Imports: []string{"github.com/segmentio/kafka-go"}}, CodePhaseOneInfrastructure},
		{"provider SDK outside adapter", Package{ImportPath: p.Module + "/internal/domains/people", Imports: []string{"github.com/aws/aws-sdk-go-v2/service/s3"}}, CodeProviderBoundary},
		{"provider type exposed by domain", Package{ImportPath: p.Module + "/internal/domains/people", ExportedTypeImports: []string{"github.com/aws/aws-sdk-go-v2/service/s3/types"}}, CodeProviderTypeExposure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckPackage(p, tc.pkg)
			if !hasCode(got, tc.code) {
				t.Fatalf("CheckPackage() = %#v, want code %s", got, tc.code)
			}
		})
	}

	clean := CheckPackage(p, Package{
		ImportPath: p.Module + "/internal/connectivity/adapters/acme",
		Imports:    []string{"github.com/aws/aws-sdk-go-v2/service/s3"},
	})
	if len(clean) != 0 {
		t.Fatalf("replaceable provider adapter rejected: %#v", clean)
	}
}

func TestTodo_LIB_013_Property(t *testing.T) {
	p := testPolicy()
	for _, prefix := range []string{"gorm.io/gorm", "entgo.io/ent", "go.temporal.io/sdk", "go.starlark.net", "github.com/segmentio/kafka-go"} {
		for _, suffix := range []string{"", "/subpkg", "/deep/package"} {
			got := CheckPackage(p, Package{ImportPath: p.Module + "/internal/domains/people", Imports: []string{prefix + suffix}})
			if len(got) == 0 {
				t.Errorf("module family %q escaped policy", prefix+suffix)
			}
		}
		lookalike := prefix + "-lookalike"
		if got := CheckPackage(p, Package{ImportPath: p.Module + "/internal/domains/people", Imports: []string{lookalike}}); len(got) != 0 {
			t.Errorf("prefix lookalike %q was classified: %#v", lookalike, got)
		}
	}
}

func TestTodo_LIB_013_Golden(t *testing.T) {
	p := testPolicy()
	var got []string
	for _, pkg := range []Package{
		{ImportPath: p.Module + "/internal/workflow", Imports: []string{"go.temporal.io/sdk/workflow"}},
		{ImportPath: p.Module + "/internal/data/outbox", Imports: []string{"github.com/segmentio/kafka-go"}},
		{ImportPath: p.Module + "/internal/domains/people", ExportedTypeImports: []string{"github.com/aws/aws-sdk-go-v2/service/s3/types"}},
	} {
		for _, violation := range CheckPackage(p, pkg) {
			got = append(got, violation.String())
		}
	}
	sort.Strings(got)
	want := []string{
		"internal/data/outbox: PHASE1_INFRASTRUCTURE_PROHIBITED broker github.com/segmentio/kafka-go (broker)",
		"internal/domains/people: PROVIDER_TYPE_EXPOSURE provider type github.com/aws/aws-sdk-go-v2/service/s3/types (provider)",
		"internal/workflow: PROHIBITED_SEMANTIC_FRAMEWORK workflow engine go.temporal.io/sdk/workflow (workflow)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("golden mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestTodo_LIB_013_Integration(t *testing.T) {
	root := repopath.RootDir()
	p, err := Load(filepath.Join(root, "definitions", "architecture", "prohibited-frameworks.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	violations, err := ScanRepository(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		for _, violation := range violations {
			t.Errorf("%s", violation.String())
		}
	}
}

func TestTodo_LIB_013_Conformance(t *testing.T) {
	p := testPolicy()
	vectors := []struct {
		category    string
		importPath  string
		wantCode    string
		allowedRoot string
	}{
		{"ORM", "gorm.io/gorm", CodeSemanticFramework, ""},
		{"WORKFLOW_ENGINE", "go.temporal.io/sdk/client", CodeSemanticFramework, ""},
		{"CUSTOMER_RULE_RUNTIME", "github.com/yuin/gopher-lua", CodeSemanticFramework, ""},
		{"PHASE1_BROKER", "github.com/IBM/sarama", CodePhaseOneInfrastructure, ""},
		{"PROVIDER_SDK", "github.com/Azure/azure-sdk-for-go/sdk/storage", CodeProviderBoundary, "internal/connectivity/adapters/azure"},
	}
	for _, vector := range vectors {
		t.Run(vector.category, func(t *testing.T) {
			blocked := CheckPackage(p, Package{ImportPath: p.Module + "/internal/domains/people", Imports: []string{vector.importPath}})
			if !hasCode(blocked, vector.wantCode) {
				t.Fatalf("blocked vector = %#v, want %s", blocked, vector.wantCode)
			}
			if vector.allowedRoot != "" {
				allowed := CheckPackage(p, Package{ImportPath: p.Module + "/" + vector.allowedRoot, Imports: []string{vector.importPath}})
				if len(allowed) != 0 {
					t.Fatalf("allowed adapter vector rejected: %#v", allowed)
				}
			}
		})
	}

	for _, path := range []string{"internal/rules/eval.py", "cmd/server/plugin.js", "gen/go/hook.lua", "migrations/backfill.star"} {
		if violations := CheckRuntimeFile(p, path); !hasCode(violations, CodeRuntimeScript) {
			t.Errorf("runtime script %q escaped: %#v", path, violations)
		}
	}
	if violations := CheckRuntimeFile(p, "tools/dev/generate.py"); len(violations) != 0 {
		t.Fatalf("developer tool script rejected: %#v", violations)
	}
}

func TestTodo_LIB_013_Mutation(t *testing.T) {
	baseline := testPolicy()
	mutants := []struct {
		name   string
		mutate func(*Policy)
	}{
		{"delete ORM rule", func(p *Policy) { p.Rules = p.Rules[1:] }},
		{"weaken workflow disposition", func(p *Policy) {
			p.Rules[1].Disposition = DispositionAdapterOnly
			p.Rules[1].AllowedRoots = []string{"internal/workflow"}
		}},
		{"allow broker in data", func(p *Policy) { p.Rules[3].AllowedRoots = []string{"internal/data"} }},
		{"broaden provider adapter to internal", func(p *Policy) { p.Rules[4].AllowedRoots = []string{"internal"} }},
	}
	for _, mutant := range mutants {
		t.Run(mutant.name, func(t *testing.T) {
			p := clonePolicy(baseline)
			mutant.mutate(&p)
			if violations := Validate(p); len(violations) == 0 {
				t.Fatal("mutated policy passed validation")
			}
		})
	}
}

func clonePolicy(p Policy) Policy {
	clone := p
	clone.RuntimeRoots = append([]string(nil), p.RuntimeRoots...)
	clone.SemanticRoots = append([]string(nil), p.SemanticRoots...)
	clone.Rules = append([]Rule(nil), p.Rules...)
	for i := range clone.Rules {
		clone.Rules[i].ImportPrefixes = append([]string(nil), p.Rules[i].ImportPrefixes...)
		clone.Rules[i].AllowedRoots = append([]string(nil), p.Rules[i].AllowedRoots...)
	}
	return clone
}

func hasCode(violations []Violation, code string) bool {
	for _, violation := range violations {
		if strings.EqualFold(violation.Code, code) {
			return true
		}
	}
	return false
}
