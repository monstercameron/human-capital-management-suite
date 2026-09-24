// Command vpatreport generates the versioned UX-003 accessibility evidence
// report in the ITI VPAT WCAG-edition structure.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/vpat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

func main() {
	_, source, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	defaultOutput := filepath.Join(repoRoot, "tools", "uxqual", "vpat", "reports", "wcag-2.2-aa-v1.json")
	output := flag.String("out", defaultOutput, "report JSON output path")
	flag.Parse()

	fixture := forms.FixtureWithValidationError()
	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		fatalf("render SSR fixture: %v", err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		fatalf("render GWC fixture: %v", err)
	}
	ssrResults, gwcResults := wcag.Score(ssrDoc), wcag.Score(gwcDoc)
	latest, err := wcag.LatestRunRecord()
	if err != nil {
		fatalf("load UX-003 run journal: %v; run go run ./tools/uxqual/cmd/ux003run first", err)
	}
	if !latest.MatchesResults(ssrResults, gwcResults) {
		fatalf("latest UX-003 journal results do not match current scorecard; rerun go run ./tools/uxqual/cmd/ux003run")
	}
	evidence, err := wcag.LoadEvidence()
	if err != nil {
		fatalf("load UX-003 evidence: %v", err)
	}
	if err := evidence.Validate(); err != nil {
		fatalf("validate UX-003 evidence: %v", err)
	}
	evidenceDigest, err := evidence.EvidenceDigest()
	if err != nil || evidenceDigest != latest.EvidenceDigest {
		fatalf("latest UX-003 journal does not match current scenario evidence; rerun go run ./tools/uxqual/cmd/ux003run")
	}
	report, err := vpat.GenerateFromRun(latest, nil)
	if err != nil {
		fatalf("generate report from UX-003 run: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}
	if err := report.WriteJSON(*output); err != nil {
		fatalf("write report: %v", err)
	}
	markdownPath := strings.TrimSuffix(*output, filepath.Ext(*output)) + ".md"
	if err := report.WriteMarkdown(markdownPath); err != nil {
		fatalf("write report table: %v", err)
	}
	fmt.Printf("wrote %s\nwrote %s\nversion=%s\nsource_run=%s\nsource_sequence=%d\nsha256=%s\ncriteria=%d\n", *output, markdownPath, report.Version, latest.ID, latest.Sequence, report.Digest, len(report.Rows))
}

func fatalf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
