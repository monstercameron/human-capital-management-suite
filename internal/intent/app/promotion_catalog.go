package app

// WF-RUN-034: the promotion catalog the served workforce's aggregate
// projection records at bootstrap. It is derived from the one published
// ladder Propose's gate reads (publishedPromotionPaths) and the workforce
// options a created worker is placed from, so every published target a
// served proposal can name has a job row, OPEN positions in the organization
// units that ladder applies to, and a compensation pool to reserve against.

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
)

// PromotionPoolAmount is the compensation pool every organization unit's
// promotions reserve against in the demo catalog.
const PromotionPoolAmount = "5000000.00"

// PromotionAggregateCatalog derives the catalog [demoworkforce.SeedAggregateCatalog]
// records: a job for every published placement and ladder target, OPEN
// vacancies for every ladder target in each organization unit the ladder
// applies to (a demo edge's own unit; every corpus unit for a catalog-level
// corpus path), and one pool per organization unit.
func PromotionAggregateCatalog(recordedAt time.Time) (demoworkforce.AggregateCatalog, error) {
	options, err := workforceOptions()
	if err != nil {
		return demoworkforce.AggregateCatalog{}, err
	}
	paths, err := publishedPromotionPaths()
	if err != nil {
		return demoworkforce.AggregateCatalog{}, err
	}
	currency := options.Currency
	if currency == "" {
		currency = "USD"
	}
	catalog := demoworkforce.AggregateCatalog{
		LegalEntityName: demoworkforce.HarborCare.LegalEntity, BudgetCurrency: currency,
		BudgetAmount: PromotionPoolAmount, RecordedAt: recordedAt,
	}
	jobs := map[string]bool{}
	addJob := func(code, grade, title string) {
		if code == "" || grade == "" || jobs[code+"/"+grade] {
			return
		}
		jobs[code+"/"+grade] = true
		catalog.Jobs = append(catalog.Jobs, demoworkforce.CatalogJob{Code: code, Grade: grade, Title: title})
	}
	units := map[string]bool{}
	addUnit := func(code string) {
		if code != "" && !units[code] {
			units[code] = true
			catalog.BudgetOrgUnits = append(catalog.BudgetOrgUnits, code)
		}
	}
	for _, unit := range demoworkforce.HarborCare.Units {
		addUnit(unit.Code)
	}
	for _, unit := range options.OrgUnits {
		addUnit(unit)
	}
	vacancies := map[string]bool{}
	addVacancy := func(unit, code, grade, title string) {
		key := unit + "|" + code + "|" + grade
		if vacancies[key] {
			return
		}
		vacancies[key] = true
		catalog.Vacancies = append(catalog.Vacancies, demoworkforce.CatalogVacancy{
			OrgUnit: unit, JobCode: code, Grade: grade, Title: title, Location: demoworkforce.HarborCare.Headquarters,
		})
	}
	demoEdges := map[string]string{}
	for _, edge := range demoworkforce.PromotionPaths() {
		demoEdges["demoworkforce:"+edge.OrgUnit+":"+edge.SourceJobCode+"->"+edge.TargetJobCode] = edge.OrgUnit
	}
	for _, path := range paths {
		option := path.Option
		addJob(option.SourceJobCode, option.SourceGrade, "")
		addJob(option.TargetJobCode, option.TargetGrade, option.TargetTitle)
		if unit, ok := demoEdges[option.PathRef]; ok {
			addVacancy(unit, option.TargetJobCode, option.TargetGrade, option.TargetTitle)
			continue
		}
		for _, unit := range options.OrgUnits {
			addVacancy(unit, option.TargetJobCode, option.TargetGrade, option.TargetTitle)
		}
	}
	for _, placement := range options.Placements {
		addJob(placement.JobCode, placement.Grade, "")
	}
	if len(catalog.Vacancies) == 0 {
		return demoworkforce.AggregateCatalog{}, fmt.Errorf("app: the published ladder yields no promotion vacancies")
	}
	return catalog, nil
}
