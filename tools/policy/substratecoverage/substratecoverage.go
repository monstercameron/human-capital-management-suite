// Package substratecoverage makes production substrate ownership explicit
// and rejects newly implicit persistence, scheduling, messaging, secrets,
// telemetry, search, cache, or file-transfer responsibilities.
package substratecoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Responsibility is one declared production substrate owner and its proving
// todo. Package is relative to the repository root and must be under
// internal/.
type Responsibility struct {
	Name    string `json:"name"`
	Package string `json:"package"`
	Todo    string `json:"todo"`
	Owner   string `json:"owner"`
}

// ImplicitEntry records an existing package that is not the canonical owner
// but remains visible while a migration or decomposition is pending.
type ImplicitEntry struct {
	Package string `json:"package"`
	Owner   string `json:"owner"`
	Reason  string `json:"reason"`
}

// Finding is one named coverage defect or allowlisted implicit package.
type Finding struct {
	Kind           string `json:"kind"`
	Responsibility string `json:"responsibility,omitempty"`
	Package        string `json:"package"`
	Detail         string `json:"detail"`
	Owner          string `json:"owner,omitempty"`
	Allowlisted    bool   `json:"allowlisted"`
}

// Report is the complete deterministic substrate scan.
type Report struct {
	Responsibilities []Responsibility `json:"responsibilities"`
	Findings         []Finding        `json:"findings"`
}

// Violations returns missing, untested, malformed, and newly implicit rows.
func (r Report) Violations() []Finding {
	var out []Finding
	for _, finding := range r.Findings {
		if !finding.Allowlisted {
			out = append(out, finding)
		}
	}
	return out
}

// OK reports whether there are no unallowlisted gaps.
func (r Report) OK() bool { return len(r.Violations()) == 0 }

// Digest returns a stable report identity.
func (r Report) Digest() string {
	data, _ := json.Marshal(r)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Responsibilities is the production substrate table required by
// SUBSTRATE-COVERAGE-001. Keep one canonical row per responsibility.
var Responsibilities = []Responsibility{
	{Name: "persistence", Package: "internal/data/store", Todo: "STORE-002", Owner: "data platform"},
	{Name: "scheduling", Package: "internal/engines/schedule", Todo: "SCHED-001", Owner: "scheduling platform"},
	{Name: "messaging", Package: "internal/messaging", Todo: "MSG-001", Owner: "messaging platform"},
	{Name: "secrets", Package: "internal/trust/secrets", Todo: "TRUST-015", Owner: "trust platform"},
	{Name: "telemetry", Package: "internal/platform/telemetry", Todo: "OBS-012", Owner: "observability platform"},
	{Name: "search", Package: "internal/data/search", Todo: "SEARCH-001", Owner: "data platform"},
	{Name: "cache", Package: "internal/platform/cache", Todo: "CACHE-001", Owner: "platform foundation"},
	{Name: "file transfer", Package: "internal/data/artifacts", Todo: "DATA-016", Owner: "artifact platform"},
}

// ImplicitAllowlist is reviewed, owner-bearing evidence for known secondary
// implementations. It is exact-path based so adding a new package cannot be
// hidden by a broad prefix.
var ImplicitAllowlist = []ImplicitEntry{
	{Package: "internal/data/inbox", Owner: "messaging platform", Reason: "inbound delivery mechanics are not the semantic messaging owner"},
	{Package: "internal/data/inboundmsg", Owner: "messaging platform", Reason: "inbound message persistence remains a supporting adapter"},
	{Package: "internal/data/jobs", Owner: "scheduling platform", Reason: "durable job storage remains below the scheduling engine"},
	{Package: "internal/data/outbox", Owner: "messaging platform", Reason: "transactional outbox mechanics remain below the messaging owner"},
	{Package: "internal/engines/messagetemplate", Owner: "workflow platform", Reason: "message rendering is a semantic engine, not the messaging substrate owner"},
	{Package: "internal/engines/search", Owner: "search platform", Reason: "authorized search digests through canonicalbytes; the engine is a consumer of canonical encoding, not its owner"},
	{Package: "internal/experience/reportschedule", Owner: "experience platform", Reason: "report scheduling is an experience-owned consumer of scheduling"},
	{Package: "internal/platform/telemetry/boundary", Owner: "observability platform", Reason: "telemetry boundary mechanics remain below the telemetry owner"},
	{Package: "internal/platform/telemetry/otel", Owner: "observability platform", Reason: "OpenTelemetry adapter mechanics remain below the telemetry owner"},
	{Package: "internal/platform/telemetry/otel/testexport", Owner: "observability platform", Reason: "telemetry test exporter is test support, not semantic ownership"},
	{Package: "internal/platform/telemetry/testexport", Owner: "observability platform", Reason: "telemetry test exporter is test support, not semantic ownership"},
	{Package: "internal/workflow/conformance/transfer", Owner: "workflow platform", Reason: "transfer conformance is a workflow consumer of artifact mechanics"},
	{Package: "internal/data/jobarchstore", Owner: "data platform", Reason: "job architecture persistence (PERSIST-JOBARCH-001) is a store adapter below the persistence owner"},
	{Package: "internal/domains/jobarch", Owner: "workforce domain", Reason: "job architecture is a domain model; its scheduling vocabulary is not the scheduling substrate"},
	{Package: "internal/governance/legal/researchgaps", Owner: "legal platform", Reason: "legal research-gap search is a governance consumer of search mechanics, not the search owner"},
	{Package: "internal/platform/execution/scheduler", Owner: "scheduling platform", Reason: "durable workflow timer and ready-work dispatch (SVC-004) runs below the scheduling engine as its process-role adapter"},
	{Package: "internal/platform/telemetry/securityevidence", Owner: "observability platform", Reason: "security evidence emission (SECARCH-008) remains below the telemetry owner"},
	{Package: "internal/workflow/migrate/artifacts", Owner: "artifact platform", Reason: "workflow version-migration artifacts are a workflow consumer of artifact mechanics"},
	{Package: "internal/platform/telemetry/queue", Owner: "observability platform", Reason: "bounded telemetry queue, retry and backpressure mechanics (OBS-019) remain below the telemetry owner"},
	{Package: "internal/connectivity/artifactstore", Owner: "integration platform", Reason: "connector consumer of substrate mechanics (Gate A wave 2026-09-06)"},
	{Package: "internal/operations/telemetry", Owner: "operations platform", Reason: "operational consumer of substrate mechanics (Gate A wave 2026-09-06)"},
	{Package: "internal/operations/telemetryhealth", Owner: "operations platform", Reason: "operational consumer of substrate mechanics (Gate A wave 2026-09-06)"},
	{Package: "internal/platform/telemetry/backends", Owner: "observability platform", Reason: "telemetry mechanics remain below the telemetry owner (Gate A wave 2026-09-06)"},
	{Package: "internal/platform/telemetry/diagnostic", Owner: "observability platform", Reason: "telemetry mechanics remain below the telemetry owner (Gate A wave 2026-09-06)"},
	{Package: "internal/platform/telemetry/lifecycle", Owner: "observability platform", Reason: "telemetry mechanics remain below the telemetry owner (Gate A wave 2026-09-06)"},
	{Package: "internal/platform/telemetry/correlation", Owner: "observability platform", Reason: "signal-correlation helpers and join scoping (OBS-016) remain below the telemetry owner"},
	{Package: "internal/connectivity/syncjob", Owner: "integration platform", Reason: "the resumable SyncJob kernel (INTG-019) is pure: it plans batches and returns a cursor, and the scheduling and persistence of that cursor stay with its callers"},
}

// Options allows focused tests and future reviewed table revisions without
// changing the default repository contract.
type Options struct {
	Responsibilities []Responsibility
	Allowlist        []ImplicitEntry
}

func (o Options) effective() Options {
	if o.Responsibilities == nil {
		o.Responsibilities = append([]Responsibility(nil), Responsibilities...)
	}
	if o.Allowlist == nil {
		o.Allowlist = append([]ImplicitEntry(nil), ImplicitAllowlist...)
	}
	return o
}

// Scan validates the declared table and scans production-looking internal
// packages for responsibilities that have not been named.
func Scan(root string, options Options) (Report, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("substratecoverage: resolve root: %w", err)
	}
	options = options.effective()
	report := Report{Responsibilities: append([]Responsibility(nil), options.Responsibilities...)}
	declared := map[string]bool{}
	for _, responsibility := range options.Responsibilities {
		if responsibility.Name == "" || responsibility.Package == "" || responsibility.Todo == "" || responsibility.Owner == "" {
			report.Findings = append(report.Findings, Finding{Kind: "INVALID", Responsibility: responsibility.Name, Package: responsibility.Package, Detail: "responsibility requires name, package, todo, and owner"})
			continue
		}
		if declared[responsibility.Package] {
			report.Findings = append(report.Findings, Finding{Kind: "DUPLICATE", Responsibility: responsibility.Name, Package: responsibility.Package, Detail: "package is named by more than one responsibility"})
			continue
		}
		declared[responsibility.Package] = true
		if !strings.HasPrefix(responsibility.Package, "internal/") {
			report.Findings = append(report.Findings, Finding{Kind: "OUTSIDE_INTERNAL", Responsibility: responsibility.Name, Package: responsibility.Package, Detail: "production substrate package must be under internal/"})
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(responsibility.Package))
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			report.Findings = append(report.Findings, Finding{Kind: "MISSING_PACKAGE", Responsibility: responsibility.Name, Package: responsibility.Package, Detail: "declared package does not exist"})
			continue
		}
		if !hasGoFile(path, false) {
			report.Findings = append(report.Findings, Finding{Kind: "NO_PRODUCTION_CODE", Responsibility: responsibility.Name, Package: responsibility.Package, Detail: "declared package has no production Go file"})
		}
		if !hasGoFile(path, true) {
			report.Findings = append(report.Findings, Finding{Kind: "NO_TEST", Responsibility: responsibility.Name, Package: responsibility.Package, Detail: "declared package has no test file"})
		}
	}

	allowlisted := map[string]ImplicitEntry{}
	for _, entry := range options.Allowlist {
		if entry.Package != "" && entry.Owner != "" && entry.Reason != "" {
			allowlisted[entry.Package] = entry
		}
	}
	implicit, err := discoverImplicit(root, declared, allowlisted)
	if err != nil {
		return Report{}, err
	}
	report.Findings = append(report.Findings, implicit...)
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Package != report.Findings[j].Package {
			return report.Findings[i].Package < report.Findings[j].Package
		}
		return report.Findings[i].Kind < report.Findings[j].Kind
	})
	return report, nil
}

// Check runs the default table against root and returns the first exact gap.
func Check(root string) error {
	report, err := Scan(root, Options{})
	if err != nil {
		return err
	}
	violations := report.Violations()
	if len(violations) > 0 {
		gap := violations[0]
		return fmt.Errorf("substratecoverage: %s %s: %s", gap.Kind, gap.Package, gap.Detail)
	}
	return nil
}

func hasGoFile(dir string, test bool) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		isTest := strings.HasSuffix(entry.Name(), "_test.go")
		if isTest == test {
			return true
		}
	}
	return false
}

func discoverImplicit(root string, declared map[string]bool, allowlisted map[string]ImplicitEntry) ([]Finding, error) {
	internal := filepath.Join(root, "internal")
	if _, err := os.Stat(internal); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("substratecoverage: stat internal packages: %w", err)
	}
	var findings []Finding
	err := filepath.WalkDir(internal, func(path string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !dirEntry.IsDir() || path == internal {
			return nil
		}
		if !hasGoFile(path, false) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if declared[rel] || !looksLikeSubstrate(rel) {
			return nil
		}
		entry, ok := allowlisted[rel]
		finding := Finding{Kind: "IMPLICIT", Package: rel, Detail: "production substrate package is not the declared responsibility owner", Allowlisted: ok}
		if ok {
			finding.Owner = entry.Owner
			finding.Detail = entry.Reason
		}
		findings = append(findings, finding)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("substratecoverage: scan internal packages: %w", err)
	}
	return findings, nil
}

func looksLikeSubstrate(path string) bool {
	lower := strings.ToLower(path)
	for _, term := range []string{"artifact", "cache", "inbox", "inbound", "job", "message", "outbox", "schedule", "search", "secret", "telemetry", "transfer"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}
