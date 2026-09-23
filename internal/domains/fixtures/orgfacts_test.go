package fixtures_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func orgInstant(t *testing.T) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, "2026-05-15T12:00:00Z")
	if err != nil {
		t.Fatalf("parse instant: %v", err)
	}
	return values.NewInstant(parsed.UTC())
}

func orgQuery(t *testing.T, worker values.EntityRef) org.WorkerFactsQuery {
	t.Helper()
	known, err := values.NewKnownAt(orgInstant(t))
	if err != nil {
		t.Fatalf("NewKnownAt: %v", err)
	}
	return org.WorkerFactsQuery{
		Tenant:  fixtures.Tenant,
		Worker:  worker,
		AsOf:    orgInstant(t),
		KnownAt: known,
	}
}

func TestMemoryOrgFacts(t *testing.T) {
	reader, err := fixtures.NewMemoryOrgFacts()
	if err != nil {
		t.Fatalf("NewMemoryOrgFacts: %v", err)
	}
	ctx := context.Background()

	omar, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("WorkerRef: %v", err)
	}
	set, err := reader.WorkerFactsAt(ctx, orgQuery(t, omar))
	if err != nil {
		t.Fatalf("WorkerFactsAt: %v", err)
	}
	if !set.Exists || len(set.Relationships) != 1 {
		t.Fatalf("set=%+v", set)
	}
	rel := set.Relationships[0]
	if rel.RelationshipID != "rel_mgr_1002" || rel.Type != org.RelationshipDirectManager {
		t.Fatalf("rel=%+v", rel)
	}
	noor, err := fixtures.WorkerRef("noor-haddad")
	if err != nil {
		t.Fatalf("WorkerRef: %v", err)
	}
	if rel.Manager != noor {
		t.Fatalf("manager=%s, want %s", rel.Manager, noor)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// A worker the graph does not name is unknown, which ends a chain
	// cleanly rather than failing the read.
	unknown := values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker, Id: "99999999-9999-4999-8999-999999999999"}
	absent, err := reader.WorkerFactsAt(ctx, orgQuery(t, unknown))
	if err != nil {
		t.Fatalf("WorkerFactsAt unknown: %v", err)
	}
	if absent.Exists || len(absent.Relationships) != 0 {
		t.Fatalf("absent=%+v", absent)
	}

	// The whole graph answers at one watermark and policy version, or the
	// resolver reports a disagreeing graph.
	chair := values.EntityRef{Tenant: fixtures.Tenant, Kind: people.KindWorker, Id: "88888888-8888-4888-8888-888888888888"}
	top, err := reader.WorkerFactsAt(ctx, orgQuery(t, chair))
	if err != nil {
		t.Fatalf("WorkerFactsAt sentinel: %v", err)
	}
	if top.Exists {
		t.Fatalf("sentinel=%+v", top)
	}
	if !set.Watermark.Equal(top.Watermark) || set.PolicyVersion != top.PolicyVersion {
		t.Fatal("graph watermark or policy version is not uniform")
	}

	// The top of the corpus hierarchy reports to the external chair: one
	// hop to an external party, then the chain ends.
	topSet, err := reader.WorkerFactsAt(ctx, orgQuery(t, noor))
	if err != nil {
		t.Fatalf("WorkerFactsAt noor: %v", err)
	}
	if !topSet.Exists || len(topSet.Relationships) != 1 || topSet.Relationships[0].Manager != chair {
		t.Fatalf("noor=%+v", topSet)
	}

	if _, err := reader.WorkerFactsAt(ctx, org.WorkerFactsQuery{}); err == nil {
		t.Fatal("empty query succeeded")
	}
}
