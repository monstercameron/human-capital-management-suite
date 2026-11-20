//go:build js && wasm

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/clienttelemetry"
)

var clientTelemetrySession struct {
	gate    telemetryGate
	nonce   string
	locale  string
	build   string
	started bool
}

// installClientTelemetry arms the beacon (#3798): one bounded, PII-free record
// when the page is hidden, and one on the first window error or unhandled
// promise rejection. The error text is never read, only counted against a site
// label, so nothing a person typed can leave the page.
func installClientTelemetry(locale string) {
	state := &clientTelemetrySession
	if state.started {
		return
	}
	window := js.Global()
	if !window.Truthy() || !window.Get("addEventListener").Truthy() {
		return
	}
	state.started = true
	state.locale = locale
	if locale == "" {
		state.locale = "und"
	}
	state.build = telemetryBuildID()
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return
	}
	state.nonce = hex.EncodeToString(buffer)

	listen := func(event string, handler func()) {
		window.Call("addEventListener", event, js.FuncOf(func(js.Value, []js.Value) any {
			handler()
			return nil
		}))
	}
	listen("pagehide", func() { sendClientTelemetry(clienttelemetry.ReasonPageHide) })
	listen("error", func() {
		clientTelemetry.CountPanic("window.error")
		sendClientTelemetry(clienttelemetry.ReasonError)
	})
	listen("unhandledrejection", func() {
		clientTelemetry.CountPanic("window.rejection")
		sendClientTelemetry(clienttelemetry.ReasonError)
	})
}

func telemetryBuildID() string {
	revision := buildinfo.Current().Revision
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if revision == "" {
		return "dev"
	}
	return revision
}

// telemetryMarkTimes reads the "hcm:" User Timing marks as milliseconds since
// navigation start.
func telemetryMarkTimes() map[string]float64 {
	marks := map[string]float64{}
	performance := js.Global().Get("performance")
	if !performance.Truthy() || !performance.Get("getEntriesByType").Truthy() {
		return marks
	}
	entries := performance.Call("getEntriesByType", "mark")
	for i := 0; i < entries.Length(); i++ {
		entry := entries.Index(i)
		marks[entry.Get("name").String()] = entry.Get("startTime").Float()
	}
	return marks
}

func sendClientTelemetry(reason string) {
	defer recoverAt("client-telemetry.send")
	state := &clientTelemetrySession
	if state.nonce == "" || !state.gate.Allow(reason) {
		return
	}
	stream, panics := clientTelemetry.Snapshot()
	record, ok := buildClientTelemetryRecord(state.build, state.locale, reason, state.nonce, telemetryMarkTimes(), stream, panics)
	if !ok {
		return
	}
	body, err := json.Marshal(record)
	if err != nil {
		return
	}
	navigator := js.Global().Get("navigator")
	if !navigator.Truthy() || !navigator.Get("sendBeacon").Truthy() {
		return
	}
	blob := js.Global().Get("Blob").New([]any{string(body)}, map[string]any{"type": "application/json"})
	navigator.Call("sendBeacon", clientTelemetryPath, blob)
}
