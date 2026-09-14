package hashchain

import (
	"testing"
)

func TestTodo_DATA_012(t *testing.T) {
	digester := testDigester(t)
	view := scanFixture(t, digester, "ledger-stream-1", 8)
	report := mustScan(t, ScanInput{Mode: ScanFull, Streams: []StreamView{view}})
	if report.Quality != QualityHealthy {
		t.Fatalf("quality=%v findings=%+v", report.Quality, report.Findings)
	}
	if report.Watermark != 8 {
		t.Fatalf("watermark=%d", report.Watermark)
	}
	// A sequence gap is never missed: the scan emits the exact finding
	// with the affected set and degrades quality.
	gapped := scanFixture(t, digester, "ledger-stream-1", 8)
	gapped.Links = append(gapped.Links[:4], gapped.Links[5:]...)
	broken := mustScan(t, ScanInput{Mode: ScanFull, Streams: []StreamView{gapped}})
	if broken.Quality != QualityDegraded {
		t.Fatalf("gapped quality=%v", broken.Quality)
	}
	found := false
	for _, finding := range broken.Findings {
		if finding.Code == FindingSequenceGap {
			found = true
			if finding.Watermark != 4 {
				t.Fatalf("gap watermark=%d", finding.Watermark)
			}
		}
	}
	if !found {
		t.Fatalf("no gap finding: %+v", broken.Findings)
	}
	if broken.IncidentRef == "" || broken.RepairRef == "" {
		t.Fatalf("incident/repair not opened: %+v", broken)
	}
}
