package journey_test

// REV-091-02: InspectJourney carries the engine's reporting-line and pay
// guardrail review on JourneyDetail.promotion_review. The transport is a field
// copy that can only narrow: an unavailable guardrail reaches the wire with
// its status and reason and no amount, and an undisclosed manager with no
// name, even when a malformed engine answer carries more.

import (
	"regexp"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

func rev09102Money(t *testing.T, text string) values.Money {
	t.Helper()
	m, err := values.NewMoney(text, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func rev09102Guardrail(t *testing.T) promotion.CompensationGuardrail {
	t.Helper()
	permitted, err := values.NewPercentage("0.180000", 6, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	basis, err := values.ParseLocalDate("2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	return promotion.CompensationGuardrail{
		Status:            promotion.GuardrailStatusAvailable,
		CurrentAnnualized: rev09102Money(t, "100000.00"), MinimumAnnualized: rev09102Money(t, "90000.00"),
		MaximumAnnualized: rev09102Money(t, "118000.00"), PermittedIncreasePercent: permitted,
		BandPosition: payband.PlacementInBand, Currency: "USD", EffectiveDateBasis: basis,
		BandID: "band-1", BandVersion: "v1",
	}
}

func rev09102Impact() promotion.ManagementImpact {
	return promotion.ManagementImpact{
		TargetManager: values.EntityRef{Tenant: "acme-corp", Kind: people.KindWorker, Id: "33333333-3333-4333-8333-333333333333"},
		CycleSafe:     true,
	}
}

func TestInspectJourneyCarriesPromotionReview(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	detail := fixtureDetail()
	detail.Review = &workspace.JourneyPromotionReview{
		Impact: rev09102Impact(), TargetManagerName: "Dana Lee", ManagerUnchanged: true,
		Guardrail:            rev09102Guardrail(t),
		CurrentPositionTitle: "Care Coordinator", TargetPositionTitle: "Care Team Lead", TargetOrganizationName: "Care Operations",
	}
	engine.setDetail(detail)

	resp, err := client.InspectJourney(testContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
	if err != nil {
		t.Fatalf("InspectJourney: %v", err)
	}
	review := resp.GetDetail().GetPromotionReview()
	if !review.GetTargetManagerEvaluated() || review.GetTargetManagerDisplayName() != "Dana Lee" || !review.GetCycleSafe() ||
		!review.GetManagerUnchanged() || review.GetTargetPositionTitle() != "Care Team Lead" ||
		review.GetCurrentPositionTitle() != "Care Coordinator" || review.GetTargetOrganizationName() != "Care Operations" {
		t.Fatalf("promotion_review = %v, want the engine's disclosed review", review)
	}
	g := review.GetCompensationGuardrail()
	if g.GetStatus() != journeyv1.JourneyCompensationGuardrail_STATUS_AVAILABLE ||
		g.GetCurrentAnnualized() != "100000.00" || g.GetMinimumAnnualized() != "90000.00" || g.GetMaximumAnnualized() != "118000.00" ||
		g.GetPermittedIncreaseFraction() != "0.180000" || g.GetBandPosition() != "IN_BAND" || g.GetCurrency() != "USD" ||
		g.GetEffectiveDateBasis() != "2026-10-01" {
		t.Fatalf("guardrail = %v, want the engine's exact decimal text", g)
	}
	if resp.GetDetail().GetDetailDigest() != journey.DetailDigestForTest(resp.GetDetail()) {
		t.Fatal("the detail digest does not cover the review")
	}
}

func TestTodo_REV_091_02_Security(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	digits := regexp.MustCompile(`[0-9]`)

	t.Run("an unavailable guardrail reaches the wire with no figure, whatever the engine carried", func(t *testing.T) {
		leaky := rev09102Guardrail(t)
		leaky.Status = promotion.GuardrailStatusUnavailable
		leaky.Reason = promotion.GuardrailReasonNotAuthorized
		detail := fixtureDetail()
		detail.Review = &workspace.JourneyPromotionReview{Guardrail: leaky}
		engine.setDetail(detail)

		resp, err := client.InspectJourney(testContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney: %v", err)
		}
		g := resp.GetDetail().GetPromotionReview().GetCompensationGuardrail()
		if g.GetStatus() != journeyv1.JourneyCompensationGuardrail_STATUS_UNAVAILABLE || g.GetUnavailableReason() != "NOT_AUTHORIZED" {
			t.Fatalf("guardrail = %v, want UNAVAILABLE NOT_AUTHORIZED", g)
		}
		encoded, err := protojson.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if digits.Match(encoded) {
			t.Fatalf("withheld guardrail carries a figure on the wire: %s", encoded)
		}
	})

	t.Run("a manager the engine did not disclose is never named", func(t *testing.T) {
		detail := fixtureDetail()
		detail.Review = &workspace.JourneyPromotionReview{TargetManagerName: "Dana Lee", Guardrail: rev09102Guardrail(t)}
		engine.setDetail(detail)
		resp, err := client.InspectJourney(testContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney: %v", err)
		}
		review := resp.GetDetail().GetPromotionReview()
		if review.GetTargetManagerEvaluated() || review.GetTargetManagerDisplayName() != "" || review.GetCycleSafe() {
			t.Fatalf("promotion_review = %v, want no manager for an unevaluated impact", review)
		}
	})

	t.Run("no review from the engine is no review on the wire", func(t *testing.T) {
		engine.setDetail(fixtureDetail())
		resp, err := client.InspectJourney(testContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney: %v", err)
		}
		if resp.GetDetail().GetPromotionReview() != nil {
			t.Fatalf("promotion_review = %v, want unset", resp.GetDetail().GetPromotionReview())
		}
	})
}
