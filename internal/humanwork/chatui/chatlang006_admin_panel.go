package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type translationAdminProps struct {
	Locale, Conversation string
	Text                 func(string) string
}

// translationAdminPanel loads the administration for the conversation and
// binds the forms to the server. Every change answers the whole view, which
// replaces what is shown: the page never guesses what the server kept.
func translationAdminPanel(props translationAdminProps) ui.Node {
	state := ui.UseState(TranslationAdminModel{Locale: props.Locale, Conversation: props.Conversation})
	apply := func(data TranslationAdminData, status int, err error, saved bool) {
		current := state.Get()
		current.Loading = false
		switch {
		case err == nil:
			current.Data, current.Loaded, current.Failed, current.Denied, current.Saved = data, true, false, false, saved
		case status == 403:
			current.Denied, current.Loaded, current.Failed = true, true, false
		default:
			current.Failed, current.Saved = true, false
		}
		state.Set(current)
	}
	ui.UseEffectOf(func() func() {
		return translationAdminCall("settings", props.Conversation, nil, func(data TranslationAdminData, status int, err error) { apply(data, status, err, false) })
	}, struct{ Room string }{props.Conversation})
	send := func(action string, body any) {
		current := state.Get()
		current.Loading, current.Failed, current.Saved = true, false, false
		state.Set(current)
		translationAdminCall(action, props.Conversation, body, func(data TranslationAdminData, status int, err error) { apply(data, status, err, true) })
	}
	saveWorkspace := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		body, err := translationAdminReadWorkspace(event, state.Get().Data)
		if err != nil {
			apply(TranslationAdminData{}, 0, err, false)
			return
		}
		send("workspace", body)
	})
	saveChannel := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		body, err := translationAdminReadChannel(event, props.Conversation)
		if err != nil {
			apply(TranslationAdminData{}, 0, err, false)
			return
		}
		send("channel", body)
	})
	addTerm := ui.UseEvent(func(event ui.Event) {
		event.PreventDefault()
		body, err := translationAdminReadTerm(event)
		if err != nil {
			apply(TranslationAdminData{}, 0, err, false)
			return
		}
		send("glossary/add", body)
	})
	removeTerm := ui.UseEvent(func(event ui.Event) {
		id := translationAdminReadTermID(event)
		if id == "" {
			return
		}
		send("glossary/remove", map[string]string{"id": id})
	})
	model := state.Get()
	model.SaveWorkspace, model.SaveChannel, model.AddTerm, model.RemoveTerm = saveWorkspace, saveChannel, addTerm, removeTerm
	return TranslationAdminForm(model, props.Text)
}
