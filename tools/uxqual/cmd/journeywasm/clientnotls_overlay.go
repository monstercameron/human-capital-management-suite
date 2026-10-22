//go:build !(js && wasm)

package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// The browser client linked about 1.2 MB of TLS: crypto/tls and crypto/x509
// with their curves, key exchange, ASN.1 and certificate parsing. None of it can
// run in a page. Every request goes through the browser's fetch, the WebSocket
// tunnel is opened by the browser, and neither gives the page a socket to put
// TLS on. The code stayed because net/http, gRPC's credentials and
// golang.org/x/net/http2 name crypto/tls types and functions, and the linker
// cannot prove that no caller reaches the functions that use them.
//
// The client build therefore compiles two stand-in packages in place of
// crypto/tls and crypto/x509 (clientnotls/*.gostub). They carry only the names
// those importers mention; every call that would start a handshake, dial or
// listen panics with a message naming the call.
//
// Like the fetch-only net/http build (chatperf_fetch_only.go) this is a build
// time overlay over the toolchain's own files, never a change to the
// repository's sources, and it applies only to the toolchain it was written for:
// the toolchain version and a digest of every file the overlay replaces must
// match what is pinned below. Any other toolchain gets the ordinary build and a
// line saying so, and the size gate decides whether that build ships.

// errClientNoTLSUnavailable reports a toolchain the stand-in packages were not
// written for.
var errClientNoTLSUnavailable = errors.New("crypto/tls and crypto/x509 in this toolchain do not match the stand-ins for the client build")

//go:embed clientnotls/tls.gostub clientnotls/x509.gostub
var clientNoTLSSources embed.FS

// clientNoTLSToolchain is the toolchain version the stand-ins were checked
// against, and clientNoTLSDigests pins what that toolchain's packages held.
const clientNoTLSToolchain = "go1.26.3"

var clientNoTLSDigests = map[string]string{
	"crypto/tls":  "ea5436a5feffa0f46beb19cc714dc70238e01d7d22b0fb0d5e798f39c71082e8",
	"crypto/x509": "94437898cc9d0c177375787a1dae3e0ddee43ed826020e75243905ddf4a97038",
}

// clientNoTLSStandIns maps each replaced package to its stand-in source.
var clientNoTLSStandIns = map[string]string{
	"crypto/tls":  "clientnotls/tls.gostub",
	"crypto/x509": "clientnotls/x509.gostub",
}

// clientNoTLSStandInFile is the name the stand-in takes inside the package's
// directory. Every other source file of the package is removed from the build.
const clientNoTLSStandInFile = "zz_hcmnext_standin.go"

// clientNoTLSPackageDigest identifies a toolchain package by the bytes the
// overlay replaces: the name and content of every non-test source file, with
// line endings normalised so a CRLF checkout holds the same digest.
func clientNoTLSPackageDigest(dir string) (string, []string, error) {
	files, err := clientNoTLSPackageFiles(dir)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.New()
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", nil, err
		}
		body = []byte(strings.ReplaceAll(string(body), "\r\n", "\n"))
		fmt.Fprintf(sum, "%s\x00%d\x00", name, len(body))
		sum.Write(body)
	}
	return hex.EncodeToString(sum.Sum(nil)), files, nil
}

// clientNoTLSPackageFiles lists the build inputs of a toolchain package that the
// overlay removes, sorted.
func clientNoTLSPackageFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, "_test.go") {
			continue
		}
		switch filepath.Ext(name) {
		case ".go", ".s", ".h", ".c":
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

// clientNoTLSReplacements checks the toolchain and returns the overlay entries
// that swap crypto/tls and crypto/x509 for their stand-ins. The stand-in files
// are written into dir.
func clientNoTLSReplacements(goBin, dir string) (map[string]string, error) {
	versionOut, err := exec.Command(goBin, "env", "GOVERSION").Output()
	if err != nil {
		return nil, fmt.Errorf("asking %s for GOVERSION: %w", goBin, err)
	}
	root, err := goRoot(goBin)
	if err != nil {
		return nil, err
	}
	return clientNoTLSPlan(strings.TrimSpace(string(versionOut)), root, dir, clientNoTLSDigests)
}

// clientNoTLSPlan is clientNoTLSReplacements for a toolchain already asked its
// version and root, against the digests it is to be checked with.
func clientNoTLSPlan(version, root, dir string, digests map[string]string) (map[string]string, error) {
	if version != clientNoTLSToolchain {
		return nil, fmt.Errorf("%w: toolchain is %s, stand-ins were written for %s", errClientNoTLSUnavailable, version, clientNoTLSToolchain)
	}
	replace := map[string]string{}
	for _, pkg := range []string{"crypto/tls", "crypto/x509"} {
		pkgDir := filepath.Join(root, "src", filepath.FromSlash(pkg))
		digest, files, err := clientNoTLSPackageDigest(pkgDir)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errClientNoTLSUnavailable, err)
		}
		if want := digests[pkg]; digest != want {
			return nil, fmt.Errorf("%w: %s digest is %s, want %s", errClientNoTLSUnavailable, pkg, digest, want)
		}
		source, err := clientNoTLSSources.ReadFile(clientNoTLSStandIns[pkg])
		if err != nil {
			return nil, err
		}
		target := filepath.Join(dir, strings.ReplaceAll(pkg, "/", "_")+"_standin.go")
		if err := os.WriteFile(target, source, 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", target, err)
		}
		for _, name := range files {
			// An empty target removes the file from the build.
			replace[filepath.Join(pkgDir, name)] = ""
		}
		replace[filepath.Join(pkgDir, clientNoTLSStandInFile)] = target
	}
	return replace, nil
}

// mergeOverlay adds replace to the overlay file at path, creating it when it does
// not exist, and returns the path. The go command takes a single -overlay.
func mergeOverlay(path string, replace map[string]string) error {
	merged := goOverlay{Replace: map[string]string{}}
	if body, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(body, &merged); err != nil {
			return fmt.Errorf("reading overlay %s: %w", path, err)
		}
		if merged.Replace == nil {
			merged.Replace = map[string]string{}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for from, to := range replace {
		merged.Replace[from] = to
	}
	body, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("encode overlay: %w", err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}
