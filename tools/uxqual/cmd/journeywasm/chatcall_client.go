package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// CallClientFinding is one answered browser-side question of CHATCALL-004.
// The client is Go compiled to WebAssembly with no script layer; every
// finding is grounded in what that client already drives from Go.
type CallClientFinding struct {
	Topic    string
	Finding  string
	Evidence string
}

// CallClientFindings returns the browser-side record in question order.
func CallClientFindings() []CallClientFinding {
	return []CallClientFinding{
		{Topic: "driving peer connections from Go",
			Finding:  "Yes: the browser's RTCPeerConnection, media device and track interfaces are driven entirely from Go through the script bridge, without blocking the client's single thread. The event flow is callbacks that return immediately plus goroutines that do the work: each js.FuncOf callback hands its event (an ICE candidate, a track arrival, a connection-state change) to a Go channel read by a goroutine, and SDP offer/answer handling runs on goroutines. The known trap — a blocking call inside a callback freezes the page, because the Go runtime runs on the page's single thread — is avoided by construction: no callback ever waits.",
			Evidence: "chatvoice_recorder_wasm.go Start bridges the getUserMedia promise through resolve/reject callbacks into a result channel; installChatVoice keeps every document listener non-blocking the same way."},
		{Topic: "permissions, devices and hot-plug",
			Finding:  "getUserMedia prompts once per origin for microphone and camera; enumerateDevices lists devices after permission is granted; deviceId constraints select the chosen microphone, camera and speaker; the devicechange event covers hot-plug during a call. A call that starts with no usable device shows a plain missing-device state rather than failing silently.",
			Evidence: "The voice recorder already distinguishes denied, missing and failed microphone states (voiceDenied, voiceNoMicrophone, voiceFailed); calls reuse those states with a camera half."},
		{Topic: "echo cancellation and noise suppression",
			Finding:  "Leave the browser defaults on: echoCancellation, noiseSuppression and autoGainControl all true in the audio constraints. The product has no custom audio processing to replace them, and turning them off for 'quality' is how calls start echoing on laptops.",
			Evidence: "getUserMedia audio constraints; no product audio pipeline exists to take over."},
		{Topic: "audio codecs",
			Finding:  "Opus everywhere, negotiated as the preferred codec in every call; G.711 alongside it as the mandated fallback both ends must offer. Opus carries voice at the planning rate of about 24 kbps and survives the loss profiles of CHATCALL-002 without PLC artefacts worth planning around.",
			Evidence: "WebRTC mandates Opus and G.711 in every implementation; the voice-message path already records audio/webm with the Opus codec."},
		{Topic: "video codecs",
			Finding:  "VP8 is the baseline every supported browser (Chrome, Edge, Firefox, Safari/WebKit) negotiates; H.264 alongside it for Safari hardware paths and constrained devices; VP9 where both ends are Chromium or Firefox. Prefer VP8 and accept H.264: the simulcast layers of CHATCALL-002 are VP8 layers.",
			Evidence: "Per-browser WebRTC codec tables as published; re-verify at build time, because codec tables move yearly and Safari moves slowest."},
		{Topic: "screen sharing",
			Finding:  "getDisplayMedia with video-only constraints, one sharer at a time per CHATCALL-001. The shared screen is one more video stream on the call's own topology — peer-to-peer in a one-to-one call, forwarded in a drop-in. Capturing the sharer's system audio stays off: other people's notification sounds are a consent question, not a default.",
			Evidence: "getDisplayMedia; the one-sharer ceiling of CHATCALL-001."},
		{Topic: "phones and background tabs",
			Finding:  "Mobile browsers join with the same code, but a backgrounded tab or a locked phone loses its camera and usually its microphone within seconds: that is OS policy, not ours. A call must survive that as muted-with-a-banner and re-acquire devices on foreground. One-to-one ringing on phones needs the push path decided separately — a browser cannot ring a closed page, and this record does not pretend otherwise.",
			Evidence: "Mobile OS background-media policy; the ringing rules of CHATCALL-001 cover the open page only."},
		{Topic: "accessibility",
			Finding:  "Call controls are buttons with accessible names in the product's three languages, following the voice controls' data-action pattern; captions, if ever offered, render in a live region; every video tile carries its person's name; reduced motion still removes all transitions. A call must be joinable, mutable and leavable by keyboard alone.",
			Evidence: "The data-chatvoice-action controls and their locale copies in chatui; the motion-token rules every Chat control already follows."},
		{Topic: "unattended test strategy",
			Finding:  "The unattended suite runs the automated browser with --use-fake-device-for-media-stream and --use-fake-ui-for-media-stream, puts two browser contexts in a call with each other through the review cell, injects network impairment at the browser or the cell, and asserts signalling completes, audio flows both ways (analyser levels, not ears), and hang-up releases every track and closes the connection.",
			Evidence: "The prototype contract below; the voice recorder's analyser metering shows the level-reading pattern."},
		{Topic: "what cannot be tested that way",
			Finding:  "Real echo cancellation, real device quirks (hot-plug timing, Bluetooth handover, a camera another app holds), actual perceived audio quality, and OS-level interruptions need hands-on passes on real phones and laptops. The unattended suite proves the wiring — offers answered, media flowing, devices released — not the experience.",
			Evidence: "Fake devices produce clean signals by design; no flag emulates a laptop fan or a train tunnel."},
	}
}

// CallPrototype is the acceptance contract for the throwaway two-context
// prototype. Status is NOT_RUN until the run reports back.
type CallPrototype struct {
	Contexts         int
	SyntheticDevices []string
	Impairment       []string
	Pass             []string
	Status           string
}

// CallPrototypeContract returns the prototype contract.
func CallPrototypeContract() CallPrototype {
	return CallPrototype{
		Contexts:         2,
		SyntheticDevices: []string{"--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"},
		Impairment:       []string{"5% loss with 150 ms RTT", "severe pass: 10% loss with 300 ms RTT"},
		Pass: []string{
			"signalling completes over the tunnel",
			"audio flows both ways at analyser level",
			"hang-up releases every track and closes the connection",
			"a second call reuses no state from the first",
		},
		Status: "NOT_RUN",
	}
}

// RenderCallClient is the stable text form used by the golden test.
func RenderCallClient() string {
	var b strings.Builder
	b.WriteString("CHATCALL-004 browser-side record recorded 2026-10-22\n")
	for _, f := range CallClientFindings() {
		fmt.Fprintf(&b, "\n%s\n%s\n  evidence: %s\n", f.Topic, f.Finding, f.Evidence)
	}
	c := CallPrototypeContract()
	fmt.Fprintf(&b, "\nprototype contexts: %d\ndevices: %s\nimpairment: %s\nstatus: %s\n",
		c.Contexts, strings.Join(c.SyntheticDevices, " "), strings.Join(c.Impairment, "; "), c.Status)
	for _, p := range c.Pass {
		fmt.Fprintf(&b, "pass: %s\n", p)
	}
	return b.String()
}

// CallClientDigest is the digest of the rendered record.
func CallClientDigest() string {
	sum := sha256.Sum256([]byte(RenderCallClient()))
	return hex.EncodeToString(sum[:])
}
