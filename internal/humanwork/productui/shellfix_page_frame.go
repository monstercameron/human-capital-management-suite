package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ProductPageFrame renders the standard product document frame. Its exact
// markup contract is `<section class="product-page-frame">`, an optional
// `<nav class="product-page-frame-breadcrumbs">`, then
// `<header class="product-page-frame-title-row"><h1>…</h1><div
// class="product-page-frame-actions">…</div></header>`, followed by body
// content. Page owners pass localized content; this shell primitive only
// provides shared geometry and title/action alignment.
type ProductPageFrameProps struct {
	Class       string
	Dir         string
	Aria        map[string]string
	Raw         map[string]any
	Breadcrumbs ui.Node
	Title       string
	TitleID     string
	Actions     []ui.Node
	Body        []ui.Node
}

func ProductPageFrame(props ProductPageFrameProps) ui.Node {
	class := "product-page-frame"
	if extra := strings.TrimSpace(props.Class); extra != "" {
		class += " " + extra
	}
	children := make([]ui.Node, 0, len(props.Body)+3)
	if props.Breadcrumbs != nil {
		children = append(children, html.Nav(html.Props{Class: "product-page-frame-breadcrumbs"}, props.Breadcrumbs))
	}
	row := []ui.Node{html.H1(html.Props{ID: strings.TrimSpace(props.TitleID)}, ui.Text(props.Title))}
	if len(props.Actions) > 0 {
		row = append(row, html.Div(html.Props{Class: "product-page-frame-actions"}, props.Actions...))
	}
	children = append(children, html.Header(html.Props{Class: "product-page-frame-title-row"}, row...))
	children = append(children, props.Body...)
	return html.Section(html.Props{Class: class, Dir: props.Dir, Aria: props.Aria, Raw: props.Raw}, children...)
}
