package productclient

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTodo_I18N_005_Recovery(t *testing.T) {
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return nil, status.Error(codes.Unavailable, "journey service unavailable")
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetPreferences: func(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
			return &journeyv1.GetProductPreferencesResponse{User: &journeyv1.UserPreferences{Locale: "ar"}}, nil
		},
	}
	state, err := ParseState(productui.Path(productui.PageHome), "")
	if err != nil {
		t.Fatal(err)
	}
	view, loadErr := Load(context.Background(), service, Session{Tenant: "tenant-a", Principal: "alice"}, state)
	if loadErr == nil {
		t.Fatal("failed journey read returned no recoverable load error")
	}
	if view.Locale.Resolved != "ar" || view.Locale.Direction != "rtl" {
		t.Fatalf("saved locale was not applied to the failed client load: %+v", view.Locale)
	}
	view.LoadError = loadErr.Error()
	doc, err := ui.RenderToString(productui.Build(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{view.Locale.Text("shell.load_recovery"), view.Locale.Text("shell.load_retry")} {
		if !strings.Contains(doc, want) {
			t.Errorf("localized error/recovery rendering missing %q", want)
		}
	}
}
