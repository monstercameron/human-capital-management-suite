package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Once UXBLIND-085 let the appearance page's brand-asset request through the
// content policy, the client read next_before with js.Value.Int; the server
// omits it on the last page, the read panicked and the WASM client exited.
func TestDecodeBrandAssetPageToleratesTheLastPageAndRemovedRevisions(t *testing.T) {
	locale := productui.ResolveProductLocale("en-US")
	body := []byte(`{"assets":[{"revision":3,"head_revision":3,"digest":"d3","name":"Ironridge","url":"/workspace/brand-assets/d3","width":180,"height":40,"can_remove":true},{"revision":2,"head_revision":3,"digest":"d2","name":"Old","removed":true}]}`)
	assets, next, err := decodeBrandAssetPage(body, locale)
	if err != nil {
		t.Fatal(err)
	}
	if next != 0 {
		t.Fatalf("last page cursor = %d, want 0", next)
	}
	if len(assets) != 1 {
		t.Fatalf("assets = %+v, want the one live revision", assets)
	}
	got := assets[0]
	if got.Revision != 3 || got.HeadRevision != 3 || got.URL != "/workspace/brand-assets/d3" || got.Width != 180 || got.Height != 40 || !got.CanRemove || got.CanRollback || got.Digest != "d3" {
		t.Fatalf("projected option = %+v", got)
	}
	if got.Label != "Ironridge · "+locale.Text("appearance.asset_revision_label", map[string]string{"revision": "3"}) {
		t.Fatalf("label = %q", got.Label)
	}

	_, next, err = decodeBrandAssetPage([]byte(`{"assets":[],"next_before":7}`), locale)
	if err != nil || next != 7 {
		t.Fatalf("paged cursor = %d, %v; want 7", next, err)
	}
	if _, _, err := decodeBrandAssetPage([]byte(`<html>`), locale); err == nil {
		t.Fatal("a non-JSON answer decoded without error")
	}
}
