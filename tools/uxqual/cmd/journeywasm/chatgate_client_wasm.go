//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func chatgateFetch(cfg journeyclient.Config, r chatgateClientRequest) (chatgateClientReply, int) {
	if !js.Global().Get("fetch").Truthy() || !js.Global().Get("AbortController").Truthy() {
		return chatgateClientReply{Error: "unavailable"}, 503
	}
	path := "/api/chat/gates"
	headers := js.Global().Get("Object").New()
	headers.Set(journeyclient.AuthorizationHeader, journeyclient.BearerScheme+cfg.Bearer)
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Chat-Tenant", cfg.Tenant)
	headers.Set("X-Chat-Conversation", r.Conversation)
	options := js.Global().Get("Object").New()
	options.Set("headers", headers)
	options.Set("cache", "no-store")
	options.Set("credentials", "same-origin")
	controller := js.Global().Get("AbortController").New()
	options.Set("signal", controller.Get("signal"))
	timer := time.AfterFunc(20*time.Second, func() { controller.Call("abort") })
	defer timer.Stop()
	if r.Action == chatgateListAction {
		options.Set("method", "GET")
		path += "?list=1"
	} else if r.Action == "get" {
		options.Set("method", "GET")
		path += "?conversation=" + url.QueryEscape(r.Conversation) + "&locale=" + url.QueryEscape(r.Locale)
	} else {
		options.Set("method", "POST")
		b, e := json.Marshal(r)
		if e != nil {
			return chatgateClientReply{Error: "invalid_argument"}, 400
		}
		options.Set("body", string(b))
	}
	response := awaitChatJS(js.Global().Call("fetch", path, options))
	if !response.Truthy() {
		return chatgateClientReply{Error: "unavailable"}, 503
	}
	status := response.Get("status").Int()
	body := awaitChatJS(response.Call("text"))
	var reply chatgateClientReply
	if !body.Truthy() || json.Unmarshal([]byte(body.String()), &reply) != nil {
		return chatgateClientReply{Error: "unavailable"}, status
	}
	return reply, status
}

// openChatgateForJoin is called before the ordinary join command. Failed gate
// reads never fall through to membership. An unmounted gate API remains ungated.
func openChatgateForJoin(cfg journeyclient.Config, id string) bool {
	model := chatBrowser.snapshot()
	if features := model.ChatFeatures; features != nil && !features.Gates {
		return false
	}
	if chatgateKnownUngated(model.GateJoins, id) {
		// The list of gates was read and this channel is not in it.
		return false
	}
	active := chatBrowser.config(cfg)
	reply, status := chatgateFetch(active, chatgateClientRequest{Action: "get", Conversation: id, Locale: chatBrowser.snapshot().Locale})
	if status == 404 {
		return false
	}
	if reply.View != nil && reply.View.Gate.Current == "" {
		return false
	}
	if reply.View != nil && (reply.View.Gate.State == "paused" || reply.View.Gate.State == "retired") {
		return false
	}
	if reply.View == nil {
		reply.View = &chatui.GateView{Conversation: id, Locale: chatBrowser.snapshot().Locale, Error: "unavailable"}
	}
	mountChatgate(active, *reply.View)
	return true
}

// chatgateRefreshJoins reads the gates the person may know of and keeps them
// on the model: the Browse list says what joining each one takes, and a member
// who must answer again is told above the conversation (CHATGATE-005). A read
// that fails keeps what the page had; with nothing read the page offers a
// plain Join and asks about the gate when it is pressed.
func chatgateRefreshJoins(cfg journeyclient.Config) {
	active := chatBrowser.config(cfg)
	if active.Bearer == "" {
		return
	}
	reply, status := chatgateFetch(active, chatgateClientRequest{Action: chatgateListAction})
	if status != 200 {
		return
	}
	joins, ok := chatgateJoins(reply.Result)
	if !ok {
		return
	}
	current := chatBrowser.config(cfg)
	if current.Tenant != active.Tenant || current.Subject != active.Subject {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) { m.GateJoins = joins })
	chatStreamRender.Schedule()
}
func init() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	hook := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || !target.Get("closest").Truthy() {
			return nil
		}
		if target.Call("closest", `[data-action="open-browse"]`).Truthy() {
			// Browse is opening: its rows say what each gate takes.
			go chatgateRefreshJoins(journeyclient.Config{})
			return nil
		}
		link := target.Call("closest", "[data-gate-open]")
		if !link.Truthy() {
			return nil
		}
		args[0].Call("preventDefault")
		id := link.Call("getAttribute", "data-gate-open").String()
		go func() {
			cfg := chatBrowser.config(journeyclient.Config{})
			reply, _ := chatgateFetch(cfg, chatgateClientRequest{Conversation: id, Action: "get", Locale: chatBrowser.snapshot().Locale})
			if reply.View == nil {
				reply.View = &chatui.GateView{Conversation: id, Locale: chatBrowser.snapshot().Locale, Error: "unavailable"}
			}
			mountChatgate(cfg, *reply.View)
		}()
		return nil
	})
	doc.Call("addEventListener", "click", hook)
}
func mountChatgate(cfg journeyclient.Config, view chatui.GateView) {
	doc := js.Global().Get("document")
	if doc.Call("querySelector", ".chatgate-overlay").Truthy() {
		return
	}
	previous := doc.Get("activeElement")
	overlay := doc.Call("createElement", "div")
	overlay.Set("className", "chatgate-overlay")
	overlay.Call("setAttribute", "role", "dialog")
	overlay.Call("setAttribute", "aria-modal", "true")
	overlay.Call("setAttribute", "aria-label", chatui.GateText(view.Locale, "gate"))
	doc.Get("body").Call("appendChild", overlay)
	closed, busy := false, false
	var click, key, input, submit js.Func
	close := func() {
		if closed {
			return
		}
		closed = true
		overlay.Call("removeEventListener", "click", click)
		overlay.Call("removeEventListener", "keydown", key)
		overlay.Call("removeEventListener", "input", input)
		overlay.Call("removeEventListener", "submit", submit)
		overlay.Call("remove")
		click.Release()
		key.Release()
		input.Release()
		submit.Release()
		if previous.Truthy() {
			previous.Call("focus")
		}
	}
	render := func() {
		if closed {
			return
		}
		active := chatBrowser.config(cfg)
		if active.Tenant != cfg.Tenant || active.Subject != cfg.Subject {
			close()
			return
		}
		markup, e := ui.RenderToString(chatui.RenderGate(view))
		if e != nil {
			return
		}
		overlay.Set("innerHTML", markup)
		chatgatePopulate(overlay, view)
		focus := overlay.Call("querySelector", "button,input,select,textarea")
		if invalid := overlay.Call("querySelector", "[aria-invalid=true]"); invalid.Truthy() {
			focus = invalid
		}
		if focus.Truthy() {
			focus.Call("focus")
		}
	}
	click = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || closed {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || !target.Get("closest").Truthy() {
			return nil
		}
		button := target.Call("closest", "[data-gate-action]")
		if !button.Truthy() {
			return nil
		}
		args[0].Call("preventDefault")
		action := button.Call("getAttribute", "data-gate-action").String()
		id := button.Call("getAttribute", "data-id").String()
		if action == "close" {
			close()
			return nil
		}
		if busy {
			return nil
		}
		values, checks := chatgateEditorDOM(overlay)
		answers := chatgateAnswerDOM(overlay)
		view.Answers, _ = chatgateValues(chatgateViewDefinition(view), answers)
		d := chatgateViewDefinition(view)
		if view.Administrator {
			edited, e := chatgateEditedDefinition(d, values, checks)
			if e == nil {
				d = edited
			}
		}
		if strings.HasPrefix(action, "question-") {
			index, _ := strconv.Atoi(id)
			updated, e := chatui.EditGateQuestion(d, action, index)
			if e == nil {
				view.Gate.Draft = &updated
				render()
			}
			return nil
		}
		if action == "withdraw" {
			view.ConfirmWithdrawal = true
			render()
			return nil
		}
		if action == "confirm-withdraw" {
			action = "withdraw"
		}
		if action == "retry" {
			action = "get"
		}
		if action == "submit" && !view.Administrator {
			// CHATGATE-005: what the form can check itself is checked before the
			// answers are sent. Each wrong question says why under itself, and
			// focus goes to the first of them.
			if problems := chatgateSubmitProblems(view.Locale, d, answers); len(problems) > 0 {
				view.FieldErrors = problems
				view.Notice = chatui.GateText(view.Locale, "fixAnswers")
				render()
				return nil
			}
			view.FieldErrors = nil
		}
		req := chatgateClientRequest{Conversation: view.Conversation, Locale: view.Locale, Action: action, Key: js.Global().Get("crypto").Call("randomUUID").String(), ExpectedRevision: view.Gate.Revision, Version: view.Gate.Current, Definition: d, SubmissionID: id}
		req.Answers, _ = chatgateValues(d, answers)
		if action == "bulk-admit" || action == "bulk-decline" {
			selected := overlay.Call("querySelectorAll", "[data-gate-select]:checked")
			for i := 0; i < selected.Length(); i++ {
				selectedID := selected.Index(i).Call("getAttribute", "data-gate-select").String()
				for _, sub := range view.Submissions {
					if sub.ID == selectedID {
						req.Reviews = append(req.Reviews, chatgate.ReviewDecision{SubmissionID: sub.ID, Revision: sub.Revision, Admit: action == "bulk-admit"})
					}
				}
			}
			req.Reason = values["gate-bulk-reason"]
		}
		if view.Submission != nil && action == "withdraw" {
			req.SubmissionID = view.Submission.ID
			req.SubmissionRevision = view.Submission.Revision
		}
		if action == "publish" {
			req.Version = values["gate-version"]
		}
		if action == "try" {
			req.Person = values["gate-sample-person"]
		}
		for _, sub := range view.Submissions {
			if sub.ID == id {
				req.SubmissionRevision = sub.Revision
				req.Reason = values["gate-reason-"+id]
			}
		}
		busy = true
		buttons := overlay.Call("querySelectorAll", "button")
		for i := 0; i < buttons.Length(); i++ {
			buttons.Index(i).Set("disabled", true)
		}
		go func() {
			reply, status := chatgateFetch(cfg, req)
			busy = false
			if closed {
				return
			}
			if action == "export" && status < 400 {
				var csv string
				if json.Unmarshal(reply.Result, &csv) == nil {
					blob := js.Global().Get("Blob").New([]any{csv}, map[string]any{"type": "text/csv;charset=utf-8"})
					href := js.Global().Get("URL").Call("createObjectURL", blob)
					link := doc.Call("createElement", "a")
					link.Set("href", href)
					link.Set("download", "gate-answers.csv")
					link.Call("click")
					js.Global().Get("URL").Call("revokeObjectURL", href)
				}
				render()
				return
			}
			if status >= 400 || reply.View == nil {
				if action == "try" && status < 400 {
					var outcome string
					_ = json.Unmarshal(reply.Result, &outcome)
					label := map[string]string{"admitted": "joined", "declined": "declined", "review": "waiting"}[outcome]
					view.Notice = chatui.GateText(view.Locale, "preview") + ": " + chatui.GateText(view.Locale, label)
				} else {
					view.Notice = chatui.GateText(view.Locale, "next")
					view.FieldErrors = map[string]string{}
					for field, code := range reply.Fields {
						key := "invalid"
						if code == "answers_required" {
							key = "requiredError"
						}
						view.FieldErrors[field] = chatui.GateText(view.Locale, key)
					}
					for _, f := range d.Fields {
						if f.Required && len(answers[f.ID]) == 0 {
							view.FieldErrors[f.ID] = chatui.GateText(view.Locale, "requiredError")
						}
					}
				}
			} else {
				view = *reply.View
				if action == "save" {
					view.Notice = chatui.GateText(view.Locale, "saved")
				}
				if view.Submission != nil && view.Submission.Status == "admitted" && action == "submit" {
					chatBrowser.mutate(func(m *chatui.Model) {
						m.JoinPromptID = ""
						m.JoinPromptPending = false
						m.PreviewConversation = nil
						for i := range m.Conversations {
							if m.Conversations[i].ID == view.Conversation {
								m.Conversations[i].Joined = true
							}
						}
					})
					invalidateChatRecipientProjection()
					pushChatHistoryForSelection(view.Conversation)
					openChatConversation(cfg, view.Conversation)
				}
				if action != "get" && action != "save" {
					// A gate that was published, paused or answered changes what
					// Browse and the banner say.
					go chatgateRefreshJoins(cfg)
				}
			}
			render()
		}()
		return nil
	})
	key = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		e := args[0]
		if e.Get("key").String() == "Escape" {
			close()
			return nil
		}
		if e.Get("key").String() == "Tab" {
			all := overlay.Call("querySelectorAll", "button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href]")
			if all.Length() > 0 {
				first, last := all.Index(0), all.Index(all.Length()-1)
				active := doc.Get("activeElement")
				if e.Get("shiftKey").Bool() && active.Equal(first) {
					e.Call("preventDefault")
					last.Call("focus")
				} else if !e.Get("shiftKey").Bool() && active.Equal(last) {
					e.Call("preventDefault")
					first.Call("focus")
				}
			}
		}
		return nil
	})
	overlay.Call("addEventListener", "click", click)
	overlay.Call("addEventListener", "keydown", key)
	input = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if target.Get("id").String() == "gate-filter" {
			query := strings.ToLower(target.Get("value").String())
			rows := overlay.Call("querySelectorAll", "[data-gate-answer-row]")
			n := 0
			for i := 0; i < rows.Length(); i++ {
				row := rows.Index(i)
				show := strings.Contains(strings.ToLower(row.Get("textContent").String()), query)
				row.Set("hidden", !show)
				if show {
					n++
				}
			}
			count := overlay.Call("querySelector", "#gate-answer-count")
			if count.Truthy() {
				count.Set("textContent", fmt.Sprintf(chatui.GateText(view.Locale, "count"), n))
			}
		} else if view.Administrator {
			values, checks := chatgateEditorDOM(overlay)
			// The draft is read whether or not its rule is finished: the sentence
			// under the rule follows every keystroke, and choosing the rule's
			// subject or a question's kind redraws the builder for it.
			d, ok := chatgateDraftDefinition(chatgateViewDefinition(view), values, checks)
			if ok {
				preview := overlay.Call("querySelector", "#gate-rule-preview")
				if preview.Truthy() {
					preview.Set("textContent", chatui.GateRulePreview(view.Locale, d))
				}
				if id := target.Get("id").String(); strings.HasSuffix(id, "-kind") || id == "gate-rule-field" || id == "gate-mode" {
					view.Gate.Draft = &d
					render()
					if again := overlay.Call("querySelector", "[id=\""+id+"\"]"); again.Truthy() {
						again.Call("focus")
					}
				}
			}
		} else if field := target.Call("getAttribute", "data-gate-field"); !field.IsNull() && len(view.FieldErrors) > 0 {
			// A question that was marked wrong is checked again as it is answered,
			// in place: the form is not redrawn under the person's hands.
			id := field.String()
			problems := chatgateSubmitProblems(view.Locale, chatgateViewDefinition(view), chatgateAnswerDOM(overlay))
			text := problems[id]
			if text == "" {
				delete(view.FieldErrors, id)
			} else {
				view.FieldErrors[id] = text
			}
			if line := overlay.Call("querySelector", "[id=\"gate-field-"+id+"-error\"]"); line.Truthy() {
				line.Set("textContent", text)
			}
			inputs := overlay.Call("querySelectorAll", "[data-gate-field=\""+id+"\"]")
			for i := 0; i < inputs.Length(); i++ {
				inputs.Index(i).Call("setAttribute", "aria-invalid", strconv.FormatBool(text != ""))
			}
		}
		return nil
	})
	overlay.Call("addEventListener", "input", input)
	submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			args[0].Call("preventDefault")
		}
		button := overlay.Call("querySelector", "[data-gate-action=submit]")
		if button.Truthy() {
			button.Call("click")
		}
		return nil
	})
	overlay.Call("addEventListener", "submit", submit)
	render()
}
func chatgateEditorDOM(root js.Value) (map[string]string, map[string]bool) {
	values := map[string]string{}
	checks := map[string]bool{}
	inputs := root.Call("querySelectorAll", "input[id],textarea[id],select[id]")
	for i := 0; i < inputs.Length(); i++ {
		x := inputs.Index(i)
		id := x.Get("id").String()
		values[id] = x.Get("value").String()
		if x.Get("tagName").String() == "SELECT" && x.Get("multiple").Bool() {
			selected := x.Get("selectedOptions")
			list := []string{}
			for n := 0; n < selected.Length(); n++ {
				list = append(list, selected.Index(n).Get("value").String())
			}
			b, _ := json.Marshal(list)
			values[id] = string(b)
		}
		if x.Get("type").String() == "checkbox" {
			checks[id] = x.Get("checked").Bool()
		}
	}
	return values, checks
}
func chatgateAnswerDOM(root js.Value) map[string][]string {
	values := map[string][]string{}
	inputs := root.Call("querySelectorAll", "[data-gate-field]")
	for i := 0; i < inputs.Length(); i++ {
		x := inputs.Index(i)
		id := x.Call("getAttribute", "data-gate-field").String()
		kind := x.Call("getAttribute", "data-kind").String()
		if kind == "multiple_choice" {
			if _, ok := values[id]; !ok {
				values[id] = []string{}
			}
			if x.Get("checked").Bool() {
				values[id] = append(values[id], x.Get("value").String())
			}
		} else if kind == "boolean" || kind == "acknowledgement" {
			values[id] = []string{strconv.FormatBool(x.Get("checked").Bool())}
		} else {
			values[id] = []string{x.Get("value").String()}
		}
	}
	return values
}
func chatgatePopulate(root js.Value, v chatui.GateView) {
	set := func(id, value string) {
		x := root.Call("querySelector", "[id=\""+id+"\"]")
		if x.Truthy() {
			x.Set("value", value)
		}
	}
	d := chatgateViewDefinition(v)
	set("gate-purpose", d.Purpose)
	set("gate-mode", d.Mode)
	set("gate-version", chatgateSuggestedVersion(v))
	if !d.AnswerBy.IsZero() {
		set("gate-answer-by", d.AnswerBy.Format("2006-01-02"))
	}
	for i, f := range d.Fields {
		p := "gate-editor-" + strconv.Itoa(i) + "-"
		set(p+"label", f.Label)
		set(p+"help", f.Help)
		set(p+"purpose", f.Purpose)
		set(p+"days", strconv.Itoa(f.RetentionDays))
		set(p+"kind", f.Kind)
		set(p+"class", f.DataClass)
		set(p+"options", strings.Join(f.Options, "\n"))
	}
	for _, f := range d.Fields {
		var value string
		if json.Unmarshal(v.Answers[f.ID], &value) == nil {
			set("gate-field-"+f.ID, value)
		}
	}
	if len(d.Rules) > 0 {
		// The subject, the ticked answers and the others typed are drawn with the
		// builder (chatui.gateRuleEditor); the reason is a text box's value.
		rule := chatui.GateRuleOf(d)
		set("gate-rule-field", rule.Subject)
		set("gate-rule-else", rule.Else)
		set("gate-rule-reason", rule.Reason)
	}
}
