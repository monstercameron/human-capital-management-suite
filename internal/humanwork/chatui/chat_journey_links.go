package chatui

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// JourneyReference is a shared in-progress workflow (a promotion journey)
// named by its canonical same-origin address in a chat message.
type JourneyReference struct {
	IntentID   string
	Start, End int
}

// JourneyPreview is the viewer-authorized quick look at a journey. Every
// field is scalar so the preview cache can compare values with ==, and every
// label is already localized by the resolver except the stage, which the
// renderer localizes from StageTone and Stage.
type JourneyPreview struct {
	IntentID string
	Readable bool
	State    string // loading, ready, restricted, unavailable
	Workflow string // e.g. "Promotion"
	Worker   string
	From, To string // "SAL-AE3 · P4" → "SAL-DIR · M4"
	// Stage is the JourneyStage enum name; StageTone groups it: active,
	// blocked or done.
	Stage, StageTone string
	Effective        string // YYYY-MM-DD
	Approver         string
}

var journeyURLPattern = regexp.MustCompile(`https?://[^\s<>"']+|/workspace/app/journeys\?[^\s<>"']+`)
var journeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

// journeyReferenceKeys are the selectors a shared journey address may carry;
// only journey selects the target.
var journeyReferenceKeys = map[string]bool{"journey": true, "locale": true, "nav": true, "menu_q": true, "favorites": true}

// JourneyReferenceURL is the canonical in-app address of a journey.
func JourneyReferenceURL(intentID string) string {
	if !journeyIDPattern.MatchString(intentID) {
		return ""
	}
	return "/workspace/app/journeys?journey=" + url.QueryEscape(intentID)
}

// JourneyReferences recognizes canonical same-origin journey addresses.
// Foreign origins, unknown keys and malformed IDs are ignored; the ID is a
// selector only and authorization stays with the journey service.
func JourneyReferences(body, origin string) []JourneyReference {
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	base, err := url.Parse(origin)
	if err != nil || !base.IsAbs() || base.Host == "" {
		return nil
	}
	var refs []JourneyReference
	for _, span := range journeyURLPattern.FindAllStringIndex(body, 16) {
		candidate := strings.TrimRight(body[span[0]:span[1]], ".,!?;:)]}>")
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Path != "/workspace/app/journeys" {
			continue
		}
		if parsed.IsAbs() && (!strings.EqualFold(parsed.Scheme, base.Scheme) || !strings.EqualFold(parsed.Host, base.Host)) {
			continue
		}
		values, err := url.ParseQuery(parsed.RawQuery)
		if err != nil || len(values["journey"]) != 1 || values.Encode() != parsed.RawQuery {
			continue
		}
		known := true
		for key, entries := range values {
			if !journeyReferenceKeys[key] || len(entries) != 1 {
				known = false
				break
			}
		}
		if id := values.Get("journey"); known && journeyIDPattern.MatchString(id) {
			refs = append(refs, JourneyReference{IntentID: id, Start: span[0], End: span[0] + len(candidate)})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Start < refs[j].Start })
	return refs
}

func journeyPreviewReady(p JourneyPreview, id string) bool {
	return p.State == "ready" && p.Readable && p.IntentID == id && strings.TrimSpace(p.Worker) != ""
}

// journeyReferenceBody replaces journey addresses with a titled link once an
// authorized preview is ready, and neutral text otherwise; every other run
// of text continues through the project, document and channel renderers.
func journeyReferenceBody(m Model, body string) []ui.Node {
	refs := JourneyReferences(body, m.EmbedOrigin)
	if len(refs) == 0 {
		return projectTaskReferenceBody(m, body)
	}
	var nodes []ui.Node
	last := 0
	for _, ref := range refs {
		if ref.Start < last {
			continue
		}
		if ref.Start > last {
			nodes = append(nodes, projectTaskReferenceBody(m, body[last:ref.Start])...)
		}
		if preview, ok := m.JourneyPreviews[ref.IntentID]; ok && journeyPreviewReady(preview, ref.IntentID) {
			label := m.tf(KeyJourneyLinkLabel, map[string]string{"workflow": journeyWorkflowName(m, preview), "name": preview.Worker})
			nodes = append(nodes, html.A(html.Props{Class: "chat-project-task-reference", Href: JourneyReferenceURL(ref.IntentID), Data: map[string]string{"action": "open-journey-reference", "id": ref.IntentID}, Aria: map[string]string{"label": m.tf(KeyOpenJourney, map[string]string{"title": label})}}, ui.Text(label)))
		} else {
			nodes = append(nodes, html.Span(html.Props{Class: "chat-project-task-restricted"}, ui.Text(m.t(KeyJourneyRestricted))))
		}
		last = ref.End
	}
	if last < len(body) {
		nodes = append(nodes, projectTaskReferenceBody(m, body[last:])...)
	}
	return nodes
}

// resolveJourneyReferencesForSnippet keeps raw journey addresses out of
// plain-text search snippets.
func resolveJourneyReferencesForSnippet(m Model, body string) string {
	refs := JourneyReferences(body, m.EmbedOrigin)
	if len(refs) == 0 {
		return body
	}
	var out strings.Builder
	last := 0
	for _, ref := range refs {
		if ref.Start < last {
			continue
		}
		out.WriteString(body[last:ref.Start])
		label := m.t(KeyJourneyRestricted)
		if preview, ok := m.JourneyPreviews[ref.IntentID]; ok && journeyPreviewReady(preview, ref.IntentID) {
			label = m.tf(KeyJourneyLinkLabel, map[string]string{"workflow": journeyWorkflowName(m, preview), "name": preview.Worker})
		}
		out.WriteString(label)
		last = ref.End
	}
	out.WriteString(body[last:])
	return out.String()
}

func journeyWorkflowName(m Model, p JourneyPreview) string {
	if name := strings.TrimSpace(p.Workflow); name != "" {
		return name
	}
	return m.t(KeyJourneyPromotion)
}

// journeyPreviewEmbeds renders one quick-look card per distinct journey.
func journeyPreviewEmbeds(m Model, body string) []ui.Node {
	refs := JourneyReferences(body, m.EmbedOrigin)
	seen := map[string]bool{}
	var out []ui.Node
	for _, ref := range refs {
		if seen[ref.IntentID] {
			continue
		}
		seen[ref.IntentID] = true
		preview, ok := m.JourneyPreviews[ref.IntentID]
		if !ok || preview.IntentID != ref.IntentID {
			preview = JourneyPreview{IntentID: ref.IntentID, State: "loading"}
		}
		out = append(out, html.WithKey(journeyPreviewCard(m, preview), "journey-embed:"+ref.IntentID))
	}
	return out
}

func journeyPreviewCard(m Model, p JourneyPreview) ui.Node {
	head := []ui.Node{html.Span(html.Props{Class: "chat-embed-label chat-project-embed-kind"}, journeyGlyph(), ui.Text(m.tf(KeyJourneyEmbed, map[string]string{"workflow": journeyWorkflowName(m, p)})))}
	switch {
	case journeyPreviewReady(p, p.IntentID):
	case p.State == "ready" || p.State == "restricted" || p.State == "unavailable":
		return html.Div(html.Props{Class: "chat-embed chat-project-embed chat-journey-embed is-locked", Data: map[string]string{"embed-state": p.State}}, append(head, html.Span(html.Props{Class: "chat-doc-embed-locked", Text: m.t(KeyJourneyRestricted)}))...)
	default:
		return html.Div(html.Props{Class: "chat-embed chat-project-embed chat-journey-embed is-loading", Data: map[string]string{"embed-state": "loading"}, Aria: map[string]string{"busy": "true"}},
			append(head,
				html.Span(html.Props{Class: "chat-doc-skeleton chat-doc-skeleton-title", Aria: map[string]string{"hidden": "true"}}),
				html.Span(html.Props{Class: "chat-doc-skeleton chat-doc-skeleton-byline", Aria: map[string]string{"hidden": "true"}}),
				html.Span(html.Props{Class: "sr-only", Text: m.t(KeyProjectEmbedLoading)}))...)
	}
	children := append(head, html.Strong(html.Props{Class: "chat-embed-source chat-project-embed-title", Dir: "auto", Text: p.Worker}))
	if p.From != "" || p.To != "" {
		children = append(children, html.Span(html.Props{Class: "chat-journey-embed-move", Dir: "ltr"}, html.Span(html.Props{Text: p.From}), html.Span(html.Props{Class: "chat-journey-embed-arrow", Aria: map[string]string{"hidden": "true"}, Text: "→"}), html.Strong(html.Props{Text: p.To})))
	}
	meta := []ui.Node{html.Span(html.Props{Class: "chat-project-embed-status", Data: map[string]string{"tone": journeyTone(p.StageTone)}}, html.Span(html.Props{Class: "chat-project-embed-dot", Aria: map[string]string{"hidden": "true"}}), ui.Text(journeyStageLabel(m, p.Stage)))}
	if effective, err := time.Parse("2006-01-02", p.Effective); err == nil {
		meta = append(meta, html.Span(html.Props{Class: "chat-project-embed-due", Text: m.tf(KeyJourneyEffective, map[string]string{"date": formatShortDate(m.Locale, effective)})}))
	}
	if p.Approver != "" {
		meta = append(meta, html.Span(html.Props{Class: "chat-project-embed-assignee"}, ui.Text(m.tf(KeyJourneyApprover, map[string]string{"name": p.Approver}))))
	}
	children = append(children, html.Span(html.Props{Class: "chat-project-embed-meta"}, meta...))
	return html.A(html.Props{Class: "chat-embed chat-embed-link chat-project-embed chat-journey-embed", Href: JourneyReferenceURL(p.IntentID), Data: map[string]string{"action": "open-journey-reference", "id": p.IntentID, "tone": p.StageTone}, Aria: map[string]string{"label": m.tf(KeyOpenJourney, map[string]string{"title": p.Worker})}}, children...)
}

// journeyTone maps a stage group onto the shared status-pill tones.
func journeyTone(group string) string {
	switch group {
	case "done":
		return "done"
	case "blocked":
		return "todo"
	}
	return "doing"
}

var journeyStageKeys = map[string]string{
	"JOURNEY_STAGE_PROPOSED": KeyJourneyStageProposed, "JOURNEY_STAGE_BLOCKED": KeyJourneyStageBlocked, "JOURNEY_STAGE_AWAITING_APPROVAL": KeyJourneyStageAwaiting,
	"JOURNEY_STAGE_COMPLETED": KeyJourneyStageCompleted, "JOURNEY_STAGE_REJECTED": KeyJourneyStageRejected, "JOURNEY_STAGE_FAILED": KeyJourneyStageFailed,
	"JOURNEY_STAGE_FINANCE_APPROVAL": KeyJourneyStageFinance, "JOURNEY_STAGE_MANAGER_APPROVAL": KeyJourneyStageManager,
	"JOURNEY_STAGE_WAITING_EFFECTIVE_DATE": KeyJourneyStageWaiting, "JOURNEY_STAGE_REVALIDATION": KeyJourneyStageRevalidation,
}

func journeyStageLabel(m Model, stage string) string {
	if key := journeyStageKeys[stage]; key != "" {
		return m.t(key)
	}
	return m.t(KeyJourneyStageInProgress)
}

// JourneyStageGroup is the resolver's tone for a JourneyStage enum name.
func JourneyStageGroup(stage string) string {
	switch stage {
	case "JOURNEY_STAGE_COMPLETED", "JOURNEY_STAGE_REJECTED", "JOURNEY_STAGE_FAILED":
		return "done"
	case "JOURNEY_STAGE_BLOCKED":
		return "blocked"
	}
	return "active"
}

func journeyGlyph() ui.Node {
	return html.Tag("svg", html.Props{Class: "chat-project-embed-glyph", Raw: map[string]any{"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "2", "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false"}}, html.Tag("path", html.Props{Raw: map[string]any{"d": "M5 6h4v4H5zM15 14h4v4h-4zM7 10v3a2 2 0 0 0 2 2h6"}}))
}

// journeyReferenceNavigate opens a shared journey in-app on a plain click.
func journeyReferenceNavigate(m Model, intentID string, plain bool) bool {
	href := JourneyReferenceURL(intentID)
	if !plain || m.Callbacks.Navigate == nil || href == "" {
		return false
	}
	m.Callbacks.Navigate(href)
	return true
}

const journeyEmbedStyles = `.chat-journey-embed{border-inline-start-color:var(--hcm-color-warning,#b45309)}` +
	`.chat-journey-embed[data-tone="done"]{border-inline-start-color:var(--hcm-color-success,#15803d)}` +
	`.chat-journey-embed-move{display:flex;flex-wrap:wrap;align-items:baseline;gap:6px;font-size:.8125rem;color:var(--muted);font-variant-numeric:tabular-nums}` +
	`.chat-journey-embed-move strong{color:var(--ink);font-weight:600}` +
	`.chat-journey-embed-arrow{color:var(--muted)}`
