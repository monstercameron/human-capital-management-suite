package bufprotovalidatekit_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

type qualificationManifest struct {
	Version                         int    `yaml:"version"`
	Todo                            string `yaml:"todo"`
	Verdict                         string `yaml:"verdict"`
	RuntimeDependencyGraphUnchanged bool   `yaml:"runtime_dependency_graph_unchanged"`
	Command                         string `yaml:"command"`
	Components                      struct {
		Buf struct {
			Verdict    string `yaml:"verdict"`
			VersionPin string `yaml:"version_pin"`
			Role       string `yaml:"role"`
		} `yaml:"buf"`
		Protovalidate struct {
			Verdict string `yaml:"verdict"`
			Role    string `yaml:"role"`
		} `yaml:"protovalidate"`
	} `yaml:"components"`
	Boundaries struct {
		AllowedScopes       []string `yaml:"allowed_scopes"`
		ForbiddenScopes     []string `yaml:"forbidden_scopes"`
		DynamicTypePolicy   string   `yaml:"dynamic_type_policy"`
		BusinessOwner       string   `yaml:"business_owner"`
		GeneratedTypesCross bool     `yaml:"generated_types_cross_boundary"`
	} `yaml:"boundaries"`
	Alternative struct {
		Implementation string   `yaml:"implementation"`
		Packages       []string `yaml:"packages"`
	} `yaml:"alternative"`
	Scope struct {
		Covers   []string `yaml:"covers"`
		Excludes []string `yaml:"excludes"`
	} `yaml:"scope"`
	Evidence []struct {
		Test    string `yaml:"test"`
		Package string `yaml:"package"`
	} `yaml:"evidence"`
}

func loadManifest(t *testing.T) qualificationManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "definitions", "architecture", "buf-protovalidate-qualification.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest qualificationManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// TestTodo_LIB_019_Conformance proves the owned offline fallback is
// deterministic and non-mutating, and that qualification does not claim
// unimplemented production transport wiring or Protovalidate admission.
func TestTodo_LIB_019_Conformance(t *testing.T) {
	validator, descriptor := fixtureValidator(t)
	message := requestMessage(descriptor, "", "123456789", "type.googleapis.com/unregistered.Payload", []byte{1})
	before := proto.Clone(message)
	violations := validator.Validate(message)
	if len(violations) == 0 {
		t.Fatal("invalid request unexpectedly passed offline structural validation")
	}
	if again := validator.Validate(message); !reflect.DeepEqual(violations, again) {
		t.Fatalf("offline validation is not deterministic: first=%+v again=%+v", violations, again)
	}
	if !proto.Equal(message, before) {
		t.Fatal("offline validation mutated its protobuf input")
	}

	m := loadManifest(t)
	if m.Version != 1 || m.Todo != "LIB-019" || m.Verdict != "REJECT" {
		t.Fatalf("manifest identity/verdict drift: %+v", m)
	}
	if m.Components.Buf.Verdict != "ADOPT" || m.Components.Buf.VersionPin != "1.72.0" || m.Components.Buf.Role != "developer_tool" {
		t.Fatalf("Buf qualification drift: %+v", m.Components.Buf)
	}
	if m.Components.Protovalidate.Verdict != "REJECT" || m.Components.Protovalidate.Role != "runtime_candidate" {
		t.Fatalf("Protovalidate qualification drift: %+v", m.Components.Protovalidate)
	}
	if !m.RuntimeDependencyGraphUnchanged || m.Command != "go test -count=1 ./tools/quality/bufprotovalidatekit" {
		t.Fatalf("graph/command drift: unchanged=%v command=%q", m.RuntimeDependencyGraphUnchanged, m.Command)
	}
	wantAllowed := []string{"structural", "local"}
	wantForbidden := []string{"business", "authorization", "legal", "eligibility", "mutation"}
	if !reflect.DeepEqual(m.Boundaries.AllowedScopes, wantAllowed) || !reflect.DeepEqual(m.Boundaries.ForbiddenScopes, wantForbidden) {
		t.Fatalf("scope boundary drift: %+v", m.Boundaries)
	}
	if m.Boundaries.DynamicTypePolicy != "explicit_allowlist" || m.Boundaries.BusinessOwner != "SchemaFlux_and_HCM_owned_capabilities" || m.Boundaries.GeneratedTypesCross {
		t.Fatalf("ownership boundary drift: %+v", m.Boundaries)
	}
	if m.Alternative.Implementation != "tools/quality/bufprotovalidatekit" || len(m.Alternative.Packages) < 3 {
		t.Fatalf("fallback is incomplete: %+v", m.Alternative)
	}
	if !slices.Contains(m.Scope.Covers, "offline owned stable field violations from local descriptors") {
		t.Fatalf("qualification does not describe its offline fallback: %+v", m.Scope.Covers)
	}
	if !slices.Contains(m.Scope.Excludes, "production transport registration or cross-transport parity") {
		t.Fatalf("qualification overclaims production transport integration: %+v", m.Scope.Excludes)
	}

	wantEvidence := map[string]bool{
		"TestBufProtovalidateQualificationCannotBecomeBusinessAuthority": false,
		"TestTodo_LIB_019_Property":                                      false,
		"TestTodo_LIB_019_Golden":                                        false,
		"FuzzTodo_LIB_019":                                               false,
		"TestTodo_LIB_019_Security":                                      false,
		"TestTodo_LIB_019_Conformance":                                   false,
	}
	for _, evidence := range m.Evidence {
		if _, ok := wantEvidence[evidence.Test]; !ok {
			t.Errorf("unexpected evidence test %q", evidence.Test)
			continue
		}
		if evidence.Package != "tools/quality/bufprotovalidatekit" {
			t.Errorf("%s package=%q", evidence.Test, evidence.Package)
		}
		wantEvidence[evidence.Test] = true
	}
	for name, found := range wantEvidence {
		if !found {
			t.Errorf("evidence missing %q", name)
		}
	}

	root := repoRoot(t)
	toolLock, err := os.ReadFile(filepath.Join(root, "gen", "TOOLS.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(toolLock), `"buf_version": "1.72.0"`) {
		t.Fatal("Buf version is not pinned in gen/TOOLS.lock")
	}
	bufConfig, err := os.ReadFile(filepath.Join(root, "buf.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bufConfig), "- STANDARD") || strings.Contains(string(bufConfig), "deps:") {
		t.Fatalf("Buf lint is not an offline STANDARD configuration:\n%s", bufConfig)
	}
	bufGenerate, err := os.ReadFile(filepath.Join(root, "buf.gen.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	generateText := string(bufGenerate)
	if !strings.Contains(generateText, "local:") || strings.Contains(generateText, "remote:") || strings.Contains(generateText, "buf.build/") {
		t.Fatalf("Buf generation requires a remote or unpinned plugin:\n%s", bufGenerate)
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "protovalidate") || strings.Contains(lower, "buf.build/gen/go/bufbuild/protovalidate") {
			t.Fatalf("%s admits Protovalidate despite REJECT decision", name)
		}
	}
}
