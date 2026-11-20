//go:build !(js && wasm)

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const issue3799Module = "github.com/monstercameron/human-capital-management-suite/"

// leb encodes v as an unsigned LEB128 integer.
func leb(v uint32) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

func wasmSection(id byte, body []byte) []byte {
	return append(append([]byte{id}, leb(uint32(len(body)))...), body...)
}

func wasmName(s string) []byte { return append(leb(uint32(len(s))), []byte(s)...) }

// syntheticWasm builds a module with one imported function and one imported
// memory (so the parser must skip both kinds), the named functions in order
// with bodies of the given sizes, an optional data section and a name section
// that also names the imported function.
func syntheticWasm(imported string, sizes map[string]int, order []string, dataBytes int) []byte {
	module := []byte{0x00, 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00}
	imp := append(leb(2), wasmName("go")...)
	imp = append(imp, wasmName("x")...)
	imp = append(imp, 0x00, 0x00)
	imp = append(imp, wasmName("go")...)
	imp = append(imp, wasmName("mem")...)
	imp = append(imp, 0x02, 0x01, 0x01, 0x10)
	module = append(module, wasmSection(2, imp)...)
	code := leb(uint32(len(order)))
	for _, name := range order {
		code = append(code, leb(uint32(sizes[name]))...)
		code = append(code, make([]byte, sizes[name])...)
	}
	module = append(module, wasmSection(10, code)...)
	if dataBytes > 0 {
		module = append(module, wasmSection(11, make([]byte, dataBytes))...)
	}
	names := append(leb(uint32(len(order)+1)), leb(0)...)
	names = append(names, wasmName(imported)...)
	for i, name := range order {
		names = append(names, leb(uint32(i+1))...)
		names = append(names, wasmName(name)...)
	}
	sub := append([]byte{1}, leb(uint32(len(names)))...)
	sub = append(sub, names...)
	custom := append(wasmName("name"), sub...)
	return append(module, wasmSection(0, custom)...)
}

func TestIssue3799_PackageOfSymbol(t *testing.T) {
	for symbol, want := range map[string]string{
		"runtime.mallocgc":                                  "runtime",
		"net/http.(*Transport).roundTrip":                   "net/http",
		"github.com/yuin/goldmark/parser.(*parser).Parse":   "github.com/yuin/goldmark/parser",
		"gopkg.in/yaml%2ev3.(*decoder).unmarshal":           "gopkg.in/yaml.v3",
		"example.com/m/p.Map[example.com/m/q.K,string].Get": "example.com/m/p",
		"example.com/m/p.Func.func1":                        "example.com/m/p",
		"example.com/m/p.init.0":                            "example.com/m/p",
		"type:.eq.example.com/m/p.T":                        "(compiler generated)",
		"wasm_export_run":                                   "(runtime glue)",
		"go_buildid":                                        "(runtime glue)",
		"type_.eq.example_com_m_p.T":                        "(compiler generated)",
		// The names the Go linker writes into a wasm name section.
		"github.com_yuin_goldmark_ast.__CodeBlock_.AttributeString":                        "github.com/yuin/goldmark/ast",
		"crypto_tls.__prefixNonceAEAD_.explicitNonceLen":                                   "crypto/tls",
		"slices.rotateCmpFunc_go.shape.struct___Key_reflect.Value__Value_reflect.Value___": "slices",
		"google.golang.org_grpc_internal_transport.__http2Client_.Close":                   "google.golang.org/grpc/internal/transport",
		"main.drawnKey": "main",
		"github.com_monstercameron_human_capital_management_suite_gen_go_hcmnext_time_v1.PunchRejectionReason.X": issue3799Module + "gen/go/hcmnext/time/v1",
		"github.com_monstercameron_human_capital_management_suite_internal_humanwork_chatui.EnsureShareFocus":    issue3799Module + "internal/humanwork/chatui",
	} {
		if got := packageOfSymbol(symbol); got != want {
			t.Errorf("packageOfSymbol(%q) = %q, want %q", symbol, got, want)
		}
	}
}

func TestIssue3799_GroupOfPackage(t *testing.T) {
	for pkg, want := range map[string]string{
		issue3799Module + "gen/go/hcmnext/payroll/v1":    "gen/go/hcmnext/payroll",
		issue3799Module + "gen/go/hcmnext/chat/v1":       "gen/go/hcmnext/chat",
		issue3799Module + "internal/humanwork/chatui":    "internal/humanwork",
		issue3799Module + "tools/uxqual/cmd/journeywasm": "tools/uxqual",
		"github.com/yuin/goldmark/parser":                "github.com/yuin/goldmark",
		"google.golang.org/grpc/internal/transport":      "google.golang.org/grpc",
		"golang.org/x/net/http2":                         "golang.org/x/net",
		"net/http":                                       "net",
		"runtime":                                        "runtime",
		"(compiler generated)":                           "(compiler generated)",
	} {
		if got := groupOfPackage(pkg); got != want {
			t.Errorf("groupOfPackage(%q) = %q, want %q", pkg, got, want)
		}
	}
	if !isChatPackage(issue3799Module+"gen/go/hcmnext/chat/v1") || !isChatPackage(issue3799Module+"internal/humanwork/chatui") {
		t.Error("chat packages were not recognised as chat")
	}
	if isChatPackage(issue3799Module+"gen/go/hcmnext/payroll/v1") || isChatPackage("net/http") {
		t.Error("non-chat packages were recognised as chat")
	}
}

func TestIssue3799_AttributeWasmSumsFunctionBytesPerPackage(t *testing.T) {
	marshal := issue3799Module + "gen/go/hcmnext/payroll/v1.(*Run).Marshal"
	reset := issue3799Module + "gen/go/hcmnext/payroll/v1.(*Run).Reset"
	render := issue3799Module + "internal/humanwork/chatui.Render"
	module := syntheticWasm("runtime.wasmImport", map[string]int{marshal: 300, reset: 100, render: 50, "net/http.Get": 10},
		[]string{marshal, reset, render, "net/http.Get"}, 700)
	report, err := attributeWasm(module)
	if err != nil {
		t.Fatalf("attributeWasm: %v", err)
	}
	if report.TotalBytes != int64(len(module)) {
		t.Fatalf("total = %d, want %d", report.TotalBytes, len(module))
	}
	if report.DataBytes < 700 || report.DataBytes > 710 {
		t.Fatalf("data bytes = %d, want about 700", report.DataBytes)
	}
	if report.Functions != 4 {
		t.Fatalf("functions = %d, want 4", report.Functions)
	}
	byPkg := map[string]packageSize{}
	for _, p := range report.Packages {
		byPkg[p.Package] = p
	}
	payroll := byPkg[issue3799Module+"gen/go/hcmnext/payroll/v1"]
	if payroll.Bytes < 400 || payroll.Bytes > 410 || payroll.Functions != 2 || payroll.Chat {
		t.Fatalf("payroll = %+v, want about 400 bytes over 2 functions, not chat", payroll)
	}
	chat := byPkg[issue3799Module+"internal/humanwork/chatui"]
	if chat.Bytes < 50 || !chat.Chat {
		t.Fatalf("chatui = %+v, want >= 50 bytes and chat", chat)
	}
	if report.Packages[0].Package != payroll.Package {
		t.Fatalf("packages are not sorted largest first: %+v", report.Packages)
	}
	if report.NonChatProtoBytes < 400 {
		t.Fatalf("non-chat proto bytes = %d, want payroll's 400+", report.NonChatProtoBytes)
	}
	text := report.Text(10)
	for _, want := range []string{"gen/go/hcmnext/payroll/v1", "Top packages", "Top groups", "Gzip"} {
		if !strings.Contains(text, want) {
			t.Errorf("text report lacks %q:\n%s", want, text)
		}
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("report does not marshal: %v", err)
	}
}

func TestIssue3799_AttributeWasmRefusesNonModules(t *testing.T) {
	for name, body := range map[string][]byte{
		"empty":     nil,
		"not wasm":  []byte("hello world, this is not a module"),
		"truncated": append([]byte{0x00, 'a', 's', 'm', 1, 0, 0, 0}, 10, 0xff, 0xff),
	} {
		if _, err := attributeWasm(body); err == nil {
			t.Errorf("%s: attributeWasm accepted the input", name)
		}
	}
}

func TestIssue3799_WriteSizeReportHeadroomAgainstCeilings(t *testing.T) {
	dir := t.TempDir()
	wasm := filepath.Join(dir, wasmFile)
	if err := os.WriteFile(wasm, syntheticWasm("runtime.x", map[string]int{"net/http.Get": 20}, []string{"net/http.Get"}, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeGzip(wasm); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	report := filepath.Join(dir, "reports", "sizes.json")
	if err := writeSizeReport(dir, "", report, &out); err != nil {
		t.Fatalf("writeSizeReport: %v", err)
	}
	body, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var got wasmSizeReport
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	if got.TotalBytes <= 0 || got.GzipBytes <= 0 {
		t.Fatalf("total = %d gzip = %d", got.TotalBytes, got.GzipBytes)
	}
	if !strings.Contains(out.String(), "size report") {
		t.Fatalf("nothing said about the report: %q", out.String())
	}
	txt := filepath.Join(dir, "sizes.txt")
	if err := writeSizeReport(dir, "", txt, &out); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(txt); !strings.Contains(string(body), "Top packages") {
		t.Fatalf("text report = %q", body)
	}
	if err := writeSizeReport(filepath.Join(dir, "missing"), "", txt, &out); err == nil {
		t.Fatal("a missing bundle produced a report")
	}
}

func TestIssue3799_RunWritesTheReportEvenWhenTheBundleIsOverBudget(t *testing.T) {
	// -size-report without -out is a usage error; the flag must be known.
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-size-report", filepath.Join(t.TempDir(), "r.json")}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2 (an output directory is still required)", code)
	}
	if strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("-size-report is not a known flag: %s", stderr.String())
	}

	original := bundleBuilder
	t.Cleanup(func() { bundleBuilder = original })
	fakeModule := syntheticWasm("runtime.x", map[string]int{"net/http.Get": 40}, []string{"net/http.Get"}, 0)

	// The gate refuses the bundle after both links: the failure is reported and
	// the attribution is still written.
	out := t.TempDir()
	report := filepath.Join(t.TempDir(), "over.json")
	var symbolsSeen string
	bundleBuilder = func(outDir, symbolsPath string, _ io.Writer) error {
		symbolsSeen = symbolsPath
		if err := os.WriteFile(filepath.Join(outDir, wasmFile), fakeModule, 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(symbolsPath, fakeModule, 0o600); err != nil {
			return err
		}
		return errors.New("frontend performance budget exceeded")
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-out", out, "-size-report", report}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d, want 1 for an over-budget bundle: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "budget exceeded") {
		t.Fatalf("the build failure was swallowed: %q", stderr.String())
	}
	if body, err := os.ReadFile(report); err != nil || !strings.Contains(string(body), `"total_bytes"`) {
		t.Fatalf("no attribution written for the over-budget bundle: %v %q", err, body)
	}
	if _, err := os.Stat(symbolsSeen); err == nil {
		t.Fatal("the symbol module was left behind after the run")
	}

	// A build that dies before the symbol link writes no report and does not
	// mask the real error with a report error.
	missing := filepath.Join(t.TempDir(), "none.json")
	bundleBuilder = func(string, string, io.Writer) error { return errors.New("link failed") }
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-out", t.TempDir(), "-size-report", missing}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "link failed") {
		t.Fatalf("exit = %d stderr %q, want 1 with the link failure", code, stderr.String())
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("a report was written although no symbol module existed")
	}

	// Without -size-report no second link is asked for.
	bundleBuilder = func(_, symbolsPath string, _ io.Writer) error {
		if symbolsPath != "" {
			t.Errorf("symbols requested without -size-report: %q", symbolsPath)
		}
		return nil
	}
	if code := run([]string{"-out", t.TempDir()}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr.String())
	}
}

// A real build with -size-report: the symbol link must name the module's own
// functions, agree with the shipped module on code size, and leave the report
// out of the directory the asset manifest is written from.
func TestIssue3799_BuildWritesAttributionForTheRealBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("two wasm links take minutes; skipped under -short")
	}
	if _, err := goBinary(); err != nil {
		t.Skipf("no go toolchain available to build with: %v", err)
	}
	out := t.TempDir()
	report := filepath.Join(t.TempDir(), "sizes.json")
	var stdout, stderr bytes.Buffer
	// Other work in the tree can push the bundle over its ceiling; the report
	// is meant to exist exactly then, so only that refusal is tolerated here.
	if code := run([]string{"-out", out, "-size-report", report}, &stdout, &stderr); code != 0 && !strings.Contains(stderr.String(), "frontend performance budget") {
		t.Fatalf("run exit %d: %s%s", code, stdout.String(), stderr.String())
	}
	body, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var got wasmSizeReport
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(out, wasmFile))
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalBytes != info.Size() {
		t.Fatalf("report total %d does not match the shipped %d bytes", got.TotalBytes, info.Size())
	}
	if got.Functions < 1000 || len(got.Packages) < 20 {
		t.Fatalf("report looks empty: %d functions, %d packages", got.Functions, len(got.Packages))
	}
	if named := got.CodeBytes - got.UnnamedBytes; named*10 < got.CodeBytes*9 {
		t.Fatalf("only %d of %d code bytes are named", named, got.CodeBytes)
	}
	if got.ChatBytes <= 0 {
		t.Fatalf("no chat code attributed: %+v", got.Groups[:min(5, len(got.Groups))])
	}
	if _, err := os.Stat(filepath.Join(out, "sizes.json")); err == nil {
		t.Fatal("the report was written into the asset directory")
	}
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if strings.Contains(e.Name(), "symbols") {
			t.Fatalf("the symbol module leaked into the asset directory: %s", e.Name())
		}
	}
	t.Logf("%s", got.Text(15))
}
