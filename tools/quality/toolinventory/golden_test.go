package toolinventory_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/toolinventory"
)

// TestTodo_TOOL_025_Golden pins the exact set of tool names and
// categories Generate must produce - the inventory's shape - without
// pinning any specific third-party version string, which would make this
// test fail on every legitimate dependency bump. It also pins which
// entries must carry a digest: every entry whose identity is a
// "name@version" pin (go.mod tool directives, embedded-postgres,
// gen/TOOLS.lock's buf version, package.json's node/npm engine ranges) or
// a hashed file (a .husky hook) always has one; npm-derived entries only
// have one when package-lock.json records an integrity hash for them
// (root-level direct devDependencies in this lockfile do not - see
// loadPackageLockEntries's comment).
func TestTodo_TOOL_025_Golden(t *testing.T) {
	root := repoRoot(t)
	m, err := toolinventory.Generate(root)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	wantCategories := map[string]string{
		"buf":                "protobuf_toolchain",
		"protoc-gen-go":      "protobuf_toolchain",
		"protoc-gen-go-grpc": "protobuf_toolchain",
		"staticcheck":        "go_static_analysis",
		"embedded-postgres":  "database_fixture",
		"node":               "node_runtime",
		"npm":                "node_runtime",
		"prettier":           "node_toolchain",
		"eslint":             "node_toolchain",
		"vitest":             "node_toolchain",
		"husky":              "node_toolchain",
		"lint-staged":        "node_toolchain",
		"husky:pre-commit":   "git_hook",
	}
	wantDigested := map[string]bool{
		"protoc-gen-go": true, "protoc-gen-go-grpc": true, "staticcheck": true,
		"embedded-postgres": true, "node": true, "npm": true, "husky:pre-commit": true,
	}

	got := make(map[string]toolinventory.Entry, len(m.Tools))
	for _, e := range m.Tools {
		if _, dup := got[e.Name]; dup {
			t.Fatalf("tool name %q reported more than once", e.Name)
		}
		got[e.Name] = e
	}

	if len(got) != len(wantCategories) {
		t.Errorf("generated %d tools, want exactly %d: got=%v", len(got), len(wantCategories), toolNames(m))
	}
	for name, wantCategory := range wantCategories {
		e, ok := got[name]
		if !ok {
			t.Errorf("missing expected tool %q", name)
			continue
		}
		if e.Category != wantCategory {
			t.Errorf("%s: category = %q, want %q", name, e.Category, wantCategory)
		}
		if wantDigested[name] && e.Digest == "" {
			t.Errorf("%s: expected a digest (derived from a locally hashable file), got none", name)
		}
		if name == "buf" {
			if e.Digest != "" || e.PlatformDigests["windows/amd64"] != "6e8f6d043e520bc81cae7b85d4cd6d93e57716a8a9842d5d18200191ee259cb5" || e.PlatformDigests["windows/arm64"] != "cc06910c1b69715b598fc8d1958538c86b656c05f6dd0a516dfa90c325dcbead" || e.PlatformDigests["linux/amd64"] != "8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af" {
				t.Errorf("buf platform digests are not the locked release binary hashes: %v", e.PlatformDigests)
			}
		}
		if (name == "protoc-gen-go" || name == "protoc-gen-go-grpc") && !strings.HasPrefix(e.Digest, "h1:") {
			t.Errorf("%s digest = %q, want authentic Go module h1 checksum", name, e.Digest)
		}
	}
	for name := range got {
		if _, expected := wantCategories[name]; !expected {
			t.Errorf("unexpected tool %q not in the golden name set", name)
		}
	}
}
