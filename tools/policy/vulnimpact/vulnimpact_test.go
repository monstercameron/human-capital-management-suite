package vulnimpact_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/sbom"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/vulnimpact"
)

// These pins follow the checked-in definitions/supply-chain/sbom.cdx.json as
// staged in the working tree. The pins were last set at commit 63aaab83, when
// the SBOM still had its earlier Metadata.Timestamp and serial number. The
// SBOM was regenerated afterwards (76898526 and the current regeneration adds
// the go-spew and golang/snappy components and a new timestamp). The canonical
// SBOM digest covers the whole document, so each regeneration moves the SBOM,
// finding and report digests together. Re-pin whenever the SBOM is regenerated.
const (
	checkedInImpactDigest  = "daf1f1dab357d2011355a1ba261bc81cace32adb1a300b2687944b9d6f098074"
	checkedInFindingDigest = "9b95163480ef8ca18fb22abcb392da5c9d72eb33ec91aa49e556ab26b1c014cb"
	checkedInCanonicalSBOM = "fb84700864c1ebae0740ee7736755a99932727ceb3b0e02940421aab54717d07"
)

func checkedInSBOM(t *testing.T) sbom.Document {
	t.Helper()
	root := repopath.RootDir()
	data, err := os.ReadFile(filepath.Join(root, "definitions", "supply-chain", "sbom.cdx.json"))
	if err != nil {
		t.Fatalf("read checked-in SBOM: %v", err)
	}
	var document sbom.Document
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode checked-in SBOM: %v", err)
	}
	return document
}

func testVulnerability() vulnimpact.Vulnerability {
	return vulnimpact.Vulnerability{
		ID:                   "GHSA-fixture-uuid",
		Module:               "github.com/google/uuid",
		AffectedVersionRange: "=v1.6.0",
		Severity:             vulnimpact.SeverityHigh,
	}
}

// TestTodo_SUPPLY_002 is SUPPLY-002's PRIMARY test: the pure resolver joins
// a module/range vulnerability to an artifact and then to explicit tenants.
func TestTodo_SUPPLY_002(t *testing.T) {
	artifact := vulnimpact.Artifact{ID: "hcmnext-release", SBOM: checkedInSBOM(t)}
	report, err := vulnimpact.Analyze([]vulnimpact.Artifact{artifact}, testVulnerability(), vulnimpact.DeploymentMap{
		"hcmnext-release": {"tenant-b", "tenant-a", "tenant-a"},
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Module != "github.com/google/uuid" {
		t.Fatalf("findings=%+v, want one google/uuid finding", report.Findings)
	}
	if len(report.AffectedArtifacts) != 1 || len(report.AffectedTenants) != 2 {
		t.Fatalf("artifacts=%v tenants=%v, want one artifact and two tenants", report.AffectedArtifacts, report.AffectedTenants)
	}
	if report.Findings[0].Severity != vulnimpact.SeverityHigh || report.Findings[0].Digest == "" || report.Digest == "" {
		t.Fatalf("report lacks typed severity/digests: %+v", report)
	}
	if !report.CustomerNotification || report.RemediationOwner == "" || report.CompensatingControl == "" || report.Watermark == "" {
		t.Fatalf("report lacks operational evidence: %+v", report)
	}
}

// TestTodo_SUPPLY_002_Golden pins the real checked-in SBOM fixture's impact
// shape. The digest assertion is filled after the canonical report is built;
// this prevents a future serializer or field-order change from silently
// changing evidence identity.
func TestTodo_SUPPLY_002_Golden(t *testing.T) {
	artifact := vulnimpact.Artifact{ID: "hcmnext-release", SBOM: checkedInSBOM(t)}
	report, err := vulnimpact.Analyze([]vulnimpact.Artifact{artifact}, testVulnerability(), vulnimpact.DeploymentMap{
		"hcmnext-release": {"tenant-a", "tenant-b"},
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got, want := len(report.Findings), 1; got != want {
		t.Fatalf("finding count=%d, want %d", got, want)
	}
	if report.Findings[0].Version != "v1.6.0" || report.Findings[0].Severity != vulnimpact.SeverityHigh {
		t.Fatalf("finding=%+v, want v1.6.0 HIGH", report.Findings[0])
	}
	if report.Digest != checkedInImpactDigest {
		t.Fatalf("report digest=%s, want pinned %s", report.Digest, checkedInImpactDigest)
	}
	if report.Findings[0].Digest != checkedInFindingDigest {
		t.Fatalf("finding digest=%s, want pinned %s", report.Findings[0].Digest, checkedInFindingDigest)
	}
	if report.Findings[0].SBOMDigest != checkedInCanonicalSBOM {
		t.Fatalf("canonical SBOM digest=%s, want pinned %s", report.Findings[0].SBOMDigest, checkedInCanonicalSBOM)
	}
	if !strings.Contains(report.Explain(), "vulnerability impact GHSA-fixture-uuid") {
		t.Fatalf("Explain()=%q, want vulnerability identity", report.Explain())
	}
}

// TestTodo_SUPPLY_002_Security proves that a nonmatching range and a tenant
// absent from the explicit deployment map never become an affected tenant.
func TestTodo_SUPPLY_002_Security(t *testing.T) {
	findings, err := vulnimpact.Resolve(checkedInSBOM(t), vulnimpact.Vulnerability{
		ID:                   "GHSA-no-match",
		Module:               "github.com/google/uuid",
		AffectedVersionRange: "<v1.6.0",
		Severity:             vulnimpact.SeverityCritical,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("nonmatching range produced findings=%+v", findings)
	}
	report, err := vulnimpact.Analyze([]vulnimpact.Artifact{{ID: "hcmnext-release", SBOM: checkedInSBOM(t)}}, testVulnerability(), vulnimpact.DeploymentMap{})
	if err != nil {
		t.Fatalf("Analyze without deployment: %v", err)
	}
	if len(report.AffectedTenants) != 0 || report.CustomerNotification {
		t.Fatalf("unmapped deployment produced tenant impact: %+v", report)
	}
}

func TestVersionRangesHandleBoundsAndAlternatives(t *testing.T) {
	document := sbom.Document{Metadata: sbom.Metadata{Component: sbom.Component{Name: "app", Version: "v1.0.0"}}, Components: []sbom.Component{
		{Name: "example/module", Version: "v1.2.3"},
		{Name: "example/module", Version: "v2.0.0"},
	}}
	findings, err := vulnimpact.Resolve(document, vulnimpact.Vulnerability{Module: "example/module", AffectedVersionRange: ">=v1.0.0 <v2.0.0", Severity: vulnimpact.SeverityLow})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(findings) != 1 || findings[0].Version != "v1.2.3" {
		t.Fatalf("findings=%+v, want only v1.2.3", findings)
	}
}
