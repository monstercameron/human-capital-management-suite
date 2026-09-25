package productui

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const docsJourneyPath = "/workspace/app/journeys"

var docsJourneyID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

// DocsJourneyPreview is a server-authorized quick look at a shared
// in-progress workflow (a promotion journey). Authorized stays false when
// the journey service refused, did not find, or failed to read it.
type DocsJourneyPreview struct {
	IntentID   string
	Authorized bool
	Workflow   string `json:",omitempty"`
	Worker     string `json:",omitempty"`
	From       string `json:",omitempty"`
	To         string `json:",omitempty"`
	Stage      string `json:",omitempty"` // JourneyStage enum name
	StageGroup string `json:",omitempty"` // active, blocked or done
	Effective  string `json:",omitempty"` // YYYY-MM-DD
	Approver   string `json:",omitempty"`
}

var docsJourneyKeys = map[string]bool{"journey": true, "locale": true, "nav": true}

// ParseDocsJourneyReference accepts a relative or absolute journey address
// and returns its intent ID; the renderer gates absolute ones to this
// workspace's origin.
func ParseDocsJourneyReference(target string) (string, bool) {
	relative, _ := docsProjectRelative(target)
	parsed, err := url.ParseRequestURI(relative)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path != docsJourneyPath || parsed.Fragment != "" {
		return "", false
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(values["journey"]) != 1 {
		return "", false
	}
	for key, entries := range values {
		if !docsJourneyKeys[key] || len(entries) != 1 {
			return "", false
		}
	}
	id := values.Get("journey")
	return id, docsJourneyID.MatchString(id)
}

// DocsJourneyHref is the canonical in-app address of a journey.
func DocsJourneyHref(id string) string {
	if !docsJourneyID.MatchString(id) {
		return ""
	}
	return docsJourneyPath + "?" + url.Values{"journey": []string{id}}.Encode()
}

// DocsJourneyReferences lists distinct journey IDs linked or pasted in a
// document, in source order, for the resolver.
func DocsJourneyReferences(markdown string) []string {
	if len(markdown) > 64*1024 {
		return nil
	}
	source := []byte(markdown)
	root := docsMarkdownParser.Parse(text.NewReader(source))
	seen := map[string]bool{}
	var ids []string
	add := func(target string) {
		if id, ok := ParseDocsJourneyReference(target); ok && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Link:
			add(string(n.Destination))
		case *ast.AutoLink:
			add(string(n.URL(source)))
		case *ast.Text:
			for _, word := range strings.Fields(string(n.Segment.Value(source))) {
				add(word)
			}
		}
		return ast.WalkContinue, nil
	})
	return ids
}

func encodeDocsJourneys(previews []DocsJourneyPreview) string {
	if len(previews) == 0 {
		return ""
	}
	data, err := json.Marshal(previews)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeDocsJourneys(value string) []DocsJourneyPreview {
	var previews []DocsJourneyPreview
	if value != "" {
		_ = json.Unmarshal([]byte(value), &previews)
	}
	return previews
}

func docsJourneyPreview(view View, id string) (DocsJourneyPreview, bool) {
	if view.Document == nil {
		return DocsJourneyPreview{}, false
	}
	for _, preview := range view.Document.Journeys {
		if preview.IntentID == id && preview.Authorized && strings.TrimSpace(preview.Worker) != "" {
			return preview, true
		}
	}
	return DocsJourneyPreview{}, false
}

// docsJourneyLinkTarget renders an inline journey link, or the neutral
// unavailable text, for a same-origin journey address.
func docsJourneyLinkTarget(view View, target string) (ui.Node, bool) {
	id, ok := ParseDocsJourneyReference(target)
	if !ok || !docsProjectSameOrigin(view, target) {
		return nil, false
	}
	copy := docsJourneyCopy(view.Locale.Resolved)
	preview, found := docsJourneyPreview(view, id)
	if !found {
		return html.Span(html.Props{Class: "docs-project-task-restricted", Role: "status"}, ui.Text(copy.unavailable)), true
	}
	href := DocsJourneyHref(id)
	label := copy.workflow(preview) + ": " + preview.Worker
	return softwareLink(view.Navigate, html.Props{Class: "docs-project-task-link", Data: map[string]string{"docs-journey": href}, Raw: map[string]any{"aria-label": copy.open + ": " + label}}, href,
		html.Span(html.Props{Class: "docs-project-task-title"}, ui.Text(label)),
		html.Span(html.Props{Class: "docs-project-task-status"}, ui.Text(copy.stage(preview.Stage)))), true
}

// docsJourneyEmbedParagraph unfurls a paragraph holding only a journey
// address into a quick-look card.
func docsJourneyEmbedParagraph(view View, paragraph *ast.Paragraph, source []byte) (ui.Node, bool) {
	var target, textValue string
	for child := paragraph.FirstChild(); child != nil; child = child.NextSibling() {
		switch node := child.(type) {
		case *ast.Link:
			if target != "" {
				return nil, false
			}
			target = string(node.Destination)
		case *ast.AutoLink:
			if target != "" {
				return nil, false
			}
			target = string(node.URL(source))
		case *ast.Text:
			textValue += string(node.Segment.Value(source))
		default:
			return nil, false
		}
	}
	if textValue = strings.TrimSpace(textValue); textValue != "" {
		if target != "" || strings.ContainsAny(textValue, " \t\n") {
			return nil, false
		}
		target = textValue
	}
	id, ok := ParseDocsJourneyReference(target)
	if !ok || !docsProjectSameOrigin(view, target) {
		return nil, false
	}
	preview, found := docsJourneyPreview(view, id)
	if !found {
		return nil, false
	}
	copy := docsJourneyCopy(view.Locale.Resolved)
	children := []ui.Node{
		html.Span(html.Props{Class: "docs-project-embed-kind"}, ui.Text(copy.kind+" · "+copy.workflow(preview))),
		html.Span(html.Props{Class: "docs-project-embed-titlerow"}, html.Span(html.Props{Class: "docs-project-embed-title", Dir: "auto"}, ui.Text(preview.Worker))),
	}
	if preview.From != "" || preview.To != "" {
		children = append(children, html.Span(html.Props{Class: "docs-journey-embed-move", Dir: "ltr"}, html.Span(html.Props{}, ui.Text(preview.From)), html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, ui.Text("→")), html.Strong(html.Props{}, ui.Text(preview.To))))
	}
	tone := "doing"
	switch preview.StageGroup {
	case "done":
		tone = "done"
	case "blocked":
		tone = "todo"
	}
	meta := []ui.Node{html.Span(html.Props{Class: "docs-project-embed-status", Data: map[string]string{"tone": tone}}, html.Span(html.Props{Class: "docs-project-embed-dot", Aria: map[string]string{"hidden": "true"}}), ui.Text(copy.stage(preview.Stage)))}
	if effective, err := time.Parse("2006-01-02", preview.Effective); err == nil {
		meta = append(meta, html.Span(html.Props{Class: "docs-project-embed-byline"}, ui.Text(strings.ReplaceAll(copy.effective, "{date}", docsShortDateLabel(view.Locale.Resolved, effective)))))
	}
	if preview.Approver != "" {
		meta = append(meta, html.Span(html.Props{Class: "docs-project-embed-byline"}, ui.Text(strings.ReplaceAll(copy.approver, "{name}", preview.Approver))))
	}
	children = append(children, html.Span(html.Props{Class: "docs-project-embed-meta"}, meta...))
	href := DocsJourneyHref(id)
	return softwareLink(view.Navigate, html.Props{Class: "docs-project-embed docs-journey-embed", Data: map[string]string{"docs-journey": href, "tone": preview.StageGroup}, Raw: map[string]any{"aria-label": copy.open + ": " + preview.Worker}}, href, children...), true
}

type docsJourneyLocaleCopy struct {
	open, unavailable, kind, promotion, effective, approver string
	stages                                                  map[string]string
	inProgress                                              string
}

func (c docsJourneyLocaleCopy) workflow(p DocsJourneyPreview) string {
	if name := strings.TrimSpace(p.Workflow); name != "" {
		return name
	}
	return c.promotion
}

func (c docsJourneyLocaleCopy) stage(stage string) string {
	if label := c.stages[stage]; label != "" {
		return label
	}
	return c.inProgress
}

func docsJourneyCopy(locale string) docsJourneyLocaleCopy {
	stage := func(proposed, blocked, awaiting, completed, rejected, failed, finance, manager, waiting, recheck string) map[string]string {
		return map[string]string{"JOURNEY_STAGE_PROPOSED": proposed, "JOURNEY_STAGE_BLOCKED": blocked, "JOURNEY_STAGE_AWAITING_APPROVAL": awaiting, "JOURNEY_STAGE_COMPLETED": completed, "JOURNEY_STAGE_REJECTED": rejected, "JOURNEY_STAGE_FAILED": failed, "JOURNEY_STAGE_FINANCE_APPROVAL": finance, "JOURNEY_STAGE_MANAGER_APPROVAL": manager, "JOURNEY_STAGE_WAITING_EFFECTIVE_DATE": waiting, "JOURNEY_STAGE_REVALIDATION": recheck}
	}
	switch locale {
	case "de-DE":
		return docsJourneyLocaleCopy{open: "Workflow öffnen", unavailable: "Workflow nicht verfügbar", kind: "Workflow", promotion: "Beförderung", effective: "Wirksam ab {date}", approver: "Genehmigt durch: {name}", inProgress: "In Bearbeitung",
			stages: stage("Vorgeschlagen", "Blockiert", "Wartet auf Genehmigung", "Abgeschlossen", "Abgelehnt", "Beendet", "Finanzfreigabe", "Freigabe durch Führungskraft", "Wartet auf Stichtag", "Wird erneut geprüft")}
	case "ar":
		return docsJourneyLocaleCopy{open: "فتح سير العمل", unavailable: "سير العمل غير متاح", kind: "سير عمل", promotion: "ترقية", effective: "يسري في {date}", approver: "المعتمِد: {name}", inProgress: "قيد التنفيذ",
			stages: stage("مقترح", "متوقف", "بانتظار الموافقة", "مكتمل", "مرفوض", "منتهٍ", "موافقة المالية", "موافقة المدير", "بانتظار تاريخ السريان", "قيد إعادة التحقق")}
	default:
		return docsJourneyLocaleCopy{open: "Open workflow", unavailable: "Workflow unavailable", kind: "Workflow", promotion: "Promotion", effective: "Effective {date}", approver: "Approver: {name}", inProgress: "In progress",
			stages: stage("Proposed", "Blocked", "Awaiting approval", "Completed", "Rejected", "Stopped", "Finance approval", "Manager approval", "Waiting for effective date", "Rechecking")}
	}
}

func docsJourneyStylesheet() string {
	return `.docs-markdown .docs-journey-embed{border-inline-start-color:var(--hcm-color-warning,#b45309)}` +
		`.docs-markdown .docs-journey-embed[data-tone="done"]{border-inline-start-color:var(--hcm-color-success,#15803d)}` +
		`.docs-journey-embed-move{display:flex;flex-wrap:wrap;align-items:baseline;gap:.4rem;color:var(--muted);font-size:var(--hcm-font-size-small);font-variant-numeric:tabular-nums}` +
		`.docs-journey-embed-move strong{color:var(--ink);font-weight:600}`
}
