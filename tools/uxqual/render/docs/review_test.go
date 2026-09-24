package docs

import (
	"strings"
	"testing"
)

// TestRenderReview exercises review.go: the review screen shows the exact
// hash, scope and diff, and the review/deploy forms both carry the same
// version hash so the UI cannot diverge between what a reviewer approves
// and what a deploy submits.
func TestRenderReview(t *testing.T) {
	page, err := RenderReview(ReviewPage{
		Locale: "en-US", Title: "Review and publish",
		Reviews: []ReviewItem{{
			DocumentID: "doc-team", VersionID: "version-7", VersionHash: "9e1e4a7c", Title: "Team handbook",
			Scope: "People Ops (team)", Diff: "- old\n+ new",
			ReviewState: "pending", ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
			CanReview: true, CanDeploy: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"9e1e4a7c", "People Ops (team)", "- old", "&#43; new",
		`action="/docs/doc-team/review"`, `action="/docs/doc-team/deploy"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("review page missing %q", want)
		}
	}
	if strings.Count(page, "9e1e4a7c") < 3 {
		t.Fatal("version hash must appear on screen and in both review and deploy forms")
	}
}

// TestRenderReview_UnauthorizedHidesActions exercises the security-critical
// path: when neither review nor deploy is currently authorized, no action
// form is rendered, regardless of a prior review state.
func TestRenderReview_UnauthorizedHidesActions(t *testing.T) {
	page, err := RenderReview(ReviewPage{
		Locale: "en-US", Title: "Review and publish",
		Reviews: []ReviewItem{{
			DocumentID: "doc-team", VersionID: "version-7", VersionHash: "9e1e4a7c", Title: "Team handbook",
			ReviewState: "approved", ReviewAction: "/docs/doc-team/review", DeployAction: "/docs/doc-team/deploy",
			CanReview: false, CanDeploy: false,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "<form") {
		t.Fatal("unauthorized review item rendered an action form")
	}
	if !strings.Contains(page, "do not currently have authority") {
		t.Fatal("unauthorized review item did not explain why")
	}
}
