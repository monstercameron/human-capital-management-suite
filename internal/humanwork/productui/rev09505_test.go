package productui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// REV-095-05: UXLIVE-018 gave every work filter its own count, which made
// each entry wider, and its evidence recorded that the strip still wrapped
// at 1280 px with the sidebar expanded. The first fix (a fixed three inline
// entries) was measured live and still clipped the active "Tracked requests"
// tab on Home's ~580 px card, so each entry now carries the container width
// it needs and a container query moves it behind More below that width.

func rev09505Strip(t *testing.T, view View, options workCollectionOptions) string {
	t.Helper()
	markup, err := ui.RenderToString(ui.CreateElement(WorkCollection, workCollectionProps(view, options)))
	if err != nil {
		t.Fatal(err)
	}
	strip := regexp.MustCompile(`(?s)<nav aria-label="[^"]*" class="tabs work-tabs">.*?</nav>`).FindString(markup)
	if strip == "" {
		t.Fatalf("no filter strip:\n%s", markup)
	}
	return strip
}

// rev09505Visible is what a strip of width px shows, applying the same rules
// the stylesheet's container queries apply.
func rev09505Visible(tabs []WorkTabProps, layout workStripLayout, width int) (inline []WorkTabProps, more bool) {
	for index, tab := range tabs {
		if layout.Fit[index] == 0 || layout.Fit[index] <= width {
			inline = append(inline, tab)
		}
	}
	return inline, width < layout.More
}

// TestTodo_REV_095_05 is the primary test: every filter but the active one
// is in the track and behind More with the same threshold, the active filter
// has none, thresholds grow in reading order, and a longer locale needs more
// room for the same filter.
func TestTodo_REV_095_05(t *testing.T) {
	view := promoux012View(PageWork)
	view.WorkFilter = "tracked"
	for name, options := range map[string]workCollectionOptions{"Home": {}, "My Work": {ListDetail: true}} {
		props := workCollectionProps(view, options)
		layout := planWorkStrip(props.Tabs, view.Locale.Text("work.more_filters"))
		strip := rev09505Strip(t, view, options)
		previous := 0
		for index, tab := range props.Tabs {
			fit := layout.Fit[index]
			if tab.Active {
				if fit != 0 {
					t.Fatalf("%s: the active filter %q can be hidden", name, tab.Label)
				}
				continue
			}
			if fit < previous || fit > layout.More {
				t.Fatalf("%s: threshold %d for %q is out of order (previous %d, More %d)", name, fit, tab.Label, previous, layout.More)
			}
			previous = fit
			attr := `data-strip-fit="` + strconv.Itoa(fit) + `"`
			if strings.Count(strip, attr) < 2 {
				t.Fatalf("%s: %q is not both inline and behind More with %s:\n%s", name, tab.Label, attr, strip)
			}
		}
		if !strings.Contains(strip, `aria-current="page" class="tab active"`) || !strings.Contains(strip, `aria-label="More work filters"`) {
			t.Fatalf("%s: the active filter or the named More control is missing:\n%s", name, strip)
		}
	}
	german := view
	german.Locale = ResolveProductLocale("de-DE")
	en := planWorkStrip(workCollectionProps(view, workCollectionOptions{}).Tabs, "More")
	de := planWorkStrip(workCollectionProps(german, workCollectionOptions{}).Tabs, "Mehr")
	if de.More <= en.More {
		t.Fatalf("German labels need no more room than English: %d vs %d", de.More, en.More)
	}
}

// TestTodo_REV_095_05_Browser checks the served stylesheet carries the
// container ladder and, at the strip widths measured on the live server
// (Home's card at 1280 px with the sidebar expanded ~536 px of strip, with it
// collapsed ~640 px, 1440 px ~740 px, and a 390 px phone ~316 px), the
// entries left inline plus More fit, with the active filter never dropped.
func TestTodo_REV_095_05_Browser(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.work-list .tabs.work-tabs{`, `container-type:inline-size`, `container-name:work-strip`,
		`@container work-strip (max-width:579.98px){.work-tab-slot[data-strip-fit="580"]{display:none;}`,
		`.work-tabs-more-item[data-strip-fit="580"]{display:list-item;}`,
		`.work-list:has(.work-tabs-more[open]){overflow:visible;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("stylesheet is missing %q", want)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := promoux012View(PageWork)
		view.Locale = ResolveProductLocale(locale)
		view.WorkFilter = "tracked"
		tabs := workCollectionProps(view, workCollectionOptions{}).Tabs
		moreLabel := view.Locale.Text("work.more_filters")
		layout := planWorkStrip(tabs, moreLabel)
		moreWidth := float64(len([]rune(moreLabel)))*workStripGlyphPx + 18
		for _, width := range []int{316, 536, 640, 740, 1100} {
			inline, more := rev09505Visible(tabs, layout, width)
			used := 0.0
			active := false
			for _, tab := range inline {
				used += workTabWidth(tab) + workStripGapPx
				active = active || tab.Active
			}
			if more {
				used += moreWidth
			} else {
				used -= workStripGapPx
			}
			if !active {
				t.Fatalf("%s at %d px: the active filter left the strip", locale, width)
			}
			if len(inline) > 1 && used > float64(width) {
				t.Fatalf("%s at %d px: %d inline entries need %.0f px", locale, width, len(inline), used)
			}
			if !more && len(inline) != len(tabs) {
				t.Fatalf("%s at %d px: entries are hidden with no More control", locale, width)
			}
		}
	}
}

// TestTodoREV09505WorkCountStaysBesideTitle keeps "0 items" on the title's
// line on a phone.
func TestTodoREV09505WorkCountStaysBesideTitle(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, ".work-list>.section-head{align-items:flex-start;flex-wrap:nowrap;}") ||
		!strings.Contains(css, ".work-list>.section-head>.count{flex:none;white-space:nowrap;}") {
		t.Fatal("the work card's count can still wrap under its description")
	}
}
