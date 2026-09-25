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
	const want = "{\n  \"buf_version\": \"1.72.0\",\n  \"buf_checksums_source\": \"https://github.com/bufbuild/buf/releases/download/v1.72.0/sha256.txt\",\n  \"buf_binary_sha256_by_platform\": {\n    \"darwin/amd64\": \"eb815a2708d4a43d31799049d5a2987ea81d0a9e98b53976d47bd1e78d154a8f\",\n    \"darwin/arm64\": \"5176f23a6118b9978de1340c3e3301a4ed0d48e16a669510be44b4c355170d57\",\n    \"freebsd/amd64\": \"611fab61e2ec8feab3ea54a0b821a76aaab28775361c493e958e7f1d208cd227\",\n    \"freebsd/arm64\": \"1c34b2a46dc6b1e16e8ab5326ad49f320a3c1338ae96d637bb94855bff3614f1\",\n    \"linux/amd64\": \"8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af\",\n    \"linux/arm\": \"9c3383b274211d77c33e596bc906d02547b6fa0a9323d63aa8498cce590fcdd4\",\n    \"linux/arm64\": \"bdbb275fb9624104ef4d8513d269cc410a153138646e67136cb3f8cc185be289\",\n    \"linux/ppc64le\": \"019520499e3270a485b425f1d445b817eb0b853a5e6a098ca759d506793deb75\",\n    \"linux/riscv64\": \"f444176f943b65d420460ac4d77ebbb8e145022cf67e0d19049109ea847224ef\",\n    \"linux/s390x\": \"33e31891250000c2276c803f4777384251b79be058a9b847ebdbb7fc61d4690d\",\n    \"openbsd/amd64\": \"b6b2fa5dc5873084403dfa790bd1bf48394e4e69b93166bf210e2ff73c7a8577\",\n    \"openbsd/arm64\": \"68641dacd5174322df3c556e89d48db2e98909410f6be79c50c2f9e28a3109dd\",\n    \"windows/amd64\": \"6e8f6d043e520bc81cae7b85d4cd6d93e57716a8a9842d5d18200191ee259cb5\",\n    \"windows/arm64\": \"cc06910c1b69715b598fc8d1958538c86b656c05f6dd0a516dfa90c325dcbead\"\n  },\n  \"protoc_gen_go_version\": \"v1.36.12\",\n  \"protoc_gen_go_module_sum\": \"h1:pJOKDDOyeXErUroCihFAd5LQuwXBSpVnKGrj5o/fwxc=\",\n  \"protoc_gen_go_grpc_version\": \"v1.6.2\",\n  \"protoc_gen_go_grpc_module_sum\": \"h1:rgSNvqscFZ1JgV/4wH5GOsZFSFkR2Eua9As3KIr2LlM=\"\n}\n"
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
