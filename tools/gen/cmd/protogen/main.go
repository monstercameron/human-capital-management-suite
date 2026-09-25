// Command protogen runs the repository's pinned Protobuf generation.
// Run it from the repository root with `go run ./tools/gen/cmd/protogen`.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type toolLock struct {
	BufVersion                string            `json:"buf_version"`
	BufChecksumsSource        string            `json:"buf_checksums_source"`
	BufBinarySHA256ByPlatform map[string]string `json:"buf_binary_sha256_by_platform"`
}

type commandRunner func(context.Context, string, io.Writer, io.Writer, string, ...string) ([]byte, error)
type executableLocator func(string) (string, error)

func main() {
	if err := generate(context.Background(), ".", os.Stdout, os.Stderr, execRunner, resolveBufExecutable); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// resolveBufExecutable accepts an explicit BUF_BINARY, then the ignored local
// .artifacts/tools/buf copy, then PATH. generate verifies every candidate
// against the platform checksum before invoking it.
func resolveBufExecutable(name string) (string, error) {
	if configured := os.Getenv("BUF_BINARY"); configured != "" {
		return configured, nil
	}
	if name != "buf" {
		return exec.LookPath(name)
	}
	filename := "buf"
	if runtime.GOOS == "windows" {
		filename += ".exe"
	}
	artifactCopy := filepath.Join(".artifacts", "tools", "buf", filename)
	if _, err := os.Stat(artifactCopy); err == nil {
		return artifactCopy, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect local Buf artifact %s: %w", artifactCopy, err)
	}
	return exec.LookPath(name)
}

func generate(ctx context.Context, root string, stdout, stderr io.Writer, run commandRunner, locate executableLocator) error {
	lockPath := filepath.Join(root, "gen", "TOOLS.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return fmt.Errorf("read generator tool lock: %w", err)
	}
	var lock toolLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return fmt.Errorf("parse generator tool lock: %w", err)
	}
	if lock.BufVersion == "" {
		return fmt.Errorf("generator tool lock has no buf_version")
	}
	bufPath, err := locate("buf")
	if err != nil {
		return fmt.Errorf("locate pinned buf executable: %w", err)
	}
	if err := verifyBufBinary(bufPath, lock.BufBinarySHA256ByPlatform, runtime.GOOS, runtime.GOARCH); err != nil {
		return err
	}
	version, err := run(ctx, root, stdout, stderr, bufPath, "--version")
	if err != nil {
		return fmt.Errorf("check pinned buf %s: %w", lock.BufVersion, err)
	}
	if got := strings.TrimSpace(string(version)); got != lock.BufVersion {
		return fmt.Errorf("buf version %q does not match gen/TOOLS.lock pin %q", got, lock.BufVersion)
	}
	artifactRoot := filepath.Join(root, ".artifacts", "lanes", "tool002-protogen-tmp")
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		return fmt.Errorf("create generator temporary directory: %w", err)
	}
	tempDir, err := os.MkdirTemp(artifactRoot, "protogen-")
	if err != nil {
		return fmt.Errorf("create generator temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	if _, err := run(ctx, root, stdout, stderr, bufPath, "generate", "--template", filepath.Join(root, "buf.gen.yaml"), "-o", tempDir); err != nil {
		return fmt.Errorf("buf generate: %w", err)
	}
	return publishGeneratedTree(filepath.Join(tempDir, "gen", "go"), filepath.Join(root, "gen", "go"))
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

func execRunner(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = stderr
	err := cmd.Run()
	if stdout != nil {
		_, _ = stdout.Write(output.Bytes())
	}
	return output.Bytes(), err
}

func publishGeneratedTree(stagedRoot, targetRoot string) error {
	var stagedFiles []string
	if err := filepath.WalkDir(stagedRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(stagedRoot, path)
		if err != nil {
			return err
		}
		stagedFiles = append(stagedFiles, rel)
		return nil
	}); err != nil {
		return fmt.Errorf("walk staged generated files: %w", err)
	}
	staged := make(map[string]bool, len(stagedFiles))
	for _, rel := range stagedFiles {
		staged[rel] = true
	}
	if err := filepath.WalkDir(targetRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(targetRoot, path)
		if err != nil {
			return err
		}
		if staged[rel] || !isProtobufGenerated(path) {
			return nil
		}
		return os.Remove(path)
	}); err != nil {
		return fmt.Errorf("remove stale Protobuf output: %w", err)
	}
	for _, rel := range stagedFiles {
		source := filepath.Join(stagedRoot, rel)
		target := filepath.Join(targetRoot, rel)
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read staged output %s: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create output directory for %s: %w", rel, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("write generated output %s: %w", rel, err)
		}
	}
	return nil
}

func isProtobufGenerated(path string) bool {
	if !strings.HasSuffix(path, ".pb.go") {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var header [256]byte
	n, _ := file.Read(header[:])
	return strings.Contains(string(header[:n]), "Code generated by protoc-gen-go") || strings.Contains(string(header[:n]), "Code generated by protoc-gen-go-grpc")
}
