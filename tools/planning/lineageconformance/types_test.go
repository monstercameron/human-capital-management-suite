package lineageconformance

import (
	"slices"
	"testing"
)

func TestLinkVocabularyOrderAndValidity(t *testing.T) {
	links := ChainLinks()
	if len(links) != 12 || links[0] != LinkIntent || links[11] != LinkCorrection {
		t.Fatalf("chain = %v, want twelve links from INTENT to CORRECTION", links)
	}
	if LinkCausation.rank() != 0 || LinkIntent.rank() != 1 || LinkCorrection.rank() != 12 {
		t.Fatalf("ranks: causation=%d intent=%d correction=%d", LinkCausation.rank(), LinkIntent.rank(), LinkCorrection.rank())
	}
	for i := 1; i < len(links); i++ {
		if links[i-1].rank() >= links[i].rank() {
			t.Fatalf("%s does not rank before %s", links[i-1], links[i])
		}
	}
	if Link("TELEPATHY").Valid() || Link("").Valid() || !LinkCausation.Valid() || !LinkObservation.Valid() {
		t.Fatal("link validity is wrong")
	}
	if !slices.Equal(Statuses(), []Status{StatusComplete, StatusPartial, StatusUnknown, StatusDefective}) {
		t.Fatalf("statuses = %v", Statuses())
	}
}

func TestFindingCodesSplitIntoGapsAndDefects(t *testing.T) {
	for _, gap := range []string{CodeLinkMissing, CodeNoProducer, CodeProducerInvalid, CodeDefinitionAbsent, CodeChildNotAccepted} {
		if !isGap(gap) {
			t.Fatalf("%s should only leave a link unproven", gap)
		}
	}
	for _, defect := range []string{CodeLinkDuplicate, CodeCrossTenant, CodeOverDisclosed, CodeRedactionReason, CodeBrokenCausation,
		CodeDigestMismatch, CodeWatermarkMissing, CodeNotRebuildable, CodeHistoryRewritten, CodeHistoryBroken,
		CodeTriggerMismatch, CodeNotApplicableFound, CodeUnknownLink, CodeProducerOrphan} {
		if isGap(defect) {
			t.Fatalf("%s must be a defect", defect)
		}
	}
}
