package chatui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3756: the built-in General agent can be asked from any conversation.
// "/ask" with no agent name asks it. The question is the person's own task: it
// is not posted to the conversation, the answer comes back to the person alone
// (with the way to open the task in Agents), and only an explicit "Post answer
// here" puts words in the conversation, as the person's own message that names
// where the answer came from.

const (
	issue3756PostAction    = "general-ask-post"
	issue3756RetryAction   = "general-ask-retry"
	issue3756DismissAction = "general-ask-dismiss"

	issue3756Asking   = "asking"
	issue3756Answered = "answered"
	issue3756Working  = "working"
	issue3756Failed   = "failed"

	// issue3756TaskHref opens one task on the Agents page.
	issue3756TaskHref = "/workspace/app/chat/agents?task="
)

// GeneralAgentAnswer is what the page learns when the General agent task it
// started has been accepted: the task, where it stands, and the answer when it
// is already done. Reason names a refusal the person can act on ("disabled":
// agents are off, "denied": no access); empty means the generic sentence.
type GeneralAgentAnswer struct {
	TaskID string
	State  string
	Answer string
	Reason string
}

// issue3756State is the card above the composer: one question at a time, kept
// with the conversation it was asked in.
type issue3756State struct {
	Room, Question, Phase string
	TaskID, Answer        string
	Reason                string
}

// issue3756Available says the page can reach the General agent.
func issue3756Available(m Model) bool { return m.Callbacks.AskGeneralAgent != nil }

// issue3756Start asks the General agent. Nothing is sent to the conversation.
func issue3756Start(m Model, local localStore, question string) {
	question = strings.TrimSpace(question)
	if question == "" || m.SelectedID == "" || m.Callbacks.AskGeneralAgent == nil {
		return
	}
	if local.get().generalAsk.Phase == issue3756Asking {
		local.update(func(u *localUI) { u.composerNotice = issue3756Text(m, "busy") })
		return
	}
	room := m.SelectedID
	local.update(func(u *localUI) {
		u.composerNotice = ""
		u.generalAsk = issue3756State{Room: room, Question: question, Phase: issue3756Asking}
	})
	m.Callbacks.AskGeneralAgent(question, func(answer GeneralAgentAnswer, err error) {
		local.update(func(u *localUI) {
			if u.generalAsk.Room != room || u.generalAsk.Question != question || u.generalAsk.Phase != issue3756Asking {
				return
			}
			next := issue3756State{Room: room, Question: question, TaskID: strings.TrimSpace(answer.TaskID)}
			switch {
			case err != nil || next.TaskID == "":
				next.Phase, next.Reason, next.TaskID = issue3756Failed, strings.TrimSpace(answer.Reason), ""
			case strings.TrimSpace(answer.Answer) != "":
				next.Phase, next.Answer = issue3756Answered, strings.TrimSpace(answer.Answer)
			default:
				next.Phase = issue3756Working
			}
			u.generalAsk = next
		})
	})
}

// issue3756Retry asks the same question again after a failure.
func issue3756Retry(m Model, local localStore) bool {
	st := local.get().generalAsk
	if st.Room != m.SelectedID || st.Phase != issue3756Failed || st.Question == "" {
		return false
	}
	issue3756Start(m, local, st.Question)
	return local.get().generalAsk.Phase == issue3756Asking
}

// issue3756Body is the message "Post answer here" sends: the person's own words
// framing the General agent's answer, so nobody mistakes it for the agent
// speaking in the channel.
func issue3756Body(m Model, st issue3756State) string {
	intro := strings.ReplaceAll(issue3756Text(m, "answer_intro"), "{question}", st.Question)
	return intro + "\n\n" + st.Answer
}

// issue3756Post puts the answer in the open conversation as the person's own
// message. It is the only way the answer leaves the person's screen.
func issue3756Post(m Model, local localStore) bool {
	st := local.get().generalAsk
	if st.Room != m.SelectedID || st.Phase != issue3756Answered || st.Answer == "" || m.Callbacks.SendMessage == nil {
		return false
	}
	body := issue3756Body(m, st)
	if notice := chat3715LengthNotice(m.Locale, body); notice != "" {
		local.update(func(u *localUI) { u.composerNotice = notice })
		return false
	}
	local.update(func(u *localUI) { u.generalAsk = issue3756State{} })
	m.Callbacks.SendMessage(m.SelectedID, body)
	return true
}

// issue3756Click handles the card's buttons. It reports whether the press was on one.
func issue3756Click(e ui.MouseEvent, m Model, local localStore) bool {
	action, _, _ := eventAction(e)
	switch action {
	case issue3756PostAction:
		e.PreventDefault()
		issue3756Post(m, local)
		return true
	case issue3756RetryAction:
		e.PreventDefault()
		issue3756Retry(m, local)
		return true
	case issue3756DismissAction:
		e.PreventDefault()
		local.update(func(u *localUI) { u.generalAsk = issue3756State{} })
		return true
	}
	return false
}

// issue3756Card is the General agent's card above the composer, shown only in
// the conversation the question was asked in.
func issue3756Card(m Model, local localUI) ui.Node {
	st := local.generalAsk
	if st.Phase == "" || st.Room != m.SelectedID {
		return nil
	}
	title := html.Strong(html.Props{Class: "general-ask-title", Dir: "auto", Text: issue3756Text(m, "title")})
	question := html.Span(html.Props{Class: "general-ask-question", Dir: "auto", Text: st.Question})
	button := func(action, key string) ui.Node {
		return html.Button(html.Props{Class: "general-ask-button", Type: "button", Data: map[string]string{"action": action}, Text: issue3756Text(m, key)})
	}
	open := func() ui.Node {
		return html.A(html.Props{Class: "general-ask-button general-ask-open", Href: issue3756TaskHref + url.QueryEscape(st.TaskID), Data: map[string]string{"general-ask": "open-task"}, Text: issue3756Text(m, "open")})
	}
	props := html.Props{Class: "general-ask", Role: "status", Aria: map[string]string{"live": "polite"}, Data: map[string]string{"general-ask": st.Phase}}
	var children []ui.Node
	switch st.Phase {
	case issue3756Asking:
		props.Aria = map[string]string{"live": "polite", "busy": "true"}
		children = []ui.Node{title, question, html.P(html.Props{Class: "general-ask-line", Dir: "auto", Text: issue3756Text(m, "asking")})}
	case issue3756Answered:
		children = []ui.Node{title, question,
			html.P(html.Props{Class: "general-ask-line", Dir: "auto", Text: issue3756Text(m, "answered")}),
			html.Blockquote(html.Props{Class: "general-ask-answer", Dir: "auto", Text: st.Answer}),
			html.Div(html.Props{Class: "general-ask-actions"}, button(issue3756PostAction, "post"), open(), button(issue3756DismissAction, "dismiss"))}
	case issue3756Working:
		children = []ui.Node{title, question,
			html.P(html.Props{Class: "general-ask-line", Dir: "auto", Text: issue3756Text(m, "working")}),
			html.Div(html.Props{Class: "general-ask-actions"}, open(), button(issue3756DismissAction, "dismiss"))}
	default:
		props.Role, props.Aria = "alert", map[string]string{"live": "assertive"}
		line := issue3756Text(m, "reason_"+st.Reason)
		if line == "" {
			line = issue3756Text(m, "failed")
		}
		children = []ui.Node{title, question,
			html.P(html.Props{Class: "general-ask-line", Dir: "auto", Text: line + " " + issue3756Text(m, "failed_nothing_posted")}),
			html.Div(html.Props{Class: "general-ask-actions"}, button(issue3756RetryAction, "retry"), button(issue3756DismissAction, "dismiss"))}
	}
	return html.Div(props, children...)
}

func issue3756Text(m Model, key string) string {
	copy := map[string][3]string{
		"title":    {"General agent", "Allgemeiner Agent", "الوكيل العام"},
		"asking":   {"Asking the General agent. Only you will see the answer.", "Der Allgemeine Agent wird gefragt. Nur Sie sehen die Antwort.", "جارٍ سؤال الوكيل العام. أنت فقط من سيرى الإجابة."},
		"answered": {"Only you can see this. Nothing was posted to this conversation.", "Nur Sie sehen das. In diese Unterhaltung wurde nichts gepostet.", "أنت فقط من يرى هذا. لم يُنشر أي شيء في هذه المحادثة."},
		"working": {"The General agent is still working on it. Open it in Agents to follow it. Nothing was posted to this conversation.",
			"Der Allgemeine Agent arbeitet noch daran. Öffnen Sie die Aufgabe unter Agenten, um sie zu verfolgen. In diese Unterhaltung wurde nichts gepostet.",
			"لا يزال الوكيل العام يعمل على ذلك. افتحه في الوكلاء لمتابعته. لم يُنشر أي شيء في هذه المحادثة."},
		"failed":                {"The General agent could not answer.", "Der Allgemeine Agent konnte nicht antworten.", "تعذّر على الوكيل العام الإجابة."},
		"failed_nothing_posted": {"Nothing was posted to this conversation.", "In diese Unterhaltung wurde nichts gepostet.", "لم يُنشر أي شيء في هذه المحادثة."},
		"reason_disabled":       {"Agents are turned off for your organization.", "Agenten sind für Ihre Organisation ausgeschaltet.", "الوكلاء متوقفون عن العمل في مؤسستك."},
		"reason_denied":         {"You do not have access to agents.", "Sie haben keinen Zugriff auf Agenten.", "ليس لديك صلاحية الوصول إلى الوكلاء."},
		"open":                  {"Open in Agents", "Unter Agenten öffnen", "فتح في الوكلاء"},
		"post":                  {"Post answer here", "Antwort hier posten", "نشر الإجابة هنا"},
		"retry":                 {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
		"dismiss":               {"Dismiss", "Schließen", "إغلاق"},
		"needs_question":        {"Write your question after /ask.", "Schreiben Sie Ihre Frage hinter /ask.", "اكتب سؤالك بعد /ask."},
		"busy":                  {"Still waiting for the General agent. Ask again when it has answered.", "Der Allgemeine Agent antwortet noch. Fragen Sie erneut, wenn er geantwortet hat.", "لا يزال الوكيل العام يجيب. اسأل مرة أخرى بعد أن يجيب."},
		"answer_intro":          {"General agent answered “{question}”:", "Der Allgemeine Agent hat „{question}“ beantwortet:", "أجاب الوكيل العام عن «{question}»:"},
	}
	return chatbug039Text("issue3756."+key, copy[key][chatbug039LocaleIndex(m.Locale)], copy[key][0])
}

// chatIssue3756Styles lays the card out above the composer: wrapping text, an
// answer that keeps its line breaks, and touch-sized buttons on a phone.
const chatIssue3756Styles = `.general-ask{margin:2px 6px 6px;padding:8px 10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);display:flex;flex-direction:column;gap:4px;min-width:0;font-size:.8125rem;color:var(--ink)}` +
	`.general-ask-title{font-weight:600}.general-ask-question,.general-ask-line{overflow-wrap:anywhere;min-width:0;margin:0}` +
	`.general-ask-question{color:var(--muted,inherit);font-style:italic}` +
	`.general-ask-answer{margin:0;padding-inline-start:10px;border-inline-start:3px solid var(--line);white-space:pre-wrap;overflow-wrap:anywhere;max-height:40vh;overflow:auto}` +
	`.general-ask-actions{display:flex;flex-wrap:wrap;gap:6px}` +
	`.general-ask-button{display:inline-flex;align-items:center;min-height:28px;padding:0 10px;border:1px solid var(--line);border-radius:999px;background:transparent;color:inherit;font:inherit;text-decoration:none;cursor:pointer}` +
	`@media (pointer:coarse){.general-ask-button{min-height:44px}}`
