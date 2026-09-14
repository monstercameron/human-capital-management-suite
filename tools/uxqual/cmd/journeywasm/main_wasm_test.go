//go:build js && wasm

package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// These tests carry the same build tag as main_wasm.go so that
// `GOOS=js GOARCH=wasm go vet ./tools/uxqual/cmd/journeywasm/` type-checks
// the entrypoint with its test alongside it.
//
// They do not call start, mount or readIsland: there is no document under
// `go test`, and every one of those reaches into one. What can be checked
// without a browser is the part that is a value rather than a side effect --
// the failure page, and the two identifiers this command shares with the
// shell that loads it.

func TestStartupFailurePageOffersRecovery(t *testing.T) {
	locale := productui.ResolveProductLocale("en-US")
	page := startupFailurePage(locale)

	if page.Notice == nil {
		t.Fatal("the failure page carries no notice")
	}
	if page.Notice.Tone != "danger" {
		t.Errorf("notice tone = %q, want danger", page.Notice.Tone)
	}
	if page.Notice.Detail != locale.Text("journey.startup_detail") {
		t.Errorf("notice detail = %q, want the reviewed recovery step", page.Notice.Detail)
	}
	if page.Brand != journeyclient.Brand {
		t.Errorf("brand = %q, want %q", page.Brand, journeyclient.Brand)
	}
	// A page that cannot start still leaves the reader somewhere to go.
	if len(page.Nav) == 0 || page.Nav[0].Href != journeyclient.WorkspacePath {
		t.Errorf("nav = %+v, want a link to the server-rendered workspace", page.Nav)
	}
	if page.List != nil || page.Detail != nil {
		t.Error("the failure page pretends to have data")
	}
}

func TestStartupFailurePageRenders(t *testing.T) {
	html, err := journey.RenderToString(startupFailurePage(productui.ResolveProductLocale("en-US")))
	if err != nil {
		t.Fatalf("rendering the failure page: %v", err)
	}
	if !strings.Contains(html, "This page could not start") {
		t.Error("the rendered failure page does not carry its own title")
	}
	if !strings.Contains(html, "Return to the workspace and try again") || strings.Contains(html, "no tunnel") {
		t.Error("the rendered failure page lost recovery guidance or exposed a technical cause")
	}
}

func TestStartupFailurePageUsesRequestedLocale(t *testing.T) {
	for _, language := range productui.SupportedProductLocales() {
		locale := productui.ResolveProductLocale(language)
		page := startupFailurePage(locale)
		if page.Notice == nil || page.Notice.Title != locale.Text("journey.startup_title") || page.Notice.Detail != locale.Text("journey.startup_detail") {
			t.Errorf("%s notice = %+v, want localized recovery copy", language, page.Notice)
		}
	}
}

// TestTheShellContractIdentifiers pins the two ids this command reads the
// document with. internal/humanwork/workspace publishes them as
// JourneyConfigElementID and JourneyRootElementID; they are restated here
// rather than imported so a wasm bundle does not carry the server.
func TestTheShellContractIdentifiers(t *testing.T) {
	if configElementID != "journey-config" {
		t.Errorf("configElementID = %q, want journey-config", configElementID)
	}
	if rootElementID != "app" {
		t.Errorf("rootElementID = %q, want app", rootElementID)
	}
	if rootSelector != "#app" {
		t.Errorf("rootSelector = %q, want #app", rootSelector)
	}
}

// TestDialTargetIsResolvableWithoutDNS keeps the browser client off the
// default resolver: grpc.NewClient's default scheme is dns, which a page
// cannot run.
func TestDialTargetIsResolvableWithoutDNS(t *testing.T) {
	cfg := journeyclient.Config{TunnelURL: "wss://cell.example/grpc", Bearer: "tok"}
	if got := cfg.DialTarget(); !strings.HasPrefix(got, "passthrough:///") {
		t.Errorf("dial target = %q, want a passthrough target", got)
	}
}

func TestProductPathRecognizesTheShellRootAndCanonicalDeepLinks(t *testing.T) {
	for _, path := range []string{"/workspace/app/", "/workspace/app/journeys", "/workspace/app/people"} {
		if !isProductPath(path) {
			t.Errorf("isProductPath(%q) = false, want product shell routing", path)
		}
	}
	for _, path := range []string{"/workspace/app", "/workspace/journey", "/workspace/promotion", "/workspace/appetite", " /workspace/app/people", "/workspace/app/people "} {
		if isProductPath(path) {
			t.Errorf("isProductPath(%q) = true, want standalone/document routing", path)
		}
	}
}
