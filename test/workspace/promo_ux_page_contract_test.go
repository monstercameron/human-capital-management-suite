package workspace_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
)

// TestPromoUXWorkspaceSelectionKeepsExactMoney is the served-page half of
// the promotion regression. The worker and target position are rendered from
// the live cell, and the compensation control keeps decimal text intact for
// the journey client to send to the server.
func TestPromoUXWorkspaceSelectionKeepsExactMoney(t *testing.T) {
	t.Parallel()
	c := newCell(t, true)
	before := c.fingerprint()

	page := c.get(promotionURL, compAdmin.name)
	if page.Status != http.StatusOK {
		t.Fatalf("promotion workspace answered %d, want 200\n%s", page.Status, page.Body)
	}
	for _, want := range []string{"omar-reyes", "USD 98,000.00", "Run simulation"} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("promotion workspace omitted %q", want)
		}
	}
	if strings.Contains(page.Body, "POS-HRBP-301") {
		t.Fatal("legacy corpus position was presented as a verified live vacancy")
	}
	answers, err := forms.ExtractFormAnswers(page.Body)
	if err != nil {
		t.Fatalf("extract the served promotion form: %v", err)
	}
	canonical, err := workspace.CanonicalizePresentationAnswers(workspace.ResolveLocale("en-US"), "USD", answers)
	if err != nil {
		t.Fatalf("canonicalize the served promotion form: %v", err)
	}
	if got := canonical[workspace.FieldProposedComp]; got != "98000.00" {
		t.Fatalf("served compensation control canonicalized to %q, want exact 98000.00", got)
	}
	if got := answers[workspace.FieldTargetPosition]; got != "" {
		t.Fatalf("unverified target position = %q, want no preselected vacancy", got)
	}
	if !strings.Contains(answers[workspace.FieldProposedComp], ".00") {
		t.Error("promotion workspace rendered the money control without its exact decimal scale")
	}
	if after := c.fingerprint(); after != before {
		t.Fatalf("rendering the promotion workspace changed PostgreSQL\nbefore: %s\nafter:  %s", before, after)
	}
}
