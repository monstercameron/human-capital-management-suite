package journeyclient

import (
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestHourlyPayLinesReadAsRates(t *testing.T) {
	if got := payLineBasisLocale("", "USD", "28.00", "34.50", payBasisHourly, payBasisHourly); !strings.Contains(got, "28.00/hr") || !strings.Contains(got, "34.50/hr") || !strings.Contains(got, "%") {
		t.Fatalf("hourly pay line = %q", got)
	}
	if got := payLineBasisLocale("", "USD", "40.00", "108000.00", payBasisHourly, ""); !strings.Contains(got, "40.00/hr") || strings.Contains(got, "108,000.00/hr") || strings.Contains(got, "%") {
		t.Fatalf("rate-to-salary pay line = %q", got)
	}
	if got, want := payLineBasisLocale("", "USD", "93000.00", "98000.00", "", ""), payLineLocale("", "USD", "93000.00", "98000.00"); got != want {
		t.Fatalf("a salaried pay line changed: %q != %q", got, want)
	}
	if got := payLineBasisLocale("de-DE", "USD", "28.00", "34.50", payBasisHourly, payBasisHourly); !strings.Contains(got, "/Std.") {
		t.Fatalf("German hourly pay line = %q", got)
	}
	if got := payLineBasisLocale("ar", "USD", "28.00", "34.50", payBasisHourly, payBasisHourly); !strings.Contains(got, "/ساعة") {
		t.Fatalf("Arabic hourly pay line = %q", got)
	}
}

func TestHourlyProposalFormAsksForARate(t *testing.T) {
	worker := &journeyv1.Worker{BasePay: "34.50", Currency: "USD", PayBasis: payBasisHourly}
	path := &journeyv1.PromotionPathOption{TargetJobCode: "IR-FMN", MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.5000", TargetPayBasis: payBasisHourly}
	payRange := proposalPayRangeFor(worker, nil, path)
	if payRange == nil || payRange.minimum.Amount().String() != "36.23" || payRange.maximum.Amount().String() != "51.75" {
		t.Fatalf("hourly range = %+v", payRange)
	}
	form := journey.ProposalForm{Fields: []journey.Field{{ID: FieldBase}}}
	copy := productui.ResolveProductLocale("")
	localizeProposalForm(&form, copy, true, path, payRange)
	if form.Fields[0].Suffix != "per hour" || !strings.Contains(form.Fields[0].Help, "per hour") {
		t.Fatalf("hourly base field = %+v", form.Fields[0])
	}

	// A foreman's rate annualizes before a superintendent's salary range is
	// computed from it.
	foreman := &journeyv1.Worker{BasePay: "40.00", Currency: "USD", PayBasis: payBasisHourly}
	cross := &journeyv1.PromotionPathOption{TargetJobCode: "IR-SUP", MinimumBaseIncrease: "0.0500", MaximumBaseIncrease: "0.5000", AnnualizationHours: 2080}
	annual := proposalPayRangeFor(foreman, nil, cross)
	if annual == nil || annual.minimum.Amount().String() != "87360.00" || annual.maximum.Amount().String() != "124800.00" {
		t.Fatalf("annualized range = %+v", annual)
	}
	salaried := journey.ProposalForm{Fields: []journey.Field{{ID: FieldBase}}}
	localizeProposalForm(&salaried, copy, true, cross, annual)
	if salaried.Fields[0].Suffix != "per year" {
		t.Fatalf("a salaried target reads %q", salaried.Fields[0].Suffix)
	}
}
