package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/floorplan"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/page"
)

func digest(t *testing.T, s string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TestTodo_WEB_121_Golden pins the rendered tree's digest for the canonical
// Intent Workspace shell around both real Promotion pages, at a fixed route
// and a fixed session, resolved and widget-mounted through the exact same
// real registries TestTodo_WEB_121_Integration exercises
// (floorplan.PromotionRegistry, page.PromotionWidgetRegistry). A silent
// change to the shell's own chrome (nav, authority strip, document order)
// or to anything the page renderer produces (including its one canonical
// status region) or its registered widgets produce shows up here as a digest
// mismatch, the same way
// tools/uxqual/render/page's own TestTodo_WEB_026_Golden pins the page
// renderer alone.
//
// It also proves Build is deterministic directly: building each page a
// second time from the same Input produces the identical digest.
//
// UXAUDIT-007 re-pinned both digests: [SessionStrip] no longer renders
// fixtureSession's Purpose ("promotion_review") at all -- task-specific
// review context does not belong in this persistent, page-independent
// region -- so the shell's authority-context region changed shape even
// though the fixture session's other three fields are unchanged.
func TestTodo_WEB_121_Golden(t *testing.T) {
	widgets := page.PromotionWidgetRegistry()
	session := fixtureSession()

	cases := []struct {
		name       string
		resolution func(*testing.T) floorplan.Resolution
		route      journeyclient.Route
		wantDigest string
	}{
		{
			name:       "list",
			resolution: listResolution,
			route:      journeyclient.Parse(""),
			wantDigest: "sha256:757fd013f3a624cf7840d89fde25ab2ee9dcd4249ff9ecb97a01302b79bb959c",
		},
		{
			name:       "detail",
			resolution: detailResolution,
			route:      journeyclient.Parse("#/journeys/int_01JX6Y8B2C7D9EFG"),
			wantDigest: "sha256:c5984f6fdd025727dacfb58eacb463b6a4c1f380c6b18c6640cbf69294ba532b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{Resolution: tc.resolution(t), Widgets: widgets, Route: tc.route, Session: session}

			node1, err := Build(in)
			if err != nil {
				t.Fatalf("Build (first call): %v", err)
			}
			digest1 := digest(t, renderNode(t, node1))

			node2, err := Build(in)
			if err != nil {
				t.Fatalf("Build (second call): %v", err)
			}
			digest2 := digest(t, renderNode(t, node2))

			if digest1 != digest2 {
				t.Fatalf("Build is not deterministic for %q: first call digest %q, second call digest %q", tc.name, digest1, digest2)
			}
			if digest1 != tc.wantDigest {
				t.Fatalf("rendered shell digest for %q = %q, want pinned golden %q", tc.name, digest1, tc.wantDigest)
			}
		})
	}
}
