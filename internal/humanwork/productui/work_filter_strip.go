package productui

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// REV-095-05: Home's attention strip and My Work's filter strip stay on one
// line at every width. UXLIVE-018's per-filter counts made the six entries
// about 900 px wide in English, far wider than the strip's card on Home at
// 1280 px with the sidebar expanded (about 580 px) or on a phone.
//
// A fixed "first N inline" rule cannot work: the card's width depends on the
// viewport, the sidebar, the page and the locale, and a German label is half
// as long again as its English one. So each entry carries the container
// width it needs to stay inline, computed from its own label and count, and
// the stylesheet hides it from the track -- and shows its copy behind More --
// with a container query whenever the strip is narrower than that. The
// active filter is always inline, so the strip never hides where the reader
// is, and every hidden copy is display:none, so a filter is exposed to
// assistive technology exactly once.

const (
	workStripGlyphPx      = 8.2 // conservative average glyph at the strip's 14 px
	workStripCountGlyphPx = 7.0 // count pill digits at 12 px
	workStripGapPx        = 16.0
	workStripSafetyPx     = 8.0
	workStripBucketPx     = 20
	workStripMaxBucketPx  = 2400
	workStripAttr         = "strip-fit"
)

// workTabWidth estimates one entry's rendered width in CSS pixels.
func workTabWidth(tab WorkTabProps) float64 {
	width := float64(utf8.RuneCountInString(tab.Label)) * workStripGlyphPx
	if count := strings.TrimSpace(tab.Count); count != "" {
		// .work-tab-count: 6 px margin, 12 px padding, 20 px minimum width.
		width += 6 + math.Max(20, 12+float64(utf8.RuneCountInString(count))*workStripCountGlyphPx)
	}
	return width
}

// workStripBucket rounds a width up to the stylesheet's container-query step.
func workStripBucket(width float64) int {
	bucket := int(math.Ceil(width/workStripBucketPx)) * workStripBucketPx
	if bucket < workStripBucketPx {
		return workStripBucketPx
	}
	if bucket > workStripMaxBucketPx {
		return workStripMaxBucketPx
	}
	return bucket
}

// workStripLayout is the strip's fit plan: for every entry other than the
// active one, the container width at or above which it stays inline, and the
// width below which the More control is needed.
type workStripLayout struct {
	Fit  []int // per entry; 0 means always inline (the active filter)
	More int
}

func planWorkStrip(tabs []WorkTabProps, moreLabel string) workStripLayout {
	layout := workStripLayout{Fit: make([]int, len(tabs))}
	reserved := 0.0
	for _, tab := range tabs {
		if tab.Active {
			reserved += workTabWidth(tab) + workStripGapPx
		}
	}
	more := float64(utf8.RuneCountInString(moreLabel))*workStripGlyphPx + 18 // trigger text plus chevron
	all := reserved
	for _, tab := range tabs {
		if !tab.Active {
			all += workTabWidth(tab) + workStripGapPx
		}
	}
	whole := all - workStripGapPx + workStripSafetyPx
	layout.More = workStripBucket(whole)
	running := reserved
	for index, tab := range tabs {
		if tab.Active {
			continue
		}
		running += workTabWidth(tab) + workStripGapPx
		// Everything fits without More once the strip is as wide as the
		// whole set, so no entry ever needs more than that.
		layout.Fit[index] = workStripBucket(math.Min(running+more+workStripSafetyPx, whole))
	}
	return layout
}

// workFilterStrip renders the one-line filter strip shared by Home and My
// Work: a single-line track of filters and, when some do not fit the strip's
// actual width, an explicit More disclosure listing them with their counts.
func workFilterStrip(props WorkCollectionProps) ui.Node {
	moreLabel := props.Text("work.more_filters")
	layout := planWorkStrip(props.Tabs, moreLabel)
	track := make([]ui.Node, 0, len(props.Tabs))
	overflow := make([]ui.Node, 0, len(props.Tabs))
	for index, item := range props.Tabs {
		if layout.Fit[index] == 0 {
			track = append(track, html.Span(html.Props{Class: "work-tab-slot"}, ui.CreateElement(WorkTab, item)))
			continue
		}
		fit := html.DataAttribute{Name: workStripAttr, Value: strconv.Itoa(layout.Fit[index])}
		track = append(track, html.Span(html.Props{Class: "work-tab-slot", DataAttr: fit}, ui.CreateElement(WorkTab, item)))
		overflow = append(overflow, html.Li(html.Props{Class: "work-tabs-more-item", DataAttr: fit}, ui.CreateElement(WorkTab, item)))
	}
	children := []ui.Node{html.Div(html.Props{Class: "work-tabs-track"}, track...)}
	if len(overflow) > 0 {
		children = append(children, html.Div(html.Props{Class: "work-tabs-more-slot", DataAttr: html.DataAttribute{Name: workStripAttr, Value: strconv.Itoa(layout.More)}},
			ui.CreateElement(TransientPopover, TransientPopoverProps{
				Kind: "work-filters", Class: "work-tabs-more", TriggerClass: "tab work-tabs-more-trigger",
				Label:      props.Text("work.more_filters_aria"),
				Trigger:    []ui.Node{ui.Text(moreLabel), productIcon("expand", "work-tabs-more-chevron")},
				PanelClass: "work-tabs-more-panel",
				Children:   []ui.Node{html.Ul(html.Props{Class: "work-tabs-more-list"}, overflow...)},
			})))
	}
	return html.Nav(html.Props{Class: "tabs work-tabs", Aria: map[string]string{"label": props.Text("work.filter_label")}}, children...)
}

// workFilterStripStylesheet keeps the strip on one line at every width. The
// nav is the query container; each bucket rule hides the inline entries that
// need more width than the strip has and shows their copies behind More.
func workFilterStripStylesheet() string {
	typed := buildTypedSheet(func() {
		declareGlobal(".work-list .tabs.work-tabs",
			gwccss.Raw("flex-wrap", "nowrap"),
			gwccss.Items.Stretch,
			gwccss.Gap(gwccss.Px(16)),
			gwccss.Raw("overflow", "visible"),
			gwccss.Raw("container-type", "inline-size"),
			gwccss.Raw("container-name", "work-strip"),
		)
		declareGlobal(".work-tabs-track",
			gwccss.Display.Flex,
			gwccss.Raw("flex-wrap", "nowrap"),
			gwccss.Gap(gwccss.Px(16)),
			gwccss.Raw("flex", "0 1 auto"),
			gwccss.Raw("min-inline-size", "0"),
		)
		declareGlobal(".work-tab-slot",
			gwccss.Display.Flex,
			gwccss.Raw("flex", "none"),
		)
		declareGlobal(".work-tabs .tab",
			gwccss.Raw("white-space", "nowrap"),
			gwccss.Raw("flex", "none"),
		)
		declareGlobal(".work-tabs-more-slot",
			gwccss.Display.None,
			gwccss.Raw("flex", "none"),
		)
		declareGlobal(".work-tabs-more",
			gwccss.Position.Relative,
			gwccss.Raw("flex", "none"),
		)
		declareGlobal(".work-tabs-more>summary.work-tabs-more-trigger",
			gwccss.Raw("cursor", "pointer"),
			gwccss.Raw("list-style", "none"),
		)
		declareGlobal(".work-tabs-more-chevron",
			gwccss.W(gwccss.Px(14)), gwccss.H(gwccss.Px(14)),
			gwccss.Raw("margin-inline-start", "4px"),
			gwccss.Transform(gwccss.Rotate(gwccss.Deg(90))),
		)
		declareGlobal(".work-tabs-more-panel",
			gwccss.MinWidth(gwccss.Px(220)),
			gwccss.Padding(gwccss.Px(6)),
		)
		declareGlobal(".work-tabs-more-list",
			gwccss.Raw("list-style", "none"),
			gwccss.Margin(gwccss.Zero),
			gwccss.Padding(gwccss.Zero),
		)
		declareGlobal(".work-tabs-more-item",
			gwccss.Display.None,
		)
		declareGlobal(".work-tabs-more-list .tab",
			gwccss.Display.Flex,
			gwccss.Raw("justify-content", "space-between"),
			gwccss.PaddingY(gwccss.Px(9)), gwccss.PaddingX(gwccss.Px(10)),
			gwccss.Raw("border-bottom", "0"),
		)
		// The work list clips its rounded corners; an open More menu must
		// not be clipped with them.
		declareGlobal(".work-list:has(.work-tabs-more[open])",
			gwccss.Raw("overflow", "visible"),
		)
		// The card's count stays beside its title on a phone instead of
		// dropping onto a line of its own under the description.
		declareGlobal(".work-list>.section-head",
			gwccss.Raw("flex-wrap", "nowrap"),
			gwccss.Raw("align-items", "flex-start"),
		)
		declareGlobal(".work-list>.section-head>:first-child",
			gwccss.Raw("flex", "1 1 auto"),
			gwccss.Raw("min-inline-size", "0"),
		)
		declareGlobal(".work-list>.section-head>.count",
			gwccss.Raw("flex", "none"),
			gwccss.Raw("white-space", "nowrap"),
		)
	})
	return typed + workStripContainerRules()
}

// workStripContainerRules is the bucketed @container ladder. gwccss emits
// only @media at-rules, so this fixed, deterministic text is appended to the
// typed sheet (it is hashed into the CSP with the rest of the stylesheet).
func workStripContainerRules() string {
	var out strings.Builder
	for bucket := workStripBucketPx; bucket <= workStripMaxBucketPx; bucket += workStripBucketPx {
		value := strconv.Itoa(bucket)
		out.WriteString("@container work-strip (max-width:" + strconv.Itoa(bucket-1) + ".98px){")
		out.WriteString(`.work-tab-slot[data-strip-fit="` + value + `"]{display:none;}`)
		out.WriteString(`.work-tabs-more-item[data-strip-fit="` + value + `"]{display:list-item;}`)
		out.WriteString(`.work-tabs-more-slot[data-strip-fit="` + value + `"]{display:flex;}`)
		out.WriteString("}")
	}
	return out.String()
}
