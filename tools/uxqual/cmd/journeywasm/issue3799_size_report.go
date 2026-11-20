//go:build !(js && wasm)

package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Size attribution for the browser bundle (issue #3799).
//
// The build reports how large journey.wasm is; it cannot say
// what to cut when it stops fitting. This file reads the module's own
// function-name section (the Go linker keeps it for tracebacks), adds each
// function's code bytes to its package, and rolls packages up into groups, so
// a build can name the heaviest packages and how many bytes belong to
// generated clients of services Chat never calls. Standard library only: no
// toolchain, symbol table or network is needed beyond the built file.

const (
	repoModulePrefix = "github.com/monstercameron/human-capital-management-suite/"
	// reportTopN is how many packages and groups the text report lists.
	reportTopN = 25
)

type packageSize struct {
	Package   string  `json:"package"`
	Group     string  `json:"group"`
	Bytes     int64   `json:"bytes"`
	Functions int     `json:"functions"`
	Share     float64 `json:"share"`
	Chat      bool    `json:"chat"`
	Proto     bool    `json:"proto"`
}

type groupSize struct {
	Group     string  `json:"group"`
	Bytes     int64   `json:"bytes"`
	Functions int     `json:"functions"`
	Share     float64 `json:"share"`
	Chat      bool    `json:"chat"`
}

// wasmSizeReport is the published attribution. Package and group bytes are
// function body bytes; DataBytes and the remainder (types, tables, names) are
// reported separately so the parts add up to TotalBytes.
type wasmSizeReport struct {
	TotalBytes int64 `json:"total_bytes"`
	GzipBytes  int64 `json:"gzip_bytes"`
	CodeBytes  int64 `json:"code_bytes"`
	DataBytes  int64 `json:"data_bytes"`
	// NameSectionBytes is the function-name table the module carries for its
	// own tracebacks; it is part of TotalBytes but not of any package.
	NameSectionBytes int64 `json:"name_section_bytes"`
	UnnamedBytes     int64 `json:"unnamed_code_bytes"`
	Functions        int   `json:"functions"`
	// NonChatProtoBytes is code in generated protobuf/gRPC packages that are
	// not Chat's own: the first candidates for leaving the Chat entry path.
	// It counts what is linked in, not what Chat provably never calls.
	NonChatProtoBytes int64 `json:"non_chat_proto_bytes"`
	ChatBytes         int64 `json:"chat_bytes"`

	Packages []packageSize `json:"packages"`
	Groups   []groupSize   `json:"groups"`
}

type wasmReader struct {
	b   []byte
	pos int
	err error
}

var errWasmTruncated = errors.New("wasm module is truncated or malformed")

func (r *wasmReader) fail() { r.err = errWasmTruncated }

func (r *wasmReader) byte() byte {
	if r.err != nil || r.pos >= len(r.b) {
		r.fail()
		return 0
	}
	v := r.b[r.pos]
	r.pos++
	return v
}

func (r *wasmReader) u32() uint32 {
	var v uint64
	for shift := uint(0); shift < 70; shift += 7 {
		b := r.byte()
		if r.err != nil {
			return 0
		}
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			if v > 1<<32-1 {
				r.fail()
				return 0
			}
			return uint32(v)
		}
	}
	r.fail()
	return 0
}

func (r *wasmReader) take(n uint32) []byte {
	if r.err != nil || uint64(r.pos)+uint64(n) > uint64(len(r.b)) {
		r.fail()
		return nil
	}
	v := r.b[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return v
}

func (r *wasmReader) name() string { return string(r.take(r.u32())) }

func leb128Len(v uint32) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}

// attributeWasm reads a module's function sizes and names and returns the
// per-package attribution. It refuses anything that is not a well-formed
// module rather than reporting numbers it could not have measured.
func attributeWasm(module []byte) (*wasmSizeReport, error) {
	if len(module) < 8 || string(module[:4]) != "\x00asm" {
		return nil, errors.New("not a wasm module (missing \\0asm header)")
	}
	r := &wasmReader{b: module, pos: 8}
	report := &wasmSizeReport{TotalBytes: int64(len(module))}
	var importedFuncs uint32
	var bodySizes []int64
	names := map[uint32]string{}
	for r.pos < len(module) {
		id := r.byte()
		size := r.u32()
		body := r.take(size)
		if r.err != nil {
			return nil, r.err
		}
		sr := &wasmReader{b: body}
		switch id {
		case 2:
			for count := sr.u32(); count > 0 && sr.err == nil; count-- {
				sr.name()
				sr.name()
				switch sr.byte() {
				case 0:
					sr.u32()
					importedFuncs++
				case 1:
					sr.byte()
					skipLimits(sr)
				case 2:
					skipLimits(sr)
				case 3:
					sr.byte()
					sr.byte()
				case 4:
					sr.byte()
					sr.u32()
				default:
					sr.fail()
				}
			}
		case 10:
			for count := sr.u32(); count > 0 && sr.err == nil; count-- {
				n := sr.u32()
				sr.take(n)
				bodySizes = append(bodySizes, int64(n)+int64(leb128Len(n)))
			}
			report.CodeBytes = int64(size)
		case 11:
			report.DataBytes = int64(size)
		case 0:
			if sr.name() != "name" || sr.err != nil {
				continue
			}
			report.NameSectionBytes = int64(size)
			for sr.pos < len(body) && sr.err == nil {
				sub := sr.byte()
				subBody := sr.take(sr.u32())
				if sub != 1 || sr.err != nil {
					continue
				}
				nr := &wasmReader{b: subBody}
				for count := nr.u32(); count > 0 && nr.err == nil; count-- {
					idx := nr.u32()
					names[idx] = nr.name()
				}
				if nr.err != nil {
					return nil, nr.err
				}
			}
		}
		if sr.err != nil {
			return nil, sr.err
		}
	}
	if len(bodySizes) == 0 {
		return nil, errors.New("wasm module has no code section")
	}
	report.Functions = len(bodySizes)

	packages := map[string]*packageSize{}
	for i, n := range bodySizes {
		name, ok := names[importedFuncs+uint32(i)]
		if !ok || name == "" {
			report.UnnamedBytes += n
			name = "(unnamed)"
		}
		pkg := packageOfSymbol(name)
		if name == "(unnamed)" {
			pkg = "(unnamed)"
		}
		entry := packages[pkg]
		if entry == nil {
			entry = &packageSize{Package: pkg, Group: groupOfPackage(pkg), Chat: isChatPackage(pkg), Proto: isProtoPackage(pkg)}
			packages[pkg] = entry
		}
		entry.Bytes += n
		entry.Functions++
	}
	groups := map[string]*groupSize{}
	for _, p := range packages {
		p.Share = float64(p.Bytes) / float64(report.TotalBytes)
		report.Packages = append(report.Packages, *p)
		g := groups[p.Group]
		if g == nil {
			g = &groupSize{Group: p.Group}
			groups[p.Group] = g
		}
		g.Bytes += p.Bytes
		g.Functions += p.Functions
		g.Chat = g.Chat || p.Chat
		if p.Chat {
			report.ChatBytes += p.Bytes
		}
		if p.Proto && !p.Chat {
			report.NonChatProtoBytes += p.Bytes
		}
	}
	sort.Slice(report.Packages, func(i, j int) bool {
		if report.Packages[i].Bytes != report.Packages[j].Bytes {
			return report.Packages[i].Bytes > report.Packages[j].Bytes
		}
		return report.Packages[i].Package < report.Packages[j].Package
	})
	for _, g := range groups {
		g.Share = float64(g.Bytes) / float64(report.TotalBytes)
		report.Groups = append(report.Groups, *g)
	}
	sort.Slice(report.Groups, func(i, j int) bool {
		if report.Groups[i].Bytes != report.Groups[j].Bytes {
			return report.Groups[i].Bytes > report.Groups[j].Bytes
		}
		return report.Groups[i].Group < report.Groups[j].Group
	})
	return report, nil
}

func skipLimits(r *wasmReader) {
	flag := r.byte()
	r.u32()
	if flag&1 != 0 {
		r.u32()
	}
}

// packageOfSymbol returns the Go package a linker symbol belongs to.
func packageOfSymbol(symbol string) string {
	if strings.HasPrefix(symbol, "type:") || strings.HasPrefix(symbol, "type_.") || strings.HasPrefix(symbol, "go:") || strings.HasPrefix(symbol, "go_.") {
		return "(compiler generated)"
	}
	if !strings.Contains(symbol, "/") {
		return packageOfMangledSymbol(symbol)
	}
	// Generic instantiations carry type arguments in brackets, which can hold
	// slashes and dots of their own.
	var plain strings.Builder
	depth := 0
	for _, c := range symbol {
		switch {
		case c == '[':
			depth++
		case c == ']' && depth > 0:
			depth--
		case depth == 0:
			plain.WriteRune(c)
		}
	}
	s := plain.String()
	slash := strings.LastIndexByte(s, '/')
	dot := strings.IndexByte(s[slash+1:], '.')
	if dot < 0 {
		return "(runtime glue)"
	}
	return strings.ReplaceAll(s[:slash+1+dot], "%2e", ".")
}

// rebase restates the report against the shipped module's size: the symbol
// module has the same code but also carries a function-name table the shipped
// one does not, so shares and the total are the shipped module's.
func (r *wasmSizeReport) rebase(total int64) {
	r.DataBytes = min(r.DataBytes, total)
	r.NameSectionBytes = 0
	r.TotalBytes = total
	for i := range r.Packages {
		r.Packages[i].Share = float64(r.Packages[i].Bytes) / float64(total)
	}
	for i := range r.Groups {
		r.Groups[i].Share = float64(r.Groups[i].Bytes) / float64(total)
	}
}

// mangledHost matches the host of a module path at the start of a wasm
// function name. The Go linker writes names in the module's name section with
// every character that is not a letter, digit or dot turned into an
// underscore, so "github.com/yuin/goldmark/ast.(*CodeBlock).Kind" is stored as
// "github.com_yuin_goldmark_ast.__CodeBlock_.Kind": the host's dots survive,
// the slashes do not, and the package ends at the first dot after the host.
var mangledHost = regexp.MustCompile(`^([a-z0-9-]+\.)+(com|org|io|net|in|dev)_`)

// mangledRepoPath is the repo module's path as the linker mangles it.
const mangledRepoPath = "github.com_monstercameron_human_capital_management_suite"

// packageOfMangledSymbol undoes that mangling as far as it can be undone:
// underscores inside the package path become slashes, which is exact for every
// package this module and its dependencies name without an underscore.
func packageOfMangledSymbol(symbol string) string {
	start := 0
	if loc := mangledHost.FindStringIndex(symbol); loc != nil {
		start = loc[1]
	}
	dot := strings.IndexByte(symbol[start:], '.')
	if dot < 0 {
		return "(runtime glue)"
	}
	pkg := symbol[:start+dot]
	if rest, ok := strings.CutPrefix(pkg, mangledRepoPath+"_"); ok {
		return repoModulePrefix + strings.ReplaceAll(rest, "_", "/")
	}
	return strings.ReplaceAll(pkg, "_", "/")
}

// groupOfPackage rolls a package up to the unit a person would remove or
// lazy-load: a generated service, a repo area, a third-party module, or a
// standard-library tree.
func groupOfPackage(pkg string) string {
	if rest, ok := strings.CutPrefix(pkg, repoModulePrefix); ok {
		parts := strings.Split(rest, "/")
		if len(parts) >= 4 && parts[0] == "gen" && parts[1] == "go" {
			return strings.Join(parts[:4], "/")
		}
		return strings.Join(parts[:min(2, len(parts))], "/")
	}
	parts := strings.Split(pkg, "/")
	if strings.Contains(parts[0], ".") {
		// Hosts that keep many modules under an owner name the module in the
		// third element; other hosts (google.golang.org, gopkg.in) in the second.
		depth := 2
		switch parts[0] {
		case "github.com", "gitlab.com", "bitbucket.org", "golang.org":
			depth = 3
		}
		return strings.Join(parts[:min(depth, len(parts))], "/")
	}
	return parts[0]
}

func isChatPackage(pkg string) bool {
	return strings.Contains(strings.ToLower(strings.TrimPrefix(pkg, repoModulePrefix)), "chat")
}

func isProtoPackage(pkg string) bool {
	return strings.HasPrefix(pkg, repoModulePrefix+"gen/go/")
}

func pct(share float64) string { return fmt.Sprintf("%5.2f%%", share*100) }

// Text renders the report for a build log or CI artifact.
func (r *wasmSizeReport) Text(topN int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "journey.wasm size attribution\n\n")
	fmt.Fprintf(&b, "Total      %s\n", humanSize(r.TotalBytes))
	fmt.Fprintf(&b, "Gzip       %s\n", humanSize(r.GzipBytes))
	fmt.Fprintf(&b, "Code       %s over %d functions (%s unnamed)\n", humanSize(r.CodeBytes), r.Functions, humanSize(r.UnnamedBytes))
	fmt.Fprintf(&b, "Data       %s (strings, descriptors and tables; not attributed to packages)\n", humanSize(r.DataBytes))
	fmt.Fprintf(&b, "Chat code  %s in packages whose path names chat\n", humanSize(r.ChatBytes))
	fmt.Fprintf(&b, "Non-chat generated clients %s linked in (candidates to leave the Chat entry path)\n\n", humanSize(r.NonChatProtoBytes))
	fmt.Fprintf(&b, "Top packages by code bytes\n")
	for i, p := range r.Packages {
		if i >= topN {
			break
		}
		tag := ""
		switch {
		case p.Chat:
			tag = "  [chat]"
		case p.Proto:
			tag = "  [generated client]"
		}
		fmt.Fprintf(&b, "%12d  %s  %s%s\n", p.Bytes, pct(p.Share), p.Package, tag)
	}
	fmt.Fprintf(&b, "\nTop groups by code bytes\n")
	for i, g := range r.Groups {
		if i >= topN {
			break
		}
		fmt.Fprintf(&b, "%12d  %s  %s\n", g.Bytes, pct(g.Share), g.Group)
	}
	return b.String()
}

// writeSizeReport attributes the bundle built into outDir and writes the
// report to reportPath: JSON when the path ends in .json, text otherwise. The
// report sits outside the bundle directory on purpose, so it is never hashed
// into the asset manifest or embedded in the server.
//
// symbolsPath is the same module linked with its function names kept (the
// shipped one is stripped); its code sizes are attributed and the totals are
// the shipped bundle's. When it is empty the shipped module itself is read,
// which only attributes anything if that module still has its names.
func writeSizeReport(outDir, symbolsPath, reportPath string, stdout io.Writer) error {
	wasmPath := filepath.Join(outDir, wasmFile)
	shipped, err := os.Stat(wasmPath)
	if err != nil {
		return fmt.Errorf("size report: %w", err)
	}
	module, err := os.ReadFile(cmp.Or(symbolsPath, wasmPath))
	if err != nil {
		return fmt.Errorf("size report: %w", err)
	}
	report, err := attributeWasm(module)
	if err != nil {
		return fmt.Errorf("size report: %s: %w", wasmFile, err)
	}
	if symbolsPath != "" {
		report.rebase(shipped.Size())
	}
	if info, statErr := os.Stat(wasmPath + ".gz"); statErr == nil {
		report.GzipBytes = info.Size()
	}
	var body []byte
	if strings.EqualFold(filepath.Ext(reportPath), ".json") {
		if body, err = json.MarshalIndent(report, "", "  "); err != nil {
			return fmt.Errorf("size report: encode: %w", err)
		}
		body = append(body, '\n')
	} else {
		body = []byte(report.Text(reportTopN))
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return fmt.Errorf("size report: %w", err)
	}
	if err := os.WriteFile(reportPath, body, 0o644); err != nil {
		return fmt.Errorf("size report: %w", err)
	}
	fmt.Fprintf(stdout, "%s  size report (%d packages, %s of code)\n", reportPath, len(report.Packages), humanSize(report.CodeBytes))
	fmt.Fprint(stdout, report.Text(10))
	return nil
}
