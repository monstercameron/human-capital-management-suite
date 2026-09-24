//go:build !(js && wasm)

package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTodo_REV_063_01(t *testing.T) {
	view := productui.NewView(productui.PagePeople, "Tenant", "Worker", "")
	view.SignedOut = productui.UnauthenticatedRecovery()
	view, regionFailed := productRouteFailure(view, productui.PagePeople, status.Error(codes.Unauthenticated, "expired bearer"))
	if regionFailed || view.LoadError != "" || view.SignedOut == nil {
		t.Fatalf("route failure state = (region=%v load-error=%q signed-out=%v)", regionFailed, view.LoadError, view.SignedOut != nil)
	}
	markup, err := productui.Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `id="signed-out"`) || !strings.Contains(markup, `href="/workspace/login"`) || strings.Contains(markup, "Try again") {
		t.Fatal("route shell did not replace the retry state with its sign-in recovery panel")
	}
}

func TestTodo_REV_063_01_Integration(t *testing.T) {
	view := productui.NewView(productui.PagePeople, "Tenant", "Worker", "")
	view.SignedOut = productui.UnauthenticatedRecovery()
	view.LoadError = productLoadErrorMessage
	got, regionFailed := productRouteFailure(view, productui.PagePeople, status.Error(codes.Unauthenticated, "expired"))
	if regionFailed || got.LoadError != "" || got.SignedOut == nil {
		t.Fatalf("recovery projection failed to suppress stale shell error: %+v region=%v", got, regionFailed)
	}
}
