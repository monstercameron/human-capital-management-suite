package workspace

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestJourneyPromotionReviewManagerDisclosed(t *testing.T) {
	if (JourneyPromotionReview{}).ManagerDisclosed() {
		t.Fatal("a review with no evaluated impact reported a disclosed manager")
	}
	manager := values.EntityRef{Tenant: "acme-corp", Kind: "worker", Id: "33333333-3333-4333-8333-333333333333"}
	review := JourneyPromotionReview{Impact: promotion.ManagementImpact{TargetManager: manager, CycleSafe: true}, TargetManagerName: "Dana Lee"}
	if !review.ManagerDisclosed() {
		t.Fatal("a review with an evaluated impact did not report its manager as disclosed")
	}
}
