package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// UXLIVE-013's RED was measured on the running server: journey.wasm is
// 37.7 MB decoded and 7.8 MB encoded, and PerformanceResourceTiming reported
// a full transfer on every workspace navigation -- 200, not 304 -- while
// manifest.json and wasm_exec.js, same origin and same headers, reported 0.
// Every in-app link is a document load, so the bundle was paid for on every
// click.
//
// The server was already correct about revalidation; what it could not do
// was let the browser keep a copy. An address that names the bytes it wants
// can be answered once and kept, because the address changes whenever the
// bytes do.

func uxlive013Asset(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	handler, token := newShellHandler(t, false)
	target := "http://cell.test" + PathWasmExec
	if query != "" {
		target += "?" + query
	}
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("asset route %q = %d: %s", target, recorder.Code, recorder.Body.String())
	}
	return recorder
}

// TestTodo_UXLIVE_013 is the primary red/green test: an address that names
// the asset's own digest is answered immutably; anything else still
// revalidates.
func TestTodo_UXLIVE_013(t *testing.T) {
	plain := uxlive013Asset(t, "")
	if got := plain.Header().Get("Cache-Control"); got != "private, max-age=0, must-revalidate" {
		t.Fatalf("an unversioned asset address changed policy: %q", got)
	}
	digest := strings.Trim(plain.Header().Get("ETag"), `"`)
	if digest == "" {
		t.Fatalf("the asset carries no ETag to address it by")
	}

	addressed := uxlive013Asset(t, "v="+digest)
	got := addressed.Header().Get("Cache-Control")
	if !strings.Contains(got, "immutable") {
		t.Fatalf("a content-addressed asset is still revalidated on every load: %q", got)
	}
	if !strings.HasPrefix(got, "private,") {
		t.Fatalf("an authenticated asset was made cacheable beyond this reader: %q", got)
	}
	if addressed.Code != http.StatusOK || addressed.Body.Len() != plain.Body.Len() {
		t.Fatalf("the addressed response differs from the plain one: %d/%d bytes, status %d",
			addressed.Body.Len(), plain.Body.Len(), addressed.Code)
	}

	// A wrong or stale address is not trusted: it falls back to revalidation
	// rather than pinning bytes the caller guessed at.
	wrong := uxlive013Asset(t, "v=0000000000000000000000000000000000000000000000000000000000000000")
	if strings.Contains(wrong.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("an address naming the wrong digest was answered immutably")
	}

	// The budget the GREEN asks for, measured the way the reader pays it: a
	// repeat navigation must transfer no executable bundle bytes at all. The
	// ceiling is zero rather than a tolerance, because anything above zero
	// means the browser is being asked for the bundle again on a click.
	handler, token := newShellHandler(t, false)
	kept := map[string]bool{}
	first := uxlive013Navigation(t, handler, token, kept)
	if first == 0 {
		t.Fatalf("the first navigation transferred nothing; the measurement is not exercising the bundle")
	}
	if repeat := uxlive013Navigation(t, handler, token, kept); repeat != 0 {
		t.Fatalf("a repeat navigation transferred %d bundle bytes, budget 0 (first navigation was %d)", repeat, first)
	}
}

// TestTodo_UXLIVE_013_Browser proves the loader stamps the address, so the
// policy above is actually reached by the page rather than only reachable.
func TestTodo_UXLIVE_013_Browser(t *testing.T) {
	for name, source := range map[string]string{"product": loaderSource, "journey": journeyLoaderSource} {
		if !strings.Contains(source, `function v(a){return a&&a.sha256?"?v="+encodeURIComponent(a.sha256):""}`) {
			t.Fatalf("%s loader does not derive an address from the manifest digest", name)
		}
		if strings.Count(source, "+v(") != 2 {
			t.Fatalf("%s loader stamps %d of its 2 asset addresses", name, strings.Count(source, "+v("))
		}
		if !strings.Contains(source, `integrity:i||""`) {
			t.Fatalf("%s loader dropped subresource integrity, which is what makes a kept copy safe", name)
		}
		// The immutable policy is necessary but not sufficient. Measured in a
		// browser, every asset under it is stored and reused except the
		// bundle, which is refused for its size alone, so the loader keeps
		// its own copy in storage that has no such cap.
		if !strings.Contains(source, `caches.open("hcmnext-assets")`) {
			t.Fatalf("%s loader relies only on the HTTP cache, which refuses an entry the size of the bundle", name)
		}
		if strings.Count(source, "k(\"") != 2 {
			t.Fatalf("%s loader routes %d of its 2 executable fetches through the cache", name, strings.Count(source, "k(\""))
		}
	}
}

// TestTodo_UXLIVE_013_Security keeps the asset boundary where it was: the
// address is presentation only, it never selects bytes, and the response
// stays private to the authenticated reader.
func TestTodo_UXLIVE_013_Security(t *testing.T) {
	plain := uxlive013Asset(t, "")
	digest := strings.Trim(plain.Header().Get("ETag"), `"`)
	addressed := uxlive013Asset(t, "v="+digest)
	if addressed.Header().Get("ETag") != plain.Header().Get("ETag") {
		t.Fatalf("the address changed which bytes were served")
	}
	if strings.Contains(addressed.Header().Get("Cache-Control"), "public") {
		t.Fatalf("an authenticated asset became publicly cacheable: %q", addressed.Header().Get("Cache-Control"))
	}
	if addressed.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("the addressed response lost its content-type guard")
	}

	// What the loader keeps is keyed by the address, which always carries the
	// digest, and a stored entry under any other digest for the same asset is
	// dropped as soon as a new one is kept. So a kept copy can never answer
	// for bytes it is not, and a superseded bundle cannot linger.
	for name, source := range map[string]string{"product": loaderSource, "journey": journeyLoaderSource} {
		if !strings.Contains(source, "c.match(u)") || !strings.Contains(source, "c.put(u,r.clone())") {
			t.Errorf("%s loader keys its stored copy by something other than the addressed URL", name)
		}
		if !strings.Contains(source, "c.delete(ks[n])") {
			t.Errorf("%s loader never drops a superseded bundle", name)
		}
		if !strings.Contains(source, "if(r.ok){c.put(") {
			t.Errorf("%s loader would keep a failed or error response", name)
		}
	}
}

// TestTodo_UXLIVE_013_Golden pins the two policies by their exact text. They
// are the whole of the fix: one says "ask me again every time", the other
// says "never ask again", and which one a request gets is decided solely by
// whether its address names the bytes.
func TestTodo_UXLIVE_013_Golden(t *testing.T) {
	plain := uxlive013Asset(t, "")
	digest := strings.Trim(plain.Header().Get("ETag"), `"`)
	addressed := uxlive013Asset(t, assetVersionQueryKey+"="+digest)

	golden := map[string]string{
		"unversioned": "private, max-age=0, must-revalidate",
		"addressed":   "private, max-age=31536000, immutable",
	}
	got := map[string]string{
		"unversioned": plain.Header().Get("Cache-Control"),
		"addressed":   addressed.Header().Get("Cache-Control"),
	}
	for key, want := range golden {
		if got[key] != want {
			t.Errorf("%s policy = %q, want %q", key, got[key], want)
		}
	}
	if assetVersionQueryKey != "v" {
		t.Fatalf("the address key is %q; the loader writes \"v\"", assetVersionQueryKey)
	}
}

// uxlive013Navigation replays one document load's asset fetches through a
// cache that obeys Cache-Control the way a browser does, and returns the
// bytes it had to transfer.
func uxlive013Navigation(t *testing.T, handler *Handler, token string, kept map[string]bool) int {
	t.Helper()
	manifest, err := BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		t.Fatal(err)
	}
	transferred := 0
	for _, asset := range manifest.Assets {
		if asset.Path != PathWasm && asset.Path != PathJourneyWasm && asset.Path != PathWasmExec {
			continue
		}
		target := "http://cell.test" + asset.Path + "?" + assetVersionQueryKey + "=" + asset.SHA256
		if kept[target] {
			continue
		}
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s = %d", asset.Path, recorder.Code)
		}
		transferred += recorder.Body.Len()
		if strings.Contains(recorder.Header().Get("Cache-Control"), "immutable") {
			kept[target] = true
		}
	}
	return transferred
}

// BenchmarkTodo_UXLIVE_013 reports the same budget as a number, so a
// regression shows up as bytes per navigation rather than only as a failure.
func BenchmarkTodo_UXLIVE_013(b *testing.B) {
	handler, token := newShellHandler(&testing.T{}, false)
	manifest, err := BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		b.Fatal(err)
	}
	var bundle AssetIntegrity
	for _, asset := range manifest.Assets {
		if asset.Path == PathJourneyWasm {
			bundle = asset
		}
	}
	if bundle.SHA256 == "" {
		b.Fatalf("the bundle carries no digest to address it by")
	}
	target := "http://cell.test" + bundle.Path + "?" + assetVersionQueryKey + "=" + bundle.SHA256

	transferred := 0
	kept := false
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if kept {
			continue
		}
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		transferred += recorder.Body.Len()
		kept = strings.Contains(recorder.Header().Get("Cache-Control"), "immutable")
	}
	b.StopTimer()
	if b.N > 0 {
		b.ReportMetric(float64(transferred)/float64(b.N), "bytes/nav")
	}
}
