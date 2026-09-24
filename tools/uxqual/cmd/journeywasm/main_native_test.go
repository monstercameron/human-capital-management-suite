//go:build !(js && wasm)

package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunRequiresAnOutputDirectory: the command writes a multi-megabyte
// binary somewhere, so it never guesses where.
func TestRunRequiresAnOutputDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code == 0 {
		t.Fatal("run with no -out succeeded; it must refuse")
	}
	if !strings.Contains(stderr.String(), "-out is required") {
		t.Errorf("stderr = %q, want it to name the missing flag", stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Error("the refusal did not print usage")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing on a refusal", stdout.String())
	}
}

func TestRunRejectsAnUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-into", "somewhere"}, &stdout, &stderr); code == 0 {
		t.Fatal("run accepted an unknown flag")
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunRejectsABlankOutputDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-out", "   "}, &stdout, &stderr); code == 0 {
		t.Fatal("run accepted a blank -out")
	}
}

// TestBuildProducesBothHalvesOfTheBundle is the command's real test: it
// builds the client for js/wasm into a temporary directory and checks that
// what came out is a WebAssembly module and the toolchain's own shim.
//
// It is skipped rather than failed when there is no toolchain to build with,
// because that is an environment fact rather than a defect in this command.
func TestBuildProducesBothHalvesOfTheBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("compiling a wasm module takes tens of seconds; skipped under -short")
	}
	if _, err := goBinary(); err != nil {
		t.Skipf("no go toolchain available to build with: %v", err)
	}

	out := t.TempDir()
	var stdout bytes.Buffer
	if err := build(out, &stdout); err != nil {
		t.Fatalf("build: %v", err)
	}

	wasmPath := filepath.Join(out, wasmFile)
	module, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("reading the built module: %v", err)
	}
	// Every WebAssembly module starts with this preamble; a Go binary built
	// for the host would not, which is the failure this catches.
	if len(module) < 8 || !bytes.HasPrefix(module, []byte("\x00asm")) {
		t.Fatalf("%s does not start with the WebAssembly magic; it was not built for js/wasm", wasmFile)
	}

	shim, err := os.ReadFile(filepath.Join(out, wasmExecFile))
	if err != nil {
		t.Fatalf("reading the shim: %v", err)
	}
	if !bytes.Contains(shim, []byte("globalThis.Go")) {
		t.Errorf("%s does not define globalThis.Go; the shell's loader would find no window.Go", wasmExecFile)
	}

	printed := stdout.String()
	for _, want := range []string{wasmFile, wasmFile + ".gz", wasmExecFile, wasmExecFile + ".gz", "bytes"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the command printed %q, which does not mention %q", printed, want)
		}
	}
}

func TestWriteGoOverlayEmbedsBuiltArtifactsWithoutSourceCopies(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, ".artifacts", "embed")
	assets := filepath.Join(root, "internal", "humanwork", "workspace", "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{wasmFile, wasmFile + ".gz", wasmExecFile, wasmExecFile + ".gz", "manifest.json"} {
		if err := os.WriteFile(filepath.Join(out, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	overlayPath := filepath.Join(root, ".artifacts", "embed", "assets.overlay.json")
	if err := writeGoOverlay(root, out, overlayPath); err != nil {
		t.Fatalf("writeGoOverlay: %v", err)
	}
	body, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	var overlay goOverlay
	if err := json.Unmarshal(body, &overlay); err != nil {
		t.Fatal(err)
	}
	if len(overlay.Replace) != 5 {
		t.Fatalf("overlay replacements = %d, want 5", len(overlay.Replace))
	}
	for _, name := range []string{wasmFile, wasmFile + ".gz", wasmExecFile, wasmExecFile + ".gz", "manifest.json"} {
		virtual := filepath.Join(assets, name)
		if got, want := overlay.Replace[virtual], filepath.Join(out, name); got != want {
			t.Errorf("overlay[%q] = %q, want %q", virtual, got, want)
		}
	}
}

func TestWriteGzipRoundTripsSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.wasm")
	want := bytes.Repeat([]byte("webassembly-transfer-payload"), 256)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeGzip(path); err != nil {
		t.Fatal(err)
	}
	compressed, err := os.Open(path + ".gz")
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("precompressed bundle did not round-trip")
	}
}

func TestWasmExecSourcePrefersLibWasm(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{filepath.Join(root, "lib", "wasm"), filepath.Join(root, "misc", "wasm")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("preparing %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, wasmExecFile), []byte(dir), 0o644); err != nil {
			t.Fatalf("writing a fake shim: %v", err)
		}
	}

	// wasmExecSource asks the toolchain for GOROOT, so it is exercised here
	// through the same lookup with a stand-in root.
	found := ""
	for _, candidate := range []string{
		filepath.Join(root, "lib", "wasm", wasmExecFile),
		filepath.Join(root, "misc", "wasm", wasmExecFile),
	} {
		if _, err := os.Stat(candidate); err == nil {
			found = candidate
			break
		}
	}
	if found != filepath.Join(root, "lib", "wasm", wasmExecFile) {
		t.Fatalf("the lookup order picked %q, want the Go 1.24+ lib/wasm location first", found)
	}
}

// TestWasmExecSourceComesFromTheBuildingToolchain keeps the shim and the
// module versioned together: a mismatch instantiates and then fails inside
// the runtime, which is the hardest kind of failure to read.
func TestWasmExecSourceComesFromTheBuildingToolchain(t *testing.T) {
	goBin, err := goBinary()
	if err != nil {
		t.Skipf("no go toolchain available: %v", err)
	}
	source, err := wasmExecSource(goBin)
	if err != nil {
		t.Fatalf("locating the shim: %v", err)
	}
	root, err := goRoot(goBin)
	if err != nil {
		t.Fatalf("locating GOROOT: %v", err)
	}
	if !strings.HasPrefix(source, root) {
		t.Errorf("the shim at %q does not come from the building toolchain at %q", source, root)
	}
}

func TestCopyFileReplacesWhateverWasThere(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.js")
	destination := filepath.Join(dir, "destination.js")
	if err := os.WriteFile(source, []byte("new"), 0o644); err != nil {
		t.Fatalf("writing the source: %v", err)
	}
	if err := os.WriteFile(destination, []byte("a much longer stale file"), 0o644); err != nil {
		t.Fatalf("writing the stale destination: %v", err)
	}

	if err := copyFile(source, destination); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(body) != "new" {
		t.Errorf("destination = %q, want the source's content with no remnant of the old file", body)
	}
}

func TestCopyFileReportsAMissingSource(t *testing.T) {
	dir := t.TempDir()
	if err := copyFile(filepath.Join(dir, "absent"), filepath.Join(dir, "out")); err == nil {
		t.Fatal("copyFile of a missing source reported success")
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:       "0 bytes",
		512:     "512 bytes",
		2048:    "2.0 KB (2048 bytes)",
		1048576: "1.0 MB (1048576 bytes)",
		9437184: "9.0 MB (9437184 bytes)",
	}
	for size, want := range cases {
		if got := humanSize(size); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestBuildRefusesAnUnwritableOutputDirectory(t *testing.T) {
	// A path whose parent is an existing file cannot be created as a
	// directory on any platform this runs on.
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("preparing: %v", err)
	}
	var stdout bytes.Buffer
	if err := build(filepath.Join(file, "assets"), &stdout); err == nil {
		t.Fatal("build into an impossible directory reported success")
	}
}
