//go:build js && wasm

// Command timeclock is the browser host for the standalone managed-tablet
// kiosk. All business behaviour remains in internal/timeclockapp; this file
// only bridges browser storage, fetch and the GoWebComponents mount.
package main

import (
	"context"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/timeclockapp"
)

var mountCleared bool

func main() {
	prepareClockStorage(func(store timeclockapp.Storage, err error) {
		clearMount()
		if err != nil {
			ui.Render(unavailableView(err.Error()), "#app")
			return
		}
		mount(store)
	})
	select {}
}

func unavailableView(reason string) ui.Node {
	return html.Main(html.Props{Role: "alert", Class: "timeclock-kiosk"}, html.H1(html.Props{}, ui.Text("Time clock unavailable")), html.P(html.Props{}, ui.Text(reason)))
}

func clearMount() {
	if mountCleared {
		return
	}
	mountCleared = true
	root := js.Global().Get("document").Call("getElementById", "app")
	if root.IsNull() || root.IsUndefined() {
		return
	}
	root.Set("textContent", "")
}

func mount(store timeclockapp.Storage) {
	api := timeclockapp.NewHTTPClient("/timeclock", nil)
	model := timeclockapp.NewKiosk(api, store, time.Now)
	ctx := context.Background()
	var render func()
	render = func() {
		props := timeclockapp.KioskViewProps{Model: model, Context: ctx, Now: time.Now(), OnEnroll: func(code string) { go func() { _ = model.Enroll(ctx, code); render() }() }, OnPIN: func(pin string) { go func() { _ = model.IdentifyPIN(ctx, pin); render() }() }, OnBadge: func(value string, qr bool) { go func() { _ = model.IdentifyBadge(ctx, value, qr); render() }() }, OnPunch: func(event timev1.PunchEventType) { go func() { _, _ = model.Punch(ctx, event); render() }() }, OnCancel: func() { model.ClearWorker(); render() }, OnLocale: func(locale timeclockapp.Locale) { model.SetLocale(locale); render() }}
		ui.Render(ui.CreateElement(func(p timeclockapp.KioskViewProps) ui.Node { return timeclockapp.KioskView(p) }, props), "#app")
	}
	render()
}
