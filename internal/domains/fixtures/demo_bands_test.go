package fixtures

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// demoBandAsOf is a business date inside every demo band's effective window.
func demoBandAsOf(t *testing.T) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate("2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	return date
}

// lookupDemoBand asks the served catalog for one placement.
func lookupDemoBand(t *testing.T, c *MemoryBandCatalog, jobCode, grade, payZone, currency string, asOf values.LocalDate) (rewards.BandRecord, error) {
	t.Helper()
	return c.LookupBand(context.Background(), rewards.BandQuery{
		Tenant: Tenant, JobCode: jobCode, Grade: grade, PayZone: payZone, Currency: currency, AsOf: asOf,
	})
}

// TestDemoBandsPriceEveryDemoRole pins demo-bands.json to the demo workforce
// it prices: every seeded worker's own placement resolves to a band whose
// midpoint is that role's seeded base pay with the declared 80%-120% range,
// and every published ladder edge's target resolves in every pay zone a
// worker holding its source role sits in. A role added to
// internal/data/demoworkforce without a band fails here, not at simulation
// time as promotion.pay_band_not_found.
func TestDemoBandsPriceEveryDemoRole(t *testing.T) {
	catalog, err := NewMemoryBandCatalog()
	if err != nil {
		t.Fatal(err)
	}
	employees, err := demoworkforce.Plan(uuid.MustParse("5bd94389-b627-4f25-8755-68e44d53fddd"))
	if err != nil {
		t.Fatalf("demoworkforce.Plan: %v", err)
	}
	if len(employees) == 0 {
		t.Fatal("the demo plan has no workers; the test proves nothing")
	}
	asOf := demoBandAsOf(t)
	holderZones := map[string]map[string]string{} // job/grade -> pay zone -> currency
	for _, employee := range employees {
		row := employee.Row
		record, err := lookupDemoBand(t, catalog, row.JobCode, row.Grade, row.PayZone, row.Currency, asOf)
		if err != nil {
			t.Errorf("seeded worker %s on %s/%s/%s in %s: %v", row.WorkerKey, row.JobCode, row.Grade, row.PayZone, row.Currency, err)
			continue
		}
		if record.CatalogVersion != demoBands.CatalogVersion {
			t.Errorf("%s priced by catalog %q, want the demo catalog %q", row.WorkerKey, record.CatalogVersion, demoBands.CatalogVersion)
		}
		mid, _ := new(big.Rat).SetString(row.BasePay)
		for _, bound := range []struct {
			name  string
			got   values.Money
			ratio *big.Rat
		}{
			{"minimum", record.Band.Minimum, big.NewRat(4, 5)},
			{"midpoint", record.Band.Midpoint, big.NewRat(1, 1)},
			{"maximum", record.Band.Maximum, big.NewRat(6, 5)},
		} {
			want := new(big.Rat).Mul(mid, bound.ratio).FloatString(2)
			if got := bound.got.Amount().String(); got != want {
				t.Errorf("%s/%s/%s %s = %s, want %s (the seeded base pay %s times %s)",
					row.JobCode, row.Grade, row.PayZone, bound.name, got, want, row.BasePay, bound.ratio.FloatString(2))
			}
		}
		key := row.JobCode + "/" + row.Grade
		if holderZones[key] == nil {
			holderZones[key] = map[string]string{}
		}
		holderZones[key][row.PayZone] = row.Currency
	}

	edges := demoworkforce.PromotionPaths()
	if len(edges) == 0 {
		t.Fatal("the demo company publishes no ladder edge; the test proves nothing about targets")
	}
	for _, edge := range edges {
		zones := holderZones[edge.SourceJobCode+"/"+edge.SourceGrade]
		if len(zones) == 0 {
			t.Errorf("edge %s/%s -> %s/%s has no seeded source holder", edge.SourceJobCode, edge.SourceGrade, edge.TargetJobCode, edge.TargetGrade)
		}
		for zone, currency := range zones {
			if _, err := lookupDemoBand(t, catalog, edge.TargetJobCode, edge.TargetGrade, zone, currency, asOf); err != nil {
				t.Errorf("edge %s/%s -> %s/%s in %s: %v", edge.SourceJobCode, edge.SourceGrade, edge.TargetJobCode, edge.TargetGrade, zone, err)
			}
		}
	}
}

// TestDemoBandsAreEffectiveDated proves the demo catalog's effective window is
// enforced by the lookup rather than merely recorded: a date before a band's
// effective_from finds no band, and the corpus bands.json bands, which declare
// no window, still answer on that same date.
func TestDemoBandsAreEffectiveDated(t *testing.T) {
	catalog, err := NewMemoryBandCatalog()
	if err != nil {
		t.Fatal(err)
	}
	demo := demoBands.Bands[0]
	if demo.EffectiveFrom == "" {
		t.Fatal("the first demo band declares no effective_from; the fixture lacks the window under test")
	}
	from, err := values.ParseLocalDate(demo.EffectiveFrom)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lookupDemoBand(t, catalog, demo.JobCode, demo.Grade, demo.PayZone, demo.Currency, from); err != nil {
		t.Fatalf("lookup on effective_from %s: %v", from, err)
	}
	before := from.AddDays(-1)
	if _, err := lookupDemoBand(t, catalog, demo.JobCode, demo.Grade, demo.PayZone, demo.Currency, before); !errors.Is(err, rewards.ErrBandNotFound) {
		t.Fatalf("lookup the day before effective_from = %v, want ErrBandNotFound", err)
	}

	corpus := bands.Bands[0]
	record, err := lookupDemoBand(t, catalog, corpus.JobCode, corpus.Grade, corpus.PayZone, corpus.Currency, before)
	if err != nil {
		t.Fatalf("corpus band lookup: %v", err)
	}
	if record.CatalogVersion != bands.CatalogVersion {
		t.Errorf("corpus band reports catalog %q, want %q", record.CatalogVersion, bands.CatalogVersion)
	}

	closed := bandRecord{ID: "closed", EffectiveFrom: "2026-01-01", EffectiveTo: "2026-07-01"}
	for date, want := range map[string]bool{"2025-12-31": false, "2026-01-01": true, "2026-06-30": true, "2026-07-01": false} {
		on, err := values.ParseLocalDate(date)
		if err != nil {
			t.Fatal(err)
		}
		got, err := closed.effectiveOn(on)
		if err != nil || got != want {
			t.Errorf("effectiveOn(%s) = %v, %v; want %v", date, got, err, want)
		}
	}
	if _, err := (bandRecord{ID: "bad", EffectiveFrom: "not-a-date"}).effectiveOn(from); err == nil {
		t.Error("a malformed effective_from was accepted")
	}
	if _, err := (bandRecord{ID: "bad", EffectiveTo: "not-a-date"}).effectiveOn(from); err == nil {
		t.Error("a malformed effective_to was accepted")
	}
}

// TestMergeBandCatalogsRefusesAmbiguousScopes proves the merged catalog can
// never answer one placement from two bands: a repeated id or a second band
// for the same scope on a shared date is refused, while back-to-back windows
// for one scope are admitted.
func TestMergeBandCatalogsRefusesAmbiguousScopes(t *testing.T) {
	band := func(id, from, to string) bandRecord {
		return bandRecord{ID: id, JobCode: "J", Grade: "G", PayZone: "Z", Currency: "USD", EffectiveFrom: from, EffectiveTo: to}
	}
	for name, tc := range map[string]struct {
		files   []*bandFile
		refused string
	}{
		"duplicate id":         {files: []*bandFile{{Bands: []bandRecord{band("a", "", "")}}, {Bands: []bandRecord{{ID: "a", JobCode: "K"}}}}, refused: "published twice"},
		"unbounded overlap":    {files: []*bandFile{{Bands: []bandRecord{band("a", "", "")}}, {Bands: []bandRecord{band("b", "2026-01-01", "")}}}, refused: "same scope"},
		"shared date":          {files: []*bandFile{{Bands: []bandRecord{band("a", "2026-01-01", "2026-07-01"), band("b", "2026-06-30", "")}}}, refused: "same scope"},
		"back to back windows": {files: []*bandFile{{Bands: []bandRecord{band("a", "2026-01-01", "2026-07-01"), band("b", "2026-07-01", "")}}}},
	} {
		t.Run(name, func(t *testing.T) {
			merged, err := mergeBandCatalogs(tc.files...)
			if tc.refused == "" {
				if err != nil || len(merged) != 2 {
					t.Fatalf("mergeBandCatalogs = %d bands, %v; want both admitted", len(merged), err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.refused) {
				t.Fatalf("mergeBandCatalogs = %v, want a refusal naming %q", err, tc.refused)
			}
		})
	}
}
