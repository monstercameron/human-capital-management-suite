package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestTodo_PROMOUX_009_Browser is PROMOUX-009's BROWSER matrix test. There is
// no live surface to drive here: findings come from a promotion simulation,
// every seeded journey in local dev already sits past that stage, and
// producing one means proposing a promotion, which mutates state -- exactly
// what P1A's zero-effect claim forbids doing just to look at a screen. This
// test instead proves the boundary the PROMOUX-009 fix actually depends on:
// the client-facing render path is a faithful, order- and count-preserving
// passthrough of whatever findings arrived on the wire, and does not attempt
// its own deduplication.
//
// That passthrough behaviour is not a gap; it is the other half of REFACTOR's
// requirement ("deduplication operates on typed finding identity before
// presentation, never on rendered strings"). [journeyv1.Finding], the wire
// message this package receives from the engine, carries only Severity, Code
// and Message -- no Field, no Owner -- because
// github.com/monstercameron/human-capital-management-suite/internal/domains/promotion.Finding's
// typed identity does not survive the trip across the schema/proto wire
// contract (out of scope for this todo). A renderer that tried to
// deduplicate at this layer could only do it by comparing Code/Message
// strings, which is exactly the rendered-string deduplication REFACTOR
// prohibits. So the correct fix is entirely upstream, in
// [github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract.Assemble],
// and this package's job is to render whatever already-canonical set it is
// handed without silently reordering, dropping or re-merging any of it.
func TestTodo_PROMOUX_009_Browser(t *testing.T) {
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)

	// Findings as they would arrive already deduplicated and ordered by an
	// upstream simcontract.Assemble: one canonicalized budget observation,
	// one corroborated increase-threshold advisory, one blocking finding.
	// Wire order is deliberately not alphabetical, to prove the renderer
	// preserves whatever order it was given rather than imposing its own.
	detail.Findings = []*journeyv1.Finding{
		{Severity: "BLOCKING", Code: "promotion.same_grade", Message: "target grade equals the current grade"},
		{Severity: "ADVISORY", Code: "promotion.budget_authority_observation_only", Message: "observed budget is short of the annualized cost"},
		{Severity: "ADVISORY", Code: "compensation.increase_over_ten_percent", Message: "annualized increase exceeds the review threshold"},
	}

	p := DetailPage(testConfig(), detail, nil, nil)
	got := p.Detail.Findings
	if len(got) != len(detail.Findings) {
		t.Fatalf("len(rendered Findings) = %d, want %d: the render boundary must preserve count exactly", len(got), len(detail.Findings))
	}
	for i, wire := range detail.Findings {
		if got[i].Code != wire.Code || got[i].Message != wire.Message {
			t.Fatalf("rendered finding %d = %+v, want Code/Message from wire finding %+v (order must be preserved, not resorted)", i, got[i], wire)
		}
	}
	if got[0].Severity != severityBlocking {
		t.Fatalf("rendered severity[0] = %q, want %q", got[0].Severity, severityBlocking)
	}

	// If the engine ever sent a genuine duplicate (upstream dedup failed, or
	// this is data from before PROMOUX-009 shipped), the renderer must still
	// not collapse it on its own: doing so here, with only Code/Message/
	// Severity in hand and no Field or Owner, would have to compare rendered
	// strings, which is precisely what REFACTOR forbids. Two byte-identical
	// wire findings must render as two rows.
	dupDetail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED)
	dupDetail.Findings = []*journeyv1.Finding{
		{Severity: "WARNING", Code: "compensation.increase_over_ten_percent", Message: "annualized increase exceeds the review threshold"},
		{Severity: "WARNING", Code: "compensation.increase_over_ten_percent", Message: "annualized increase exceeds the review threshold"},
	}
	dupPage := DetailPage(testConfig(), dupDetail, nil, nil)
	if len(dupPage.Detail.Findings) != 2 {
		t.Fatalf("len(rendered Findings) = %d, want 2: the render layer must never silently deduplicate on rendered strings", len(dupPage.Detail.Findings))
	}
}
