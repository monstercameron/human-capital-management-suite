package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_UXBLIND_123_ProjectClockFailsClosed(t *testing.T) {
	if got := projectClock(nil); got == nil || got.Enabled || got.ViewerIsAdmin {
		t.Fatalf("a missing island projected %+v, want a clock that is not running", got)
	}
	if got := projectClock(&journeyclient.Clock{Enabled: true, ViewerIsAdmin: true}); !got.Enabled || !got.ViewerIsAdmin {
		t.Fatalf("projected %+v", got)
	}
	// The island decodes from the exact JSON the server writes.
	var config journeyclient.Config
	if err := json.Unmarshal([]byte(`{"clock":{"enabled":true,"viewer_is_admin":false}}`), &config); err != nil || config.Clock == nil || !config.Clock.Enabled || config.Clock.ViewerIsAdmin {
		t.Fatalf("island clock = %+v err=%v", config.Clock, err)
	}
}

func TestTodo_UXBLIND_123_LastEventIsAnInstantOnlyWhenTheServerSaidSo(t *testing.T) {
	if at, ok := clockLastEventTime(" 2026-09-29T14:03:00Z "); !ok || at.UTC().Format("15:04") != "14:03" {
		t.Fatalf("instant = %v %v", at, ok)
	}
	for _, label := range []string{"No event", "", "Kein Ereignis", "2026-09-29 14:03"} {
		if _, ok := clockLastEventTime(label); ok {
			t.Fatalf("%q parsed as an instant", label)
		}
	}
	if got := formatClockInstant(time.Date(2026, 9, 29, 14, 3, 0, 0, time.UTC), "en-US"); got == "" {
		t.Fatal("no formatted instant")
	}
}

// TestTodo_UXBLIND_123_ClientHandsTheClockTheSameProjectionTheServerBuilt pins
// the wiring in the client entry point: the workspace availability reaches the
// session, an unavailable workspace is not asked, and the available clock and
// break actions are bound for the server's position.
func TestTodo_UXBLIND_123_ClientHandsTheClockTheSameProjectionTheServerBuilt(t *testing.T) {
	body, err := os.ReadFile("product_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, want := range []string{"Clock: projectClock(cfg.Clock)", "newClockLiveBinding(cfg, conn)", "clockActionForPhase(view.ClockProjection.Phase)", "view.ClockProjection.StartBreak = bindAction(\"start_break\"", "view.ClockProjection.EndBreak = bindAction(\"end_break\"", "productui.ClockReasonNotEnabled", "formatClockInstant("} {
		if !strings.Contains(source, want) {
			t.Fatalf("product_wasm.go is missing %q", want)
		}
	}
	if strings.Contains(source, `view.ClockProjection.ClockIn = bindAction("in", "clock_page.clock_in")
							view.ClockProjection.ClockOut`) {
		t.Fatal("clock in and clock out are bound together again")
	}
	if got := productui.NormalizeClockReason(string(productui.ClockReasonExempt)); got != productui.ClockReasonExempt {
		t.Fatalf("normalized reason = %q", got)
	}
}

func TestTodo_UXBLIND_123_RelatedLinksAreOfferedOnlyForPagesTheViewerMayOpen(t *testing.T) {
	newView := func(permissions ...productui.RolePagePermission) productui.View {
		view := productui.NewView(productui.PageClock, "tenant", "worker", "")
		view.Locale = productui.ResolveProductLocale("en-US")
		view.EffectivePermissions = permissions
		return view
	}
	timecard, fix := clockRelatedLinks(newView(
		productui.RolePagePermission{Page: productui.PageTimecard, View: true},
		productui.RolePagePermission{Page: productui.PageMissingPunch, View: true},
	))
	if timecard == nil || timecard.Href != "/workspace/app/time/timecard" || timecard.Label == "" || fix == nil || fix.Href != "/workspace/app/time/missing-punch" || fix.Label == "" {
		t.Fatalf("links for a viewer who may open both = %+v %+v", timecard, fix)
	}
	timecard, fix = clockRelatedLinks(newView(productui.RolePagePermission{Page: productui.PageTimecard, View: true}, productui.RolePagePermission{Page: productui.PageMissingPunch}))
	if timecard == nil || fix != nil {
		t.Fatalf("links when the missing-punch page is refused = %+v %+v", timecard, fix)
	}
	timecard, fix = clockRelatedLinks(newView(productui.RolePagePermission{Page: productui.PageClock, View: true}))
	if timecard != nil || fix != nil {
		t.Fatalf("links for a viewer with neither page = %+v %+v", timecard, fix)
	}
}
