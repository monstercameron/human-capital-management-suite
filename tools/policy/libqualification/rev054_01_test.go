package libqualification_test

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"gopkg.in/yaml.v3"
)

type protovalidateDecision struct {
	Todo       string `yaml:"todo"`
	Verdict    string `yaml:"verdict"`
	Decision   string `yaml:"decision"`
	Components struct {
		Buf struct {
			Verdict    string `yaml:"verdict"`
			VersionPin string `yaml:"version_pin"`
			Role       string `yaml:"role"`
		} `yaml:"buf"`
		Protovalidate struct {
			Verdict string `yaml:"verdict"`
			Reason  string `yaml:"reason"`
		} `yaml:"protovalidate"`
	} `yaml:"components"`
	Scope struct {
		Covers   []string `yaml:"covers"`
		Excludes []string `yaml:"excludes"`
	} `yaml:"scope"`
}

func readProtovalidateDecision(t *testing.T) (string, protovalidateDecision) {
	t.Helper()
	root := repopath.RootDir()
	path := filepath.Join(root, "definitions", "architecture", "buf-protovalidate-qualification.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decision protovalidateDecision
	if err := yaml.Unmarshal(data, &decision); err != nil {
		t.Fatalf("decode qualification decision: %v", err)
	}
	return root, decision
}

func readLIB019Block(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "planning", "todos.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "- [x] `LIB-019`")
	if start < 0 {
		start = strings.Index(text, "- [ ] `LIB-019`")
	}
	if start < 0 {
		t.Fatal("LIB-019 todo item is missing")
	}
	rest := text[start:]
	if end := strings.Index(rest[1:], "\n- ["); end >= 0 {
		rest = rest[:end+1]
	}
	return rest
}

// TestTodo_REV_054_01 reconciles LIB-019's open implementation requirement
// with the formal rejection of Protovalidate in the qualification record.
func TestTodo_REV_054_01(t *testing.T) {
	root, decision := readProtovalidateDecision(t)
	block := readLIB019Block(t, root)
	if decision.Todo != "LIB-019" || decision.Verdict != "REJECT" || decision.Components.Protovalidate.Verdict != "REJECT" {
		t.Fatalf("qualification decision does not reject Protovalidate: todo=%q verdict=%q component=%q", decision.Todo, decision.Verdict, decision.Components.Protovalidate.Verdict)
	}
	if !strings.HasPrefix(block, "- [ ]") {
		t.Fatal("LIB-019 must remain open because its original interceptor/parity GREEN is not implemented")
	}
	greenStart := strings.Index(block, "  - **GREEN:**")
	if greenStart < 0 {
		t.Fatal("LIB-019 has no GREEN contract")
	}
	greenEnd := strings.Index(block[greenStart:], "\n")
	if greenEnd < 0 {
		greenEnd = len(block) - greenStart
	}
	green := block[greenStart : greenStart+greenEnd]
	if !strings.Contains(green, "Protovalidate interceptors") || !strings.Contains(green, "identically across transports") {
		t.Fatal("LIB-019's implementation GREEN was narrowed instead of leaving its unmet interceptor/parity requirement open")
	}
	evidence := strings.ToLower(block)
	if !strings.Contains(evidence, "remains open") && !strings.Contains(evidence, "deferred") {
		t.Fatal("open LIB-019 evidence does not say the implementation remains open or deferred")
	}
}

// TestTodo_REV_054_01_Golden pins the decision boundary: Buf remains a pinned
// developer tool, while Protovalidate transport registration is excluded.
func TestTodo_REV_054_01_Golden(t *testing.T) {
	_, decision := readProtovalidateDecision(t)
	projection := struct {
		Todo    string `json:"todo"`
		Verdict string `json:"verdict"`
		Buf     struct {
			Verdict    string `json:"verdict"`
			VersionPin string `json:"version_pin"`
			Role       string `json:"role"`
		} `json:"buf"`
		Protovalidate string   `json:"protovalidate"`
		Excluded      []string `json:"excluded"`
	}{
		Todo:          decision.Todo,
		Verdict:       decision.Verdict,
		Protovalidate: decision.Components.Protovalidate.Verdict,
		Excluded:      decision.Scope.Excludes,
	}
	projection.Buf.Verdict = decision.Components.Buf.Verdict
	projection.Buf.VersionPin = decision.Components.Buf.VersionPin
	projection.Buf.Role = decision.Components.Buf.Role
	got, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"todo":"LIB-019","verdict":"REJECT","buf":{"verdict":"ADOPT","version_pin":"1.72.0","role":"developer_tool"},"protovalidate":"REJECT","excluded":["authorization, legal, eligibility, policy, or business decisions","state mutation or external effects","remote Buf modules or plugins","CEL publication or evaluation","production transport registration or cross-transport parity"]}`
	if string(got) != want {
		t.Fatalf("qualification golden drifted:\n got: %s\nwant: %s", got, want)
	}
}

// TestTodo_REV_054_01_Integration joins the architecture decision to the live
// module and schema inventory, proving the rejected runtime is absent from
// both dependency manifests and checked-in validation annotations.
func TestTodo_REV_054_01_Integration(t *testing.T) {
	root, decision := readProtovalidateDecision(t)
	if decision.Verdict != "REJECT" || decision.Components.Protovalidate.Verdict != "REJECT" {
		t.Fatalf("integration requires explicit REJECT decision, got %q/%q", decision.Verdict, decision.Components.Protovalidate.Verdict)
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(data)), "protovalidate") || strings.Contains(string(data), "buf.build/gen/go/bufbuild/protovalidate") {
			t.Fatalf("%s contains a rejected Protovalidate dependency", name)
		}
	}
	protoRoot := filepath.Join(root, "schema", "proto")
	if err := filepath.WalkDir(protoRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".proto") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "buf.validate") || strings.Contains(string(data), "protovalidate") {
			t.Errorf("schema %s contains annotations for the rejected runtime", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk Protobuf schema tree: %v", err)
	}
}

// TestTodo_REV_054_01_Conformance verifies every production Go package remains
// free of Protovalidate imports and the declared fallback does not claim
// transport parity.
func TestTodo_REV_054_01_Conformance(t *testing.T) {
	root, decision := readProtovalidateDecision(t)
	if decision.Verdict != "REJECT" || decision.Components.Protovalidate.Verdict != "REJECT" {
		t.Fatalf("conformance requires rejected runtime decision, got %q/%q", decision.Verdict, decision.Components.Protovalidate.Verdict)
	}
	if !contains(decision.Scope.Covers, "offline owned stable field violations from local descriptors") ||
		!contains(decision.Scope.Excludes, "production transport registration or cross-transport parity") {
		t.Fatalf("scope does not describe the qualified fallback boundary: covers=%v excludes=%v", decision.Scope.Covers, decision.Scope.Excludes)
	}
	transportRoot := filepath.Join(root, "internal", "transport")
	if err := filepath.WalkDir(transportRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.Contains(imported, "protovalidate") || strings.Contains(imported, "buf.validate") {
				t.Errorf("rejected runtime imported by %s: %s", path, imported)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("scan transport production imports: %v", err)
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
