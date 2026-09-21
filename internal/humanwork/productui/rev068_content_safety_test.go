package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// RED for REV-068-01: the content-safety publication gate. The
// editor sniffs only tag-open grammar and explicitly defers
// entities, URLs and origins to publication-time content-safety
// gates, but PublishGoverned runs no such gate: a content-tier
// binding whose Value or DisplayValue is set by any path other
// than EditBindingContent (bulk import, a from-scratch draft
// assembled directly) publishes with zero content inspection,
// and even editor-passed values carrying javascript:/data: URLs
// or encoded entities pass publication unexamined.
func TestTodo_REV_068_01(t *testing.T) {
	poisoned := func(value, display string) PublicationRequest {
		draft := NewPageDraftFromScratch("studio")
		draft.Composition = PageComposition{
			Purpose: "Track promotion journeys", Audience: "managers",
			Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
			Regions: []string{"primary"},
			Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
				Classification: "internal", SourceType: "projection", SourceID: "journeys",
				Value: value, DisplayValue: display}},
			Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
		}
		plan := PreviewPlan{Page: "studio", Fixture: "journeys-live", Dimensions: []PreviewDimension{
			{Axis: PreviewAxisLocale, Cases: []string{"en-US"}},
		}}
		cases, err := ExpandPreviewPlan(plan)
		if err != nil {
			t.Fatal(err)
		}
		return PublicationRequest{Draft: draft, Version: 1,
			Catalog: RegisteredFloorplans(), Registry: RegisteredWidgets(),
			Preview: plan, PreviewCases: cases,
			Review: PublicationReview{Reviewer: "a.muster", Approved: true}}
	}
	for _, bad := range []struct {
		name    string
		value   string
		display string
	}{
		{"script url value", "javascript:alert(document.domain)", ""},
		{"script url display", "3 open", "javascript:alert(1)"},
		{"data origin value", "data:text/html,<b>hi</b>", ""},
		{"encoded entity value", "Fish &amp; Chips", ""},
		{"numeric entity display", "open", "&#65;dmin"},
		{"raw markup direct", "<script>alert(1)</script>", ""},
	} {
		request := poisoned(bad.value, bad.display)
		_, err := PublishGoverned(&PageRevisionLog{}, request)
		if err == nil {
			t.Fatalf("%s publishes with no content-safety refusal", bad.name)
		}
		if !strings.Contains(err.Error(), "content-safety") {
			t.Fatalf("%s refusal names no content-safety step: %v", bad.name, err)
		}
		// The gate is deterministic: the same draft refuses identically.
		_, again := PublishGoverned(&PageRevisionLog{}, request)
		if again == nil || again.Error() != err.Error() {
			t.Fatalf("%s refusal unstable: %v vs %v", bad.name, err, again)
		}
	}

	// The clean control still publishes: the gate refuses payloads,
	// not content bindings.
	clean := poisoned("3 open requests", "")
	var log PageRevisionLog
	if _, err := PublishGoverned(&log, clean); err != nil {
		t.Fatalf("clean draft refuses: %v", err)
	}
}

// Security: the adversary matrix. Every stored-content injection
// class the editor defers — raw markup, script/data URL schemes,
// encoded entities — is refused at publication no matter which
// authoring path produced the binding, while plain text carrying
// bare ampersands or angle brackets still clears the gate and
// non-content tiers stay outside it.
func TestTodo_REV_068_01_Security(t *testing.T) {
	draftWith := func(binding WidgetBinding) PageDraft {
		draft := NewPageDraftFromScratch("studio")
		draft.Composition = PageComposition{
			Purpose: "Track promotion journeys", Audience: "managers",
			Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
			Regions: []string{"primary"},
			Widgets: []WidgetBinding{binding},
			Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}},
		}
		return draft
	}
	content := func(value, display string) WidgetBinding {
		return WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
			Classification: "internal", SourceType: "projection", SourceID: "journeys",
			Value: value, DisplayValue: display}
	}
	for _, bad := range []struct {
		name    string
		binding WidgetBinding
	}{
		{"uppercase scheme", content("JavaScript:alert(1)", "")},
		{"padded scheme", content("  javascript:alert(1)", "")},
		{"vbscript scheme", content("vbscript:msgbox(1)", "")},
		{"data scheme display", content("", "DATA:text/html,hi")},
		{"named entity", content("price &lt; 5", "")},
		{"hex entity", content("&#x41;dmin", "")},
		{"tag open", content("click <b>here</b>", "")},
		{"comment", content("a <!-- note --> b", "")},
	} {
		report := ValidateComposition(draftWith(bad.binding), RegisteredFloorplans(), RegisteredWidgets())
		if report.Compatible {
			t.Fatalf("%s clears validation", bad.name)
		}
		found := false
		for _, finding := range report.Findings {
			if finding.Step != ValidationStepContentSafety {
				continue
			}
			found = true
			if finding.Compatible || len(finding.Reasons) == 0 {
				t.Fatalf("%s content-safety finding passes: %+v", bad.name, finding)
			}
		}
		if !found {
			t.Fatalf("%s reports no content-safety step: %+v", bad.name, report.Findings)
		}
	}

	// Plain text is not an attack: bare ampersands, bare angle
	// brackets and ordinary URLs clear the gate.
	for _, fine := range []WidgetBinding{
		content("Fish & Chips", ""),
		content("a < b and 3 > 2", ""),
		content("see https://example.com/report", "shown"),
		content("", ""),
	} {
		report := ValidateComposition(draftWith(fine), RegisteredFloorplans(), RegisteredWidgets())
		if !report.Compatible {
			t.Fatalf("%+v refuses: %+v", fine, report.Findings)
		}
	}

	// Non-content tiers stay outside the gate: the same payload on
	// a governed or external binding is not a content-safety
	// finding (those tiers carry typed contracts or sandboxed
	// embeds, not sanitized summaries).
	for _, other := range []WidgetBinding{
		{WidgetType: "proposal-form", WidgetVersion: 1, AuthorityClass: "manager",
			Classification: "confidential", SourceType: "workflow", SourceID: "intent-1",
			Value: "javascript:alert(1)"},
		{WidgetType: "external-frame", WidgetVersion: 1, AuthorityClass: "external",
			Classification: "public", SourceType: "embed", SourceID: "partner",
			Value: "data:text/html,hi"},
	} {
		report := ValidateComposition(draftWith(other), RegisteredFloorplans(), RegisteredWidgets())
		for _, finding := range report.Findings {
			if finding.Step == ValidationStepContentSafety && !finding.Compatible {
				t.Fatalf("non-content tier gated: %+v", finding)
			}
		}
	}
}

// Golden: content-safety verdicts over a payload matrix.
func TestTodo_REV_068_01_Golden(t *testing.T) {
	values := []string{
		"3 open requests",
		"Fish & Chips",
		"a < b",
		"javascript:alert(1)",
		"  JavaScript:alert(1)",
		"data:text/html,hi",
		"vbscript:x",
		"https://example.com/x",
		"Fish &amp; Chips",
		"&#65;",
		"&#x41;",
		"click <b>here</b>",
		"",
	}
	var builder strings.Builder
	for _, value := range values {
		draft := PageDraft{Page: "studio", Composition: PageComposition{Widgets: []WidgetBinding{
			{WidgetType: "metric-display", Value: value, DisplayValue: value},
		}}}
		verdict := ValidateContentSafety(draft, RegisteredWidgets())
		if verdict.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(value)
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(verdict.Reasons, ";"))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "518984de2bcbe830bc55ab2150476421c15847b63a7afd3c677fad5f9da1067b"
	if got != want {
		t.Fatalf("content-safety digest = %s, want %s", got, want)
	}
}
