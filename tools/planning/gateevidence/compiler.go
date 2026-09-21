package gateevidence

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/phaseone"
)

// Verdict is the exact, closed set of outcomes one EvidenceEntry compiles
// to (NEXT-003 GREEN/RED).
type Verdict string

const (
	// VerdictOK means the named test exists, has a result recorded no
	// older than the manifest's freshness window, that result is PASS,
	// the result's package matches the manifest's declared package, and
	// the package imports nothing on the manifest's forbidden list.
	VerdictOK Verdict = "OK"
	// VerdictMissing means no such test exists in the repository, or no
	// result (live or checked-in) exists for it, or the checked-in
	// result recorded a non-PASS outcome.
	VerdictMissing Verdict = "MISSING"
	// VerdictStale means a PASS result exists but is older than the
	// manifest's freshness window.
	VerdictStale Verdict = "STALE"
	// VerdictOutOfManifest means a result was found for the named test,
	// but recorded against a different package than the manifest
	// declares - evidence resolved from a system outside the signed
	// closure.
	VerdictOutOfManifest Verdict = "OUT_OF_MANIFEST"
	// VerdictEffectful means the evidence package imports a path the
	// manifest's ForbiddenImportPrefixes names, regardless of whether the
	// test itself passes.
	VerdictEffectful Verdict = "EFFECTFUL"
)

// Finding is one EvidenceEntry's compiled verdict.
type Finding struct {
	TodoID  string
	Test    string
	Package string
	Verdict Verdict
	Detail  string
}

// GateDecision is the compiler's own evidence-closure verdict. It is
// distinct from tools/planning/authoritygate.Decision, the human-signed
// Gate A/B/C authority decision: NEXT-003's GREEN clause is explicit that
// "passing evidence can yield a signed decision but never write authority".
type GateDecision string

const (
	GateDecisionClear   GateDecision = "GATE_CLEAR"
	GateDecisionBlocked GateDecision = "GATE_BLOCKED"
)

// Report is the compiled result of one manifest closure.
type Report struct {
	ManifestTodoID string
	ManifestDigest string
	AsOf           string
	Decision       GateDecision
	Findings       []Finding
}

// RunGoTestFunc runs `go test -count=1 -run ^name$ pkg` (or an equivalent)
// and returns the resulting ResultRecord. DefaultRunGoTest is the real
// implementation; tests inject a fake to stay hermetic and fast.
type RunGoTestFunc func(repoRoot, pkg, name string, now time.Time) (ResultRecord, error)

// CompileOptions configures Compile.
type CompileOptions struct {
	// RepoRoot resolves manifest Package fields (e.g. "./test/bootstrap")
	// to filesystem directories for the import scan and, when Live, for
	// the go test invocation's working directory.
	RepoRoot string
	// Now is the instant staleness is measured against. Callers that need
	// a deterministic, reproducible Report (e.g. the checked-in
	// definitions/planning/gates/p1a-evidence-report.{md,json}) must pass
	// a fixed value rather than time.Now().
	Now time.Time
	// Live, when true, runs `go test -count=1 -run <name> <pkg>` for each
	// evidence entry instead of reading Results. This is the only place
	// this package executes `go test`; every other path reads the
	// checked-in results file.
	Live bool
	// RunGoTest is used when Live is true. Defaults to DefaultRunGoTest.
	RunGoTest RunGoTestFunc
}

// Compile closes manifest's Evidence entries against results (used unless
// opts.Live is true) and returns a deterministic Report.
func Compile(manifest P1AManifest, results []ResultRecord, opts CompileOptions) (*Report, error) {
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	if opts.RunGoTest == nil {
		opts.RunGoTest = DefaultRunGoTest
	}

	digest, err := manifest.CanonicalDigest()
	if err != nil {
		return nil, fmt.Errorf("compute manifest digest: %w", err)
	}

	resultsByTest := make(map[string]ResultRecord, len(results))
	for _, r := range results {
		resultsByTest[r.Test] = r
	}

	report := &Report{
		ManifestTodoID: manifest.TodoID,
		ManifestDigest: digest,
		AsOf:           opts.Now.UTC().Format(dateLayout),
		Decision:       GateDecisionClear,
	}

	for _, entry := range manifest.Evidence {
		finding := compileOne(manifest, entry, resultsByTest, opts)
		report.Findings = append(report.Findings, finding)
		if finding.Verdict != VerdictOK {
			report.Decision = GateDecisionBlocked
		}
	}

	// Production callers historically invoked Compile directly. Keep the
	// closure check on that path so graph policy cannot be bypassed by choosing
	// the older entry point; CompileGateClosure exposes the detailed graph.
	gate, err := gateForRelease(manifest.Release)
	if err != nil {
		return nil, err
	}
	graph, err := graphFromEvidence(manifest, gate)
	if err != nil {
		return nil, err
	}
	if closure := phaseone.CompileAssuranceClosure(graph, gate); len(closure.Violations) != 0 {
		report.Decision = GateDecisionBlocked
	}

	return report, nil
}

func compileOne(manifest P1AManifest, entry EvidenceEntry, resultsByTest map[string]ResultRecord, opts CompileOptions) Finding {
	f := Finding{TodoID: entry.TodoID, Test: entry.Test, Package: entry.Package}

	pkgDir := filepath.Join(opts.RepoRoot, filepath.FromSlash(strings.TrimPrefix(entry.Package, "./")))

	existing, err := traceability.ScanTestNames(pkgDir)
	if err != nil {
		f.Verdict = VerdictMissing
		f.Detail = fmt.Sprintf("cannot scan package %s: %v", entry.Package, err)
		return f
	}
	if !existing[entry.Test] {
		f.Verdict = VerdictMissing
		f.Detail = fmt.Sprintf("no Test/Fuzz/Benchmark function named %s exists under %s", entry.Test, entry.Package)
		return f
	}

	imports, err := ScanImports(pkgDir)
	if err != nil {
		f.Verdict = VerdictMissing
		f.Detail = fmt.Sprintf("cannot scan imports of %s: %v", entry.Package, err)
		return f
	}
	if forbidden, hit := AnyForbidden(imports, manifest.ForbiddenImportPrefixes); hit {
		f.Verdict = VerdictEffectful
		f.Detail = fmt.Sprintf("package %s imports %s, which the manifest's forbidden_import_prefixes rejects as effect-bearing", entry.Package, forbidden)
		return f
	}

	var result ResultRecord
	if opts.Live {
		r, err := opts.RunGoTest(opts.RepoRoot, entry.Package, entry.Test, opts.Now)
		if err != nil {
			f.Verdict = VerdictMissing
			f.Detail = fmt.Sprintf("live run failed: %v", err)
			return f
		}
		result = r
	} else {
		r, ok := resultsByTest[entry.Test]
		if !ok {
			f.Verdict = VerdictMissing
			f.Detail = fmt.Sprintf("no checked-in result for %s (run with -live to execute it, or record a result)", entry.Test)
			return f
		}
		if r.Package != entry.Package {
			f.Verdict = VerdictOutOfManifest
			f.Detail = fmt.Sprintf("checked-in result for %s is recorded against package %s, not the manifest-declared package %s", entry.Test, r.Package, entry.Package)
			return f
		}
		result = r
	}

	if result.Result != "PASS" {
		f.Verdict = VerdictMissing
		f.Detail = fmt.Sprintf("recorded result is %s, not PASS", result.Result)
		return f
	}

	ts, err := result.ParseTimestamp()
	if err != nil {
		f.Verdict = VerdictMissing
		f.Detail = fmt.Sprintf("result timestamp %q does not parse as %s: %v", result.Timestamp, dateLayout, err)
		return f
	}
	ageDays := int(opts.Now.Sub(ts).Hours() / 24)
	if ageDays > manifest.FreshnessWindowDays {
		f.Verdict = VerdictStale
		f.Detail = fmt.Sprintf("result dated %s is %d day(s) old, exceeding the manifest's %d-day freshness window", result.Timestamp, ageDays, manifest.FreshnessWindowDays)
		return f
	}

	f.Verdict = VerdictOK
	f.Detail = fmt.Sprintf("%s PASS on %s (%s), %s", result.Timestamp, result.Environment, result.ToolchainVersion, result.Command)
	return f
}

// DefaultRunGoTest runs `go test -json -count=1 -run ^name$ pkg` in repoRoot and
// reports PASS/FAIL. It never runs unless CompileOptions.Live is true.
func DefaultRunGoTest(repoRoot, pkg, name string, now time.Time) (ResultRecord, error) {
	pattern := "^" + name + "$"
	cmd := exec.Command("go", "test", "-json", "-count=1", "-run", pattern, pkg)
	cmd.Dir = repoRoot
	output, runErr := cmd.CombinedOutput()
	result := classifyGoTestRun(string(output), runErr == nil, name, runtime.GOOS == "windows")

	return ResultRecord{
		Test:      name,
		Package:   pkg,
		Result:    result,
		Timestamp: now.UTC().Format(dateLayout),
		Command:   fmt.Sprintf("go test -json -count=1 -run %s %s", pattern, pkg),
	}, nil
}

// classifyGoTestRun requires the named test and its package to pass. An
// unrelated PASS token or a successful process with no matching tests does
// not constitute evidence. Windows may fail to unlink a passing test binary
// after the package result; accept only that exact post-result diagnostic.
func classifyGoTestRun(output string, cleanExit bool, name string, windows bool) string {
	var testPassed, packagePassed, failed, cleanup bool
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event struct {
			Action string `json:"Action"`
			Test   string `json:"Test"`
		}
		if json.Unmarshal([]byte(line), &event) == nil {
			if event.Test == name {
				testPassed = event.Action == "pass" || testPassed
				failed = event.Action == "fail" || failed
			}
			if event.Test == "" {
				packagePassed = event.Action == "pass" || packagePassed
				failed = event.Action == "fail" || failed
			}
			continue
		}
		if windows && strings.HasPrefix(line, "go: unlinkat ") && strings.Contains(line, "go-build") && strings.HasSuffix(strings.ToLower(line), ".test.exe: access is denied.") {
			cleanup = true
			continue
		}
		failed = true
	}
	if testPassed && packagePassed && !failed && (cleanExit || (windows && cleanup)) {
		return "PASS"
	}
	return "FAIL"
}
