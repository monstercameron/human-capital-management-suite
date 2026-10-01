package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	workflowinspect "github.com/monstercameron/human-capital-management-suite/internal/workflow/inspect"
)

// WorkflowHistoryTimelineProps is the safe, read-only detail projection for
// one history row. The inspect package has already applied field authority;
// this renderer does not rediscover or infer protected values.
type WorkflowHistoryTimelineProps struct {
	I18nProps
	RunID    string
	Timeline workflowinspect.PageUseTimeline
	BackHref string
}

func workflowHistoryTimelinePage(props WorkflowHistoryTimelineProps) ui.Node {
	page := props.Timeline.Page
	children := []ui.Node{
		html.H1(html.Props{ID: "workflow-history-timeline-title"}, ui.Text("Run timeline")),
		html.P(html.Props{Class: "muted"}, ui.Text("This run used the recorded page version.")),
		html.A(html.Props{Href: props.BackHref, Class: "button secondary"}, ui.Text("Back to history")),
		html.Section(html.Props{Class: "workflow-history-page-version", Raw: map[string]any{"aria-labelledby": "workflow-history-timeline-title"}},
			html.H2(html.Props{}, ui.Text("Page version")),
			html.P(html.Props{Class: "workflow-history-page-version-ref"}, ui.Text(page.WorkflowID+" / workflow v"+itoa64(int64(page.WorkflowVersion))+" / page v"+itoa64(page.PageVersion))),
			html.A(html.Props{Href: workflowHistoryPageVersionHref(props.RunID, page), Data: map[string]string{"workflow-page-version": itoa64(page.PageVersion)}}, ui.Text("Reopen recorded page")),
		),
	}

	fieldRows := make([]ui.Node, 0, len(props.Timeline.Fields))
	for _, field := range props.Timeline.Fields {
		if strings.TrimSpace(field.ID) == "" {
			continue
		}
		fieldRows = append(fieldRows, html.Tag("li", html.Props{Data: map[string]string{"workflow-page-field": field.ID}}, html.Code(html.Props{}, ui.Text(field.ID)), html.Span(html.Props{}, ui.Text(": "+field.Value))))
	}
	children = append(children, html.Section(html.Props{Class: "workflow-history-submitted-page", Raw: map[string]any{"aria-label": "Submitted page, read only"}}, html.H2(html.Props{}, ui.Text("Submitted page (read only)")), html.Ul(html.Props{}, fieldRows...)))

	rules := append([]string(nil), props.Timeline.Rules...)
	children = append(children, html.Section(html.Props{Class: "workflow-history-rules", Raw: map[string]any{"aria-label": "Rules evaluated"}}, html.H2(html.Props{}, ui.Text("Rules evaluated")), html.Ul(html.Props{}, stringsAsNodes(rules)...)))
	sops := append([]string(nil), props.Timeline.SOPVersions...)
	children = append(children, html.Section(html.Props{Class: "workflow-history-sops", Raw: map[string]any{"aria-label": "SOP versions"}}, html.H2(html.Props{}, ui.Text("SOP versions")), html.Ul(html.Props{}, stringsAsNodes(sops)...)))

	auditRows := make([]ui.Node, 0, len(props.Timeline.Audit))
	for _, event := range props.Timeline.Audit {
		actor, ok := event.ActorRef.Get()
		if !ok {
			actor = "Unavailable"
		}
		auditRows = append(auditRows, html.Tag("li", html.Props{Data: map[string]string{"workflow-page-use": string(event.Operation)}}, ui.Text(string(event.Operation)+" · "+actor+" · page v"+itoa64(event.PageVersion)+" · "+event.RecordedAt)))
	}
	children = append(children, html.Section(html.Props{Class: "workflow-history-page-use-audit", Raw: map[string]any{"aria-label": "Page use audit"}}, html.H2(html.Props{}, ui.Text("Page use audit")), html.Ul(html.Props{}, auditRows...)))
	return html.Main(html.Props{Class: "workflow-history-timeline", Raw: map[string]any{"aria-labelledby": "workflow-history-timeline-title"}}, children...)
}

func workflowHistoryPageVersionHref(runID string, page workflowinspect.PageVersionRef) string {
	return Path(PageWorkflowHistory) + "?run=" + url.QueryEscape(runID) + "&workflow_version=" + itoa64(int64(page.WorkflowVersion)) + "&page_id=" + url.QueryEscape(page.PageID) + "&page_version=" + itoa64(page.PageVersion)
}

func stringsAsNodes(values []string) []ui.Node {
	result := make([]ui.Node, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, html.Tag("li", html.Props{}, ui.Text(value)))
		}
	}
	return result
}
