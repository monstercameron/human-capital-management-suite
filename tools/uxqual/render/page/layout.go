package page

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// LayoutProps is the shared, semantic input for page layout primitives. Props
// is copied before the primitive adds its contract attributes, so callers can
// safely reuse a props value (and its Data map) for multiple regions.
//
// Spacing accepts the production spacing.1 through spacing.4 tokens. Invalid
// values are ignored rather than becoming CSS or selector input. Gap is an
// alias retained for layout vocabulary; both fields use the same token scale.
type LayoutProps struct {
	Props   html.Props
	Spacing string
	// Gap is an alias for Spacing for callers expressing the relationship as
	// a layout gap. Spacing wins when both are supplied.
	Gap string
	// Density is an optional presentation hint ("comfortable" or "dense").
	// Unknown values are omitted and therefore fail closed to the stylesheet's
	// default rhythm.
	Density string
}

// The named aliases make each primitive self-documenting while keeping one
// small contract for all five primitives.
type StackProps = LayoutProps
type ClusterProps = LayoutProps
type GridProps = LayoutProps
type SplitProps = LayoutProps
type PageFrameProps = LayoutProps

// Stack lays children in document order with a tokenized vertical rhythm.
func Stack(props StackProps, children ...ui.Node) ui.Node {
	return layout("stack", props, children...)
}

// Cluster lays children in a wrapping row. Responsive wrapping is owned by
// the shared stylesheet, not by page-specific markup.
func Cluster(props ClusterProps, children ...ui.Node) ui.Node {
	return layout("cluster", props, children...)
}

// Grid lays children in a responsive semantic grid.
func Grid(props GridProps, children ...ui.Node) ui.Node {
	return layout("grid", props, children...)
}

// Split lays children in a two-pane composition that collapses as one region
// at the stylesheet's declared breakpoint.
func Split(props SplitProps, children ...ui.Node) ui.Node {
	return layout("split", props, children...)
}

// PageFrame is the shared page measure and shell-gutter anchor. It is a
// wrapper primitive so pages can compose headers, content and supporting
// regions without page-specific margin chains.
func PageFrame(props PageFrameProps, children ...ui.Node) ui.Node {
	return layout("page-frame", props, children...)
}

func layout(kind string, in LayoutProps, children ...ui.Node) ui.Node {
	p := in.Props
	p.Class = strings.TrimSpace(strings.Join([]string{p.Class, "layout-" + kind}, " "))
	p.Data = cloneData(p.Data)
	p.Data["layout-primitive"] = kind
	spacing := in.Spacing
	if spacing == "" {
		spacing = in.Gap
	}
	if token := spacingToken(spacing); token != "" {
		p.Data["spacing-token"] = token
		p.Class = strings.TrimSpace(p.Class + " layout-spacing-" + token[len(token)-1:])
	}
	if in.Density == "dense" || in.Density == "comfortable" {
		p.Data["density"] = in.Density
	}
	return html.Div(p, children...)
}

func cloneData(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+2)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func spacingToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) != len("spacing.1") || !strings.HasPrefix(raw, "spacing.") {
		return ""
	}
	digit := raw[len(raw)-1]
	if digit < '1' || digit > '4' {
		return ""
	}
	return raw
}

// LayoutCSS is the stylesheet contract for the primitives. Production
// composition should append this once to its existing theme stylesheet. The
// selectors deliberately use the same --hcm-space-1..4 and --hcm-density
// variables as internal/humanwork/productui/theme.go.
func LayoutCSS() string {
	return `.layout-stack,.layout-cluster,.layout-grid,.layout-split,.layout-page-frame{min-inline-size:0;gap:calc(var(--hcm-space-2,.5rem) * var(--hcm-density,1));}.layout-stack{display:flex;flex-direction:column;}.layout-cluster{display:flex;flex-wrap:wrap;align-items:center;}.layout-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,18rem),1fr));}.layout-split{display:grid;grid-template-columns:minmax(0,2fr) minmax(16rem,1fr);}.layout-page-frame{display:flex;flex-direction:column;inline-size:min(100%,72rem);margin-inline:auto;padding-inline:calc(var(--hcm-space-2,.5rem) * var(--hcm-density,1));box-sizing:border-box;}.layout-spacing-1{gap:calc(var(--hcm-space-1,.25rem) * var(--hcm-density,1));}.layout-spacing-2{gap:calc(var(--hcm-space-2,.5rem) * var(--hcm-density,1));}.layout-spacing-3{gap:calc(var(--hcm-space-3,.75rem) * var(--hcm-density,1));}.layout-spacing-4{gap:calc(var(--hcm-space-4,1rem) * var(--hcm-density,1));}.layout-stack[data-density="dense"],.layout-cluster[data-density="dense"],.layout-grid[data-density="dense"],.layout-split[data-density="dense"],.layout-page-frame[data-density="dense"]{--hcm-density:.75;}.layout-stack[data-density="comfortable"],.layout-cluster[data-density="comfortable"],.layout-grid[data-density="comfortable"],.layout-split[data-density="comfortable"],.layout-page-frame[data-density="comfortable"]{--hcm-density:1.25;}@media (max-width:48rem){.layout-split{grid-template-columns:1fr;}}`
}
