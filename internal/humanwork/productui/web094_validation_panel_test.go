package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-094: the page-validation findings panel. Every
// validate step verdicts alone, but the studio has no single
// validation view: authors run seven validators and merge reasons
// by hand, and a failure in one step hides the rest. The
// lifecycle needs a pure panel running the whole draft
// composition chain — purpose, floorplan, regions, widgets,
// content safety, actions, ceiling — reporting every step with an
// overall gate.
// Rendering the panel stays out: no studio surface hosts it
// yet, and the report is the panel's content model.
func TestTodo_WEB_094(t *testing.T) {
	draft := PageDraft{Page: "studio", Composition: PageComposition{
		Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Regions: []string{"primary"},
		Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
			Classification: "internal", SourceType: "projection", SourceID: "journeys"}},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
	}}
	report := ValidateComposition(draft, RegisteredFloorplans(), RegisteredWidgets())
	if !report.Compatible {
		t.Fatalf("valid composition panel refuses: %+v", report.Findings)
	}
	if report.Page != "studio" {
		t.Fatalf("panel page = %q", report.Page)
	}
	steps := []string{}
	for _, finding := range report.Findings {
		steps = append(steps, finding.Step)
		if !finding.Compatible || len(finding.Reasons) != 0 {
			t.Fatalf("step %q fails: %+v", finding.Step, finding)
		}
	}
	if !reflect.DeepEqual(steps, []string{"purpose", "floorplan", "regions", "widgets", "content-safety", "actions", "ceiling"}) {
		t.Fatalf("panel steps = %q", steps)
	}

	// Every step runs even when earlier steps fail: one broken
	// composition reports everything at once.
	broken := PageDraft{Page: "studio", Composition: PageComposition{
		Floorplan: "teleporter", ClassificationCeiling: "internal",
		Widgets: []WidgetBinding{{WidgetType: "teleporter"}},
		Actions: []ActionBinding{{Capability: "journeys.create"}},
	}}
	panel := ValidateComposition(broken, RegisteredFloorplans(), RegisteredWidgets())
	if panel.Compatible {
		t.Fatal("broken composition panel passes")
	}
	if len(panel.Findings) != 7 {
		t.Fatalf("panel lists %d steps, want 7", len(panel.Findings))
	}
	byStep := map[string]ValidationFinding{}
	for _, finding := range panel.Findings {
		byStep[finding.Step] = finding
	}
	if byStep["purpose"].Compatible || byStep["floorplan"].Compatible || byStep["widgets"].Compatible || byStep["actions"].Compatible {
		t.Fatalf("failing steps pass: %+v", panel.Findings)
	}
	if !byStep["regions"].Compatible {
		t.Fatalf("empty regions refuse: %+v", byStep["regions"])
	}
	if byStep["ceiling"].Compatible {
		t.Fatalf("unclassified binding clears the ceiling: %+v", byStep["ceiling"])
	}
	if len(byStep["floorplan"].Reasons) == 0 || byStep["floorplan"].Reasons[0] != `unknown floorplan "teleporter"` {
		t.Fatalf("floorplan reasons = %q", byStep["floorplan"].Reasons)
	}
}

// Golden: panel outcomes over a composition matrix.
func TestTodo_WEB_094_Golden(t *testing.T) {
	validWidget := WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
		Classification: "internal", SourceType: "projection", SourceID: "journeys"}
	validAction := ActionBinding{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}
	base := PageComposition{
		Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Regions: []string{"primary"}, Widgets: []WidgetBinding{validWidget}, Actions: []ActionBinding{validAction},
	}
	compositions := []PageComposition{
		base,
		func() PageComposition { next := base; next.Purpose = ""; return next }(),
		func() PageComposition { next := base; next.Floorplan = "teleporter"; return next }(),
		func() PageComposition { next := base; next.Regions = []string{"shell"}; return next }(),
		func() PageComposition {
			next := base
			next.Widgets = []WidgetBinding{{WidgetType: "teleporter"}}
			return next
		}(),
		func() PageComposition {
			next := base
			next.Actions = []ActionBinding{{Capability: "journeys.create"}}
			return next
		}(),
		func() PageComposition { next := base; next.ClassificationCeiling = "public"; return next }(),
		{},
	}
	var builder strings.Builder
	for _, composition := range compositions {
		report := ValidateComposition(PageDraft{Page: "studio", Composition: composition}, RegisteredFloorplans(), RegisteredWidgets())
		if report.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		for _, finding := range report.Findings {
			builder.WriteString(finding.Step)
			builder.WriteString("\x00")
			if finding.Compatible {
				builder.WriteString("compatible")
			} else {
				builder.WriteString("incompatible")
			}
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(finding.Reasons, ";"))
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "f7938ce7f27153f693923297e4f33a5e91a015cfbc49719bf5bdbfcb2648a701"
	if got != want {
		t.Fatalf("panel digest = %s, want %s", got, want)
	}
}

// Browser: every registered floorplan carries a fully valid
// composition through the panel — deterministically.
func TestTodo_WEB_094_Browser(t *testing.T) {
	catalog := RegisteredFloorplans()
	registry := RegisteredWidgets()
	for _, floorplan := range catalog.Floorplans {
		draft := PageDraft{Page: "studio", Composition: PageComposition{
			Purpose: "Track promotion journeys", Audience: "managers",
			Floorplan: floorplan.ID, FloorplanVersion: floorplan.Version, ClassificationCeiling: "internal",
			Regions: []string{"primary"},
			Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
				Classification: "internal", SourceType: "projection", SourceID: "journeys"}},
			Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
		}}
		first := ValidateComposition(draft, catalog, registry)
		second := ValidateComposition(draft, catalog, registry)
		if !first.Compatible {
			t.Fatalf("floorplan %q panel refuses: %+v", floorplan.ID, first.Findings)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("floorplan %q panel is nondeterministic", floorplan.ID)
		}
	}
}

// Conformance: step order fixed, all steps always run, panel
// carries the draft page.
func TestTodo_WEB_094_Conformance(t *testing.T) {
	report := ValidateComposition(PageDraft{}, RegisteredFloorplans(), RegisteredWidgets())
	if report.Page != "" {
		t.Fatalf("empty draft panel names page %q", report.Page)
	}
	if report.Compatible || len(report.Findings) != 7 {
		t.Fatalf("empty draft panel = (%t, %d steps)", report.Compatible, len(report.Findings))
	}
	steps := []string{}
	for _, finding := range report.Findings {
		steps = append(steps, finding.Step)
	}
	if !reflect.DeepEqual(steps, []string{"purpose", "floorplan", "regions", "widgets", "content-safety", "actions", "ceiling"}) {
		t.Fatalf("panel steps = %q, want flow order", steps)
	}
	// Purpose fails but widgets still run on an empty draft.
	byStep := map[string]ValidationFinding{}
	for _, finding := range report.Findings {
		byStep[finding.Step] = finding
	}
	if byStep["purpose"].Compatible || !byStep["widgets"].Compatible || !byStep["actions"].Compatible {
		t.Fatalf("empty draft steps wrong: %+v", report.Findings)
	}
	first := ValidateComposition(PageDraft{Page: "studio"}, RegisteredFloorplans(), RegisteredWidgets())
	second := ValidateComposition(PageDraft{Page: "studio"}, RegisteredFloorplans(), RegisteredWidgets())
	if !reflect.DeepEqual(first, second) {
		t.Fatal("panel is nondeterministic")
	}
}
