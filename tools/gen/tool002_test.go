package gen

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// pinnedModuleVersionRe matches a go.mod require line's version token, e.g.
// "v1.36.12" or "v1.6.2". A bare "latest", an empty string, or anything not
// starting with a concrete "vMAJOR.MINOR.PATCH" is treated as floating.
var pinnedModuleVersionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// requireLineRe matches one go.mod require-block line:
// "\t<module path> <version> [// indirect]".
var requireLineRe = regexp.MustCompile(`(?m)^[ \t]*(\S+)[ \t]+(\S+)[ \t]*(?://.*)?$`)

// remotePluginRe detects a buf.gen.yaml plugin entry that uses a remote
// (BSR-hosted) plugin instead of a go.mod-pinned local one.
var remotePluginRe = regexp.MustCompile(`(?m)^\s*-?\s*remote:\s*\S+`)

var toolBlockRe = regexp.MustCompile(`(?ms)^tool\s*\(\s*(.*?)^\)`)

// generatorPins is the set of facts TestGeneratorLockRejectsFloatingVersion
// extracts from go.mod and buf.gen.yaml and records in gen/TOOLS.lock.
type generatorPins struct {
	BufVersion                string            `json:"buf_version"`
	BufChecksumsSource        string            `json:"buf_checksums_source"`
	BufBinarySHA256ByPlatform map[string]string `json:"buf_binary_sha256_by_platform"`
	ProtocGenGoVersion        string            `json:"protoc_gen_go_version"`
	ProtocGenGoModuleSum      string            `json:"protoc_gen_go_module_sum"`
	ProtocGenGoGrpcVersion    string            `json:"protoc_gen_go_grpc_version"`
	ProtocGenGoGrpcModuleSum  string            `json:"protoc_gen_go_grpc_module_sum"`
}

func moduleSum(goSumText, modulePath, version string) string {
	for _, line := range strings.Split(goSumText, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == modulePath && fields[1] == version && strings.HasPrefix(fields[2], "h1:") {
			return fields[2]
		}
	}
	return ""
}

func verifyBufBinary(path string, checksums map[string]string, goos, goarch string) error {
	platform := goos + "/" + goarch
	want, ok := checksums[platform]
	if !ok || len(want) != sha256.Size*2 {
		return fmt.Errorf("gen/TOOLS.lock has no valid Buf binary SHA-256 for %s", platform)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open Buf binary: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return fmt.Errorf("hash Buf binary: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("Buf binary SHA-256 mismatch for %s: got %s want %s", platform, got, want)
	}
	return nil
}

// extractRequiredVersion returns the pinned version go.mod's require block
// declares for modulePath, or an error if it is missing or not a concrete
// semantic version.
func extractRequiredVersion(goModText, modulePath string) (string, error) {
	for _, m := range requireLineRe.FindAllStringSubmatch(goModText, -1) {
		if m[1] != modulePath {
			continue
		}
		version := m[2]
		if !pinnedModuleVersionRe.MatchString(version) {
			return "", fmt.Errorf("module %s has a non-pinned version %q in go.mod", modulePath, version)
		}
		return version, nil
	}
	return "", fmt.Errorf("module %s has no require entry in go.mod", modulePath)
}

// validateGeneratorPins is the RED/GREEN gate for TOOL-002: it fails closed
// whenever buf.gen.yaml names a remote plugin, or whenever go.mod does not
// pin protoc-gen-go/protoc-gen-go-grpc to an exact version.
func validateGeneratorPins(goModText, goSumText, bufGenYAMLText string) (generatorPins, error) {
	if loc := remotePluginRe.FindString(bufGenYAMLText); loc != "" {
		return generatorPins{}, fmt.Errorf("buf.gen.yaml declares a remote plugin (%q); only go.mod-pinned local plugins are allowed", loc)
	}
	for _, plugin := range []string{"protoc-gen-go", "protoc-gen-go-grpc"} {
		if !strings.Contains(bufGenYAMLText, `local: ["go", "tool", "`+plugin+`"]`) {
			return generatorPins{}, fmt.Errorf("buf.gen.yaml must invoke %s through the pinned go tool directive", plugin)
		}
	}
	toolBlock := toolBlockRe.FindStringSubmatch(goModText)
	if len(toolBlock) != 2 {
		return generatorPins{}, fmt.Errorf("go.mod has no tool directive block for the Protobuf generators")
	}
	for _, tool := range []string{"google.golang.org/protobuf/cmd/protoc-gen-go", "google.golang.org/grpc/cmd/protoc-gen-go-grpc"} {
		if !containsTrimmedLine(toolBlock[1], tool) {
			return generatorPins{}, fmt.Errorf("go.mod has no tool directive for %s", tool)
		}
	}
	goVersion, err := extractRequiredVersion(goModText, "google.golang.org/protobuf")
	if err != nil {
		return generatorPins{}, fmt.Errorf("protoc-gen-go (google.golang.org/protobuf): %w", err)
	}
	grpcVersion, err := extractRequiredVersion(goModText, "google.golang.org/grpc/cmd/protoc-gen-go-grpc")
	if err != nil {
		return generatorPins{}, fmt.Errorf("protoc-gen-go-grpc: %w", err)
	}

	goModuleSum := moduleSum(goSumText, "google.golang.org/protobuf", goVersion)
	if goModuleSum == "" {
		return generatorPins{}, fmt.Errorf("go.sum has no module checksum for google.golang.org/protobuf %s", goVersion)
	}
	grpcModuleSum := moduleSum(goSumText, "google.golang.org/grpc/cmd/protoc-gen-go-grpc", grpcVersion)
	if grpcModuleSum == "" {
		return generatorPins{}, fmt.Errorf("go.sum has no module checksum for google.golang.org/grpc/cmd/protoc-gen-go-grpc %s", grpcVersion)
	}
	return generatorPins{ProtocGenGoVersion: goVersion, ProtocGenGoModuleSum: goModuleSum, ProtocGenGoGrpcVersion: grpcVersion, ProtocGenGoGrpcModuleSum: grpcModuleSum}, nil
}

func containsTrimmedLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// TestGeneratorLockRejectsFloatingVersion is the TOOL-002 primary test. It
// proves validateGeneratorPins accepts this repository's current pins,
// rejects floating plugin declarations, and compares artifact/module
// checksums with their pinned sources without modifying gen/TOOLS.lock.
func TestGeneratorLockRejectsFloatingVersion(t *testing.T) {
	repoRoot := findRepoRoot(t)

	goModText := readFile(t, filepath.Join(repoRoot, "go.mod"))
	bufGenYAMLText := readFile(t, filepath.Join(repoRoot, "buf.gen.yaml"))
	goSumText := readFile(t, filepath.Join(repoRoot, "go.sum"))

	t.Run("CurrentRepoIsPinned", func(t *testing.T) {
		pins, err := validateGeneratorPins(goModText, goSumText, bufGenYAMLText)
		if err != nil {
			t.Fatalf("expected the checked-in generator pins to validate, got: %v", err)
		}
		if pins.ProtocGenGoVersion == "" || pins.ProtocGenGoGrpcVersion == "" {
			t.Fatalf("expected non-empty pinned versions, got %+v", pins)
		}
		lockPath := filepath.Join(repoRoot, "gen", "TOOLS.lock")
		var locked generatorPins
		if err := json.Unmarshal([]byte(readFile(t, lockPath)), &locked); err != nil {
			t.Fatalf("parse %s: %v", lockPath, err)
		}
		if locked.BufVersion == "" || locked.BufVersion != bufVersion(t, repoRoot) {
			t.Fatalf("installed buf version does not match gen/TOOLS.lock: lock=%q installed=%q", locked.BufVersion, bufVersion(t, repoRoot))
		}
		if locked.BufChecksumsSource != "https://github.com/bufbuild/buf/releases/download/v"+locked.BufVersion+"/sha256.txt" {
			t.Fatalf("Buf checksums source is not the official versioned release manifest: %q", locked.BufChecksumsSource)
		}
		if locked.ProtocGenGoModuleSum != pins.ProtocGenGoModuleSum || locked.ProtocGenGoGrpcModuleSum != pins.ProtocGenGoGrpcModuleSum {
			t.Fatalf("generator module checksums do not match go.sum: lock=%+v pins=%+v", locked, pins)
		}
		bufPath, err := exec.LookPath("buf")
		if err != nil {
			t.Fatalf("locate pinned buf: %v", err)
		}
		if err := verifyBufBinary(bufPath, locked.BufBinarySHA256ByPlatform, runtime.GOOS, runtime.GOARCH); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("RejectsRemotePlugin", func(t *testing.T) {
		floating := "version: v2\nplugins:\n  - remote: buf.build/protocolbuffers/go\n    out: gen/go\n"
		if _, err := validateGeneratorPins(goModText, goSumText, floating); err == nil {
			t.Fatal("expected a remote plugin declaration to be rejected as floating, got nil error")
		}
	})

	t.Run("RejectsMissingVersionPin", func(t *testing.T) {
		unpinnedGoMod := "module example.com/x\n\ngo 1.26.3\n\ntool (\n\tgoogle.golang.org/protobuf/cmd/protoc-gen-go\n)\n"
		if _, err := validateGeneratorPins(unpinnedGoMod, goSumText, bufGenYAMLText); err == nil {
			t.Fatal("expected a go.mod with no require entry for protoc-gen-go to be rejected, got nil error")
		}
	})

	t.Run("RejectsNonSemverVersion", func(t *testing.T) {
		floatingGoMod := "module example.com/x\n\ngo 1.26.3\n\nrequire (\n\tgoogle.golang.org/protobuf latest\n\tgoogle.golang.org/grpc/cmd/protoc-gen-go-grpc v1.6.2\n)\n"
		if _, err := validateGeneratorPins(floatingGoMod, goSumText, bufGenYAMLText); err == nil {
			t.Fatal("expected version \"latest\" to be rejected as floating, got nil error")
		}
	})

	t.Run("RejectsSemverSuffix", func(t *testing.T) {
		unpinnedGoMod := "module example.com/x\n\ngo 1.26.3\n\ntool (\n\tgoogle.golang.org/protobuf/cmd/protoc-gen-go\n\tgoogle.golang.org/grpc/cmd/protoc-gen-go-grpc\n)\n\nrequire (\n\tgoogle.golang.org/protobuf v1.36.12-custom\n\tgoogle.golang.org/grpc/cmd/protoc-gen-go-grpc v1.6.2\n)\n"
		if _, err := validateGeneratorPins(unpinnedGoMod, goSumText, bufGenYAMLText); err == nil {
			t.Fatal("expected a version with a semver suffix to be rejected as floating, got nil error")
		}
	})

	t.Run("RejectsUnpinnedPluginCommand", func(t *testing.T) {
		unpinned := strings.Replace(bufGenYAMLText, `local: ["go", "tool", "protoc-gen-go"]`, `local: ["protoc-gen-go"]`, 1)
		if _, err := validateGeneratorPins(goModText, goSumText, unpinned); err == nil {
			t.Fatal("expected a plugin that bypasses the go tool pin to be rejected, got nil error")
		}
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
