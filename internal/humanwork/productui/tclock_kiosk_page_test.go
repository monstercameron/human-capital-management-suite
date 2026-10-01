package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_TCLOCK_016(t *testing.T) {
	called := false
	markup, err := ui.RenderToString(KioskPage(testView(PageClock), KioskProjection{
		Screen: KioskScreenConfirm, Online: false, QueueDepth: 2, SiteLabel: "Riverside", WorkerLabel: "Ava", ShiftLabel: "Morning", StatusLabel: "Clocked out",
		OnPunch: func(KioskPunchRequest) { called = true },
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"timeclock-kiosk", "Riverside", "Offline", "2 waiting to send", "Ava", "Morning", "Clock in", "Send my answer", "Record my tips", "Switch to this job", `dir="ltr"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("kiosk markup missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "/workspace") || strings.Contains(markup, "href=") || called {
		t.Fatalf("kiosk exposed a workspace route or ran a callback during render: %s", markup)
	}
}

func TestTodo_TCLOCK_016_Browser(t *testing.T) {
	markup, err := ui.RenderToString(KioskPage(testView(PageClock), KioskProjection{Screen: KioskScreenReceipt, ReceiptSeq: 7, ReceiptLabel: "Saved on this clock", OnDone: func() {}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`role="status"`, `aria-live="polite"`, `type="button"`, "Punch number 7 on this clock"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("receipt kiosk missing browser contract %q: %s", want, markup)
		}
	}
}

func TestTodo_TCLOCK_016_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(KioskPage(testView(PageClock), KioskProjection{Screen: KioskScreenIdentify, OnIdentify: func(string) {}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="kiosk-credential"`, `for="kiosk-credential"`, `type="text"`, `id="kiosk-identify-title"`, `aria-describedby="kiosk-credential-help"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("identify kiosk missing accessible association %q: %s", want, markup)
		}
	}
}

func TestTodo_TCLOCK_016_I18n(t *testing.T) {
	for _, tc := range []struct{ locale, direction, title string }{{"en-US", "ltr", "Time clock"}, {"de-DE", "ltr", "Stempeluhr"}, {"ar", "rtl", "ساعة الدوام"}} {
		view := testView(PageClock)
		view.Locale = ResolveProductLocale(tc.locale)
		markup, err := ui.RenderToString(KioskPage(view, KioskProjection{Screen: KioskScreenIdle}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, `dir="`+tc.direction+`"`) || !strings.Contains(markup, tc.title) {
			t.Fatalf("%s kiosk locale contract missing in %s", tc.locale, markup)
		}
	}
}

func TestTodo_TCLOCK_016_Security(t *testing.T) {
	markup, err := ui.RenderToString(KioskPage(testView(PageClock), KioskProjection{Screen: KioskScreenIdle, SiteLabel: "Site"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "admin") || strings.Contains(markup, "/workspace") || strings.Contains(markup, "Authorization") {
		t.Fatalf("kiosk markup disclosed or linked to admin workspace: %s", markup)
	}
}

func TestTodo_TCLOCK_016_Recovery(t *testing.T) {
	markup, err := ui.RenderToString(KioskPage(testView(PageClock), KioskProjection{Screen: KioskScreenRecovery}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Stored kiosk data needs administrator recovery") || !strings.Contains(markup, `role="alert"`) {
		t.Fatalf("recovery state is not explicit: %s", markup)
	}
}
