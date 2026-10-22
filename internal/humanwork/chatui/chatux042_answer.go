package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATUX-042: an agent's answer is one component with four parts (the author
// line, the body, the sources and the actions) and two variants. Attached is an
// answer that hangs under the question that asked for it, in a channel: it is
// drawn with the accent rule. Standalone is an answer that is a message in its
// own right (a reply in a direct conversation, an announcement): the same parts
// without the rule. The card, the announcement and the agent's message in a
// conversation all take their class from here, and the stylesheet below styles
// the parts once for all of them.
const (
	chatux042Attached   = "attached"
	chatux042Standalone = "standalone"
)

// chatux042Class is the class of an answer's root: the component, its variant,
// then whatever the surface already calls the element.
func chatux042Class(variant, existing string) string {
	class := "agent-answer agent-answer-" + variant
	if existing != "" {
		class += " " + existing
	}
	return class
}

// chatux042Article is the root of an answer drawn as its own element.
func chatux042Article(variant string, props html.Props, children ...ui.Node) ui.Node {
	props.Class = chatux042Class(variant, props.Class)
	return html.Article(props, children...)
}

// chatux042RatingButton is one of the two rating buttons. At rest it is a text
// button; the chosen rating shows a check in place of its thumb and is drawn in
// the accent colour, never as a filled primary button.
func chatux042RatingButton(label, action, invocationID, rating, glyph string, chosen, disabled bool) ui.Node {
	class := "agent-feedback-button"
	if chosen {
		class += " is-chosen"
		glyph = "check"
	}
	return html.Button(html.Props{Class: class, Type: "button", Title: label, Disabled: disabled,
		Data: map[string]string{"action": action, "id": invocationID, "extra": rating},
		Aria: map[string]string{"label": label, "pressed": boolString(chosen)}}, icon(glyph), html.Span(html.Props{Text: label}))
}

// chatux042NothingFound reports whether a failure sentence is the "found
// nothing" outcome. That is an answer (the agent looked and the documents it may
// read hold nothing about the question), so its card carries no warning glyph;
// the glyph is for the failures where the agent could not answer.
func chatux042NothingFound(locale, agent string, description chat.AgentAnswerFailure) bool {
	return description.Sentence == chat.AgentAnswerFailureFor(locale, agent, "NOTHING_FOUND").Sentence
}

// ChatUX042Styles draws the parts of an answer once. Sources are chips: a tinted
// block with a document glyph, no outline. Actions are text buttons with a glyph
// and no fill at rest. The chosen rating is accent text with a check. The hover
// bar of a message holds react, reply, save and more; a saved message's bookmark
// is the accent colour and an unsaved one the neutral outline.
const ChatUX042Styles = `
.chat-workspace .agent-answer .agent-reply-sources{display:flex;align-items:center;flex-wrap:wrap;gap:6px 8px;margin-block:8px 0;padding:0;border:0}
.chat-workspace .agent-answer .agent-reply-sources h4{flex:none;margin:0;color:var(--muted);font-size:.8125rem;font-weight:600}
.chat-workspace .agent-answer .agent-reply-source{display:inline-flex;align-items:center;gap:6px;max-inline-size:100%;min-inline-size:0;padding:3px 10px 3px 8px;border:0;border-radius:6px;background:color-mix(in srgb,var(--accent) 9%,var(--surface));color:var(--ink);font-size:.8125rem;line-height:1.5}
.chat-workspace .agent-answer .agent-reply-source .chat-icon{flex:none;inline-size:14px;block-size:14px;color:var(--accent)}
.chat-workspace .agent-answer .agent-reply-source a{color:var(--ink);text-decoration:none}
.chat-workspace .agent-answer .agent-reply-source:has(a):hover{background:color-mix(in srgb,var(--accent) 16%,var(--surface))}
.chat-workspace .agent-answer .agent-reply-source:has(a):hover a{text-decoration:underline}
.chat-workspace .agent-answer .agent-reply-source-locked{background:var(--soft);color:var(--muted)}
.chat-workspace .agent-answer .agent-reply-source-locked .chat-icon{color:var(--muted)}
.chat-workspace :is(.agent-answer,.agent-reply-row) :is(.agent-feedback-button,.agent-reply-action){display:inline-flex;align-items:center;gap:6px;min-block-size:32px;padding-inline:8px;border:0;border-radius:6px;background:transparent;color:var(--muted);font:inherit;font-size:.8125rem;font-weight:500;cursor:pointer}
.chat-workspace :is(.agent-answer,.agent-reply-row) :is(.agent-feedback-button,.agent-reply-action):hover,.chat-workspace :is(.agent-answer,.agent-reply-row) :is(.agent-feedback-button,.agent-reply-action):focus-visible{border:0;background:color-mix(in srgb,var(--ink) 6%,transparent);color:var(--ink)}
.chat-workspace :is(.agent-answer,.agent-reply-row) .agent-feedback-button.is-chosen,.chat-workspace :is(.agent-answer,.agent-reply-row) .agent-feedback-button.is-chosen:hover{background:transparent;color:var(--accent);font-weight:600}
.chat-workspace :is(.agent-answer,.agent-reply-row) .agent-feedback{display:flex;align-items:center;flex-wrap:wrap;gap:2px;margin:0}
.chat-workspace :is(.agent-answer,.agent-reply-row) .agent-reply-actions{margin-block-start:6px;gap:2px 4px}
.chat-workspace .agent-answer-standalone>.agent-feedback,.chat-workspace .agent-answer-standalone .message-content>.agent-feedback{margin-block-start:6px}
.chat-workspace .agent-failure-actions :is(.agent-ask-again,.agent-reply-dismiss,.agent-access-request){display:inline-flex;align-items:center;gap:6px;min-block-size:32px;padding-inline:8px;border:0;border-radius:6px;background:transparent;color:var(--muted);font:inherit;font-size:.8125rem;font-weight:500;cursor:pointer}
.chat-workspace .agent-failure-actions :is(.agent-ask-again,.agent-reply-dismiss,.agent-access-request):hover,.chat-workspace .agent-failure-actions :is(.agent-ask-again,.agent-reply-dismiss,.agent-access-request):focus-visible{border:0;background:color-mix(in srgb,var(--ink) 6%,transparent);color:var(--ink)}
.chat-workspace .agent-failure-actions .agent-ask-again{color:var(--ink);font-weight:600}
.chat-workspace .agent-failure-heading[data-outcome="nothing-found"]{color:var(--ink)}
.chat-workspace .persona-progress-failure:has(.agent-failure-heading[data-outcome="nothing-found"]){border-inline-start-color:var(--accent)}
.chat-workspace .message-actions .message-actions-divider{flex:none;align-self:stretch;inline-size:1px;margin-block:4px;margin-inline:2px;background:var(--line)}
@container chatmain (max-width:560px){.chat-workspace .message-actions .message-actions-divider{display:none}}
.message-menu .menu-phone-only{display:none}
@media(max-width:767px),(pointer:coarse){.message-menu .menu-phone-only{display:flex}}
.chat-workspace .message-action.chatsave-action{color:var(--muted)}
.chat-workspace .message-action.chatsave-action .chatsave-bookmark{fill:none}
.chat-workspace .message-action.chatsave-action[aria-pressed="true"],.chat-workspace .menu-item.chatsave-action[aria-pressed="true"] .chatsave-bookmark{color:var(--accent)}
.chat-workspace .message-action.chatsave-action[aria-pressed="true"] .chatsave-bookmark,.chat-workspace .menu-item.chatsave-action[aria-pressed="true"] .chatsave-bookmark{fill:currentColor}
`
