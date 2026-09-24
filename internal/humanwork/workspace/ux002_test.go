package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// TestTodo_UX_002 is the unit-level evidence for planning/todos.md's UX-002
// GREEN clause: the Promotion workspace is served by the GoWebComponents
// renderer (the qualified selection of UX-QUAL-001), rendered from the
// server's workspace contract, and - when the progressive-enhancement bundle
// is served - the browser receives everything the live renderer needs in the
// document itself.
//
// RED side (what this test refuses): a document rendered by anything other
// than the GWC renderer's own component tree; a masked field or action
// rendered instead of dropped; an enhanced document whose script inventory
// is anything other than the pinned loader and one data island; and a data
// island whose contract is not the very masked contract the tree shows.
func TestTodo_UX_002(t *testing.T) {
	t.Parallel()

	c := ux002Contract()
	// The masked field and action, verbatim: identifier, label and value.
	// None of these strings may appear in a served document.
	masked := []string{
		"proposedCompensation", "Proposed base pay", "128000.00",
		"reject", "Reject",
	}

	t.Run("the served document is the GWC renderer's output, bound to the workspace routes", func(t *testing.T) {
		doc, err := Render(c, "csrf-fixture", "worker-fixture", false)
		if err != nil {
			t.Fatalf("render the workspace document: %v", err)
		}

		// GWC provenance: GWC emits its attributes in alphabetical order, so
		// the bound request form is action, id, method. The frozen template's
		// binding writes id first. The two literals are mutually exclusive, so
		// this is a discriminator between the two qualified renderers, not a
		// restatement of what any renderer would emit.
		if !strings.Contains(doc, `<form action="`+PathSimulate+`" id="`+formID+`" method="post">`) {
			t.Error("the request form does not carry the GWC renderer's bound form tag\n" + doc)
		}
		if strings.Contains(doc, `<form id="`+formID+`" method="post" action="`) {
			t.Error("the document carries the frozen template's bound form tag; the served renderer is not the GWC selection")
		}
		if strings.Contains(doc, `action="#"`) {
			t.Error("the document still carries an unbound renderer placeholder form action")
		}

		// The session the page belongs to, bound without script.
		for _, want := range []string{
			hiddenInput(ParamCSRF, "csrf-fixture"),
			hiddenInput(ParamWorker, "worker-fixture"),
		} {
			if !strings.Contains(doc, want) {
				t.Errorf("the bound request form does not carry %s", want)
			}
		}

		// The action forms post to the same route and name the request form
		// as their form owner, in the GWC tree's own attribute order.
		const wantActionForm = `<form action="` + PathSimulate + `" method="post">` +
			`<input form="` + formID + `" name="transition" type="hidden" value="approve">`
		if !strings.Contains(doc, wantActionForm) {
			t.Error("the approve action form is not bound to the simulate route with its controls reattached to the request form")
		}
		const wantButton = `<button form="` + formID + `" data-variant="primary" type="submit">Approve</button>`
		if !strings.Contains(doc, wantButton) {
			t.Error("the approve button is not a control of the bound request form")
		}

		// The masked action is absent, not blanked: no button, no form, no
		// reason control.
		for _, needle := range masked {
			if strings.Contains(doc, needle) {
				t.Errorf("the served document renders the masked string %q", needle)
			}
		}

		// The contract's content is what the tree renders.
		for _, want := range []string{
			"Adrian Cole",               // header worker identity
			"Senior Engineer",           // the proposed job title
			"scope_growth",              // the business reason
			"Increase over ten percent", // the preflight finding
			"the proposal exceeds the default threshold", // the finding detail
			"Preflight READY",   // the simulation summary
			"request accepted",  // the timeline event
			"people.promote@v1", // the provenance footer
		} {
			if !strings.Contains(doc, want) {
				t.Errorf("the served document does not carry the contract content %q", want)
			}
		}
	})

	t.Run("the document scores against UX-QUAL-001's own criteria", func(t *testing.T) {
		doc, err := Render(c, "csrf-fixture", "worker-fixture", false)
		if err != nil {
			t.Fatalf("render the workspace document: %v", err)
		}
		result := qual.RunDocumentChecks("served-gwc-workspace", doc, masked)
		for _, criterion := range result.All() {
			if criterion.Name == "" {
				continue
			}
			if !criterion.Pass {
				t.Errorf("%s: FAIL %s", criterion.Name, criterion.Detail)
			}
		}
	})

	t.Run("the enhanced document carries one data island and exactly the pinned loader", func(t *testing.T) {
		doc, err := Render(c, "csrf-fixture", "worker-fixture", true)
		if err != nil {
			t.Fatalf("render the enhanced workspace document: %v", err)
		}

		island, _ := readUx002Island(t, doc)

		if island.Binding.Action != PathSimulate {
			t.Errorf("the island's binding targets %q, want %q", island.Binding.Action, PathSimulate)
		}
		if island.Binding.FormID != formID {
			t.Errorf("the island's binding names form %q, want %q", island.Binding.FormID, formID)
		}
		if island.Binding.Hidden[ParamCSRF] != "csrf-fixture" {
			t.Errorf("the island's binding carries csrf %q, want the token this form was bound with", island.Binding.Hidden[ParamCSRF])
		}
		if island.Binding.Hidden[ParamWorker] != "worker-fixture" {
			t.Errorf("the island's binding carries worker %q, want worker-fixture", island.Binding.Hidden[ParamWorker])
		}
		if !reflect.DeepEqual(island.Contract, c) {
			t.Errorf("the island's contract is not the masked contract the tree renders\n got: %+v\nwant: %+v", island.Contract, c)
		}

		// The complete script inventory of the enhanced document: the data
		// island and the pinned progressive-enhancement loader, and nothing
		// else (no framework, no CDN, no run-time-injected code).
		if got := strings.Count(doc, "<script"); got != 2 {
			t.Errorf("the enhanced document carries %d script elements, want exactly 2 (the data island and the pinned loader)", got)
		}
		if !strings.Contains(doc, loaderScript()) {
			t.Error("the enhanced document does not carry the pinned loader verbatim")
		}
		if m := externalRefRE.FindString(doc); m != "" {
			t.Errorf("the document references %q; the workspace runtime is self-contained", m)
		}
	})
}

// externalRefRE names a URL the document would fetch from another origin.
var externalRefRE = regexp.MustCompile(`(https?|file|data)://[^\s"']`)

// TestTodo_UX_002_Golden pins the exact served document for a fixed contract,
// csrf token and worker: the GWC renderer's tree, bound to this workspace's
// routes. A change to the renderer's output, the binding, or the document
// assembly breaks it, which is the point.
func TestTodo_UX_002_Golden(t *testing.T) {
	t.Parallel()

	doc, err := Render(ux002Contract(), "csrf-fixture", "worker-fixture", false)
	if err != nil {
		t.Fatalf("render the workspace document: %v", err)
	}
	compareGolden(t, filepath.Join("testdata", "ux002_document.golden"), doc)
}

// ux002Source is the full, unmasked record the server would hand
// NewWorkspaceContract before authorization.
func ux002Source() contract.SourceRecord {
	return contract.SourceRecord{
		WorkspaceID: "ws.promotion.fix-001",
		Title:       "Promotion for Adrian Cole",
		WorkerID:    "worker-fixture",
		WorkerName:  "Adrian Cole",
		AllFields: []contract.RequestField{
			{ID: "proposedJobTitle", Label: "Proposed job title", Kind: contract.FieldKindText, Value: "Senior Engineer", Validation: contract.FieldValidation{Required: true}},
			{ID: "currentJobTitle", Label: "Current job title", Kind: contract.FieldKindReadOnly, Value: "Engineer"},
			{ID: "proposedCompensation", Label: "Proposed base pay", Kind: contract.FieldKindMoney, Value: "128000.00"},
			{ID: "effectiveDate", Label: "Effective date", Kind: contract.FieldKindDate, Value: "2026-10-01", Validation: contract.FieldValidation{Required: true}},
			{ID: "businessReason", Label: "Business reason", Kind: contract.FieldKindTextarea, Value: "scope_growth"},
		},
		Preflight: []contract.PreflightFinding{
			{ID: "compensation.increase_over_ten_percent", Severity: contract.SeverityWarning, Label: "Increase over ten percent", Detail: "the proposal exceeds the default threshold"},
		},
		Simulation: contract.SimulationResult{
			Status:      contract.SimulationReady,
			Summary:     "Preflight READY: one warning, no blocking findings",
			GeneratedAt: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
			Checks: []contract.SimulationCheck{
				{Label: "band position", Status: contract.SeverityInfo, Detail: "within band"},
			},
		},
		Timeline: []contract.TimelineEvent{
			{At: time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC), Actor: "intake", Label: "request accepted"},
		},
		AllActions: []contract.AvailableAction{
			{ID: "approve", Label: "Approve", Transition: "approve", Variant: contract.ActionPrimary},
			{ID: "reject", Label: "Reject", Transition: "reject", Variant: contract.ActionDanger, RequiresReason: true},
			{ID: "run_simulation", Label: "Run simulation", Transition: "run_simulation", Variant: contract.ActionSecondary},
		},
		Provenance: contract.Provenance{
			CapabilityID:      "people.promote",
			CapabilityVersion: "v1",
			SourceSystem:      "hcm-next",
			AsOf:              time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
		},
	}
}

// ux002Contract masks one field and one action so the tests can pin that
// masking drops rather than blanks.
func ux002Contract() contract.WorkspaceContract {
	return contract.NewWorkspaceContract(ux002Source(),
		contract.Allow("proposedJobTitle", "currentJobTitle", "effectiveDate", "businessReason"),
		contract.Allow("approve", "run_simulation"))
}

// ux002Island is the data-island protocol the enhanced document must carry:
// the same masked contract the rendered tree shows, plus exactly what the
// live renderer needs to re-bind that tree to this workspace's routes.
//
// The implementation's island type must serialize to this shape; the field
// names are the wire contract, pinned here so the served document is tested
// against the protocol rather than against the implementation's type.
type ux002Island struct {
	Contract contract.WorkspaceContract `json:"contract"`
	Binding  struct {
		Action string            `json:"action"`
		FormID string            `json:"form_id"`
		Hidden map[string]string `json:"hidden"`
	} `json:"binding"`
}

const ux002IslandOpen = `<script type="application/json" id="gwc-contract">`

// readUx002Island reads the data island out of a document and decodes it,
// returning the island and its raw JSON.
func readUx002Island(t *testing.T, doc string) (ux002Island, string) {
	t.Helper()
	idx := strings.Index(doc, ux002IslandOpen)
	if idx < 0 {
		t.Fatalf("the enhanced document carries no contract data island\n%s", doc)
	}
	start := idx + len(ux002IslandOpen)
	end := strings.Index(doc[start:], "</script>")
	if end < 0 {
		t.Fatal("the data island is not closed")
	}
	raw := doc[start : start+end]
	var island ux002Island
	if err := json.Unmarshal([]byte(raw), &island); err != nil {
		t.Fatalf("decode the data island: %v\n%s", err, raw)
	}
	return island, raw
}

// compareGolden compares got against the golden file, writing it when
// HCMNEXT_UPDATE_GOLDEN is set.
func compareGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if normalizeGolden(string(want)) != normalizeGolden(got) {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func normalizeGolden(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
