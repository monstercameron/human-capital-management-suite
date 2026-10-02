//go:build !(js && wasm)

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CHATBUG-014: the browser client carried a complete TLS stack it can never
// run. Every request it makes goes through the browser's fetch, but net/http's
// client names http.DefaultTransport, and *http.Transport keeps its dialling
// round trip (crypto/tls, crypto/x509, the elliptic curves, ASN.1) reachable for
// a page that sets a dialer -- which a browser page cannot do. The linker cannot
// know that, so about 2.7 MB of the module (0.58 MB compressed) was code no
// request could reach.
//
// The build therefore compiles net/http with two of the toolchain's own files
// changed, through the go command's -overlay: the js/wasm round trip becomes a
// type of its own that only ever calls fetch, and http.Client falls back to
// that type instead of to DefaultTransport. Nothing in the repository's sources
// changes and no call site knows; a request behaves exactly as before, because
// DefaultTransport on js/wasm already had no dialer and always took the fetch
// path.
//
// The changed files are derived from the toolchain that is doing the build,
// never from a copy kept here, and every edit must match exactly once. A
// toolchain whose net/http no longer has these lines gets an ordinary build and
// a line saying so; the size gate then decides whether that build ships.

// errFetchOnlyUnavailable reports a toolchain whose net/http does not have the
// lines the fetch-only build changes.
var errFetchOnlyUnavailable = errors.New("net/http in this toolchain does not match the fetch-only client build")

const (
	fetchOnlyClientFile    = "client.go"
	fetchOnlyRoundTripFile = "roundtrip_js.go"
)

// fetchOnlyEdit is one exact replacement in a toolchain source file.
type fetchOnlyEdit struct{ old, new string }

// fetchOnlyClientEdits make http.Client fall back to the fetch-only round trip
// and recognise it as one that honours a request's context.
var fetchOnlyClientEdits = []fetchOnlyEdit{
	{
		old: "\treturn DefaultTransport\n}",
		new: "\treturn fetchOnlyTransport{}\n}",
	},
	{
		old: "\tswitch t := rt.(type) {\n\tcase *Transport:\n\t\tif altRT := t.alternateRoundTripper(req); altRT != nil {\n\t\t\treturn knownRoundTripperImpl(altRT, req)\n\t\t}\n\t\treturn true\n",
		new: "\tswitch rt.(type) {\n\tcase fetchOnlyTransport:\n\t\treturn true\n",
	},
}

// fetchOnlyRoundTripEdits move the fetch round trip off *Transport and drop its
// way into the dialling round trip.
var fetchOnlyRoundTripEdits = []fetchOnlyEdit{
	{
		old: "func (t *Transport) RoundTrip(req *Request) (*Response, error) {",
		new: "func (fetchOnlyTransport) RoundTrip(req *Request) (*Response, error) {",
	},
	{
		old: "\tif t.Dial != nil || t.DialContext != nil || t.DialTLS != nil || t.DialTLSContext != nil || jsFetchMissing || jsFetchDisabled {\n\t\treturn t.roundTrip(req)\n\t}",
		new: "\tif jsFetchMissing || jsFetchDisabled {\n\t\treturn nil, errors.New(\"net/http: fetch is not available\")\n\t}",
	},
}

// fetchOnlyRoundTripTail declares the fetch-only type and keeps *Transport a
// RoundTripper, which the package's own declarations require.
const fetchOnlyRoundTripTail = `
// fetchOnlyTransport is the round trip of a browser page: the Fetch API and
// nothing else. It is added by the hcm-next client build.
type fetchOnlyTransport struct{}

// RoundTrip keeps *Transport a RoundTripper. A page that builds a Transport of
// its own still makes its requests through fetch.
func (t *Transport) RoundTrip(req *Request) (*Response, error) {
	return fetchOnlyTransport{}.RoundTrip(req)
}
`

// fetchOnlySource applies edits to one toolchain file. Every edit must match
// exactly once: anything else means the file is not the one these edits were
// written for.
func fetchOnlySource(name string, source []byte, edits []fetchOnlyEdit, tail string) ([]byte, error) {
	// A toolchain checked out with CRLF line endings holds the same text.
	out := bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
	for _, edit := range edits {
		if n := bytes.Count(out, []byte(edit.old)); n != 1 {
			return nil, fmt.Errorf("%w: %s has %d matches for %q", errFetchOnlyUnavailable, name, n, edit.old)
		}
		out = bytes.Replace(out, []byte(edit.old), []byte(edit.new), 1)
	}
	return append(out, tail...), nil
}

// writeFetchOnlyOverlay writes the two changed files and the overlay that
// points the build at them into dir, and returns the overlay's path.
func writeFetchOnlyOverlay(goBin, dir string) (string, error) {
	root, err := goRoot(goBin)
	if err != nil {
		return "", err
	}
	source := filepath.Join(root, "src", "net", "http")
	replace := make(map[string]string, 2)
	for _, file := range []struct {
		name  string
		edits []fetchOnlyEdit
		tail  string
	}{
		{fetchOnlyClientFile, fetchOnlyClientEdits, ""},
		{fetchOnlyRoundTripFile, fetchOnlyRoundTripEdits, fetchOnlyRoundTripTail},
	} {
		original := filepath.Join(source, file.name)
		body, err := os.ReadFile(original)
		if err != nil {
			return "", fmt.Errorf("%w: %v", errFetchOnlyUnavailable, err)
		}
		changed, err := fetchOnlySource(file.name, body, file.edits, file.tail)
		if err != nil {
			return "", err
		}
		target := filepath.Join(dir, "net_http_"+file.name)
		if err := os.WriteFile(target, changed, 0o644); err != nil {
			return "", fmt.Errorf("writing %s: %w", target, err)
		}
		replace[original] = target
	}
	body, err := json.MarshalIndent(goOverlay{Replace: replace}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode fetch-only overlay: %w", err)
	}
	overlay := filepath.Join(dir, "fetch-only-overlay.json")
	if err := os.WriteFile(overlay, append(body, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", overlay, err)
	}
	return overlay, nil
}
