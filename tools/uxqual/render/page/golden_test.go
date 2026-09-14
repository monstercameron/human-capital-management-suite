package page

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/floorplan"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// digestOf hashes a rendered tree's bytes the same way tools/uxqual/ssrshell
// hashes RenderedShell.HTML: sha256, hex-encoded, "sha256:" prefixed. It
// exists so this package's golden test can pin a short, unambiguous digest
// rather than a multi-kilobyte literal, and so a determinism regression
// (two renders of the same input producing different bytes) is caught the
// same way even before anyone diffs the bytes by hand.
func digestOf(t *testing.T, node ui.Node) string {
	t.Helper()
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatalf("ui.RenderToString: %v", err)
	}
	sum := sha256.Sum256([]byte(out))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TestTodo_WEB_026_Golden pins the rendered tree's digest for the two real
// Promotion PageDefinitions, resolved through the real Gate A floorplan
// registry and rendered through the real Promotion widget registry -- the
// same pipeline TestTodo_WEB_026_Integration exercises, but proving
// byte-level determinism rather than substring content: a silent change to
// the landmark mapping, the region/heading/slot assembly, or any registered
// widget's own output shows up here as a digest mismatch, matching how
// tools/uxqual/ssrshell's own TestTodo_WEB_025_Golden pins
// RenderedShell.Digest for the same two pages.
//
// It also proves determinism directly: rendering each page a second time,
// from the same PageDefinition/Floorplan/Registry values, must produce the
// identical digest -- [Render] takes no clock reading, no random id, and no
// I/O, so nothing about a second call can legitimately differ.
func TestTodo_WEB_026_Golden(t *testing.T) {
	fpReg := floorplan.PromotionRegistry()
	widgets := PromotionWidgetRegistry()

	cases := []struct {
		name       string
		pd         pagedef.PageDefinition
		wantDigest string
	}{
		{"list", pagedef.PromotionListPageDefinition(), "sha256:d085a46d5fe88c92006790a644c2925ab2032e7d61b40910f47f60b4bc7d1de7"},
		{"detail", pagedef.PromotionDetailPageDefinition(), "sha256:f18e5c7689c770c4140bbdd5193128b18395867c8e9d6729e0ed452aae4479d0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := fpReg.Resolve(tc.pd)
			if err != nil {
				t.Fatalf("floorplan.Registry.Resolve: %v", err)
			}

			node1, err := Render(res, widgets)
			if err != nil {
				t.Fatalf("Render (first call): %v", err)
			}
			digest1 := digestOf(t, node1)

			node2, err := Render(res, widgets)
			if err != nil {
				t.Fatalf("Render (second call): %v", err)
			}
			digest2 := digestOf(t, node2)

			if digest1 != digest2 {
				t.Fatalf("Render is not deterministic for page %q: first call digest %q, second call digest %q", tc.pd.PageID, digest1, digest2)
			}
			if digest1 != tc.wantDigest {
				t.Fatalf("rendered tree digest for page %q = %q, want pinned golden %q", tc.pd.PageID, digest1, tc.wantDigest)
			}
		})
	}
}
