package lineageconformance

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/lineage"
)

func TestStageLinksCoverDATA015AndRefuseUnmapped(t *testing.T) {
	for _, s := range lineage.Stages {
		if _, ok := StageLink(s); !ok {
			t.Fatalf("DATA-015 stage %s has no link", s)
		}
	}
	if l, _ := StageLink(lineage.StageApproval); l != LinkWorkflow {
		t.Fatalf("approval maps to %s", l)
	}
	if _, err := stageLinksFor(append(lineage.Stages, lineage.Stage("SETTLEMENT"))); !errors.Is(err, ErrUnmappedStage) {
		t.Fatalf("unmapped stage = %v", err)
	}
	producers, err := DefaultProducers()
	if err != nil || len(producers) != 3 || producers[1].Links[0] != LinkProjection ||
		producers[2].Todo != "RECON-001" || producers[2].Links[0] != LinkReconciliation {
		t.Fatalf("default producers = %+v, %v", producers, err)
	}
	for _, p := range producers {
		if p.Case != PromotionDefinition {
			t.Fatalf("producer %s claims %s", p.ID, p.Case)
		}
	}
}

func TestFromTraceMapsWatermarksAndRestrictedStubs(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nodes := make([]lineage.Node, len(lineage.Stages))
	for i, s := range lineage.Stages {
		nodes[i] = lineage.Node{Stage: s, Field: "f", Source: "src", Version: "v1", Authority: "a", Evidence: []string{"e"},
			EffectiveAt: base, KnownAt: base.Add(time.Duration(i) * time.Minute), Digest: "d" + string(s), Payload: "p"}
		if i > 0 {
			nodes[i].PrevDigest = nodes[i-1].Digest
		}
	}
	nodes[3].StreamKey, nodes[3].Sequence = "stream", 7
	nodes[1].Restricted, nodes[1].Payload, nodes[1].Reason = true, "", "hidden"
	trace, err := lineage.Assemble("t", "i", "f", nodes)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := FromTrace("case", "t", trace)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 7 || recs[3].Link != LinkEvent || recs[3].Watermark != "stream@7" || recs[3].SourceDigest != "dEVENT" {
		t.Fatalf("event record = %+v", recs[3])
	}
	if !strings.HasPrefix(recs[0].Watermark, "src/v1@2026-01-01T00:00:00Z") || recs[0].ID != "case#PROPOSAL" {
		t.Fatalf("proposal record = %+v", recs[0])
	}
	if recs[1].Classification != RestrictedClassification || !recs[1].Redacted || recs[1].Payload != "" || recs[1].Reason != "hidden" {
		t.Fatalf("restricted record = %+v", recs[1])
	}
}
