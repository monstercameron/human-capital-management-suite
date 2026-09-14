package workspace_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// promotionURL is the workspace address for the corpus scenario worker.
const promotionURL = workspace.PathPromotion + "?" + workspace.ParamWorker + "=" + testWorker

// TestPromotionWorkspaceRendersFromTheLiveCellWithZeroEffects drives the
// served workspace end to end over a composed cell and an ephemeral
// PostgreSQL: it renders, it submits, it shows a receipt, it masks what the
// policy withheld, and it writes nothing at all while doing so.
func TestPromotionWorkspaceRendersFromTheLiveCellWithZeroEffects(t *testing.T) {
	t.Parallel()
	c := newCell(t, true)
	before := c.fingerprint()

	var (
		page    response
		session *http.Cookie
		token   string
	)

	t.Run("a GET renders the fixture worker from live cell data", func(t *testing.T) {
		page = c.get(promotionURL, compAdmin.name)
		if page.Status != http.StatusOK {
			t.Fatalf("GET %s answered %d, want 200\n%s", promotionURL, page.Status, page.Body)
		}
		session = page.sessionCookie(t)
		token = page.csrfToken(t)

		// The worker's real corpus identity and current placement, which only
		// the governed read could have supplied.
		for _, want := range []string{
			"Omar",                  // preferred name, from person.preferred_name
			"OPS-HRBP2",             // current job code, from assignment.job_code
			"OPS-HRBP3",             // the proposed placement
			"BAND-OPS-P3-USEAST",    // the band the live catalog resolved
			"Approval timeline",     // the workflow-simulation chronology section
			"req.promotion.hrbp/v1", // one approval requirement the plan would await
		} {
			if !strings.Contains(page.Body, want) {
				t.Errorf("the rendered workspace does not carry %q", want)
			}
		}
	})

	t.Run("the masked actions are absent, not blanked", func(t *testing.T) {
		for _, needle := range workspace.MaskedActionNeedles() {
			if strings.Contains(page.Body, needle) {
				t.Errorf("the rendered workspace leaks the masked action string %q", needle)
			}
		}
		if !strings.Contains(page.Body, "Run simulation") {
			t.Error("the one released action is not offered")
		}
	})

	t.Run("a caller with no compensation grant gets the page without the pay", func(t *testing.T) {
		masked := c.get(promotionURL, operations.name)
		if masked.Status != http.StatusOK {
			t.Fatalf("GET %s as %s answered %d, want 200\n%s",
				promotionURL, operations.name, masked.Status, masked.Body)
		}
		// The worker is still disclosed: this is masking, not refusal.
		if !strings.Contains(masked.Body, "OPS-HRBP2") {
			t.Error("the worker's current placement is missing from a page the policy did disclose")
		}
		// Every compensation identifier, label and value is absent from the
		// document rather than present and empty.
		for _, needle := range []string{
			workspace.FieldProposedComp, "Proposed base pay",
			workspace.FieldCurrentBasePay, "Current base pay",
			workspace.FieldBandPosition, workspace.FieldAnnualizedIncrease,
			"93000.00", "98000.00", "BAND-OPS-P3-USEAST",
		} {
			if strings.Contains(masked.Body, needle) {
				t.Errorf("a page with no compensation grant leaks %q", needle)
			}
		}
		if !strings.Contains(masked.Body, "Compensation is not disclosed to this caller") {
			t.Error("the page does not say why the compensation half is missing")
		}
	})

	t.Run("the rendered form submits its own values unchanged", func(t *testing.T) {
		// The answers are read back out of the served document with the same
		// parser FORM-004 uses (tools/uxqual/forms.ExtractFormAnswers), so
		// this submits what a person pressing the button would submit rather
		// than what the test believes the page contains. It is the check that
		// the frozen renderer's split between the field form and the action
		// form was actually bound back together.
		answers, err := forms.ExtractFormAnswers(page.Body)
		if err != nil {
			t.Fatalf("read the rendered form: %v", err)
		}
		answers[workspace.ParamCSRF] = token
		answers[workspace.ParamWorker] = testWorker
		answers[workspace.ParamTransition] = workspace.TransitionRunSimulation

		res := c.post(workspace.PathSimulate, compAdmin.name, answers, session)
		if res.Status != http.StatusOK {
			t.Fatalf("resubmitting the rendered form answered %d, want 200\n%s", res.Status, res.Body)
		}
		if !strings.Contains(res.Body, "Preflight READY") {
			t.Errorf("the corpus scenario resubmitted unchanged is not READY\n%s", res.Body)
		}
	})

	t.Run("a POST renders preflight findings and the zero-effect receipt", func(t *testing.T) {
		submitted := c.post(workspace.PathSimulate, compAdmin.name, map[string]string{
			workspace.ParamCSRF:             token,
			workspace.ParamWorker:           testWorker,
			workspace.ParamTransition:       workspace.TransitionRunSimulation,
			workspace.FieldProposedJobTitle: "OPS-HRBP3",
			workspace.FieldProposedGrade:    "P3",
			workspace.FieldProposedComp:     "105000.00",
			workspace.FieldEffectiveDate:    "2026-06-01",
			workspace.FieldBusinessReason:   "retention_adjustment",
		}, session)
		if submitted.Status != http.StatusOK {
			t.Fatalf("POST %s answered %d, want 200\n%s",
				workspace.PathSimulate, submitted.Status, submitted.Body)
		}
		// 105,000 against a 93,000 baseline is a 12.9% raise: the legacy rule
		// pack's ten-percent finding is what the page must now be showing,
		// and it must be showing it because the domain said so.
		if !strings.Contains(submitted.Body, "compensation.increase_over_ten_percent") {
			t.Errorf("the submitted proposal does not carry the over-threshold finding\n%s", submitted.Body)
		}
		if !strings.Contains(submitted.Body, "105000.00") {
			t.Error("the re-rendered form does not carry the submitted amount")
		}

		digest := receiptDigest(t, submitted.Body)
		receipt := c.get(workspace.PathReceiptPrefix+digest, compAdmin.name, session)
		if receipt.Status != http.StatusOK {
			t.Fatalf("GET the receipt answered %d, want 200\n%s", receipt.Status, receipt.Body)
		}
		for _, want := range []string{
			digest, "SIMULATE", "NOT_PLANNED",
			"domain writes 0", "provider calls 0",
		} {
			if !strings.Contains(receipt.Body, want) {
				t.Errorf("the receipt page does not carry %q", want)
			}
		}
	})

	t.Run("the response carries a strict content-security-policy and the safe server-rendered fallback", func(t *testing.T) {
		policy := page.Header.Get("Content-Security-Policy")
		for _, want := range []string{
			"default-src 'none'", "base-uri 'none'", "form-action 'self'",
			"frame-ancestors 'none'", "style-src 'sha256-",
		} {
			if !strings.Contains(policy, want) {
				t.Errorf("the content-security-policy %q is missing %q", policy, want)
			}
		}
		// The legacy uxqual.wasm enhancement is deliberately withheld
		// (internal/humanwork/workspace/assets.go, 60ca21b9): it rebuilt the
		// request form without CSRF, worker, locale and form-owner binding, so
		// BundleBuilt is false and the server-rendered POST form is the
		// production path. Pin whichever posture this build actually serves.
		if workspace.BundleBuilt() {
			for _, want := range []string{"script-src 'sha256-", "'self' 'wasm-unsafe-eval'", "connect-src 'self'"} {
				if !strings.Contains(policy, want) {
					t.Errorf("the content-security-policy %q is missing %q", policy, want)
				}
			}
			if !strings.Contains(page.Body, `<script type="application/json" id="gwc-contract">`) || !strings.Contains(page.Body, workspace.PathWasm) {
				t.Error("the enhanced workspace dropped its pinned contract island or Go/WASM client")
			}
		} else {
			if !strings.Contains(policy, "script-src 'none'") || !strings.Contains(policy, "script-src-elem 'none'") || !strings.Contains(policy, "connect-src 'none'") {
				t.Errorf("the native-only workspace's content-security-policy %q does not refuse script execution", policy)
			}
			if strings.Contains(page.Body, "<script") || strings.Contains(page.Body, workspace.PathWasm) {
				t.Error("the native-only workspace advertises a script or the withheld Go/WASM enhancement")
			}
		}
		if got := page.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options is %q, want nosniff", got)
		}
	})

	t.Run("the discovery document publishes the workspace routes", func(t *testing.T) {
		res := c.get(app.DiscoveryPath, compAdmin.name)
		if res.Status != http.StatusOK {
			t.Fatalf("GET %s answered %d, want 200", app.DiscoveryPath, res.Status)
		}
		var doc struct {
			ManifestDigest  string            `json:"manifest_digest"`
			WorkspaceRoutes []workspace.Route `json:"workspace_routes"`
		}
		if err := json.Unmarshal([]byte(res.Body), &doc); err != nil {
			t.Fatalf("decode the discovery document: %v", err)
		}
		if doc.ManifestDigest == "" {
			t.Error("splicing the workspace routes lost the manifest digest")
		}
		if len(doc.WorkspaceRoutes) != len(workspace.Routes()) {
			t.Fatalf("the discovery document publishes %d workspace routes, the handler serves %d",
				len(doc.WorkspaceRoutes), len(workspace.Routes()))
		}
		for _, route := range doc.WorkspaceRoutes {
			if route.EffectClass != "READ_ONLY" {
				t.Errorf("route %s %s declares effect class %q; every workspace route reads",
					route.Method, route.Path, route.EffectClass)
			}
		}
	})

	t.Run("-workspace=false serves the API surface alone", func(t *testing.T) {
		off := newCell(t, false)
		res := off.get(promotionURL, compAdmin.name)
		if res.Status == http.StatusOK {
			t.Fatalf("a cell composed with the workspace disabled still served %s", promotionURL)
		}
		discovery := off.get(app.DiscoveryPath, compAdmin.name)
		if strings.Contains(discovery.Body, app.WorkspaceRoutesKey) {
			t.Error("a cell that does not serve the workspace still advertises its routes")
		}
	})

	// Nothing above may have written anything: the workspace creates no
	// intent, so it appends no ledger event, advances no projection and
	// enqueues no outbox message either.
	if after := c.fingerprint(); after != before {
		t.Fatalf("the workspace changed the database\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestPromotionWorkspaceRefusesUnauthenticatedAndUnauthorizedAccess checks
// that the human surface is admitted exactly as strictly as the API: no
// credential is 401, a credential the policy will not disclose this subject
// to is 403, and a submission that was not issued to the current session is
// refused before any read runs.
func TestPromotionWorkspaceRefusesUnauthenticatedAndUnauthorizedAccess(t *testing.T) {
	t.Parallel()
	c := newCell(t, true)
	before := c.fingerprint()

	t.Run("an anonymous GET is refused", func(t *testing.T) {
		res := c.get(promotionURL, "")
		if res.Status != http.StatusUnauthorized {
			t.Fatalf("anonymous GET answered %d, want 401", res.Status)
		}
		if got := res.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
			t.Errorf("a 401 answered with WWW-Authenticate %q", got)
		}
		if strings.Contains(res.Body, "Omar") {
			t.Error("an unauthenticated refusal disclosed worker data")
		}
	})

	t.Run("an anonymous POST is refused", func(t *testing.T) {
		res := c.post(workspace.PathSimulate, "", map[string]string{
			workspace.ParamTransition: workspace.TransitionRunSimulation,
		})
		if res.Status != http.StatusUnauthorized {
			t.Fatalf("anonymous POST answered %d, want 401 after the browser boundary admitted its issued token", res.Status)
		}
	})

	t.Run("an anonymous asset request is refused", func(t *testing.T) {
		res := c.get(workspace.PathWasm, "")
		if res.Status != http.StatusUnauthorized {
			t.Fatalf("anonymous asset GET answered %d, want 401", res.Status)
		}
	})

	t.Run("a principal the policy will not disclose the subject to is refused", func(t *testing.T) {
		res := c.get(promotionURL, unrelated.name)
		if res.Status != http.StatusForbidden {
			t.Fatalf("GET as %s answered %d, want 403\n%s", unrelated.name, res.Status, res.Body)
		}
		for _, leaked := range []string{"Omar", "OPS-HRBP2", "93000.00"} {
			if strings.Contains(res.Body, leaked) {
				t.Errorf("a 403 disclosed %q", leaked)
			}
		}
	})

	t.Run("a submission with no session token is refused", func(t *testing.T) {
		res := c.post(workspace.PathSimulate, compAdmin.name, map[string]string{
			workspace.ParamWorker:     testWorker,
			workspace.ParamTransition: workspace.TransitionRunSimulation,
		})
		if res.Status != http.StatusForbidden {
			t.Fatalf("a POST with no CSRF token answered %d, want 403", res.Status)
		}
	})

	t.Run("a write transition is refused", func(t *testing.T) {
		page := c.get(promotionURL, compAdmin.name)
		res := c.post(workspace.PathSimulate, compAdmin.name, map[string]string{
			workspace.ParamCSRF:       page.csrfToken(t),
			workspace.ParamWorker:     testWorker,
			workspace.ParamTransition: workspace.ActionSubmitForApproval,
		}, page.sessionCookie(t))
		if res.Status != http.StatusBadRequest {
			t.Fatalf("a submit-for-approval transition answered %d, want 400", res.Status)
		}
	})

	t.Run("an unbuilt enhancement bundle is a 404, not a broken page", func(t *testing.T) {
		res := c.get(workspace.PathWasm, compAdmin.name)
		want := http.StatusNotFound
		if workspace.BundleBuilt() {
			want = http.StatusOK
		}
		if res.Status != want {
			t.Fatalf("GET %s answered %d, want %d", workspace.PathWasm, res.Status, want)
		}
	})

	if after := c.fingerprint(); after != before {
		t.Fatalf("a refused request changed the database\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestPromotionWorkspacePassesTheQualificationFixture scores the live
// rendered document against UX-QUAL-001's own criteria
// (tools/uxqual/qual), rather than against the hand-authored fixture that
// package ships with.
//
// That is the point of running it here: the qualification fixture already
// proves the renderer produces an accessible document from a contract, and
// this proves the document a running cell actually serves is still that
// document after the workspace has built the contract from live data, bound
// the forms to real routes and injected the session token.
func TestPromotionWorkspacePassesTheQualificationFixture(t *testing.T) {
	t.Parallel()
	c := newCell(t, true)

	page := c.get(promotionURL, compAdmin.name)
	if page.Status != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200\n%s", promotionURL, page.Status, page.Body)
	}

	result := qual.RunDocumentChecks("served-workspace", page.Body, workspace.MaskedActionNeedles())
	for _, criterion := range result.All() {
		if criterion.Name == "" {
			continue
		}
		if !criterion.Pass {
			t.Errorf("%s: FAIL %s", criterion.Name, criterion.Detail)
			continue
		}
		t.Logf("%s: pass (%s)", criterion.Name, criterion.Detail)
	}

	if buildable := qual.BuildNativePackage("./internal/humanwork/workspace/..."); !buildable.Pass {
		t.Errorf("%s: FAIL %s", buildable.Name, buildable.Detail)
	}

	// Tab order is DOM order, and DOM order is the contract's field order.
	// Pinning the sequence is what makes "keyboard-only completion" a
	// statement about this page rather than about focusable elements in
	// general.
	wantOrder := []string{
		workspace.FieldProposedJobTitle,
		workspace.FieldProposedGrade,
		workspace.FieldProposedComp,
		workspace.FieldEffectiveDate,
		workspace.FieldBusinessReason,
	}
	got := qual.FieldIDsInDocumentOrder(page.Body)
	if strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("field order is %v, want %v", got, wantOrder)
	}

	// The submitted document must reach this workspace's own route, not the
	// frozen renderer's "#" placeholder.
	if !strings.Contains(page.Body, `action="`+workspace.PathSimulate+`"`) {
		t.Error("the rendered request form does not post to the workspace simulate route")
	}
	if strings.Contains(page.Body, `action="#"`) {
		t.Error("the rendered document still carries the renderer's placeholder form action")
	}
}

var receiptDigestRE = regexp.MustCompile(`receipt (\S+) in mode`)

// receiptDigest reads the simulation's result digest back out of the page
// that reported it, so the receipt route is addressed with the identity the
// page published rather than one the test computed for itself.
func receiptDigest(t *testing.T, doc string) string {
	t.Helper()
	m := receiptDigestRE.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("the simulated workspace published no zero-effect receipt digest")
	}
	return m[1]
}
