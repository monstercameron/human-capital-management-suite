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
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
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
	return PromotionAggregateCatalogFrom(recordedAt, demoworkforce.PromotionPaths())
}

// PromotionAggregateCatalogForPack derives the catalog one demo company's
// bootstrap records, from that company's own authored ladder, legal entity,
// units and headquarters.
func PromotionAggregateCatalogForPack(pack *demoworkforce.Pack, recordedAt time.Time) (demoworkforce.AggregateCatalog, error) {
	if pack == nil {
		return demoworkforce.AggregateCatalog{}, fmt.Errorf("app: a demo company is required")
	}
	return promotionAggregateCatalogFrom(pack, recordedAt, pack.PromotionPaths())
}

// PromotionAggregateCatalogFrom derives the same catalog against a given
// company ladder. A job publishes several targets now, so every one of them
// gets its own job row and its own OPEN vacancies: a target a client can pick
// but cannot be placed into is an option the commit always refuses.
func PromotionAggregateCatalogFrom(recordedAt time.Time, ladder []demoworkforce.PromotionPathEdge) (demoworkforce.AggregateCatalog, error) {
	return promotionAggregateCatalogFrom(demoworkforce.HarborCarePack, recordedAt, ladder)
}

func promotionAggregateCatalogFrom(pack *demoworkforce.Pack, recordedAt time.Time, ladder []demoworkforce.PromotionPathEdge) (demoworkforce.AggregateCatalog, error) {
	company := pack.Company
	options, err := workforceOptionsFrom(ladder)
	if err != nil {
		return demoworkforce.AggregateCatalog{}, err
	}
	paths, err := publishedPromotionPathsFrom(ladder)
	if err != nil {
		return demoworkforce.AggregateCatalog{}, err
	}
	corpusHome, err := corpusJobUnits()
	if err != nil {
		return demoworkforce.AggregateCatalog{}, err
	}
	currency := options.Currency
	if currency == "" {
		currency = "USD"
	}
	catalog := demoworkforce.AggregateCatalog{
		LegalEntityName: company.LegalEntity, BudgetCurrency: currency,
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
	for _, unit := range company.Units {
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
			OrgUnit: unit, JobCode: code, Grade: grade, Title: title, Location: company.Headquarters,
		})
	}
	// A vacancy opens where its JOB belongs, not where the person reaching
	// for it works. Keying it by the source's unit gave every unit with
	// somebody pointing at a target its own copy of that target's
	// requisition, so the propose form offered a Director of Product seat
	// inside Engineering Platform -- an option that reads as broken and
	// would have committed a promotion into the wrong organization unit.
	//
	// [vacancyUnitFor] answers where that is: the unit this company's own
	// catalog publishes the job in, the corpus unit a corpus job is held in,
	// and only failing both the source's own unit, so a path never loses the
	// open seat that makes it proposable.
	for _, path := range paths {
		option := path.Option
		addJob(option.SourceJobCode, option.SourceGrade, "")
		addJob(option.TargetJobCode, option.TargetGrade, option.TargetTitle)
		if unit := vacancyUnitFor(option.TargetJobCode, corpusHome); unit != "" {
			addVacancy(unit, option.TargetJobCode, option.TargetGrade, option.TargetTitle)
			continue
		}
		// A target no catalog places: open it wherever the ladder points at
		// it from, which is the only unit anybody could be promoted into it
		// from anyway.
		for _, edge := range ladder {
			if edge.TargetJobCode == option.TargetJobCode && edge.TargetGrade == option.TargetGrade {
				addVacancy(edge.OrgUnit, option.TargetJobCode, option.TargetGrade, option.TargetTitle)
			}
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

// vacancyUnitFor answers the organization unit a seat for jobCode belongs to:
// the unit this company's catalog publishes it in, or the corpus unit a
// corpus job is held in. It reports "" for a job neither catalog places, and
// the caller then falls back to the units the ladder points at it from.
func vacancyUnitFor(jobCode string, corpusHome map[string]string) string {
	if unit := demoworkforce.JobHomeUnit(jobCode); unit != "" {
		return unit
	}
	return corpusHome[jobCode]
}

// corpusJobUnits maps each fixed-corpus job code to the organization unit a
// corpus worker holds it in. The corpus is the only record of where those
// jobs sit, and a seat for one belongs beside its holders.
func corpusJobUnits() (map[string]string, error) {
	profiles, err := fixtures.Workers()
	if err != nil {
		return nil, fmt.Errorf("app: read the worker corpus for vacancy placement: %w", err)
	}
	units := make(map[string]string, len(profiles))
	for _, profile := range profiles {
		if profile.JobCode == "" || profile.OrgUnit == "" {
			continue
		}
		// First holder wins, and the corpus is declared in a fixed order, so
		// the choice does not depend on map iteration.
		if _, placed := units[profile.JobCode]; !placed {
			units[profile.JobCode] = profile.OrgUnit
		}
	}
	return units, nil
}
