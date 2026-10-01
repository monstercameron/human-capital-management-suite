package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/releaseevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestTodo_ALIGN_056_Served(t *testing.T) {
	surface := NewServedAlignmentSurface()
	if surface.NewReleaseJournal == nil || surface.PlanRollback == nil || surface.EmbeddedHistory == nil {
		t.Fatal("served alignment surface omitted rollback contracts")
	}

	journal := surface.NewReleaseJournal()
	for _, release := range []releaseevidence.Release{
		{Slice: "promotion", Version: "v1", BinaryRevision: "rev-1", SchemaDigest: "sha256:schema-1", SchemaVersion: 1, Definitions: map[string]string{"page": "sha256:page-1"}, EvidenceRefs: []string{"evidence:v1"}, RecordedBy: "operator:release-captain", RecordedAt: time.Unix(1, 0).UTC()},
		{Slice: "promotion", Version: "v2", BinaryRevision: "rev-2", SchemaDigest: "sha256:schema-2", SchemaVersion: 3, Definitions: map[string]string{"page": "sha256:page-2"}, EvidenceRefs: []string{"evidence:v2"}, RecordedBy: "operator:release-captain", RecordedAt: time.Unix(2, 0).UTC()},
	} {
		if _, err := journal.Record(release); err != nil {
			t.Fatalf("record release %s: %v", release.Version, err)
		}
	}
	plan, err := surface.PlanRollback(journal, "promotion", "v2", "v1", "operator:release-captain", []releaseevidence.Migration{
		{Version: 1, Reversible: true}, {Version: 2, Reversible: true}, {Version: 3, Reversible: true},
	})
	if err != nil {
		t.Fatalf("served rollback plan: %v", err)
	}
	if plan.FromSchema != 3 || plan.ToSchema != 1 || len(plan.Steps) != 2 {
		t.Fatalf("served rollback plan = %+v, want schemas 3 -> 1 with two steps", plan)
	}
}

func servedAlignmentEnvelope(t *testing.T) conformance.QueryEnvelope {
	t.Helper()
	at := values.NewInstant(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	return conformance.QueryEnvelope{
		ContractVersion: conformance.Version(), Tenant: "acme", Purpose: "compensation_review", EffectiveAt: at,
		SchemaVersion: "schema.worker.v3", DefinitionVersion: "page.people.v2",
		Authorization: conformance.AuthorizationMetadata{Effect: authz.EffectAllow, PolicyVersion: authz.PolicyVersion, InputsDigest: "sha256:inputs", EvidenceID: "evidence:served"},
		Freshness:     conformance.Freshness{SourceVersion: "source.v1", ProjectionVersion: "projection.v1", SourceSequence: 1, ProjectionSequence: 1, ObservedAt: at},
	}
}

func TestTodo_ALIGN_057_Served(t *testing.T) {
	surface := NewServedAlignmentSurface()
	left, err := surface.ProjectSurface(conformance.SurfaceSSR, servedAlignmentEnvelope(t))
	if err != nil {
		t.Fatalf("served SSR projection: %v", err)
	}
	right, err := surface.ProjectSurface(conformance.SurfaceEnhancedBrowser, left.Envelope)
	if err != nil {
		t.Fatalf("served enhanced-browser projection: %v", err)
	}
	if err := surface.CheckNoninterference([]conformance.SurfaceProjection{left, right}, nil); err != nil {
		t.Fatalf("served semantic parity: %v", err)
	}
}

func TestTodo_ALIGN_058_Served(t *testing.T) {
	surface := NewServedAlignmentSurface()
	if surface.QueryVersion == nil || surface.ExecuteQuery == nil || surface.ProjectSurface == nil {
		t.Fatal("served alignment surface omitted query contracts")
	}
	if surface.QueryVersion() != conformance.Version() {
		t.Fatalf("served query version = %d, want %d", surface.QueryVersion(), conformance.Version())
	}
	if _, err := surface.ExecuteQuery(context.Background(), conformance.QueryRequest{}); !errors.Is(err, conformance.ErrInvalidQuery) {
		t.Fatalf("served invalid query = %v, want ErrInvalidQuery", err)
	}
}

func TestTodo_ALIGN_059_Served(t *testing.T) {
	surface := NewServedAlignmentSurface()
	if surface.AuthorizeInvalidation == nil || surface.AssertAllParity == nil {
		t.Fatal("served alignment surface omitted noninterference contracts")
	}
	at := values.NewInstant(time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC))
	if _, err := surface.AuthorizeInvalidation(authz.RepositoryScope{}, at, "projection.v1", 1, nil); !errors.Is(err, conformance.ErrInvalidationRejected) {
		t.Fatalf("served unevaluated invalidation = %v, want ErrInvalidationRejected", err)
	}
	if err := surface.AssertAllParity(servedAlignmentEnvelope(t), nil); err != nil {
		t.Fatalf("served all-surface noninterference: %v", err)
	}
}

func TestServedAlignmentAppSurface(t *testing.T) {
	if (&App{}).Alignment().ExecuteQuery == nil || (&App{}).Alignment().PlanRollback == nil {
		t.Fatal("composed app omitted alignment capabilities")
	}
	var nilApp *App
	if nilApp.Alignment().ExecuteQuery != nil {
		t.Fatal("nil app returned live alignment capabilities")
	}
}
