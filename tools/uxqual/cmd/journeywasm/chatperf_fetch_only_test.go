//go:build !(js && wasm)

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_014_FetchOnly holds the build's two changes to net/http to
// the toolchain in use: they must apply to its files exactly, the result must
// be the fetch-only client, and a file that is not the one they were written
// for must be refused rather than half-changed.
func TestTodo_CHATBUG_014_FetchOnly(t *testing.T) {
	goBin, err := goBinary()
	if err != nil {
		t.Skipf("no go toolchain available: %v", err)
	}
	dir := t.TempDir()
	overlayPath, err := writeFetchOnlyOverlay(goBin, dir)
	if err != nil {
		t.Fatalf("the fetch-only changes do not apply to this toolchain's net/http; update fetchOnlyClientEdits and fetchOnlyRoundTripEdits for it: %v", err)
	}
	raw, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	var overlay goOverlay
	if err := json.Unmarshal(raw, &overlay); err != nil {
		t.Fatalf("the overlay is not JSON the go command reads: %v", err)
	}
	if len(overlay.Replace) != 2 {
		t.Fatalf("the overlay replaces %d files, want client.go and roundtrip_js.go: %v", len(overlay.Replace), overlay.Replace)
	}
	changed := map[string]string{}
	for original, replacement := range overlay.Replace {
		if filepath.Base(filepath.Dir(original)) != "http" || filepath.Base(filepath.Dir(filepath.Dir(original))) != "net" {
			t.Fatalf("the overlay replaces %s, which is not a net/http source file", original)
		}
		body, err := os.ReadFile(replacement)
		if err != nil {
			t.Fatal(err)
		}
		changed[filepath.Base(original)] = string(body)
	}

	client := changed[fetchOnlyClientFile]
	if strings.Contains(client, "return DefaultTransport") {
		t.Fatal("http.Client still falls back to DefaultTransport, which keeps the dialling round trip and TLS in the module")
	}
	if !strings.Contains(client, "return fetchOnlyTransport{}") || !strings.Contains(client, "case fetchOnlyTransport:") {
		t.Fatal("http.Client does not fall back to, and recognise, the fetch-only round trip")
	}
	if strings.Contains(client, "case *Transport:") {
		t.Fatal("http.Client still names *Transport, which keeps its type and TLS in the module")
	}

	roundTrip := changed[fetchOnlyRoundTripFile]
	if strings.Contains(roundTrip, "t.roundTrip(req)") {
		t.Fatal("the js/wasm round trip can still reach the dialling round trip")
	}
	for _, want := range []string{"func (fetchOnlyTransport) RoundTrip(", "type fetchOnlyTransport struct{}", "func (t *Transport) RoundTrip(", `js.Global().Call("fetch"`} {
		if !strings.Contains(roundTrip, want) {
			t.Fatalf("the changed round trip is missing %q", want)
		}
	}

	// A file these edits were not written for is refused whole.
	if _, err := fetchOnlySource("client.go", []byte("package http\n"), fetchOnlyClientEdits, ""); !errors.Is(err, errFetchOnlyUnavailable) {
		t.Fatalf("an unrecognised client.go must be refused, got %v", err)
	}
	twice := "\treturn DefaultTransport\n}\n\treturn DefaultTransport\n}\n"
	if _, err := fetchOnlySource("client.go", []byte(twice), fetchOnlyClientEdits[:1], ""); !errors.Is(err, errFetchOnlyUnavailable) {
		t.Fatalf("an edit that matches twice must be refused, got %v", err)
	}
	// Line endings are not a difference in the text.
	crlf, err := fetchOnlySource("client.go", []byte("a\r\n\treturn DefaultTransport\r\n}\r\n"), fetchOnlyClientEdits[:1], "")
	if err != nil || string(crlf) != "a\n\treturn fetchOnlyTransport{}\n}\n" {
		t.Fatalf("a CRLF checkout of the same file must be accepted, got %q, %v", crlf, err)
	}
}
