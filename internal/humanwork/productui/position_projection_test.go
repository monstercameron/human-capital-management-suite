package productui

import (
	"strings"
	"testing"
)

// TestTodo_REV_076_03 proves the position pages render only the projections
// supplied by the authenticated server read.
func TestTodo_REV_076_03(t *testing.T) {
	objectView := testView(PagePositionObject)
	objectView.PositionReference = "rev:v1:position.position_1:s1"
	objectView.PositionOptions = []PositionOptionProjection{{
		Reference: objectView.PositionReference, PositionID: "11111111-1111-4111-8111-111111111111",
		Title: "Senior Engineer", Organization: "Harbor", JobCode: "ENG-1", OrgUnit: "engineering",
	}}
	objectView.PositionObject = &PositionObjectProjection{
		PositionID: "11111111-1111-4111-8111-111111111111", Revision: "rev:v1:position.position_1:s1",
		JobCode: "ENG-1", OrgUnit: "engineering", Lifecycle: "OPEN", Compatible: true,
	}
	objectMarkup, err := Render(objectView)
	if err != nil {
		t.Fatalf("render position object: %v", err)
	}
	for _, want := range []string{"ENG-1", "engineering", "Compatible", "rev:v1:position.position_1:s1", "Senior Engineer", "Harbor"} {
		if !strings.Contains(objectMarkup, want) {
			t.Errorf("position object page does not render %q: %s", want, objectMarkup)
		}
	}
	if strings.Contains(objectMarkup, "not published yet") {
		t.Fatal("position object page still presents the unavailable stub")
	}

	occupancyView := testView(PagePositionOccupancy)
	occupancyView.PositionReference = "rev:v1:position.position_1:s1"
	occupancyView.PositionOptions = objectView.PositionOptions
	occupancyView.PositionOccupancy = &PositionOccupancyProjection{
		PositionID: "11111111-1111-4111-8111-111111111111", CapacityFTE: "2.0000", CapacityHeads: 2,
		ConsumedFTE: "1.0000", ConsumedHeads: 1, AvailableFTE: "1.0000", AvailableHeads: 1,
		Occupants: []PositionOccupantProjection{{WorkerID: "22222222-2222-4222-8222-222222222222", FTE: "1.0000"}},
	}
	occupancyMarkup, err := Render(occupancyView)
	if err != nil {
		t.Fatalf("render position occupancy: %v", err)
	}
	for _, want := range []string{"22222222-2222-4222-8222-222222222222", "Available", "1.0000", "Capacity", "name=\"position_ref\"", "Senior Engineer"} {
		if !strings.Contains(occupancyMarkup, want) {
			t.Errorf("position occupancy page does not render %q: %s", want, occupancyMarkup)
		}
	}
	if strings.Contains(occupancyMarkup, "not published yet") {
		t.Fatal("position occupancy page still presents the unavailable stub")
	}
}

func TestTodo_REV_076_03_Golden(t *testing.T) {
	object := PositionObjectProjection{
		PositionID: "11111111-1111-4111-8111-111111111111", Revision: "rev:v1:position.position_1:s1",
		JobCode: "ENG-1", OrgUnit: "engineering", Lifecycle: "OPEN", Compatible: true,
	}
	got := object.PositionID + "|" + object.Revision + "|" + object.JobCode + "|" + object.OrgUnit + "|" + object.Lifecycle + "|compatible"
	const want = "11111111-1111-4111-8111-111111111111|rev:v1:position.position_1:s1|ENG-1|engineering|OPEN|compatible"
	if got != want {
		t.Fatalf("position object projection golden = %q, want %q", got, want)
	}
	occupancy := PositionOccupancyProjection{
		CapacityFTE: "2.0000", CapacityHeads: 2, ConsumedFTE: "1.0000", ConsumedHeads: 1,
		AvailableFTE: "1.0000", AvailableHeads: 1,
		Occupants: []PositionOccupantProjection{{WorkerID: "22222222-2222-4222-8222-222222222222", FTE: "1.0000"}},
	}
	got = occupancy.Occupants[0].WorkerID + "|" + occupancy.Occupants[0].FTE + "|" + occupancy.CapacityFTE + "|" + occupancy.ConsumedFTE + "|" + occupancy.AvailableFTE + "|" + "2|1|1"
	const occupancyWant = "22222222-2222-4222-8222-222222222222|1.0000|2.0000|1.0000|1.0000|2|1|1"
	if got != occupancyWant {
		t.Fatalf("position occupancy projection golden = %q, want %q", got, occupancyWant)
	}
}

func TestPositionReferenceChangeClearsOldPositionProjection(t *testing.T) {
	view := testView(PagePositionObject)
	view.PositionReference = "rev-old"
	view.PositionObject = &PositionObjectProjection{PositionID: "old-position"}
	view.PositionOccupancy = &PositionOccupancyProjection{PositionID: "old-position"}
	updated := ApplyRequest(view, PageRequest{Page: PagePositionObject, PositionReference: "rev-new"})
	if updated.PositionObject != nil || updated.PositionOccupancy != nil {
		t.Fatalf("changing selected position retained an old projection: object=%+v occupancy=%+v", updated.PositionObject, updated.PositionOccupancy)
	}
}

func TestAuthorizedPageSubtitlesStayTaskSpecific(t *testing.T) {
	for _, localeCode := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeCode)
		for _, page := range []PageID{PagePositionObject, PagePositionOccupancy, PageReviewParticipants} {
			view := ApplyLocale(testView(page), locale)
			if page == PageReviewParticipants {
				view.ReviewParticipants = &ReviewParticipantsProjection{Cycles: []ReviewParticipantsCycleProjection{}}
			}
			if view.Subtitle == ordinaryUnavailableCopy(locale.Resolved) {
				t.Errorf("%s subtitle for %s was replaced by generic unavailable copy", page, localeCode)
			}
			markup, err := Render(view)
			if err != nil {
				t.Fatalf("render %s in %s: %v", page, localeCode, err)
			}
			if strings.Contains(markup, ordinaryUnavailableCopy(locale.Resolved)) {
				t.Errorf("%s page in %s renders generic unavailable subtitle", page, localeCode)
			}
		}
	}
}

func TestPositionSelectorBindsAuthorizedRevisionReference(t *testing.T) {
	const reference = "rev:v1:position.position_1:s1"
	for _, page := range []PageID{PagePositionObject, PagePositionOccupancy} {
		view := testView(page)
		view.PositionReference = reference
		view.PositionOptions = []PositionOptionProjection{{
			Reference: reference, PositionID: "position-1", Title: "Senior Engineer",
			Organization: "Harbor", JobCode: "ENG-1", OrgUnit: "engineering",
		}}
		view.Navigate = func(string) {}
		markup, err := Render(view)
		if err != nil {
			t.Fatalf("render %s selector: %v", page, err)
		}
		for _, want := range []string{
			`id="position-reference"`, `name="position_ref"`, `aria-describedby="position-selector-help"`,
			`value="` + reference + `"`, "Senior Engineer · Harbor · ENG-1 · engineering",
		} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s selector markup does not contain %q: %s", page, want, markup)
			}
		}
		href := positionOptionHref(view, reference)
		if !strings.Contains(href, "position_ref=rev%3Av1%3Aposition.position_1%3As1") {
			t.Errorf("%s selection href does not carry canonical revision ref: %s", page, href)
		}
	}
}

func TestPositionSelectorRemainsAvailableWithoutSelection(t *testing.T) {
	for _, page := range []PageID{PagePositionObject, PagePositionOccupancy} {
		markup, err := Render(testView(page))
		if err != nil {
			t.Fatalf("render empty %s: %v", page, err)
		}
		if !strings.Contains(markup, `name="position_ref"`) || !strings.Contains(markup, "position-selector-empty") {
			t.Errorf("%s does not offer a selector when no position is selected: %s", page, markup)
		}
	}
}

func TestPositionSelectorUsesTokenBasedSpacing(t *testing.T) {
	css := Stylesheet()
	form := declarationsFor(css, ".position-selector")
	if !strings.Contains(form, "display:grid") || !strings.Contains(form, "gap:calc(var(--hcm-space-2) * var(--hcm-density))") {
		t.Fatalf("position selector does not separate its label and control using spacing tokens: %s", form)
	}
	control := declarationsFor(css, ".position-selector select")
	for _, want := range []string{
		"width:100%", "min-height:var(--hcm-control-height)",
		"padding-left:calc(var(--hcm-space-2) * var(--hcm-density))", "padding-right:calc(var(--hcm-space-2) * var(--hcm-density))",
	} {
		if !strings.Contains(control, want) {
			t.Errorf("position selector control is missing %q: %s", want, control)
		}
	}
}
