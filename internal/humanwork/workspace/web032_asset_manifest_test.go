package workspace

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTodo_WEB_032(t *testing.T) {
	manifest, err := BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Assets) == 0 {
		t.Fatal("generated manifest is empty")
	}
	for _, asset := range manifest.Assets {
		body, ok := embeddedAsset(strings.TrimPrefix(asset.Path, PathAssetPrefix))
		if !ok {
			t.Fatalf("manifest includes unavailable asset %q", asset.Path)
		}
		if err := manifest.VerifyAsset(asset.Path, "identity", body); err != nil {
			t.Fatalf("verify %q: %v", asset.Path, err)
		}
	}
}

func TestTodo_WEB_032_Golden(t *testing.T) {
	first, err := BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildEmbeddedAssetIntegrityManifest()
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := first.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := second.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("manifest generation is not reproducible")
	}
	committedJSON, err := fs.ReadFile(assetsFS, "assets/"+AssetIntegrityManifestName)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(committedJSON) {
		t.Fatal("generated manifest artifact does not exactly describe the clean embedded catalog")
	}
	var decoded map[string]any
	if err := json.Unmarshal(firstJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["version"] != AssetIntegrityManifestVersion {
		t.Fatalf("manifest version = %v", decoded["version"])
	}
}

func TestTodo_WEB_032_Browser(t *testing.T) {
	handler, token := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathAssetPrefix+AssetIntegrityManifestName, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("manifest route = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("manifest content type = %q", got)
	}
	if _, err := ParseAssetIntegrityManifest(recorder.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(recorder.Body.String(), "tenant") || strings.Contains(recorder.Body.String(), "principal") {
		t.Fatal("asset manifest carries browser/business authority")
	}
}

func TestTodo_WEB_032_Conformance(t *testing.T) {
	manifest, err := GenerateAssetIntegrityManifest([]AssetIntegritySource{
		{Name: "z.js", Body: []byte("z"), ContentType: "text/javascript"},
		{Name: "a.js", Body: []byte("a"), ContentType: "text/javascript"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Assets[0].Path != PathAssetPrefix+"a.js" {
		t.Fatalf("canonical order = %q", manifest.Assets[0].Path)
	}
	if !strings.HasPrefix(manifest.Assets[0].Integrity, "sha256-") {
		t.Fatalf("missing SRI token: %q", manifest.Assets[0].Integrity)
	}
	for name, source := range map[string]string{"journey": journeyLoaderSource, "workspace": loaderSource} {
		// The executable fetches went through a caching helper for UXLIVE-013,
		// so they now name their integrity as an argument rather than at the
		// call site. What this test is actually protecting is unchanged: every
		// executable byte is requested through o(), which is the only thing
		// that carries the bearer token and the SRI pin.
		for _, required := range []string{PathAssetManifest, PathWasmExec, "k(u,i)", "return fetch(u,o(i))", ",s.integrity)", ",a.integrity)", `authorization:"Bearer "+(j.bearer||"")`, `credentials:"same-origin"`} {
			if !strings.Contains(source, required) {
				t.Errorf("%s loader does not authenticate and integrity-pin %q", name, required)
			}
		}
		if strings.Contains(source, `fetch("`+PathJourneyWasm+`",{integrity:`) {
			t.Errorf("%s loader fetches executable bytes without the authenticated options helper", name)
		}
	}
}

func TestTodo_WEB_032_Security(t *testing.T) {
	for _, name := range []string{"../secret.js", `..\\secret.js`, "/secret.js", "x.js?token=secret", "x.js#fragment", "x\x00.js"} {
		if _, err := GenerateAssetIntegrityManifest([]AssetIntegritySource{{Name: name, Body: []byte("x"), ContentType: "text/javascript"}}); err == nil {
			t.Errorf("unsafe asset name %q was accepted", name)
		}
	}
	manifest, err := GenerateAssetIntegrityManifest([]AssetIntegritySource{{Name: "app.js", Body: []byte("good"), ContentType: "text/javascript"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.VerifyAsset(PathAssetPrefix+"app.js", "identity", []byte("tampered")); err == nil {
		t.Fatal("tampered asset was accepted")
	}
	for label, body := range map[string]string{
		"empty catalog":      `{"version":"hcm-next.asset-integrity.v1","assets":[]}`,
		"zero bytes":         `{"version":"hcm-next.asset-integrity.v1","assets":[{"path":"/workspace/assets/a.js","content_type":"text/javascript","bytes":0,"sha256":"` + strings.Repeat("a", 64) + `","integrity":"sha256-","representations":[]}]}`,
		"invalid media type": `{"version":"hcm-next.asset-integrity.v1","assets":[{"path":"/workspace/assets/a.js","content_type":"not-a-media-type","bytes":1,"sha256":"` + strings.Repeat("a", 64) + `","integrity":"sha256-","representations":[]}]}`,
	} {
		if _, err := ParseAssetIntegrityManifest([]byte(body)); err == nil {
			t.Errorf("%s manifest was accepted", label)
		}
	}
}

func TestTodo_WEB_032_Integration(t *testing.T) {
	handler, token := newShellHandler(t, false)
	request := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathAssetPrefix+"harborcare-logo.svg", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("asset route = %d", recorder.Code)
	}
	if recorder.Header().Get("ETag") == "" {
		t.Fatal("served asset has no digest validator")
	}
	wantETag := handler.assetIndex[PathAssetPrefix+"harborcare-logo.svg"].ETags["identity"]
	if got := recorder.Header().Get("ETag"); got != wantETag {
		t.Fatalf("served asset ETag = %q, want preindexed %q", got, wantETag)
	}
	conditional := httptest.NewRequest(http.MethodGet, "http://cell.test"+PathAssetPrefix+"harborcare-logo.svg", nil)
	conditional.Header.Set("Authorization", "Bearer "+token)
	conditional.Header.Set("If-None-Match", wantETag)
	conditionalRecorder := httptest.NewRecorder()
	handler.ServeHTTP(conditionalRecorder, conditional)
	if conditionalRecorder.Code != http.StatusNotModified || conditionalRecorder.Body.Len() != 0 {
		t.Fatalf("preindexed conditional response = %d with %d bytes", conditionalRecorder.Code, conditionalRecorder.Body.Len())
	}
}

func TestTodo_WEB_032_Fault(t *testing.T) {
	manifest, err := GenerateAssetIntegrityManifest([]AssetIntegritySource{{Name: "app.js", Body: []byte("good"), ContentType: "text/javascript", GzipBody: []byte("gzip")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.VerifyAsset(PathAssetPrefix+"app.js", "br", []byte("gzip")); err == nil {
		t.Fatal("unsupported representation was accepted")
	}
	if _, err := ParseAssetIntegrityManifest([]byte(`{"version":"hcm-next.asset-integrity.v1","assets":[]} {"extra":true}`)); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	mutated := manifest
	mutated.Assets[0].SHA256 = strings.Repeat("a", 64)
	if err := ValidateAssetIntegrityCatalog(mutated, []AssetIntegritySource{{Name: "app.js", Body: []byte("good"), ContentType: "text/javascript", GzipBody: []byte("gzip")}}); err == nil {
		t.Fatal("mutated release artifact was accepted")
	}
	reordered, err := GenerateAssetIntegrityManifest([]AssetIntegritySource{{Name: "app.js", Body: []byte("good"), ContentType: "text/javascript", GzipBody: []byte("gzip")}})
	if err != nil {
		t.Fatal(err)
	}
	reordered.Assets[0].Representations[0], reordered.Assets[0].Representations[1] = reordered.Assets[0].Representations[1], reordered.Assets[0].Representations[0]
	if err := reordered.Validate(); err == nil {
		t.Fatal("noncanonical representation order was accepted")
	}
}

func BenchmarkAssetIntegrityManifest(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := BuildEmbeddedAssetIntegrityManifest(); err != nil {
			b.Fatal(err)
		}
	}
}
