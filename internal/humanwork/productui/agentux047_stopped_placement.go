package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// personaAdminStoppedPlacement is what a placement row says when the server
// stopped the agent in that conversation: the reason in plain words, and
// "Start again" once the cause is cured. Without it a stopped agent simply
// vanished from the conversation and nothing on the page said why.
func personaAdminStoppedPlacement(locale LocaleContext, persona PersonaAdminPersona, installation PersonaAdminInstallation, snapshot PersonaAdminSnapshot) ui.Node {
	if !installation.Stopped {
		return nil
	}
	sentence := strings.ReplaceAll(agentUX047Text(locale, "stopped"), "{reason}", agentUX047Text(locale, "reason_"+personaAdminStoppedReasonKey(installation.StoppedReason)))
	children := []ui.Node{html.P(html.Props{Class: "persona-admin-placement-warning", Role: "status", Raw: map[string]any{"data-placement-stopped": personaAdminStoppedReasonKey(installation.StoppedReason)}}, ui.Text(sentence))}
	switch {
	case installation.StartAgain && personaAdminCommandAllowed(snapshot, "REINSTALL"):
		// Starting again replaces the stopped placement with a fresh one for
		// the current published version, through the same command as adding.
		children = append(children, html.Form(html.Props{Class: "persona-admin-placement-start", Raw: map[string]any{"data-persona-admin-command-form": "REINSTALL"}},
			html.Input(html.Props{Name: "persona_id", Type: "hidden", Value: persona.ID}),
			html.Input(html.Props{Name: "conversation_id", Type: "hidden", Value: installation.ConversationID}),
			html.Button(html.Props{Class: "button secondary", Type: "submit"}, ui.Text(agentUX047Text(locale, "start_again"))),
		))
	case installation.StartAgain:
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(agentUX047Text(locale, "start_again_not_allowed"))))
	default:
		children = append(children, html.P(html.Props{Class: "muted"}, ui.Text(agentUX047Text(locale, "start_again_later"))))
	}
	return html.Div(html.Props{Class: "persona-admin-placement-stopped"}, children...)
}

// personaAdminStoppedReasonKey keeps the reason inside the set the page has a
// sentence for; anything else reads as "needs attention".
func personaAdminStoppedReasonKey(code string) string {
	switch code {
	case PersonaPlacementStoppedNoIdentity, PersonaPlacementStoppedIdentityInactive, PersonaPlacementStoppedNotPublished, PersonaPlacementStoppedVersionMissing:
		return code
	}
	return PersonaPlacementStoppedOther
}

func agentUX047Text(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"stopped":                 {"Stopped: {reason}.", "Angehalten: {reason}.", "متوقف: {reason}."},
		"stopped_version":         {"version {version}", "Version {version}", "الإصدار {version}"},
		"start_again":             {"Start again", "Erneut starten", "بدء التشغيل من جديد"},
		"start_again_later":       {"It can be started again once this is fixed.", "Er kann erneut gestartet werden, sobald dies behoben ist.", "يمكن بدء تشغيله من جديد بعد إصلاح ذلك."},
		"start_again_not_allowed": {"Someone who may add this agent to conversations can start it again.", "Eine Person, die diesen Agenten zu Unterhaltungen hinzufügen darf, kann ihn erneut starten.", "يمكن لمن يُسمح له بإضافة هذا الوكيل إلى المحادثات أن يبدأ تشغيله من جديد."},
		"reason_" + PersonaPlacementStoppedNoIdentity:       {"this version is not set up to run", "diese Version ist nicht für die Ausführung eingerichtet", "هذا الإصدار غير مُعدّ للتشغيل"},
		"reason_" + PersonaPlacementStoppedIdentityInactive: {"this version's setup to run is no longer active", "die Einrichtung dieser Version für die Ausführung ist nicht mehr aktiv", "إعداد تشغيل هذا الإصدار لم يعد نشطًا"},
		"reason_" + PersonaPlacementStoppedNotPublished:     {"this version is no longer published", "diese Version ist nicht mehr veröffentlicht", "هذا الإصدار لم يعد منشورًا"},
		"reason_" + PersonaPlacementStoppedVersionMissing:   {"this version is no longer available", "diese Version ist nicht mehr verfügbar", "هذا الإصدار لم يعد متاحًا"},
		"reason_" + PersonaPlacementStoppedOther:            {"it needs attention from the person who manages this agent", "die für diesen Agenten zuständige Person muss sich darum kümmern", "يحتاج إلى متابعة من الشخص الذي يدير هذا الوكيل"},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	index := 0
	switch locale.Resolved {
	case "de-DE":
		index = 1
	case "ar":
		index = 2
	}
	return values[index]
}
