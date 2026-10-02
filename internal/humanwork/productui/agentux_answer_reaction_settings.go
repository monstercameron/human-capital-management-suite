package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type AgentAnswerReactionSettingsProps struct {
	Locale                  string
	Workspace, Conversation *bool
	Configured, Editable    bool
	Save                    func(bool, func(error))
}

func agentAnswerReactionsEnabled(workspace, conversation *bool) bool {
	if conversation != nil {
		return *conversation
	}
	return workspace == nil || *workspace
}

func AgentAnswerReactionSettings(props AgentAnswerReactionSettingsProps) ui.Node {
	initial := agentAnswerReactionsEnabled(props.Workspace, props.Conversation)
	value := ui.UseState(initial)
	persisted := ui.UseState(initial)
	busy := ui.UseState(false)
	failed := ui.UseState(false)
	saved := ui.UseState(false)
	copy := agentAnswerReactionSettingsCopy(props.Locale)
	editable := props.Configured && props.Editable && props.Save != nil && !busy.Get()
	submit := ui.UseEvent(func(event ui.FormEvent) {
		event.PreventDefault()
		if !editable {
			return
		}
		busy.Set(true)
		failed.Set(false)
		saved.Set(false)
		submitted := value.Get()
		props.Save(submitted, func(err error) {
			ui.PostAsync(func() {
				busy.Set(false)
				failed.Set(err != nil)
				saved.Set(err == nil)
				if err != nil {
					value.Set(persisted.Get())
				} else {
					persisted.Set(submitted)
				}
			})
		})
	})
	var status ui.Node = html.Span(html.Props{Role: "status", ID: "chat-agent-reactions-status", Aria: map[string]string{"live": "polite"}})
	if failed.Get() {
		status = html.P(html.Props{Role: "alert", ID: "chat-agent-reactions-status", Text: copy[3]})
	} else if saved.Get() {
		status = html.P(html.Props{Role: "status", ID: "chat-agent-reactions-status", Text: copy[4]})
	}
	return html.Section(html.Props{Class: "chat-retention-settings", Aria: map[string]string{"label": copy[0]}},
		html.H2(html.Props{Text: copy[0]}),
		html.Form(html.Props{OnSubmit: submit},
			html.Button(html.Props{Type: "button", Role: "switch", Disabled: !editable, Text: copy[1], Aria: map[string]string{"checked": strconv.FormatBool(value.Get()), "describedby": "chat-agent-reactions-status"}, OnClick: ui.UseEvent(func() { value.Set(!value.Get()); saved.Set(false); failed.Set(false) })}),
			html.Button(html.Props{Type: "submit", Class: "button primary", Disabled: !editable, Text: copy[2]}), status))
}

func agentAnswerReactionSettingsCopy(locale string) [5]string {
	if strings.HasPrefix(locale, "de") {
		return [5]string{"Agenten reagieren auf Nachrichten, die sie beantworten", "Agentenreaktionen erlauben", "Einstellung speichern", "Ihre Einstellung wurde nicht gespeichert. Versuchen Sie es erneut.", "Ihre Einstellung wurde gespeichert."}
	}
	if strings.HasPrefix(locale, "ar") {
		return [5]string{"يتفاعل الوكلاء مع الرسائل التي يجيبون عنها", "اسمح بتفاعلات الوكلاء", "احفظ الإعداد", "لم يُحفظ إعدادك. حاول مرة أخرى.", "تم حفظ إعدادك."}
	}
	return [5]string{"Agents react to messages they answer", "Allow agent reactions", "Save setting", "Your setting was not saved. Try again.", "Your setting was saved."}
}
