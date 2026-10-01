package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_UXBLIND_077_Browser(t *testing.T) {
	worker := &journeyv1.Worker{WorkerRef: "worker-y", JobCode: "PPL-HRBP3", Grade: "P4", BasePay: "132000.00", Currency: "USD", PayBasis: "ANNUAL_SALARY"}
	path := &journeyv1.PromotionPathOption{SourceJobCode: "PPL-HRBP3", SourceGrade: "P4", TargetJobCode: "PPL-DIR", TargetGrade: "M4", MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.5000"}
	rule := proposalPayRangeFor(worker, nil, path)
	if rule == nil || !rule.hasBounds() {
		t.Fatal("published pay rule did not project exact entry bounds")
	}

	form := journey.ProposalForm{Action: "/workspace/journeys/propose", Submit: "Review and submit", Fields: []journey.Field{{
		ID: FieldBase, Name: NameBase, Kind: "text", Value: "250000.00", Required: true,
	}}}
	localizeProposalForm(&form, productui.ResolveProductLocale("en-US"), true, path, rule)
	markup, err := journey.RenderToString(journey.Page{Locale: "en-US", Proposal: &journey.ProposalView{Form: form}})
	if err != nil {
		t.Fatalf("render proposal form: %v", err)
	}
	for _, want := range []string{"5.00%", "50.00%", "138,600.00", "198,000.00", "Enter an amount from"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("form/validation markup missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "121,600.00") || strings.Contains(markup, "182,400.00") {
		t.Fatalf("form rendered the unrelated role-band bounds:\n%s", markup)
	}
}

func TestTodo_UXBLIND_077_Property(t *testing.T) {
	copy := productui.ResolveProductLocale("en-US")
	for _, pack := range demoworkforce.Packs() {
		for _, edge := range pack.PromotionPaths() {
			t.Run(pack.Key+"/"+edge.SourceJobCode+"-"+edge.TargetJobCode, func(t *testing.T) {
				targetBasis := ""
				if pack.IsHourlyJob(edge.TargetJobCode) {
					targetBasis = demoworkforce.PayBasisHourly
				}
				hours := int32(0)
				if pack.IsHourlyJob(edge.SourceJobCode) != pack.IsHourlyJob(edge.TargetJobCode) {
					hours = int32(pack.StandardHours())
				}
				amount := "100000.00"
				if pack.IsHourlyJob(edge.SourceJobCode) {
					amount = "40.00"
				}
				worker := &journeyv1.Worker{JobCode: edge.SourceJobCode, Grade: edge.SourceGrade, BasePay: amount, Currency: "USD", PayBasis: pack.PayBasisFor(edge.SourceJobCode)}
				path := &journeyv1.PromotionPathOption{
					SourceJobCode: edge.SourceJobCode, SourceGrade: edge.SourceGrade,
					TargetJobCode: edge.TargetJobCode, TargetGrade: edge.TargetGrade,
					MinimumBaseIncrease: edge.MinimumBaseIncrease, MaximumBaseIncrease: edge.MaximumBaseIncrease,
					TargetPayBasis: targetBasis, AnnualizationHours: hours,
				}
				rule := proposalPayRangeFor(worker, nil, path)
				if rule == nil || !rule.hasBounds() {
					t.Fatal("published pay rule did not project exact bounds")
				}
				form := journey.ProposalForm{Fields: []journey.Field{{ID: FieldBase, Name: NameBase, Value: rule.minimum.Amount().String(), Required: true}}}
				localizeProposalForm(&form, copy, true, path, rule)
				if !strings.Contains(form.Fields[0].Help, copy.FormatMoney(rule.minimum.Amount().String(), "USD", 2)) ||
					!strings.Contains(form.Fields[0].Help, copy.FormatMoney(rule.maximum.Amount().String(), "USD", 2)) {
					t.Fatalf("form hint = %q, want the projected bounds", form.Fields[0].Help)
				}
				if form.Fields[0].Error != "" {
					t.Fatalf("an inclusive lower bound was rejected: %q", form.Fields[0].Error)
				}
				if got := proposalPayError(copy, "0.00", rule); got == "" {
					t.Fatal("validation accepted a value below the projected minimum")
				}
			})
		}
	}
}
