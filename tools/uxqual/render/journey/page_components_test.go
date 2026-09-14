package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestPageHeaderIsReusableWithAndWithoutActions(t *testing.T) {
	withAction := renderNode(t, pageHeader(pageHeaderProps{
		Eyebrow: "Career", Title: "Promote Jane", Lead: "Review the change.",
		Actions: []ui.Node{html.A(html.Props{Href: "#/journeys"}, html.Text("All journeys"))},
	}))
	for _, want := range []string{"Career", "Promote Jane", "Review the change.", "All journeys", "jn-context-actions"} {
		if !strings.Contains(withAction, want) {
			t.Fatalf("header missing %q: %s", want, withAction)
		}
	}
	withoutAction := renderNode(t, pageHeader(pageHeaderProps{Title: "Promotion journeys"}))
	if strings.Contains(withoutAction, "jn-context-actions") {
		t.Fatalf("empty actions rendered a wrapper: %s", withoutAction)
	}
}

func TestEngineUnavailableCalloutIsSharedAndConditional(t *testing.T) {
	if engineUnavailableCallout("en-US", true, "not used") != nil || engineUnavailableCallout("en-US", false, "") != nil {
		t.Fatal("available or unexplained engine rendered a callout")
	}
	for _, language := range productui.SupportedProductLocales() {
		locale := productui.ResolveProductLocale(language)
		out := renderNode(t, engineUnavailableCallout(language, false, "This cell was started without -execution-authority=true."))
		if !strings.Contains(out, locale.Text("journey.actions_unavailable_title")) || !strings.Contains(out, locale.Text("journey.actions_unavailable_detail")) || strings.Contains(out, "execution-authority") {
			t.Fatalf("%s callout leaked internals or lost task copy: %s", language, out)
		}
	}
}

func TestLoadingPanelDoesNotMountDisabledControls(t *testing.T) {
	out := renderNode(t, loadingPanel("Loading employee context", "Reading the employee."))
	for _, want := range []string{`aria-busy="true"`, "Loading employee context", "Reading the employee."} {
		if !strings.Contains(out, want) {
			t.Fatalf("loading panel missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "disabled") || strings.Contains(out, "<form") {
		t.Fatalf("loading panel mounted eventual controls: %s", out)
	}
}

func TestNetworkPendingPageMakesStaleControlsInertBehindAProxy(t *testing.T) {
	page := Page{
		Notice: &Notice{Tone: toneInfo, Title: "جارٍ العمل…", Detail: "Reading journeys.", Busy: true},
		List:   &ListView{},
	}
	out := renderNode(t, BuildContent(page))
	for _, want := range []string{
		`class="jn-embedded jn-network-pending"`, `aria-busy="true"`,
		`class="jn-network-stale"`, `inert`, `aria-hidden="true"`,
		`class="jn-panel jn-network-proxy"`, "jn-proxy-row",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("network pending surface missing %q", want)
		}
	}

	page.Notice = &Notice{Tone: toneInfo, Title: "For your information", Detail: "Resolved."}
	out = renderNode(t, BuildContent(page))
	if strings.Contains(out, "jn-network-proxy") || strings.Contains(out, `aria-busy="true"`) {
		t.Fatal("an ordinary informational notice was misclassified as a pending network operation")
	}
}

func TestTodo_PROMOUX_010_Regression_ReviewStaysMountedDuringNetworkLatency(t *testing.T) {
	for _, key := range []string{"journey.busy_start", "journey.busy_approve", "journey.busy_reject"} {
		page := SampleDetailPage()
		page.Detail.Actions = []Action{{ID: "approve", Label: "Approve", Action: "/approve", ConfirmationNote: "Review this decision."}}
		page.Notice = &Notice{Tone: toneInfo, Title: "Working…", Busy: true, MessageKey: key}
		out := renderNode(t, BuildContent(page))
		for _, want := range []string{`aria-busy="true"`, `class="jn-confirm-body"`, `class="jn-confirm-actionbar"`, "Review this decision."} {
			if !strings.Contains(out, want) {
				t.Errorf("%s pending review lost %q", key, want)
			}
		}
		if strings.Contains(out, `class="jn-panel jn-network-proxy"`) || strings.Contains(out, `class="jn-network-stale"`) {
			t.Errorf("%s pending review was replaced by a page proxy", key)
		}
	}

	page := SampleDetailPage()
	page.Notice = &Notice{Tone: toneInfo, Title: "Working…", Busy: true, MessageKey: "journey.busy_detail"}
	if out := renderNode(t, BuildContent(page)); !strings.Contains(out, `class="jn-panel jn-network-proxy"`) {
		t.Fatal("ordinary detail loading lost its stable network proxy")
	}
}
