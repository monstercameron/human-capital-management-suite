package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_AGENTP_018_ServeFactoryRequiresVerifiedHuman(t *testing.T) {
	factory := newPersonaAdminServeFactory(&readOnlyPersonaAdminCatalog{})
	if factory == nil {
		t.Fatal("factory is nil for supplied client")
	}
	if got := factory.ClientForRequest(context.Background()); got != nil {
		t.Fatal("unverified context received persona admin client")
	}
	if got := newPersonaAdminServeFactory(nil); got != nil {
		t.Fatal("nil client produced an available factory")
	}
}

func TestTodo_AGENTP_018_ServeFactoryBindsVerifiedRequest(t *testing.T) {
	ctx, _ := catalogContext(t)
	want := &personaAdminRouteClientFake{}
	factory := newPersonaAdminServeFactory(want)
	bound := factory.ClientForRequest(ctx)
	if bound == nil {
		t.Fatal("verified context did not receive a request-bound client")
	}
	if _, err := bound.Snapshot(context.Background(), productui.PersonaAdminSnapshotRequest{TenantID: "forged", Principal: "forged"}); err != nil {
		t.Fatal(err)
	}
	if want.snapshot.TenantID != "tenant-a" || want.snapshot.Principal != "user-a" {
		t.Fatalf("request fields were not replaced by verified principal: %+v", want.snapshot)
	}
	var _ interface {
		ClientForRequest(context.Context) productui.PersonaAdminClient
	} = factory
}

func TestTodo_AGENTP_018_ServeWiringReportsBoundedMissingDependencyStage(t *testing.T) {
	if _, err := composePersonaServeWiring(nil, nil, nil, nil); err == nil {
		t.Fatal("missing core pool composed persona serve wiring")
	} else if got := personaServeWiringStage(err); got != "core_pool_missing" {
		t.Fatalf("failure stage = %q, want core_pool_missing", got)
	}
}

func TestTodo_AGENTP_018_ServeWiringDefaultsOptionalClock(t *testing.T) {
	if got := personaServeClock(nil); got == nil {
		t.Fatal("nil serve clock remained nil for the current-worker directory")
	}
	want := func() time.Time { return time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC) }
	if got := personaServeClock(want)(); !got.Equal(want()) {
		t.Fatalf("configured clock = %s, want %s", got, want())
	}
}
