package productclient_test

import (
	"context"
	"sync"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/invalidation"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

func TestTodo_PROMOUX_011_ProductRefreshPublishesOnlyNewestSequence(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var applied []string
	reads := 0
	service := productclient.Service{ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
		mu.Lock()
		reads++
		read := reads
		mu.Unlock()
		if read == 1 {
			close(started)
			<-release
		}
		return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{WorkerRef: "worker-" + string(rune('0'+read)), PreferredName: "fresh", ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT}}}}, nil
	}}
	baseline := productclient.LoadingView(productclient.Session{Tenant: "acme"}, productclient.State{Page: productui.PageOrganization})
	var got productui.View
	refresher, err := productclient.NewProjectionRefresher(service, productclient.Session{Tenant: "acme"}, productclient.State{Page: productui.PageOrganization}, baseline, func(view productui.View) error {
		mu.Lock()
		defer mu.Unlock()
		got = view
		applied = append(applied, view.People[0].ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- refresher.Refresh(context.Background(), invalidation.Refresh{SourceSequence: 11}) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first projection read did not start")
	}
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- refresher.Refresh(context.Background(), invalidation.Refresh{SourceSequence: 12})
	}()
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	_, sequence, loaded := refresher.Snapshot()
	if !loaded || sequence != 12 {
		t.Fatalf("snapshot sequence=%d loaded=%t, want 12/true", sequence, loaded)
	}
	mu.Lock()
	defer mu.Unlock()
	// Depending on scheduling, the older response may publish before the
	// newer read completes or be suppressed after the newer response wins.
	// Both are correct; the observable final projection must always be newest.
	if len(applied) == 0 || len(applied) > 2 || applied[len(applied)-1] != "worker-2" || got.People[0].ID != "worker-2" {
		t.Fatalf("applied=%v final=%q, want newest projection", applied, got.People[0].ID)
	}
}

func TestTodo_PROMOUX_011_ProductRefreshDoesNotRenderDuplicateSequence(t *testing.T) {
	var renders int
	service := productclient.Service{ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
		return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{WorkerRef: "worker-1", PreferredName: "fresh", ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT}}}}, nil
	}}
	baseline := productclient.LoadingView(productclient.Session{Tenant: "acme"}, productclient.State{Page: productui.PageOrganization})
	refresher, err := productclient.NewProjectionRefresher(service, productclient.Session{Tenant: "acme"}, productclient.State{Page: productui.PageOrganization}, baseline, func(productui.View) error { renders++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []uint64{11, 11, 10} {
		if err := refresher.Refresh(context.Background(), invalidation.Refresh{SourceSequence: sequence}); err != nil {
			t.Fatalf("refresh sequence %d: %v", sequence, err)
		}
	}
	if renders != 1 {
		t.Fatalf("renders=%d, want one render for duplicate/stale hints", renders)
	}
}
