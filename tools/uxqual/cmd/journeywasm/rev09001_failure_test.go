//go:build !(js && wasm)

// The shell and page renderers use GWC hooks that only the native SSR path
// can render to a string; the js/wasm lane mounts them into a live DOM.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// REV-090-01: a genuine route read failure in the live product router renders
// People, Work, History and Organization through the async-region failure
// state (BuildAsyncRegion / AsyncRegionFailure), in the geometry the region
// loads in, instead of the page-level View.LoadError panel stacked over an
// empty page. productRouteFailure and productRouteContent are the exact calls
// the js/wasm loader and route component make.

var rev09001Covered = []productui.PageID{productui.PagePeople, productui.PageWork, productui.PageHistory, productui.PageOrganization}

func rev09001Render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func rev09001FailedView(t *testing.T, page productui.PageID) (productui.View, bool) {
	t.Helper()
	view := productui.NewView(page, "Tenant", "Principal", "Scope")
	return productRouteFailure(view, page, errors.New("list workers: connection refused"))
}

func TestTodo_REV_090_01(t *testing.T) {
	for _, page := range rev09001Covered {
		view, failed := rev09001FailedView(t, page)
		if !failed || view.LoadError != "" {
			t.Fatalf("%s: failed=%v LoadError=%q; a covered region must not use the LoadError branch", page, failed, view.LoadError)
		}
		markup := rev09001Render(t, productRouteContent(view, failed))
		for _, want := range []string{`role="alert"`, `aria-live="assertive"`, "loading-proxy-" + string(page), view.Locale.Text("shell.live_unavailable")} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s failure content missing %q:\n%s", page, want, markup)
			}
		}
		if strings.Contains(markup, "connection refused") {
			t.Fatalf("%s failure leaked the transport error to the reader", page)
		}
	}
	// The live router's Retry is a real button that re-runs the same read and
	// asks for focus to return once it settles.
	oldRetry, oldPending := productRouteRetry, productRetryFocusPending
	t.Cleanup(func() { productRouteRetry, productRetryFocusPending = oldRetry, oldPending })
	reruns := 0
	productRouteRetry = func() { reruns++ }
	productRetryFocusPending = false
	view, failed := rev09001FailedView(t, productui.PagePeople)
	markup := rev09001Render(t, productRouteContent(view, failed))
	if !strings.Contains(markup, `<button class="button secondary async-region-retry" type="button">`+view.Locale.Text("shell.load_retry")+`</button>`) {
		t.Fatalf("failure region has no Retry button: %s", markup)
	}
	retryProductRoute()
	if reruns != 1 || !productRetryFocusPending {
		t.Fatalf("Retry reran %d reads (focus pending %v), want one", reruns, productRetryFocusPending)
	}
	// Without a mounted router there is nothing to revalidate: the Retry is
	// the page's own address instead, and calling retry is a no-op.
	productRouteRetry = nil
	productRetryFocusPending = false
	retryProductRoute()
	if productRetryFocusPending {
		t.Fatal("a retry with no router requested focus")
	}
	if markup := rev09001Render(t, productRouteContent(view, failed)); !strings.Contains(markup, `href="/workspace/app/people"`) || strings.Contains(markup, `<button`) {
		t.Fatalf("no-router failure did not offer the page address: %s", markup)
	}
	// Pages outside the async-region contract keep their LoadError degradation.
	view, failed = productRouteFailure(productui.NewView(productui.PageAdmin, "Tenant", "Principal", "Scope"), productui.PageAdmin, errors.New("boom"))
	if failed || view.LoadError != productLoadErrorMessage {
		t.Fatalf("admin failure = %v %q, want the LoadError contract", failed, view.LoadError)
	}
	// A successful read changes nothing.
	ok := productui.NewView(productui.PagePeople, "Tenant", "Principal", "Scope")
	if same, failed := productRouteFailure(ok, productui.PagePeople, nil); failed || same.LoadError != "" {
		t.Fatal("a successful read took a failure path")
	}
}

// TestTodo_REV_090_01_Fault: the failed region has the loading region's exact
// geometry, and the persistent shell around it carries no second, older
// failure panel.
func TestTodo_REV_090_01_Fault(t *testing.T) {
	for _, page := range rev09001Covered {
		view, failed := rev09001FailedView(t, page)
		failure := rev09001Render(t, productRouteContent(view, failed))
		loading := rev09001Render(t, productui.LoadingProxy(productui.LoadingProxyProps{Page: page}))
		failureBody := failure[strings.Index(failure, "loading-progress"):]
		loadingBody := loading[strings.Index(loading, "loading-progress"):]
		if failureBody != loadingBody {
			t.Fatalf("%s: failed region's shape differs from its loading shape", page)
		}
		shell := rev09001Render(t, productui.BuildShell(view, productRouteContent(view, failed), true))
		if strings.Contains(shell, `class="surface empty-state`) {
			t.Fatalf("%s: the shell still stacks the LoadError recovery panel over the failed region", page)
		}
		// The check can fail: the retired contract did stack that panel.
		retired := view
		retired.LoadError = productLoadErrorMessage
		if old := rev09001Render(t, productui.BuildShell(retired, productui.BuildPageContent(retired), true)); !strings.Contains(old, `class="surface empty-state`) {
			t.Fatalf("%s: the retired LoadError contract no longer renders its panel; this check proves nothing", page)
		}
		if strings.Count(shell, `role="alert"`) < 1 || !strings.Contains(shell, `id="main-content"`) {
			t.Fatalf("%s: failed region is not announced inside the kept shell", page)
		}
	}
}

func TestDocsFailureUsesRegionOnlyForTypedListFailureAndKeepsSignedOutFirst(t *testing.T) {
	view := productui.NewView(productui.PageDocs, "Tenant", "Principal", "Scope")
	view.DocumentsLoadFailed = true
	failed, region := productRouteFailure(view, productui.PageDocs, errors.New("list documents: temporary network failure"))
	if !region || failed.LoadError != "" {
		t.Fatalf("Docs transient failure = region %v LoadError %q", region, failed.LoadError)
	}
	markup := rev09001Render(t, productRouteContent(failed, region))
	if strings.Contains(markup, "Not set up for this workspace") || !strings.Contains(markup, `class="async-region-failure`) {
		t.Fatalf("Docs failure content did not retain the single failure state:\n%s", markup)
	}

	view.SignedOut = productui.UnauthenticatedRecovery()
	failed, region = productRouteFailure(view, productui.PageDocs, errors.New("list documents: permission denied"))
	markup = rev09001Render(t, productui.BuildShell(failed, productRouteContent(failed, region), false))
	if region || !strings.Contains(markup, "Sign in") || strings.Contains(markup, `class="async-region-failure`) {
		t.Fatalf("signed-out Docs state was downgraded to a data failure:\n%s", markup)
	}
}

func TestDocsLoadFailureAdapterUsesRealClientClassification(t *testing.T) {
	session := productclient.Session{Tenant: "Tenant", Principal: "Principal", Scope: "Scope"}
	state := productclient.State{Page: productui.PageDocs, Request: productui.PageRequest{Page: productui.PageDocs}}
	baseline := productclient.LoadingView(session, productclient.State{Page: productui.PageHome, Request: productui.PageRequest{Page: productui.PageHome}})
	transient, err := productclient.LoadWithBaseline(context.Background(), productclient.Service{
		ListDocuments: func(context.Context, *documentv1.ListDocumentsRequest) (*documentv1.ListDocumentsResponse, error) {
			return nil, status.Error(codes.Unavailable, "temporary network failure")
		},
	}, session, state, baseline)
	if err == nil {
		t.Fatal("transient ListDocuments failure returned no load error")
	}
	failed, region := productRouteFailure(transient, productui.PageDocs, err)
	if !region || failed.LoadError != "" {
		t.Fatalf("real transient client result mapped to region=%v LoadError=%q", region, failed.LoadError)
	}

	unconfiguredStatus, detailErr := status.New(codes.Unavailable, "not configured").WithDetails(&commonv1.ErrorDetail{ReasonRef: "document.service.not_configured"})
	if detailErr != nil {
		t.Fatal(detailErr)
	}
	unconfigured, err := productclient.LoadWithBaseline(context.Background(), productclient.Service{
		ListDocuments: func(context.Context, *documentv1.ListDocumentsRequest) (*documentv1.ListDocumentsResponse, error) {
			return nil, unconfiguredStatus.Err()
		},
	}, session, state, baseline)
	if err != nil || unconfigured.DocumentsLoadFailed || unconfigured.DocumentsReady {
		t.Fatalf("typed absent capability = failed %v, ready %v, err %v", unconfigured.DocumentsLoadFailed, unconfigured.DocumentsReady, err)
	}
	resolved, region := productRouteFailure(unconfigured, productui.PageDocs, err)
	markup := rev09001Render(t, productui.BuildShell(resolved, productRouteContent(resolved, region), false))
	if region || !strings.Contains(markup, "Not set up for this workspace") || strings.Contains(markup, "Live data unavailable") {
		t.Fatalf("typed absent capability became a retry failure:\n%s", markup)
	}
}

// rev09001GoldenDigest pins the People failure region's bytes: the visible
// notice with its Retry button over the unchanged loading body.
const rev09001GoldenDigest = "2c95340094608f98f4cd72c168f63187e71c641b2a96fd41f3117d8e9473361d"

func TestTodo_REV_090_01_Golden(t *testing.T) {
	// Pinned in the live shape: a mounted router, so Retry is a button.
	oldRetry := productRouteRetry
	t.Cleanup(func() { productRouteRetry = oldRetry })
	productRouteRetry = func() {}
	view, failed := rev09001FailedView(t, productui.PagePeople)
	markup := rev09001Render(t, productRouteContent(view, failed))
	digest := sha256.Sum256([]byte(markup))
	if got := hex.EncodeToString(digest[:]); got != rev09001GoldenDigest {
		t.Fatalf("People failure region digest = %s, want %s\n%s", got, rev09001GoldenDigest, markup)
	}
}
