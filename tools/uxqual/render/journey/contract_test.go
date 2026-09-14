package journey

import (
	"reflect"
	"strings"
	"testing"
)

// contract.go is a types-only file another lane projects engine data into,
// so there is no behaviour here to exercise. What these tests do protect is
// the two things the renderer relies on and the compiler cannot state:
//
//   - Every zero value is renderable. The projection fills a Page field by
//     field, and a half-built one must not panic.
//   - The vocabularies the field comments define ("one of info, success,
//     warning, danger") are the ones components.go actually normalises to.
//     If the two drift, a legitimate tone would render as a neutral chip
//     with no test failing anywhere else.

func TestZeroValuesOfEveryContractTypeRender(t *testing.T) {
	cases := map[string]func() string{
		"zero page": func() string {
			out, err := RenderToString(Page{})
			if err != nil {
				t.Fatalf("RenderToString: %v", err)
			}
			return out
		},
		"zero journey card":   func() string { return renderNode(t, journeyCard(JourneyCard{})) },
		"zero hero":           func() string { return renderNode(t, heroSection(JourneyCard{}, false)) },
		"zero step":           func() string { return renderNode(t, stepNode(0, Step{})) },
		"zero fact":           func() string { return renderNode(t, factsList([]Fact{{}})) },
		"zero comparison row": func() string { return renderNode(t, comparisonRow(ComparisonRow{})) },
		"zero finding":        func() string { return renderNode(t, findingRow(0, Finding{})) },
		"zero work item":      func() string { return renderNode(t, workItemCard(WorkItemCard{})) },
		"zero timeline event": func() string { return renderNode(t, timelineNode(TimelineEvent{})) },
		"zero action":         func() string { return renderNode(t, actionCard(live{}, Action{})) },
		"zero field":          func() string { return renderNode(t, fieldNode(live{}, Field{}, false)) },
		"zero ledger":         func() string { return renderNode(t, outcomeSection(&LedgerCard{}, "")) },
		"absent ledger":       func() string { return renderNode(t, outcomeSection(nil, "")) },
		"zero footer":         func() string { return renderNode(t, footer(Footer{})) },
		"zero principal":      func() string { return renderNode(t, masthead(Page{})) },
		"zero pay band":       func() string { return renderNode(t, payBandGauge(&PayBand{})) },
		"zero budget":         func() string { return renderNode(t, budgetMeter(&Budget{})) },
		"zero window": func() string {
			return renderNode(t, effectiveWindow(&EffectiveWindow{EffectiveDate: "1 Jun 2026"}))
		},
		"zero live detail": func() string { return renderNode(t, detailView(Page{}, DetailView{})) },
	}
	for name, render := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked on a zero value: %v", r)
				}
			}()
			if out := render(); out == "" {
				t.Error("rendered nothing at all")
			}
		})
	}
}

// TestContractVocabulariesMatchTheRenderer keeps the doc comments in
// contract.go honest by checking each documented value survives
// normalisation unchanged.
func TestContractVocabulariesMatchTheRenderer(t *testing.T) {
	for _, tone := range []string{"info", "success", "warning", "danger", "neutral"} {
		if got := toneOf(tone); got != tone {
			t.Errorf("contract documents the tone %q but toneOf maps it to %q", tone, got)
		}
	}
	for _, severity := range []string{"blocking", "warning", "info", "success"} {
		if got := severityOf(severity); got != severity {
			t.Errorf("contract documents the severity %q but severityOf maps it to %q", severity, got)
		}
	}
	for _, state := range []string{"done", "active", "upcoming", "failed"} {
		if got := stepStateOf(state); got != state {
			t.Errorf("contract documents the step state %q but stepStateOf maps it to %q", state, got)
		}
	}
	for _, kind := range []string{"text", "number", "date", "select", "textarea", "hidden"} {
		out := renderNode(t, fieldNode(live{}, Field{ID: "f", Name: "n", Label: "L", Kind: kind}, false))
		if out == "" {
			t.Errorf("contract documents the field kind %q but it renders nothing", kind)
		}
	}
	for _, variant := range []string{"primary", "secondary", "danger"} {
		out := renderNode(t, actionCard(live{}, Action{ID: "a", Label: "L", Variant: variant, Action: "/a"}))
		if !strings.Contains(out, `data-variant="`+variant+`"`) {
			t.Errorf("contract documents the action variant %q but it does not reach the markup", variant)
		}
	}
}

// TestPageHoldsExactlyThreeMutuallyExclusiveViews is a structural guard: if
// another view is ever added to the contract, Build's switch has to learn
// about it, and this is the test that will say so.
func TestPageHoldsExactlyThreeMutuallyExclusiveViews(t *testing.T) {
	pageType := reflect.TypeOf(Page{})
	var views []string
	for i := 0; i < pageType.NumField(); i++ {
		f := pageType.Field(i)
		if f.Type.Kind() == reflect.Pointer && strings.HasSuffix(f.Type.Elem().Name(), "View") {
			views = append(views, f.Name)
		}
	}
	want := []string{"List", "Proposal", "Detail"}
	if !reflect.DeepEqual(views, want) {
		t.Errorf("Page declares views %v, but Build only renders %v", views, want)
	}
}
