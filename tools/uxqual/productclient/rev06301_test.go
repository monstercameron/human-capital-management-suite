package productclient

import (
	"context"
	"strings"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func rev06301State(t *testing.T) State {
	t.Helper()
	state, err := ParseState(productui.Path(productui.PagePeople), "locale=de-DE")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func rev06301Service() Service {
	return Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return nil, status.Error(codes.Unavailable, "directory backend unavailable")
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return nil, status.Error(codes.Unauthenticated, "bearer secret expired")
		},
	}
}

func TestTodo_REV_063_01(t *testing.T) {
	view, err := Load(context.Background(), rev06301Service(), Session{Tenant: "tenant-a", Principal: "worker-a"}, rev06301State(t))
	if status.Code(err) != codes.Unavailable || !journeyclient.ErrorHasCode(err, codes.Unauthenticated) {
		t.Fatalf("route load aggregate = %s, want earlier UNAVAILABLE and later UNAUTHENTICATED (err %v)", status.Code(err), err)
	}
	if view.SignedOut == nil {
		t.Fatal("unauthenticated route projection has no signed-out recovery state")
	}
	if view.SignedOut.SignInHref != "/workspace/login" || view.SignedOut.Detail != "" || len(view.SignedOut.Revoked) != 0 {
		t.Fatalf("recovery projection invented or omitted facts: %+v", view.SignedOut)
	}
	markup, renderErr := productui.Render(view)
	if renderErr != nil {
		t.Fatal(renderErr)
	}
	if !strings.Contains(markup, `id="signed-out"`) || !strings.Contains(markup, `role="alert"`) || !strings.Contains(markup, `aria-labelledby="signed-out-title"`) || !strings.Contains(markup, `href="/workspace/login"`) {
		t.Fatalf("route did not render the sign-in recovery panel and link")
	}
	if strings.Contains(markup, "Try again") || strings.Contains(markup, "bearer secret expired") {
		t.Fatalf("signed-out route retained a dead-end retry or leaked transport detail")
	}
}

func TestTodo_REV_063_01_Security(t *testing.T) {
	view, err := Load(context.Background(), rev06301Service(), Session{Tenant: "tenant-a", Principal: "worker-a"}, rev06301State(t))
	if err == nil || view.SignedOut == nil {
		t.Fatalf("authentication refusal = (%+v, %v), want signed-out projection and refusal", view.SignedOut, err)
	}
	if view.SignedOut.Detail != "" || len(view.SignedOut.Revoked) != 0 {
		t.Fatalf("unauthenticated response fabricated detail or revoked grants: %+v", view.SignedOut)
	}
	view.SignedOut.SignInHref = "javascript:steal()"
	markup, renderErr := productui.Render(view)
	if renderErr != nil {
		t.Fatal(renderErr)
	}
	if strings.Contains(markup, "javascript:") || strings.Contains(markup, "bearer secret expired") {
		t.Fatalf("unsafe recovery target or private transport detail reached markup")
	}
}
