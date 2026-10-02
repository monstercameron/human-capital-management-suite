package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// PersonaActor is the trusted attribution envelope for a persona authored
// post. All fields come from the actor projection; the renderer never derives
// persona status from a name, handle, or message text.
type PersonaActor struct {
	Icon           agenticon.Value
	IconRevision   int64
	PersonaID      string
	PersonaVersion string
	AgentID        string
	InvokerHandle  string
	Trusted        bool
}

func (a *PersonaActor) valid() bool {
	return a != nil && a.Trusted && strings.TrimSpace(a.PersonaID) != "" && strings.TrimSpace(a.AgentID) != ""
}

func (a *PersonaActor) attribution() string {
	if a == nil || strings.TrimSpace(a.InvokerHandle) == "" {
		return "Acting for unavailable"
	}
	return "acting for @" + strings.TrimSpace(a.InvokerHandle)
}

// PersonaBadge renders a permanent, text-visible and screen-reader-visible
// marker. Invalid actor data is explicitly unavailable and never guessed.
func PersonaBadge(actor *PersonaActor) ui.Node {
	return PersonaBadgeLocalized("en-US", actor)
}

// AgentBadgeLabel is the compact identity marker used while a trusted agent
// invocation is still being projected separately from its authored post.
func AgentBadgeLabel(locale string) ui.Node {
	label := agentReplyFallback(locale, "chat.agent.badge", "Agent")
	return html.Span(html.Props{Class: "agent-badge agent-badge-label", Aria: map[string]string{"label": label}}, html.Strong(html.Props{Text: label}))
}

// PersonaBadgeLocalized preserves trusted attribution in the viewer's locale.
func PersonaBadgeLocalized(locale string, actor *PersonaActor) ui.Node {
	badge, unavailable, attribution := "Agent", "Agent identity unavailable", "Acting for unavailable"
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		badge, unavailable, attribution = "Agent", "Agentenidentität nicht verfügbar", "Auftraggeber nicht verfügbar"
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		badge, unavailable, attribution = "وكيل", "هوية الوكيل غير متاحة", "هوية صاحب الطلب غير متاحة"
	}
	if !actor.valid() {
		return html.Span(html.Props{Class: "agent-badge unavailable", Aria: map[string]string{"label": unavailable}, Text: unavailable})
	}
	if strings.TrimSpace(actor.InvokerHandle) != "" {
		attribution = actor.attribution()
		switch {
		case strings.HasPrefix(strings.ToLower(locale), "de"):
			attribution = "handelt für @" + strings.TrimSpace(actor.InvokerHandle)
		case strings.HasPrefix(strings.ToLower(locale), "ar"):
			attribution = "يعمل نيابة عن @" + strings.TrimSpace(actor.InvokerHandle)
		}
	}
	return html.Span(html.Props{Class: "agent-badge", Data: map[string]string{"persona-id": actor.PersonaID, "agent-id": actor.AgentID}, Aria: map[string]string{"label": badge + "; " + attribution}},
		html.Strong(html.Props{Text: badge}), html.Span(html.Props{Class: "agent-attribution", Dir: "auto", Text: attribution}))
}

func personaMessageBadge(model Model, actor *PersonaActor) ui.Node {
	if model.selected().Agent {
		return AgentBadgeLabel(model.Locale)
	}
	if !actor.valid() {
		return PersonaBadgeLocalized(model.Locale, actor)
	}
	name := strings.TrimSpace(actor.InvokerHandle)
	if name == model.CurrentUser || strings.TrimPrefix(name, "@") == model.CurrentUser || strings.HasPrefix(name, "ir-") {
		name = strings.TrimSpace(model.CurrentUserName)
	}
	badge, forWord := agentReplyFallback(model.Locale, "chat.agent.badge", "Agent"), "for"
	switch {
	case strings.HasPrefix(strings.ToLower(model.Locale), "de"):
		forWord = "für"
	case strings.HasPrefix(strings.ToLower(model.Locale), "ar"):
		forWord = "نيابةً عن"
	}
	children := []ui.Node{html.Strong(html.Props{Text: badge})}
	if name != "" {
		children = append(children, html.Span(html.Props{Class: "agent-attribution", Dir: "auto", Text: forWord + " " + name}))
	}
	return html.Span(html.Props{Class: "agent-badge", Data: map[string]string{"persona-id": actor.PersonaID, "agent-id": actor.AgentID}}, children...)
}

// PersonaBadgeStyles keeps the marker visible in light, dark, forced-colour,
// and reduced-motion modes. The host may append it with the chat stylesheet.
const PersonaBadgeStyles = `.agent-badge{display:inline-flex;align-items:center;gap:.35rem;padding:.1rem .4rem;border:1px solid currentColor;border-radius:999px;font-size:.72rem;font-weight:650;line-height:1.2;color:var(--accent);white-space:nowrap}.agent-attribution{font-weight:500;color:var(--muted)}.agent-badge.unavailable{color:var(--muted);border-style:dashed}`
