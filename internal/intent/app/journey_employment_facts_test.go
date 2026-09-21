package app

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
)

// The employment facts the object page shows survive the engine's projection
// of a durable row onto a listing row. This hop is the one place a stored
// column can be dropped without any test noticing: the row still lists, the
// page just reports the fact as unreported, which is the exact defect this
// lane removed.
func TestCreatedWorkerSummaryCarriesTheEmploymentFacts(t *testing.T) {
	row := createdRowFixture()
	row.EmploymentType = workforce.EmploymentTypeFixedTerm
	row.TimeType = workforce.TimeTypePartTime
	row.FTE = "0.6000"
	row.Company = "HarborCare Health Services, Inc."
	row.BusinessUnit = "Care Operations"
	row.CostCenter = "CC-2200 Care Coordination"
	row.WorkArrangement = workforce.WorkArrangementOnSite

	got := createdWorkerSummary(row)
	for _, tc := range []struct{ name, got, want string }{
		{"employment type", got.EmploymentType, row.EmploymentType},
		{"time type", got.TimeType, row.TimeType},
		{"company", got.Company, row.Company},
		{"business unit", got.BusinessUnit, row.BusinessUnit},
		{"cost center", got.CostCenter, row.CostCenter},
		{"work arrangement", got.WorkArrangement, row.WorkArrangement},
		{"pay basis", got.PayBasis, row.PayBasis},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want the row's own %q", tc.name, tc.got, tc.want)
		}
	}
}

// A row asserting none of them projects none of them. The listing must not
// manufacture a value the record does not carry, because the page reads an
// empty projection as "unreported" and a filled one as fact.
func TestCreatedWorkerSummaryInventsNoEmploymentFacts(t *testing.T) {
	row := createdRowFixture()
	row.EmploymentType, row.TimeType = "", ""
	row.Company, row.BusinessUnit, row.CostCenter, row.WorkArrangement = "", "", "", ""

	got := createdWorkerSummary(row)
	for _, tc := range []struct{ name, got string }{
		{"employment type", got.EmploymentType},
		{"time type", got.TimeType},
		{"company", got.Company},
		{"business unit", got.BusinessUnit},
		{"cost center", got.CostCenter},
		{"work arrangement", got.WorkArrangement},
	} {
		if tc.got != "" {
			t.Errorf("%s = %q, want empty for a row that asserts none", tc.name, tc.got)
		}
	}
}

// A corpus worker asserts none of these facts either, which is what keeps the
// object page's missing-field disclosure meaningful rather than decorative.
func TestCorpusWorkersAssertNoEmploymentFacts(t *testing.T) {
	corpus, err := corpusWorkers()
	if err != nil {
		t.Fatalf("corpus workers: %v", err)
	}
	if len(corpus) == 0 {
		t.Fatal("the corpus population is empty")
	}
	for _, worker := range corpus {
		if worker.EmploymentType != "" || worker.TimeType != "" || worker.Company != "" ||
			worker.BusinessUnit != "" || worker.CostCenter != "" || worker.WorkArrangement != "" {
			t.Errorf("corpus worker %s claims employment facts the corpus does not record: %+v", worker.WorkerRef, worker)
		}
	}
}
