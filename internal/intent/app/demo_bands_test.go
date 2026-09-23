package app

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestDemoBandsCoverEveryPublishedPromotionTargetAndStayTenantScoped(t *testing.T) {
	inputs, err := NewCorpusInputs()
	if err != nil {
		t.Fatal(err)
	}
	date, err := values.ParseLocalDate("2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range demoworkforce.PromotionPaths() {
		for _, zone := range demoworkforce.PayZones() {
			q := rewards.BandQuery{Tenant: values.TenantId(demoworkforce.CompanyKey),
				JobCode: edge.TargetJobCode, Grade: edge.TargetGrade, PayZone: zone,
				Currency: "USD", AsOf: date}
			record, err := inputs.Bands().LookupBand(context.Background(), q)
			if err != nil {
				t.Fatalf("%s/%s/%s: %v", edge.TargetJobCode, edge.TargetGrade, zone, err)
			}
			if record.CatalogVersion != demoworkforce.PayBandPolicyVersion || !record.Blocking {
				t.Fatalf("%s: unpinned or nonblocking demo band: %+v", edge.TargetJobCode, record)
			}
			q.Tenant = values.TenantId("another-tenant")
			if _, err := inputs.Bands().LookupBand(context.Background(), q); !errors.Is(err, rewards.ErrBandNotFound) {
				t.Fatalf("demo band leaked to another tenant for %s: %v", edge.TargetJobCode, err)
			}
		}
	}
	// The fixed corpus remains available even when the runtime's tenant is
	// HarborCare; this overlay must not erase Jane's conformance references.
	scopes, err := fixtures.BandScopes()
	if err != nil || len(scopes) == 0 {
		t.Fatalf("fixture scopes: %v", err)
	}
	scope := scopes[0]
	q := rewards.BandQuery{Tenant: values.TenantId(demoworkforce.CompanyKey), JobCode: scope.JobCode,
		Grade: scope.Grade, PayZone: scope.PayZone, Currency: scope.Currency, AsOf: date}
	if _, err := inputs.Bands().LookupBand(context.Background(), q); err != nil {
		t.Fatalf("conformance band hidden by demo overlay: %v", err)
	}
}
