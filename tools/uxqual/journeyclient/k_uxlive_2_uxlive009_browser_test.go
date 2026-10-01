package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	journey "github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_UXLIVE_009_Browser exercises the rendered detail tree, not only the
// token predicate. A live browser receives this same projected Fact: a
// machine-shaped value has identifier treatment, while ordinary author prose
// remains ordinary readable content.
func TestTodo_UXLIVE_009_Browser(t *testing.T) {
	detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	detail.GetJourney().BusinessReason = "promotion_into_senior_hrbp_fix_verify"

	page := DetailPage(testConfig(), detail, nil, nil)
	markup, err := journey.RenderToString(page)
	if err != nil {
		t.Fatalf("rendering token-shaped business reason: %v", err)
	}
	tokenAt := strings.Index(markup, "promotion_into_senior_hrbp_fix_verify")
	if tokenAt < 0 {
		t.Fatalf("rendered detail omitted the carried business reason: %s", markup)
	}
	window := markup[max(0, tokenAt-120):min(len(markup), tokenAt+120)]
	if !strings.Contains(window, `class="jn-mono"`) {
		t.Fatalf("token-shaped reason was rendered as prose instead of an identifier: %s", window)
	}

	detail.GetJourney().BusinessReason = "Expanded scope across the regional team."
	page = DetailPage(testConfig(), detail, nil, nil)
	markup, err = journey.RenderToString(page)
	if err != nil {
		t.Fatalf("rendering prose business reason: %v", err)
	}
	proseAt := strings.Index(markup, "Expanded scope across the regional team.")
	if proseAt < 0 {
		t.Fatalf("rendered detail omitted the author's prose reason: %s", markup)
	}
	window = markup[max(0, proseAt-120):min(len(markup), proseAt+120)]
	if strings.Contains(window, `class="jn-mono"`) {
		t.Fatalf("ordinary prose reason received identifier treatment: %s", window)
	}
}
