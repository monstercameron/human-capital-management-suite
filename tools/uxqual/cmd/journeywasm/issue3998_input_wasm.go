//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3998: fills the journey page's "Ask for input" slot with what the
// server says this viewer may do and see: an Ask for input button for the
// reviewer who holds the decision, and the questions that reviewer asked, each
// with whether it is still current. The dialog has two steps. First the
// reviewer chooses a conversation they can already post in and writes the
// question; then a preview shows the exact message, the proposal revision and
// who can read it, and only Send posts it. The message is labelled informal
// advice, not an approval, in the page's language.

var journeyInputInstalled bool

const inputDialogID = "projectui-input-dialog"

func bindJourneyInput(cfg journeyclient.Config) {
	if journeyInputInstalled {
		return
	}
	document := js.Global().Get("document")
	observer := js.Global().Get("MutationObserver")
	if !document.Truthy() || !observer.Truthy() {
		return
	}
	journeyInputInstalled = true
	watch := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		refreshJourneyInput(cfg, false)
		return nil
	})
	observer.New(watch).Call("observe", document.Get("body"), map[string]any{"childList": true, "subtree": true})
	refreshJourneyInput(cfg, false)
}

func inputWords(slot js.Value) func(string) string {
	return func(key string) string { return projectAttr(slot, "data-text-"+key) }
}

// refreshJourneyInput fills the slot once per journey; a re-rendered slot loses
// its marker and is filled again.
func refreshJourneyInput(cfg journeyclient.Config, refocus bool) {
	slot := js.Global().Get("document").Call("querySelector", "[data-projectui-input]")
	if !slot.Truthy() {
		return
	}
	journeyID := projectCurrentJourney()
	if journeyID == "" || projectAttr(slot, "data-filled-for") == journeyID {
		return
	}
	slot.Call("setAttribute", "data-filled-for", journeyID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		view, _, err := journeyInputCall(ctx, http.DefaultClient, chatBrowser.config(cfg), journeyID, nil)
		if err != nil {
			slot.Call("removeAttribute", "data-filled-for")
			return
		}
		fillJourneyInput(cfg, slot, journeyID, view, refocus)
	}()
}

func fillJourneyInput(cfg journeyclient.Config, slot js.Value, journeyID string, view inputViewJSON, refocus bool) {
	if !slot.Get("isConnected").Bool() {
		return
	}
	words := inputWords(slot)
	rows := inputRows(view.Items, words)
	slot.Set("innerHTML", "")
	if !view.CanAsk && len(rows) == 0 && !view.Incomplete {
		slot.Set("hidden", true)
		return
	}
	slot.Call("appendChild", projectElement("h2", "projectui-journey-clarify-title", words("heading")))
	if status := words("status_" + view.Status); status != "" {
		slot.Call("appendChild", projectElement("p", "projectui-journey-clarify-status", status))
	}
	var ask js.Value
	if view.CanAsk {
		ask = projectElement("button", "projectui-clarify-ask", words("ask"))
		ask.Call("setAttribute", "type", "button")
		ask.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, _ []js.Value) any {
			openInputDialog(cfg, slot, ask, journeyID, view)
			return nil
		}))
		slot.Call("appendChild", ask)
		slot.Call("appendChild", projectElement("p", "projectui-journey-clarify-note", words("informal_note")))
	}
	if len(rows) > 0 {
		list := projectElement("ul", "projectui-journey-clarify-list", "")
		for _, row := range rows {
			item := projectElement("li", "projectui-journey-clarify-item", "")
			item.Call("appendChild", projectElement("strong", "projectui-journey-clarify-who", row.Title))
			item.Call("appendChild", projectElement("span", "projectui-journey-clarify-state", row.Revision))
			item.Call("appendChild", projectElement("span", "projectui-journey-clarify-state", row.State))
			if row.Href != "" {
				link := projectElement("a", "projectui-journey-clarify-link", row.LinkLabel)
				link.Call("setAttribute", "href", row.Href)
				link.Call("setAttribute", "aria-label", row.Aria)
				item.Call("appendChild", link)
			}
			list.Call("appendChild", item)
		}
		slot.Call("appendChild", list)
	}
	if view.Incomplete {
		slot.Call("appendChild", projectElement("p", "projectui-journey-clarify-note", words("incomplete")))
	}
	slot.Set("hidden", false)
	if refocus && ask.Truthy() {
		ask.Call("focus")
	}
}

// openInputDialog is the two-step dialog. Focus goes to the conversation
// picker, Escape and Cancel close it without sending, and focus returns to the
// button that opened it.
func openInputDialog(cfg journeyclient.Config, slot, opener js.Value, journeyID string, view inputViewJSON) {
	document := js.Global().Get("document")
	if old := document.Call("getElementById", inputDialogID); old.Truthy() {
		old.Call("remove")
	}
	words := inputWords(slot)
	dialog := document.Call("createElement", "dialog")
	dialog.Set("id", inputDialogID)
	dialog.Set("className", "projectui-clarify-dialog")
	dialog.Call("setAttribute", "aria-labelledby", inputDialogID+"-title")
	if dir := projectAttr(slot, "dir"); dir != "" {
		dialog.Call("setAttribute", "dir", dir)
	}
	form := projectElement("form", "projectui-clarify-form", "")
	form.Call("setAttribute", "novalidate", "")
	title := projectElement("h2", "projectui-clarify-heading", words("ask"))
	title.Set("id", inputDialogID+"-title")
	form.Call("appendChild", title)
	form.Call("appendChild", projectElement("p", "projectui-clarify-note", words("informal_note")))
	revision := projectElement("p", "projectui-clarify-revision", clarifyFill(words("revision"), map[string]string{"revision": clarifyShortRevision(view.Revision)}))
	revision.Call("setAttribute", "title", view.Revision)
	form.Call("appendChild", revision)

	// Step one: where, what, and whether to name the person.
	edit := projectElement("div", "projectui-input-edit", "")
	pickLabel := projectElement("label", "projectui-clarify-label", words("conversation_label"))
	pickLabel.Call("setAttribute", "for", inputDialogID+"-room")
	edit.Call("appendChild", pickLabel)
	pick := projectElement("select", "projectui-input-select", "")
	pick.Set("id", inputDialogID+"-room")
	pick.Call("setAttribute", "name", "conversation")
	pick.Call("setAttribute", "required", "")
	none := projectElement("option", "", words("conversation_none"))
	none.Set("value", "")
	pick.Call("appendChild", none)
	edit.Call("appendChild", pick)
	empty := projectElement("p", "projectui-clarify-hint", words("conversation_empty"))
	empty.Set("hidden", true)
	edit.Call("appendChild", empty)
	label := projectElement("label", "projectui-clarify-label", words("question_label"))
	label.Call("setAttribute", "for", inputDialogID+"-question")
	edit.Call("appendChild", label)
	question := projectElement("textarea", "projectui-clarify-question", "")
	question.Set("id", inputDialogID+"-question")
	question.Call("setAttribute", "name", "question")
	question.Call("setAttribute", "rows", "5")
	question.Call("setAttribute", "maxlength", "1000")
	question.Call("setAttribute", "required", "")
	question.Call("setAttribute", "aria-describedby", inputDialogID+"-hint "+inputDialogID+"-error")
	edit.Call("appendChild", question)
	hint := projectElement("p", "projectui-clarify-hint", words("question_hint"))
	hint.Set("id", inputDialogID+"-hint")
	edit.Call("appendChild", hint)
	nameRow := projectElement("label", "projectui-input-name", "")
	include := projectElement("input", "", "")
	include.Call("setAttribute", "type", "checkbox")
	include.Call("setAttribute", "name", "include_case_name")
	nameRow.Call("appendChild", include)
	nameRow.Call("appendChild", projectElement("span", "", clarifyFill(words("include_name"), map[string]string{"worker": view.CaseName})))
	if strings.TrimSpace(view.CaseName) != "" {
		edit.Call("appendChild", nameRow)
		edit.Call("appendChild", projectElement("p", "projectui-clarify-hint", words("include_name_hint")))
	}
	form.Call("appendChild", edit)

	// Step two: the exact message and who can read it.
	review := projectElement("div", "projectui-input-review", "")
	review.Set("hidden", true)
	review.Call("setAttribute", "tabindex", "-1")
	reviewTitle := projectElement("h3", "projectui-clarify-label", words("preview_heading"))
	reviewTitle.Set("id", inputDialogID+"-review-title")
	review.Call("setAttribute", "role", "group")
	review.Call("setAttribute", "aria-labelledby", inputDialogID+"-review-title")
	review.Call("appendChild", reviewTitle)
	message := projectElement("pre", "projectui-input-message", "")
	review.Call("appendChild", message)
	audienceLine := projectElement("p", "projectui-clarify-audience", "")
	review.Call("appendChild", audienceLine)
	form.Call("appendChild", review)

	failure := projectElement("p", "projectui-clarify-error", "")
	failure.Set("id", inputDialogID+"-error")
	failure.Call("setAttribute", "role", "alert")
	failure.Set("hidden", true)
	form.Call("appendChild", failure)
	status := projectElement("p", "projectui-clarify-busy", "")
	status.Call("setAttribute", "role", "status")
	form.Call("appendChild", status)
	actions := projectElement("div", "projectui-clarify-actions", "")
	cancel := projectElement("button", "projectui-clarify-cancel", words("cancel"))
	cancel.Call("setAttribute", "type", "button")
	back := projectElement("button", "projectui-clarify-cancel", words("edit"))
	back.Call("setAttribute", "type", "button")
	back.Set("hidden", true)
	next := projectElement("button", "projectui-clarify-send projectui-button-primary", words("preview"))
	next.Call("setAttribute", "type", "submit")
	actions.Call("appendChild", cancel)
	actions.Call("appendChild", back)
	actions.Call("appendChild", next)
	form.Call("appendChild", actions)
	dialog.Call("appendChild", form)
	document.Get("body").Call("appendChild", dialog)

	attempt := inputAttemptKey()
	previewed := false
	var funcs []js.Func
	release := func() {
		for _, f := range funcs {
			f.Release()
		}
	}
	on := func(target js.Value, event string, handler func(args []js.Value)) {
		f := js.FuncOf(func(_ js.Value, args []js.Value) any {
			handler(args)
			return nil
		})
		funcs = append(funcs, f)
		target.Call("addEventListener", event, f)
	}
	showFailure := func(key string) {
		failure.Set("textContent", words(key))
		failure.Set("hidden", false)
	}
	setStep := func(reviewing bool) {
		previewed = reviewing
		edit.Set("hidden", reviewing)
		review.Set("hidden", !reviewing)
		back.Set("hidden", !reviewing)
		if reviewing {
			next.Set("textContent", words("send"))
			review.Call("focus")
		} else {
			next.Set("textContent", words("preview"))
			next.Set("disabled", false)
			question.Call("focus")
		}
	}
	on(cancel, "click", func([]js.Value) { dialog.Call("close") })
	on(back, "click", func([]js.Value) {
		failure.Set("hidden", true)
		setStep(false)
	})
	on(dialog, "close", func([]js.Value) {
		dialog.Call("remove")
		release()
		if opener.Get("isConnected").Bool() {
			opener.Call("focus")
		}
	})
	// A change to the choices after a preview invalidates it.
	for _, field := range []js.Value{pick, question, include} {
		on(field, "input", func([]js.Value) { previewed = false })
		on(field, "change", func([]js.Value) { previewed = false })
	}
	command := func(preview bool) *inputAskJSON {
		return &inputAskJSON{
			Revision: view.Revision, Question: strings.TrimSpace(question.Get("value").String()), ConversationID: pick.Get("value").String(),
			Label: words("label_informal"), RevisionLabel: clarifyFill(words("revision"), map[string]string{"revision": clarifyShortRevision(view.Revision)}),
			IncludeCaseName: include.Get("checked").Bool(), Preview: preview, IdempotencyKey: attempt,
		}
	}
	// The conversations the viewer can post in load once, from the same client
	// the chat page uses.
	go func() {
		ctx, stop := context.WithTimeout(context.Background(), 12*time.Second)
		defer stop()
		targets := projectShareTargetsFor(ctx, cfg)
		if !dialog.Get("isConnected").Bool() {
			return
		}
		if len(targets) == 0 {
			empty.Set("hidden", false)
		}
		for _, target := range targets {
			text := target.Label
			if target.Direct {
				text = words("direct_message") + ": " + target.Label
			}
			option := projectElement("option", "", text)
			option.Set("value", target.ID)
			pick.Call("appendChild", option)
		}
	}()
	on(form, "submit", func(args []js.Value) {
		if len(args) > 0 {
			args[0].Call("preventDefault")
		}
		if form.Call("hasAttribute", "aria-busy").Bool() || next.Get("disabled").Bool() {
			return
		}
		ask := command(!previewed)
		if ask.Question == "" || ask.ConversationID == "" {
			showFailure("err_invalid")
			if ask.ConversationID == "" {
				pick.Call("focus")
			} else {
				question.Call("focus")
			}
			return
		}
		failure.Set("hidden", true)
		form.Call("setAttribute", "aria-busy", "true")
		next.Set("disabled", true)
		if ask.Preview {
			status.Set("textContent", words("previewing"))
		} else {
			status.Set("textContent", words("sending"))
		}
		go func() {
			ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			_, asked, err := journeyInputCall(ctx, http.DefaultClient, chatBrowser.config(cfg), journeyID, ask)
			form.Call("removeAttribute", "aria-busy")
			status.Set("textContent", "")
			if err != nil {
				code := chatworkErrorCode(err)
				if code == "conflict" {
					if fresh := inputAttemptKey(); fresh != "" {
						attempt = fresh
					}
				}
				showFailure(inputErrorKey(code))
				next.Set("disabled", inputFinal(code))
				return
			}
			if ask.Preview {
				message.Set("textContent", asked.Message)
				audienceLine.Set("textContent", inputAudienceSentence(asked.Audience, words))
				next.Set("disabled", false)
				setStep(true)
				return
			}
			href := ""
			if asked.Item != nil && strings.HasPrefix(asked.Item.Href, "/workspace/app/chat") && !strings.HasPrefix(asked.Item.Href, "//") {
				href = asked.Item.Href
			}
			dialog.Call("close")
			showProjectToast(words("sent"), href, words("open"))
			slot.Call("removeAttribute", "data-filled-for")
			refreshJourneyInput(cfg, true)
		}()
	})
	func() {
		defer recoverAt("issue3998-input.open-dialog")
		dialog.Call("showModal")
	}()
	pick.Call("focus")
}
