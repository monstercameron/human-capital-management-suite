package app

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func ironridgeEdge(t *testing.T, source, target string) demoworkforce.PromotionPathEdge {
	t.Helper()
	for _, edge := range demoworkforce.IronridgePack.PromotionPaths() {
		if edge.SourceJobCode == source && edge.TargetJobCode == target {
			return edge
		}
	}
	t.Fatalf("no Ironridge edge %s -> %s", source, target)
	return demoworkforce.PromotionPathEdge{}
}

// TestHourlyPromotionValidatesOnTheRate is P2's server-side pay rule: an
// hourly move (journeyman to foreman) is checked on the rate against the
// ladder's 5%-50% bounds, and a move across bases (foreman to superintendent)
// annualizes the rate first.
func TestHourlyPromotionValidatesOnTheRate(t *testing.T) {
	ladder := demoworkforce.IronridgePack.PromotionPaths()
	journeyman := journeyCurrent{jobCode: "IR-JCP", grade: "C3", orgUnit: "field-operations"}
	rate := journeyBaselineFacts{currentBase: "34.50", currency: "USD"}
	propose := func(current journeyCurrent, baseline journeyBaselineFacts, job, grade, base string) error {
		return validatePublishedPromotionPathFrom(ladder, current, workspace.ProposalInput{TargetJobCode: job, TargetGrade: grade, ProposedBase: base}, baseline)
	}
	if err := propose(journeyman, rate, "IR-FMN", "C4", "40.00"); err != nil {
		t.Fatalf("$34.50/hr -> $40.00/hr refused: %v", err)
	}
	var input *workspace.JourneyInputError
	if err := propose(journeyman, rate, "IR-FMN", "C4", "55.00"); !errors.As(err, &input) || input.PayRange == nil ||
		input.PayRange.Minimum.Amount().String() != "36.23" || input.PayRange.Maximum.Amount().String() != "51.75" {
		t.Fatalf("an out-of-range rate = %v, want the hourly 36.23-51.75 range", err)
	}
	if err := propose(journeyman, rate, "IR-FMN", "C4", "40000.00"); err == nil {
		t.Fatal("an annual figure was accepted as a foreman's hourly rate")
	}

	foreman := journeyCurrent{jobCode: "IR-FMN", grade: "C4", orgUnit: "field-operations"}
	if err := propose(foreman, journeyBaselineFacts{currentBase: "40.00", currency: "USD"}, "IR-SUP", "S3", "108000.00"); err != nil {
		t.Fatalf("$40.00/hr -> $108,000/yr refused: %v", err)
	}
	if err := propose(foreman, journeyBaselineFacts{currentBase: "40.00", currency: "USD"}, "IR-SUP", "S3", "150000.00"); !errors.As(err, &input) ||
		input.PayRange.Minimum.Amount().String() != "87360.00" || input.PayRange.Maximum.Amount().String() != "124800.00" {
		t.Fatalf("a salary beyond the annualized range = %v", err)
	}
}

func TestPayBasisTravelsOnPathsAndSummaries(t *testing.T) {
	cross := ironridgeEdge(t, "IR-FMN", "IR-SUP")
	if edgeTargetPayBasis(cross) != "" || edgeAnnualizationHours(cross) != 2080 {
		t.Fatalf("foreman -> superintendent basis %q hours %d", edgeTargetPayBasis(cross), edgeAnnualizationHours(cross))
	}
	craft := ironridgeEdge(t, "IR-JCP", "IR-FMN")
	if edgeTargetPayBasis(craft) != "HOURLY_RATE" || edgeAnnualizationHours(craft) != 0 {
		t.Fatalf("journeyman -> foreman basis %q hours %d", edgeTargetPayBasis(craft), edgeAnnualizationHours(craft))
	}
	paths, err := publishedPromotionPathsFrom([]demoworkforce.PromotionPathEdge{craft, cross})
	if err != nil {
		t.Fatal(err)
	}
	last := paths[len(paths)-1].Option
	if paths[len(paths)-2].Option.TargetPayBasis != "HOURLY_RATE" || last.AnnualizationHours != 2080 || last.TargetPayBasis != "" {
		t.Fatalf("published options lost their basis: %+v", last)
	}
	for _, path := range paths[:len(paths)-2] {
		if path.Option.TargetPayBasis != "" || path.Option.AnnualizationHours != 0 {
			t.Fatalf("a corpus path acquired a basis: %+v", path.Option)
		}
	}
	for _, harbor := range demoworkforce.PromotionPaths() {
		if edgeTargetPayBasis(harbor) != "" || edgeAnnualizationHours(harbor) != 0 {
			t.Fatalf("HarborCare edge %+v is not salaried", harbor)
		}
	}
	if jobPayBasis("IR-APC") != "HOURLY_RATE" || jobPayBasis("IR-PM") != "" || jobPayBasis("ENG-SWE3") != "" {
		t.Fatal("summary pay basis by job is wrong")
	}
	same, err := baselineInTargetBasis(journeyBaselineFacts{currentBase: "34.50"}, craft)
	if err != nil || same.currentBase != "34.50" {
		t.Fatalf("a same-basis baseline changed: %+v %v", same, err)
	}
}
