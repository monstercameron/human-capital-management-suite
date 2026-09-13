package workspace_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// TestTodo_UX_004_Integration drives the locale path through the live cell.
// It proves localized form values become the same canonical simulation input,
// the locale survives a no-script post, and neither path writes data.
func TestTodo_UX_004_Integration(t *testing.T) {
	t.Parallel()
	c := newCell(t, true)
	before := c.fingerprint()

	localizedURL := promotionURL + "&" + workspace.ParamLocale + "=de-DE"
	page := c.get(localizedURL, compAdmin.name)
	if page.Status != http.StatusOK {
		t.Fatalf("GET localized workspace answered %d, want 200\n%s", page.Status, page.Body)
	}
	if got := page.Header.Get("X-HCM-Workspace-Locale"); got != "de-DE" {
		t.Errorf("locale header = %q, want de-DE", got)
	}
	for _, want := range []string{
		`<html lang="de-DE">`,
		`name="locale" value="de-DE"`,
		"Aktuelles Grundgehalt",
		"93.000,00 USD",
		"98.000,00 USD",
		"01. Juni 2026",
	} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("localized workspace does not carry %q", want)
		}
	}

	res := c.post(workspace.PathSimulate, compAdmin.name, map[string]string{
		workspace.ParamCSRF:             page.csrfToken(t),
		workspace.ParamWorker:           testWorker,
		workspace.ParamLocale:           "de-DE",
		workspace.ParamTransition:       workspace.TransitionRunSimulation,
		workspace.FieldProposedJobTitle: "OPS-HRBP3",
		workspace.FieldProposedGrade:    "P3",
		workspace.FieldProposedComp:     "105.000,00 USD",
		workspace.FieldEffectiveDate:    "01. Juni 2026",
		workspace.FieldBusinessReason:   "retention_adjustment",
	}, page.sessionCookie(t))
	if res.Status != http.StatusOK {
		t.Fatalf("POST localized workspace answered %d, want 200\n%s", res.Status, res.Body)
	}
	for _, want := range []string{"105.000,00 USD", "01. Juni 2026", "compensation.increase_over_ten_percent"} {
		if !strings.Contains(res.Body, want) {
			t.Errorf("localized simulation does not carry %q", want)
		}
	}

	unsupported := c.get(promotionURL+"&"+workspace.ParamLocale+"=x-unreviewed", compAdmin.name)
	if unsupported.Status != http.StatusOK {
		t.Fatalf("GET unsupported locale answered %d, want 200\n%s", unsupported.Status, unsupported.Body)
	}
	if got := unsupported.Header.Get("X-HCM-Workspace-Locale-Fallback"); got != "unsupported_locale" {
		t.Errorf("fallback header = %q, want unsupported_locale", got)
	}
	for _, want := range []string{`data-locale-fallback="unsupported_locale"`, "Requested locale x-unreviewed is unsupported; using en-US.", `name="locale" value="en-US"`} {
		if !strings.Contains(unsupported.Body, want) {
			t.Errorf("unsupported-locale response does not explicitly report %q", want)
		}
	}

	if after := c.fingerprint(); after != before {
		t.Fatalf("locale rendering changed the database\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestTodo_UX_004_Browser is the browser-equivalent Go check. It runs the
// actual localized SSR response through the repository's DOM qualification
// checks and verifies the progressive Go/WASM enhancement remains attached.
func TestTodo_UX_004_Browser(t *testing.T) {
	t.Parallel()
	c := newCell(t, true)
	page := c.get(promotionURL+"&"+workspace.ParamLocale+"=de-DE", compAdmin.name)
	if page.Status != http.StatusOK {
		t.Fatalf("GET localized workspace answered %d, want 200\n%s", page.Status, page.Body)
	}

	result := qual.RunDocumentChecks("localized-served-workspace", page.Body, workspace.MaskedActionNeedles())
	for _, criterion := range result.All() {
		if criterion.Name != "" && !criterion.Pass {
			t.Errorf("%s: FAIL %s", criterion.Name, criterion.Detail)
		}
	}
	// The legacy uxqual.wasm enhancement is deliberately withheld (see
	// internal/humanwork/workspace/assets.go), so a localized page must match
	// whichever posture this build serves rather than assume the bundle.
	if workspace.BundleBuilt() {
		if !strings.Contains(page.Body, `<script type="application/json" id="gwc-contract">`) || !strings.Contains(page.Body, workspace.PathWasm) {
			t.Error("localized workspace dropped its progressive Go/WASM enhancement")
		}
	} else if strings.Contains(page.Body, "<script") || strings.Contains(page.Body, workspace.PathWasm) {
		t.Error("localized native-only workspace advertises a script or the withheld Go/WASM enhancement")
	}
	if !strings.Contains(page.Body, `<html lang="de-DE">`) {
		t.Error("localized workspace does not publish its resolved document language")
	}
}
