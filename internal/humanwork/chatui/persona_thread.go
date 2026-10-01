package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonaThreadInvocation binds private status to its original post.
type PersonaThreadInvocation struct {
	PostID     string
	Projection PersonaProgressProjection
}

// PersonaPostActor comes only from a persisted, authorized reply receipt.
type PersonaPostActor struct {
	Actor   PersonaActor
	Display string
}

func personaTrustedMessage(model Model, message Message) Message {
	if receipt, ok := model.PersonaPostActors[message.ID]; ok && receipt.Actor.valid() {
		actor := receipt.Actor
		message.PersonaActor = &actor
		message.Author = receipt.Display
	}
	return message
}

func personaThreadProgress(model Model) ui.Node {
	children := []ui.Node{}
	for _, invocation := range model.PersonaInvocations {
		if invocation.PostID == model.ThreadParentID {
			children = append(children, html.WithKey(RenderPersonaProgress(model, invocation.Projection), invocation.Projection.InvocationID))
		}
	}
	return html.Div(html.Props{Class: "persona-thread-invocations"}, children...)
}

// PersonaProfileCard reuses the authorized profile wherever a persona appears.
func PersonaProfileCard(locale string, persona ResolvedPersonaMention) ui.Node {
	return personaProfileCard(locale, persona)
}

func personaPostProfiles(model Model, message Message) ui.Node {
	children := []ui.Node{}
	for _, persona := range model.ResolvedPersonaMentions {
		matched := message.PersonaActor != nil && message.PersonaActor.valid() && message.PersonaActor.PersonaVersion != "" && message.PersonaActor.PersonaVersion == persona.Version && (message.PersonaActor.PersonaID == persona.Reference.ID || message.PersonaActor.AgentID == persona.Reference.ID)
		for _, reference := range message.PersonaReferences {
			matched = matched || reference.Kind == "AGENT_MENTION" && reference.ID == persona.Reference.ID && reference.TenantID == persona.Reference.TenantID
		}
		if matched {
			children = append(children, html.Details(html.Props{Class: "persona-profile-post"}, html.Summary(html.Props{Text: persona.Reference.Display + " · " + personaMentionText(model.Locale, "profile")}), personaProfileCard(model.Locale, persona)))
		}
	}
	if len(children) == 0 && message.PersonaActor.valid() {
		children = append(children, html.Details(html.Props{Class: "persona-profile-post"}, html.Summary(html.Props{Text: message.Author + " · " + personaMentionText(model.Locale, "profile")}), html.Article(html.Props{Class: "mention-profile-card"}, html.P(html.Props{Text: personaMentionText(model.Locale, "version") + ": " + personaProfileFact(message.PersonaActor.PersonaVersion, model.Locale)}), html.P(html.Props{Text: personaActivityLabel(model.Locale, "Published profile is not currently available with your access.")}))))
	}
	return html.Div(html.Props{Class: "persona-post-profiles"}, children...)
}
