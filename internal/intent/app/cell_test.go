package app

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestCell_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestCell_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestCell_ClientForRequest_ReturnsNilWhenUnavailable(t *testing.T) {
	var cell *Cell
	if got := cell.ClientForRequest(context.Background()); got != nil {
		t.Fatalf("nil cell client = %T, want nil", got)
	}
	if got := (&Cell{}).ClientForRequest(context.Background()); got != nil {
		t.Fatalf("unconfigured cell client = %T, want nil", got)
	}
}

func TestCell_ClientForRequest_DelegatesRequestContext(t *testing.T) {
	type contextKey string
	key := contextKey("request")
	wantContext := context.WithValue(context.Background(), key, "bound")
	wantClient := personaAdminClientStub{}
	factory := &personaAdminClientFactoryStub{
		client: wantClient,
		check: func(ctx context.Context) {
			if got := ctx.Value(key); got != "bound" {
				t.Errorf("factory context value = %v, want bound", got)
			}
		},
	}
	cell := &Cell{PersonaAdminClientFactory: factory}

	if got := cell.ClientForRequest(wantContext); got != wantClient {
		t.Fatalf("client = %#v, want %#v", got, wantClient)
	}
	if factory.calls != 1 {
		t.Fatalf("factory calls = %d, want 1", factory.calls)
	}
}

type personaAdminClientFactoryStub struct {
	client productui.PersonaAdminClient
	check  func(context.Context)
	calls  int
}

func (f *personaAdminClientFactoryStub) ClientForRequest(ctx context.Context) productui.PersonaAdminClient {
	f.calls++
	if f.check != nil {
		f.check(ctx)
	}
	return f.client
}

type personaAdminClientStub struct{}

func (personaAdminClientStub) Snapshot(context.Context, productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	return productui.PersonaAdminSnapshot{}, nil
}
func (personaAdminClientStub) Preview(context.Context, productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	return productui.PersonaAdminPreview{}, nil
}
func (personaAdminClientStub) RequestReview(string) error   { return nil }
func (personaAdminClientStub) PublishPersona(string) error  { return nil }
func (personaAdminClientStub) RollbackPersona(string) error { return nil }
func (personaAdminClientStub) SuspendPersona(string) error  { return nil }
func (personaAdminClientStub) RetirePersona(string) error   { return nil }
