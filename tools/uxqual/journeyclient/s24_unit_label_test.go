package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// A journey's placements carry only unit codes. The compare row and the
// target organization read them through the seed's name table, so "&" survives.
func TestS24_PromotionUnitsKeepTheirAmpersand(t *testing.T) {
	if got := unitLabel("", "safety-quality"); got != "Safety & Quality" {
		t.Fatalf("unitLabel without a directory name = %q", got)
	}
	if got := unitLabel("Site Safety", "safety-quality"); got != "Site Safety" {
		t.Fatalf("the directory's own name lost to the table: %q", got)
	}
	if got := unitLabel("", "some-new-unit"); got != "Some New Unit" {
		t.Fatalf("an unknown code reads %q, want it as words", got)
	}

	journey := &journeyv1.Journey{
		Current: &journeyv1.Placement{OrgUnit: "quality-safety"},
		Target:  &journeyv1.Placement{OrgUnit: "finance-admin"},
	}
	var compared string
	for _, row := range comparisonLocale("en-US", journey) {
		if row.Label == productui.ResolveProductLocale("en-US").Text("journey.compare_org") {
			compared = row.Current + " -> " + row.Proposed
		}
	}
	if compared != "Quality & Safety -> Finance & Admin" {
		t.Errorf("the organization compare row reads %q", compared)
	}

	cards := reviewCards("en-US", &journeyv1.JourneyDetail{
		Journey:         &journeyv1.Journey{Target: &journeyv1.Placement{OrgUnit: "safety-quality"}},
		PromotionReview: &journeyv1.JourneyPromotionReview{},
	})
	if cards == nil || cards.Promotion == nil {
		t.Fatal("no review cards")
	}
	if got := cards.Promotion.Organization; !strings.Contains(got, "Safety & Quality") {
		t.Errorf("the target organization reads %q, want the unit named", got)
	}
}
