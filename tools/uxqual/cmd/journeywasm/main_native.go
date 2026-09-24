//go:build !(js && wasm)

// This build of the command is its own build tool.
//
// The journey page's client only exists as a wasm module, and the shell that
// loads it names the command that produces one:
//
//	go run ./tools/uxqual/cmd/journeywasm -out internal/humanwork/workspace/assets
//
// That sentence is printed in the served document when the bundle is
// missing, so it has to be a command that works rather than a note pointing
// at a two-line shell recipe. It builds main_wasm.go with GOOS=js
// GOARCH=wasm, copies the matching wasm_exec.js out of the toolchain that
// built it, and prints what it wrote.
//
// Both halves come from one toolchain on purpose: wasm_exec.js is the Go
// runtime's own JavaScript shim and is versioned with the compiler, so a
// shim copied from a different Go than the one that produced the module is a
// page that fails at instantiation with an error nobody can read.
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// wasmPackage is what gets built. It is the module-absolute import path
// rather than "./tools/..." so the command works from any directory inside
// the module, including the package's own directory under `go test`.
const wasmPackage = "github.com/monstercameron/human-capital-management-suite/tools/uxqual/cmd/journeywasm"

// Output file names. They are the names internal/humanwork/workspace's
// embedded asset directory serves (assets.go's assetJourneyWasm and
// assetWasmExec), and the shell's script tags point at both.
const (
	wasmFile     = "journey.wasm"
	wasmExecFile = "wasm_exec.js"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its process boundary handed in, so the whole command is
// testable.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("journeywasm", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "directory to write "+wasmFile+" and "+wasmExecFile+" into (required)")
	root := flags.String("root", ".", "module root used to create an embed overlay")
	overlay := flags.String("overlay", "", "optional Go build overlay mapping workspace embed inputs to -out")
	flags.Usage = func() {
		fmt.Fprintf(stderr, "usage: go run ./tools/uxqual/cmd/journeywasm -out <dir>\n\n"+
			"Builds the Promotion journey page's wasm client and the matching Go\n"+
			"wasm_exec.js shim into <dir>.\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*out) == "" {
		fmt.Fprintln(stderr, "journeywasm: -out is required")
		flags.Usage()
		return 2
	}
	if err := build(*out, stdout); err != nil {
		fmt.Fprintf(stderr, "journeywasm: %v\n", err)
		return 1
	}
	if strings.TrimSpace(*overlay) != "" {
		if err := writeGoOverlay(*root, *out, *overlay); err != nil {
			fmt.Fprintf(stderr, "journeywasm: %v\n", err)
			return 1
		}
	}
	return 0
}

type goOverlay struct {
	Replace map[string]string `json:"Replace"`
}

// writeGoOverlay makes Go embed the build outputs directly from -out without
// copying build products into the checked-in source asset directory.
func writeGoOverlay(root, outDir, overlayPath string) error {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve module root: %w", err)
	}
	outputPath, err := filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	assetsPath := filepath.Join(rootPath, "internal", "humanwork", "workspace", "assets")
	replace := make(map[string]string, 5)
	for _, name := range []string{wasmFile, wasmFile + ".gz", wasmExecFile, wasmExecFile + ".gz", workspace.AssetIntegrityManifestName} {
		virtual := filepath.Join(assetsPath, name)
		built := filepath.Join(outputPath, name)
		if _, err := os.Stat(built); err != nil {
			return fmt.Errorf("overlay input %s: %w", built, err)
		}
		replace[virtual] = built
	}
	body, err := json.MarshalIndent(goOverlay{Replace: replace}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Go overlay: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(overlayPath), 0o755); err != nil {
		return fmt.Errorf("create overlay directory: %w", err)
	}
	if err := os.WriteFile(overlayPath, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write Go overlay: %w", err)
	}
	return nil
}

// build writes both halves of the bundle into outDir.
func build(outDir string, stdout io.Writer) error {
	goBin, err := goBinary()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	wasmPath := filepath.Join(outDir, wasmFile)
	// Browser bundles do not need Go symbol/debug tables. Stripping them here
	// reduces both the transferred module and the work WebAssembly performs
	// while decoding it, without changing runtime behavior or stack safety.
	cmd := exec.Command(goBin, "build", "-ldflags=-s -w", "-o", wasmPath, wasmPackage)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	// The build's own diagnostics are the useful part of a failure, so they
	// are carried into the error rather than discarded.
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			return fmt.Errorf("building %s for js/wasm: %w\n%s", wasmPackage, err, output)
		}
		return fmt.Errorf("building %s for js/wasm: %w", wasmPackage, err)
	}

	shimSource, err := wasmExecSource(goBin)
	if err != nil {
		return err
	}
	shimPath := filepath.Join(outDir, wasmExecFile)
	if err := copyFile(shimSource, shimPath); err != nil {
		return fmt.Errorf("copying %s: %w", wasmExecFile, err)
	}
	for _, path := range []string{wasmPath, shimPath} {
		if err := writeGzip(path); err != nil {
			return fmt.Errorf("compressing %s: %w", filepath.Base(path), err)
		}
	}

	for _, path := range []string{wasmPath, wasmPath + ".gz", shimPath, shimPath + ".gz"} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return fmt.Errorf("stat %s: %w", path, statErr)
		}
		fmt.Fprintf(stdout, "%s  %s\n", path, humanSize(info.Size()))
	}
	if err := writeAssetIntegrityManifest(outDir); err != nil {
		return err
	}
	return nil
}

// writeAssetIntegrityManifest packages a manifest beside the generated
// bundles. It reads the output directory after all copies/compression finish,
// so the manifest describes exactly the bytes the workspace will embed.
func writeAssetIntegrityManifest(outDir string) error {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return fmt.Errorf("reading asset output: %w", err)
	}
	identities := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".gz") {
			identities[entry.Name()] = struct{}{}
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".gz") {
			continue
		}
		identity := strings.TrimSuffix(entry.Name(), ".gz")
		if _, ok := identities[identity]; !ok {
			return fmt.Errorf("orphaned compressed asset %q has no identity file", entry.Name())
		}
	}
	sources := make([]workspace.AssetIntegritySource, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == ".keep" || name == workspace.AssetIntegrityManifestName || strings.HasSuffix(name, ".gz") {
			continue
		}
		contentType, routable := workspace.FrontendAssetContentType(name)
		if !routable {
			continue
		}
		body, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			return fmt.Errorf("reading asset %q: %w", name, err)
		}
		var gzipBody []byte
		if compressed, readErr := os.ReadFile(filepath.Join(outDir, name+".gz")); readErr == nil {
			gzipBody = compressed
		} else if !os.IsNotExist(readErr) {
			return fmt.Errorf("reading compressed asset %q: %w", name, readErr)
		}
		sources = append(sources, workspace.AssetIntegritySource{
			Name: name, Body: body, ContentType: contentType, GzipBody: gzipBody,
		})
	}
	manifest, err := workspace.GenerateAssetIntegrityManifest(sources)
	if err != nil {
		return fmt.Errorf("generate asset integrity manifest: %w", err)
	}
	body, err := manifest.CanonicalJSON()
	if err != nil {
		return fmt.Errorf("encode asset integrity manifest: %w", err)
	}
	destination := filepath.Join(outDir, workspace.AssetIntegrityManifestName)
	temporary, err := os.CreateTemp(outDir, ".asset-manifest-*")
	if err != nil {
		return fmt.Errorf("create temporary asset manifest: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary asset manifest: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary asset manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary asset manifest: %w", err)
	}
	// Windows does not offer replacement through os.Rename. Removing the old
	// file creates a brief publication gap, but handler construction validates
	// this artifact against every embedded byte and therefore fails closed if
	// a build observes an interrupted packaging run.
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace asset manifest: %w", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return fmt.Errorf("publish asset manifest: %w", err)
	}
	return nil
}

// writeGzip creates the precompressed transfer representation during the
// build instead of spending CPU and delaying the first browser request.
func writeGzip(source string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.Create(source + ".gz")
	if err != nil {
		return err
	}
	writer, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		_ = output.Close()
		return err
	}
	// Pin every gzip header field that can carry host/build time metadata so
	// identical identity bytes produce byte-identical transfer artifacts.
	writer.Header.ModTime = time.Unix(0, 0)
	writer.Header.Name = ""
	writer.Header.Comment = ""
	writer.Header.OS = 255
	_, copyErr := io.Copy(writer, input)
	closeWriterErr := writer.Close()
	closeOutputErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeWriterErr != nil {
		return closeWriterErr
	}
	return closeOutputErr
}

// goBinary finds the toolchain to build with.
//
// It is the go on PATH: the one the person typing `go run` is using, and
// therefore the one whose wasm_exec.js matches the module this build
// produces. runtime.GOROOT is deliberately not consulted -- it is deprecated
// precisely because it describes the machine a binary was built on rather
// than the one it is running on, and `go env GOROOT` (below) asks the
// toolchain itself, which is always right.
func goBinary() (string, error) {
	path, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("no go toolchain found on PATH: %w", err)
	}
	return path, nil
}

// wasmExecSource locates the shim inside the toolchain that just built the
// module. Go 1.24 moved it from misc/wasm to lib/wasm; both are looked for
// so this command is not pinned to one toolchain layout.
func wasmExecSource(goBin string) (string, error) {
	root, err := goRoot(goBin)
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(root, "lib", "wasm", wasmExecFile),
		filepath.Join(root, "misc", "wasm", wasmExecFile),
	}
	for _, candidate := range candidates {
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found in %s (looked in lib/wasm and misc/wasm)", wasmExecFile, root)
}

// goRoot asks the toolchain where it lives, rather than assuming this
// process and the build share one.
func goRoot(goBin string) (string, error) {
	output, err := exec.Command(goBin, "env", "GOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("asking %s for GOROOT: %w", goBin, err)
	}
	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", fmt.Errorf("%s reported an empty GOROOT", goBin)
	}
	return root, nil
}

// copyFile writes source to destination, replacing whatever was there.
func copyFile(source, destination string) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, body, 0o644)
}

// humanSize renders a byte count the way a person reads a bundle size, with
// the exact count kept alongside it: "12.4 MB (13029312 bytes)". The exact
// figure is what a size regression is noticed by.
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return strconv.FormatInt(size, 10) + " bytes"
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB"}
	chosen := units[0]
	for _, name := range units {
		value /= unit
		chosen = name
		if value < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s (%d bytes)", value, chosen, size)
}
