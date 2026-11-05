package main

import (
	"strings"
	"testing"
)

// TestTodo_CHATCALL_004 pins the browser-side record: the Go/WASM event
// flow, devices, codecs, screen sharing, phones, accessibility and the test
// strategy, with the untestable remainder stated, not silently dropped.
func TestTodo_CHATCALL_004(t *testing.T) {
	findings := CallClientFindings()
	wantTopics := []string{
		"driving peer connections from Go",
		"permissions, devices and hot-plug",
		"echo cancellation and noise suppression",
		"audio codecs",
		"video codecs",
		"screen sharing",
		"phones and background tabs",
		"accessibility",
		"unattended test strategy",
		"what cannot be tested that way",
	}
	if len(findings) != len(wantTopics) {
		t.Fatalf("findings = %d, want %d", len(findings), len(wantTopics))
	}
	for i, f := range findings {
		if f.Topic != wantTopics[i] {
			t.Fatalf("finding %d topic = %q, want %q", i, f.Topic, wantTopics[i])
		}
		if strings.TrimSpace(f.Finding) == "" || strings.TrimSpace(f.Evidence) == "" {
			t.Fatalf("finding %q leaves its answer or evidence unanswered", f.Topic)
		}
	}
	record := RenderCallClient()
	for _, phrase := range []string{
		"RTCPeerConnection", "getUserMedia", "single thread",
		"chatvoice_recorder_wasm.go",
		"Opus", "VP8", "H.264",
		"Chrome", "Edge", "Firefox", "Safari",
		"getDisplayMedia",
		"--use-fake-device-for-media-stream",
		"NOT_RUN",
	} {
		if !strings.Contains(record, phrase) {
			t.Fatalf("record does not pin %q", phrase)
		}
	}
	contract := CallPrototypeContract()
	if contract.Contexts != 2 {
		t.Fatalf("prototype contexts = %d, want two calling each other", contract.Contexts)
	}
	if len(contract.SyntheticDevices) == 0 || len(contract.Pass) == 0 || len(contract.Impairment) == 0 {
		t.Fatalf("prototype contract leaves devices, criteria or impairment open: %+v", contract)
	}
	if contract.Status != "NOT_RUN" {
		t.Fatalf("status = %q, want NOT_RUN until the two-context run reports", contract.Status)
	}
	findings[0].Topic = "changed"
	if CallClientFindings()[0].Topic == "changed" {
		t.Fatal("the record is mutable by a caller")
	}
}

func TestTodo_CHATCALL_004_Golden(t *testing.T) {
	const digest = "5689573907b06013d65e5fd66460815dbb2e57b9427abdee16ab7bca23046686"
	if got := strings.Count(RenderCallClient(), "\n"); got < 15 {
		t.Fatalf("record has %d lines", got)
	}
	if CallClientDigest() != digest {
		t.Fatalf("the record changed; digest is now %s", CallClientDigest())
	}
}
