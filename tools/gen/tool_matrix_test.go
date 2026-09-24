package gen

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestTodo_TOOL_002_Golden pins the committed generator-lock bytes. A change
// to either tool version or the lock's serialization must be reviewed.
func TestTodo_TOOL_002_Golden(t *testing.T) {
	root := findRepoRoot(t)
	got, err := os.ReadFile(filepath.Join(root, "gen", "TOOLS.lock"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "{\n  \"buf_version\": \"1.72.0\",\n  \"protoc_gen_go_version\": \"v1.36.12\",\n  \"protoc_gen_go_grpc_version\": \"v1.6.2\"\n}\n"
	if string(got) != want {
		t.Fatalf("TOOLS.lock bytes differ from pinned generator toolchain:\n%s", got)
	}
}

// TestTodo_TOOL_003_Golden pins the SHA-256 digest of the reproducible
// generated Go tree, including relative paths and file bytes.
func TestTodo_TOOL_003_Golden(t *testing.T) {
	root := findRepoRoot(t)
	out := t.TempDir()
	runBuf(t, root, "generate", "--template", "buf.gen.yaml", "-o", out)
	got := digestTree(t, filepath.Join(out, "gen", "go"))
	const want = "54a5fecfb1e4460ddb81f80f8ef53b6d869711ed8a33fbd6a1cdaf7eb4b3b4e3"
	if got != want {
		t.Fatalf("generated tree digest = %s, want %s", got, want)
	}
}

// TestTodo_TOOL_010_Golden pins a buf-owned generated artifact so the drift
// check cannot be weakened to a file-set-only comparison.
func TestTodo_TOOL_010_Golden(t *testing.T) {
	root := findRepoRoot(t)
	path := filepath.Join(root, "gen", "go", "hcmnext", "intents", "v1", "business_intent.pb.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "878bab444045efbd5ba43702bf99e45c27315a7b1412e48174f52d7768c459d1"
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		t.Fatalf("generated business_intent.pb.go digest = %x, want %s", got, want)
	}
}

// FuzzTodo_TOOL_010 verifies that arbitrary generated-file bytes affect the
// drift fingerprint exactly when their contents differ.
func FuzzTodo_TOOL_010(f *testing.F) {
	f.Add("generated source", "generated source")
	f.Add("old schema output", "new schema output")
	f.Add("", "\x00\xff")
	f.Fuzz(func(t *testing.T, left, right string) {
		leftDir, rightDir := t.TempDir(), t.TempDir()
		if err := os.WriteFile(filepath.Join(leftDir, "generated.go"), []byte(left), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rightDir, "generated.go"), []byte(right), 0o600); err != nil {
			t.Fatal(err)
		}
		leftDigest, rightDigest := digestTree(t, leftDir), digestTree(t, rightDir)
		if (leftDigest == rightDigest) != (left == right) {
			t.Fatalf("tree fingerprints did not track content equality: left=%q right=%q", left, right)
		}
	})
}

func digestTree(t *testing.T, root string) string {
	t.Helper()
	files := listTreeFiles(t, root)
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
