package productclient

import (
	"context"
	"testing"

	positionv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/position/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_REV_076_03_PositionReferenceRouteRoundTrip(t *testing.T) {
	for _, page := range []productui.PageID{productui.PagePositionObject, productui.PagePositionOccupancy} {
		path := productui.Path(page)
		state, err := ParseState(path, "position_ref=opaque-revision-token&locale=en-US")
		if err != nil {
			t.Fatalf("ParseState(%s): %v", path, err)
		}
		if state.Request.PositionReference != "opaque-revision-token" {
			t.Errorf("%s selected reference = %q", path, state.Request.PositionReference)
		}
		if got, want := CanonicalHref(state), path+"?locale=en-US&position_ref=opaque-revision-token"; got != want {
			t.Errorf("CanonicalHref(%s) = %q, want %q", path, got, want)
		}
	}
}

func TestTodo_REV_076_03_PositionSelectorLoadsAuthorizedOptions(t *testing.T) {
	page := productui.PagePositionObject
	state := State{Page: page, Request: productui.PageRequest{Page: page}}
	session := Session{Tenant: "tenant-a", Principal: "viewer-a"}
	service := Service{ListPositionObjectOptions: func(context.Context, *positionv1.ListPositionObjectOptionsRequest) (*positionv1.ListPositionObjectOptionsResponse, error) {
		return &positionv1.ListPositionObjectOptionsResponse{Options: []*positionv1.PositionOption{{
			PositionRevisionRef: "opaque-revision-ref", PositionId: "position-1", Title: "Engineer",
			Organization: "Example", JobCode: "ENG-01", OrgUnit: "engineering",
		}}}, nil
	}}
	baseline := LoadingView(session, state)
	view, err := LoadWithBaseline(context.Background(), service, session, state, baseline)
	if err != nil {
		t.Fatalf("LoadWithBaseline: %v", err)
	}
	if len(view.PositionOptions) != 1 || view.PositionOptions[0].Reference != "opaque-revision-ref" || view.PositionOptions[0].PositionID != "position-1" {
		t.Fatalf("selector options = %+v", view.PositionOptions)
	}
}

func TestPositionSelectorBaselineCopiesOnlyMatchingPage(t *testing.T) {
	baseline := productui.View{Page: productui.PagePositionObject, PositionOptions: []productui.PositionOptionProjection{{Reference: "ref-a"}}}
	view := productui.View{Page: productui.PagePositionObject}
	seedBaselineProjection(&view, baseline)
	view.PositionOptions[0].Reference = "changed"
	if baseline.PositionOptions[0].Reference != "ref-a" {
		t.Fatal("baseline selector slice was aliased")
	}
	otherPage := productui.View{Page: productui.PageHome}
	seedBaselineProjection(&otherPage, baseline)
	if len(otherPage.PositionOptions) != 0 {
		t.Fatalf("position options carried to non-position page: %+v", otherPage.PositionOptions)
	}
}
