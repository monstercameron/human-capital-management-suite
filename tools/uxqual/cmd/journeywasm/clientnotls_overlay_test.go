//go:build !(js && wasm)

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeToolchain lays out a toolchain whose crypto/tls and crypto/x509 hold the
// given file bodies, and returns its root.
func fakeToolchain(t *testing.T, tlsBody, x509Body string) string {
	t.Helper()
	root := t.TempDir()
	for pkg, body := range map[string]string{"tls": tlsBody, "x509": x509Body} {
		dir := filepath.Join(root, "src", "crypto", pkg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{pkg + ".go": body, pkg + "_test.go": "package " + pkg + "\n", "notes.txt": "ignored"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func digestsOf(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, pkg := range []string{"tls", "x509"} {
		digest, _, err := clientNoTLSPackageDigest(filepath.Join(root, "src", "crypto", pkg))
		if err != nil {
			t.Fatal(err)
		}
		out["crypto/"+pkg] = digest
	}
	return out
}

// The overlay replaces every build input of the two packages, and only those:
// test files and notes stay out of it.
func TestClientNoTLSPlanReplacesEveryBuildInput(t *testing.T) {
	root := fakeToolchain(t, "package tls\n", "package x509\n")
	dir := t.TempDir()
	replace, err := clientNoTLSPlan(clientNoTLSToolchain, root, dir, digestsOf(t, root))
	if err != nil {
		t.Fatal(err)
	}
	tlsDir := filepath.Join(root, "src", "crypto", "tls")
	if target, ok := replace[filepath.Join(tlsDir, "tls.go")]; !ok || target != "" {
		t.Fatalf("tls.go must be removed from the build, got %q (present %v)", target, ok)
	}
	if _, ok := replace[filepath.Join(tlsDir, "tls_test.go")]; ok {
		t.Fatal("a test file is not a build input and must not be in the overlay")
	}
	standIn := replace[filepath.Join(tlsDir, clientNoTLSStandInFile)]
	if !strings.HasSuffix(standIn, "crypto_tls_standin.go") {
		t.Fatalf("the stand-in must be mapped into the package directory, got %q", standIn)
	}
	body, err := os.ReadFile(standIn)
	if err != nil || !bytes.Contains(body, []byte("package tls")) {
		t.Fatalf("the stand-in file is missing or wrong: %v", err)
	}
	if _, ok := replace[filepath.Join(root, "src", "crypto", "x509", clientNoTLSStandInFile)]; !ok {
		t.Fatal("crypto/x509 has no stand-in")
	}
}

// A toolchain the stand-ins were not written for gets the ordinary build: a
// different version, or a package whose files changed by one byte.
func TestClientNoTLSPlanRefusesAnUnknownToolchain(t *testing.T) {
	root := fakeToolchain(t, "package tls\n", "package x509\n")
	digests := digestsOf(t, root)
	if _, err := clientNoTLSPlan("go1.0.0", root, t.TempDir(), digests); !errors.Is(err, errClientNoTLSUnavailable) {
		t.Fatalf("a different toolchain version must be refused, got %v", err)
	}
	changed := fakeToolchain(t, "package tls\n// changed\n", "package x509\n")
	if _, err := clientNoTLSPlan(clientNoTLSToolchain, changed, t.TempDir(), digests); !errors.Is(err, errClientNoTLSUnavailable) {
		t.Fatalf("a changed package must be refused, got %v", err)
	}
	if _, err := clientNoTLSPlan(clientNoTLSToolchain, t.TempDir(), t.TempDir(), digests); !errors.Is(err, errClientNoTLSUnavailable) {
		t.Fatalf("a toolchain without the packages must be refused, got %v", err)
	}
}

// A CRLF checkout of the toolchain holds the same packages.
func TestClientNoTLSDigestIgnoresLineEndings(t *testing.T) {
	lf := fakeToolchain(t, "package tls\n\nvar a = 1\n", "package x509\n")
	crlf := fakeToolchain(t, "package tls\r\n\r\nvar a = 1\r\n", "package x509\n")
	if a, b := digestsOf(t, lf), digestsOf(t, crlf); a["crypto/tls"] != b["crypto/tls"] {
		t.Fatalf("line endings changed the digest: %s vs %s", a["crypto/tls"], b["crypto/tls"])
	}
}

func TestMergeOverlayKeepsWhatWasThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overlay.json")
	if err := mergeOverlay(path, map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	if err := mergeOverlay(path, map[string]string{"c": ""}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got goOverlay
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Replace) != 2 || got.Replace["a"] != "b" || got.Replace["c"] != "" {
		t.Fatalf("merged overlay = %v", got.Replace)
	}
}

// The pinned digests describe the toolchain this repository builds with. When
// it moves on, this test names the new digests so the stand-ins can be checked
// against the new packages and the pins updated in one reviewed change.
func TestClientNoTLSPinsMatchTheToolchain(t *testing.T) {
	goBin, err := goBinary()
	if err != nil {
		t.Skipf("no go toolchain available: %v", err)
	}
	version, err := exec.Command(goBin, "env", "GOVERSION").Output()
	if err != nil {
		t.Skipf("cannot read the toolchain version: %v", err)
	}
	if got := strings.TrimSpace(string(version)); got != clientNoTLSToolchain {
		t.Skipf("toolchain is %s, the stand-ins were checked against %s: the client build runs without them", got, clientNoTLSToolchain)
	}
	if _, err := clientNoTLSReplacements(goBin, t.TempDir()); err != nil {
		t.Fatalf("the pinned digests no longer match this toolchain's crypto/tls and crypto/x509: %v", err)
	}
}

// The stand-ins compile, and every operation that would start a handshake, dial
// or listen panics with a message that names crypto/tls or crypto/x509.
func TestClientNoTLSStandInsPanicWhenCalled(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a small module")
	}
	goBin, err := goBinary()
	if err != nil {
		t.Skipf("no go toolchain available: %v", err)
	}
	module := t.TempDir()
	files := map[string]string{
		"go.mod":               "module standins\n\ngo 1.26\n",
		"tls/tls.go":           "clientnotls/tls.gostub",
		"x509/x509.go":         "clientnotls/x509.gostub",
		"tls/standin_test.go":  "",
		"x509/standin_test.go": "",
	}
	for name, source := range files {
		path := filepath.Join(module, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := []byte(source)
		switch {
		case strings.HasSuffix(source, ".gostub"):
			if body, err = clientNoTLSSources.ReadFile(source); err != nil {
				t.Fatal(err)
			}
		case name == "tls/standin_test.go":
			body = []byte(standInPanicTest)
		case name == "x509/standin_test.go":
			body = []byte(x509StandInPanicTest)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The x509 stand-in is checked on its own; the tls stand-in is checked
	// against the toolchain's crypto/x509, which has every name it uses.
	for _, args := range [][]string{{"test", "-count=1", "./x509/", "./tls/"}} {
		cmd := exec.Command(goBin, args...)
		cmd.Dir = module
		output, err := cmd.CombinedOutput()
		// Windows can refuse to delete the finished test binary; the result
		// line above that message is the verdict, as in the repository's gates.
		cleanupOnly := err != nil && bytes.Contains(output, []byte("unlinkat")) && !bytes.Contains(output, []byte("FAIL"))
		if err != nil && !cleanupOnly {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
}

const standInPanicTest = `package tls

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func mustPanic(t *testing.T, name string, call func()) {
	t.Helper()
	defer func() {
		message, _ := recover().(string)
		if !strings.Contains(message, "crypto/tls is not part of this build") || !strings.Contains(message, name) {
			t.Errorf("%s: panic %q does not name the call", name, message)
		}
	}()
	call()
}

func TestEveryOperationPanics(t *testing.T) {
	conn := &Conn{}
	state := ConnectionState{}
	mustPanic(t, "Dial", func() { Dial("tcp", "example.test:443", nil) })
	mustPanic(t, "DialWithDialer", func() { DialWithDialer(nil, "tcp", "example.test:443", nil) })
	mustPanic(t, "Dialer.Dial", func() { (&Dialer{}).Dial("tcp", "example.test:443") })
	mustPanic(t, "Dialer.DialContext", func() { (&Dialer{}).DialContext(context.Background(), "tcp", "example.test:443") })
	mustPanic(t, "Client", func() { Client(nil, &Config{}) })
	mustPanic(t, "Server", func() { Server(nil, &Config{}) })
	mustPanic(t, "NewListener", func() { NewListener(nil, &Config{}) })
	mustPanic(t, "Listen", func() { Listen("tcp", ":0", &Config{}) })
	mustPanic(t, "LoadX509KeyPair", func() { LoadX509KeyPair("a", "b") })
	mustPanic(t, "Conn.Handshake", func() { conn.Handshake() })
	mustPanic(t, "Conn.HandshakeContext", func() { conn.HandshakeContext(context.Background()) })
	mustPanic(t, "Conn.ConnectionState", func() { conn.ConnectionState() })
	mustPanic(t, "Conn.NetConn", func() { conn.NetConn() })
	mustPanic(t, "Conn.CloseWrite", func() { conn.CloseWrite() })
	mustPanic(t, "Conn.VerifyHostname", func() { conn.VerifyHostname("example.test") })
	mustPanic(t, "Conn.Read", func() { conn.Read(nil) })
	mustPanic(t, "Conn.Write", func() { conn.Write(nil) })
	mustPanic(t, "Conn.Close", func() { conn.Close() })
	mustPanic(t, "Conn.SetDeadline", func() { conn.SetDeadline(time.Time{}) })
	mustPanic(t, "Conn.SetReadDeadline", func() { conn.SetReadDeadline(time.Time{}) })
	mustPanic(t, "Conn.SetWriteDeadline", func() { conn.SetWriteDeadline(time.Time{}) })
	mustPanic(t, "ConnectionState.ExportKeyingMaterial", func() { state.ExportKeyingMaterial("label", nil, 1) })
}

func TestDataOnlyNamesDoNotPanic(t *testing.T) {
	if cfg := (*Config)(nil).Clone(); cfg != nil {
		t.Fatalf("cloning a nil config = %v", cfg)
	}
	cfg := &Config{ServerName: "a", NextProtos: []string{"h2"}}
	if clone := cfg.Clone(); clone == cfg || clone.ServerName != "a" {
		t.Fatalf("clone = %+v", clone)
	}
	if len(CipherSuites()) != 0 || len(InsecureCipherSuites()) != 0 {
		t.Fatal("no cipher suite can be negotiated here")
	}
	var _ net.Conn = (*Conn)(nil)
	if msg := (RecordHeaderError{Msg: "x"}).Error(); msg != "tls: x" {
		t.Fatalf("record header error = %q", msg)
	}
}
`

const x509StandInPanicTest = `package x509

import (
	"strings"
	"testing"
)

func mustPanic(t *testing.T, name string, call func()) {
	t.Helper()
	defer func() {
		message, _ := recover().(string)
		if !strings.Contains(message, "crypto/x509 is not part of this build") || !strings.Contains(message, name) {
			t.Errorf("%s: panic %q does not name the call", name, message)
		}
	}()
	call()
}

func TestCertificateAndPoolOperationsPanic(t *testing.T) {
	mustPanic(t, "Certificate.VerifyHostname", func() { (&Certificate{}).VerifyHostname("example.test") })
	mustPanic(t, "CertPool.AppendCertsFromPEM", func() { NewCertPool().AppendCertsFromPEM([]byte("pem")) })
}
`

// The built client must not link any of the stand-ins' operations: the linker
// drops a function nothing reaches, so their messages are in the module only
// when some code path in the client can reach a TLS dial, handshake or listen.
// The real crypto/tls strings must be gone as well, which shows the stand-ins
// were the packages that were built.
func TestClientBundleCannotReachATLSDial(t *testing.T) {
	if testing.Short() {
		t.Skip("compiling a wasm module takes tens of seconds; skipped under -short")
	}
	if _, err := goBinary(); err != nil {
		t.Skipf("no go toolchain available to build with: %v", err)
	}
	out := t.TempDir()
	var stdout bytes.Buffer
	// build also checks the size ceilings; an over-ceiling bundle is still on
	// disk and still tells what it links.
	_ = build(out, &stdout)
	if strings.Contains(stdout.String(), "building without the stand-in crypto/tls") {
		t.Skipf("this toolchain runs without the stand-ins, so the module carries the real TLS stack: %s", stdout.String())
	}
	module, err := os.ReadFile(filepath.Join(out, wasmFile))
	if err != nil {
		t.Fatalf("the build produced no module: %v", err)
	}
	for _, reachable := range []string{"crypto/tls is not part of this build", "crypto/x509 is not part of this build"} {
		if bytes.Contains(module, []byte(reachable)) {
			t.Errorf("the client can reach a stand-in operation (%q is in the module): some code path would dial, handshake or listen over TLS", reachable)
		}
	}
	for _, real := range []string{"tls: failed to verify certificate", "x509: certificate signed by unknown authority"} {
		if bytes.Contains(module, []byte(real)) {
			t.Errorf("the module still carries the toolchain's TLS stack (%q)", real)
		}
	}
}
