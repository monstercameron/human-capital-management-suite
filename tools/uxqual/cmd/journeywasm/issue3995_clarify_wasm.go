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

// Issue 3995: fills the journey page's clarification slot with what the server
// says this viewer may do and see: an Ask for clarification button for the
// approver who holds the decision, and the questions the viewer is one of the
// two people of, each with where it stands. The slot stays hidden when there is
// nothing to show, so a viewer never sees a hint of a private question. The
// dialog shows the exact case, the proposal revision and who can read the
// question before anything is sent; a reply in chat changes nothing about the
// request, and the dialog says so.

var journeyClarifyInstalled bool

const clarifyDialogID = "projectui-clarify-dialog"

func bindJourneyClarify(cfg journeyclient.Config) {
	if journeyClarifyInstalled {
		return
	}
	document := js.Global().Get("document")
	observer := js.Global().Get("MutationObserver")
	if !document.Truthy() || !observer.Truthy() {
		return
	}
	journeyClarifyInstalled = true
	watch := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		refreshJourneyClarify(cfg, false)
		return nil
	})
	observer.New(watch).Call("observe", document.Get("body"), map[string]any{"childList": true, "subtree": true})
	refreshJourneyClarify(cfg, false)
}

// clarifyWords reads the slot's own words, which the server rendered in the
// page's language.
func clarifyWords(slot js.Value) func(string) string {
	return func(key string) string { return projectAttr(slot, "data-text-"+key) }
}

// refreshJourneyClarify fills the slot once per journey; a re-rendered slot
// loses its marker and is filled again. refocus puts focus on the Ask button
// afterwards, for a dialog that just closed.
func refreshJourneyClarify(cfg journeyclient.Config, refocus bool) {
	slot := js.Global().Get("document").Call("querySelector", "[data-projectui-clarify]")
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
		view, _, err := journeyClarifyCall(ctx, http.DefaultClient, chatBrowser.config(cfg), journeyID, nil)
		if err != nil {
			// Not shown: a refusal and an absence read the same, and a failed
			// read must not claim there is no question.
			slot.Call("removeAttribute", "data-filled-for")
			return
		}
		fillJourneyClarify(cfg, slot, journeyID, view, refocus)
	}()
}

func fillJourneyClarify(cfg journeyclient.Config, slot js.Value, journeyID string, view clarifyViewJSON, refocus bool) {
	if !slot.Get("isConnected").Bool() {
		return
	}
	words := clarifyWords(slot)
	rows := clarifyRows(view.Items, words)
	slot.Set("innerHTML", "")
	if !view.CanAsk && view.AskBlocked == "" && len(rows) == 0 && !view.Incomplete {
		slot.Set("hidden", true)
		return
	}
	slot.Call("appendChild", projectElement("h2", "projectui-journey-clarify-title", words("heading")))
	if status := words("status_" + view.Status); status != "" && (view.CanAsk || len(rows) > 0) {
		slot.Call("appendChild", projectElement("p", "projectui-journey-clarify-status", status))
	}
	var ask js.Value
	if view.CanAsk {
		ask = projectElement("button", "projectui-clarify-ask", words("ask"))
		ask.Call("setAttribute", "type", "button")
		ask.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, _ []js.Value) any {
			openClarifyDialog(cfg, slot, ask, journeyID, view)
			return nil
		}))
		slot.Call("appendChild", ask)
	} else if view.AskBlocked != "" {
		slot.Call("appendChild", projectElement("p", "projectui-journey-clarify-note", words("blocked")))
	}
	if len(rows) > 0 {
		list := projectElement("ul", "projectui-journey-clarify-list", "")
		for _, row := range rows {
			item := projectElement("li", "projectui-journey-clarify-item", "")
			item.Call("appendChild", projectElement("strong", "projectui-journey-clarify-who", row.Title))
			item.Call("appendChild", projectElement("span", "projectui-journey-clarify-state", row.State))
			if row.Href != "" {
				link := projectElement("a", "projectui-journey-clarify-link", row.LinkLabel)
				link.Call("setAttribute", "href", row.Href)
				link.Call("setAttribute", "aria-label", row.Aria)
				item.Call("appendChild", link)
			} else if row.Unavailable != "" {
				item.Call("appendChild", projectElement("span", "projectui-journey-clarify-state", row.Unavailable))
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

// openClarifyDialog shows the review of the question: the exact case and
// proposal revision, who can read it, and the question to write or edit. Focus
// goes to the question, Escape and Cancel close it without sending, and focus
// returns to the button that opened it.
func openClarifyDialog(cfg journeyclient.Config, slot, opener js.Value, journeyID string, view clarifyViewJSON) {
	document := js.Global().Get("document")
	if old := document.Call("getElementById", clarifyDialogID); old.Truthy() {
		old.Call("remove")
	}
	words := clarifyWords(slot)
	dialog := document.Call("createElement", "dialog")
	dialog.Set("id", clarifyDialogID)
	dialog.Set("className", "projectui-clarify-dialog")
	dialog.Call("setAttribute", "aria-labelledby", clarifyDialogID+"-title")
	if dir := projectAttr(slot, "dir"); dir != "" {
		dialog.Call("setAttribute", "dir", dir)
	}
	form := projectElement("form", "projectui-clarify-form", "")
	form.Call("setAttribute", "novalidate", "")
	title := projectElement("h2", "projectui-clarify-heading", words("ask"))
	title.Set("id", clarifyDialogID+"-title")
	form.Call("appendChild", title)
	form.Call("appendChild", projectElement("p", "projectui-clarify-case", clarifyFill(words("case"), map[string]string{"worker": view.WorkerName})))
	revision := projectElement("p", "projectui-clarify-revision", clarifyFill(words("revision"), map[string]string{"revision": clarifyShortRevision(view.Revision)}))
	revision.Call("setAttribute", "title", view.Revision)
	form.Call("appendChild", revision)
	audience := words("audience_unknown")
	if view.Audience != nil && strings.TrimSpace(view.Audience.RecipientName) != "" {
		audience = clarifyFill(words("audience"), map[string]string{"name": strings.TrimSpace(view.Audience.RecipientName)})
	}
	form.Call("appendChild", projectElement("p", "projectui-clarify-audience", audience))
	form.Call("appendChild", projectElement("p", "projectui-clarify-note", words("reply_note")))
	label := projectElement("label", "projectui-clarify-label", words("question_label"))
	label.Call("setAttribute", "for", clarifyDialogID+"-question")
	form.Call("appendChild", label)
	question := projectElement("textarea", "projectui-clarify-question", "")
	question.Set("id", clarifyDialogID+"-question")
	question.Call("setAttribute", "name", "question")
	question.Call("setAttribute", "rows", "5")
	question.Call("setAttribute", "maxlength", "1000")
	question.Call("setAttribute", "required", "")
	question.Call("setAttribute", "aria-describedby", clarifyDialogID+"-hint "+clarifyDialogID+"-error")
	form.Call("appendChild", question)
	hint := projectElement("p", "projectui-clarify-hint", words("question_hint"))
	hint.Set("id", clarifyDialogID+"-hint")
	form.Call("appendChild", hint)
	failure := projectElement("p", "projectui-clarify-error", "")
	failure.Set("id", clarifyDialogID+"-error")
	failure.Call("setAttribute", "role", "alert")
	failure.Set("hidden", true)
	form.Call("appendChild", failure)
	status := projectElement("p", "projectui-clarify-busy", "")
	status.Call("setAttribute", "role", "status")
	form.Call("appendChild", status)
	actions := projectElement("div", "projectui-clarify-actions", "")
	cancel := projectElement("button", "projectui-clarify-cancel", words("cancel"))
	cancel.Call("setAttribute", "type", "button")
	send := projectElement("button", "projectui-clarify-send projectui-button-primary", words("send"))
	send.Call("setAttribute", "type", "submit")
	actions.Call("appendChild", cancel)
	actions.Call("appendChild", send)
	form.Call("appendChild", actions)
	dialog.Call("appendChild", form)
	document.Get("body").Call("appendChild", dialog)

	attempt := clarifyAttemptKey()
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
	on(cancel, "click", func([]js.Value) { dialog.Call("close") })
	on(dialog, "close", func([]js.Value) {
		dialog.Call("remove")
		release()
		if opener.Get("isConnected").Bool() {
			opener.Call("focus")
		}
	})
	showFailure := func(key string) {
		failure.Set("textContent", words(key))
		failure.Set("hidden", false)
	}
	on(form, "submit", func(args []js.Value) {
		if len(args) > 0 {
			args[0].Call("preventDefault")
		}
		if form.Call("hasAttribute", "aria-busy").Bool() || send.Get("disabled").Bool() {
			return
		}
		text := strings.TrimSpace(question.Get("value").String())
		if text == "" {
			showFailure("err_invalid")
			question.Call("focus")
			return
		}
		failure.Set("hidden", true)
		form.Call("setAttribute", "aria-busy", "true")
		send.Set("disabled", true)
		status.Set("textContent", words("sending"))
		go func() {
			ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			_, asked, err := journeyClarifyCall(ctx, http.DefaultClient, chatBrowser.config(cfg), journeyID, &clarifyAskJSON{Revision: view.Revision, Question: text, IdempotencyKey: attempt})
			form.Call("removeAttribute", "aria-busy")
			status.Set("textContent", "")
			if err != nil {
				code := chatworkErrorCode(err)
				if code == "conflict" {
					// The earlier attempt reached the server with other words (its
					// answer was lost). The edited question is a new attempt, so it
					// gets its own key instead of failing every time.
					if fresh := clarifyAttemptKey(); fresh != "" {
						attempt = fresh
					}
				}
				showFailure(clarifyErrorKey(code))
				// The words stay for the approver to keep; Send comes back for
				// a failure a retry could fix.
				send.Set("disabled", clarifyFinal(code))
				return
			}
			name := ""
			if view.Audience != nil {
				name = strings.TrimSpace(view.Audience.RecipientName)
			}
			sent := words("sent_unknown")
			if name != "" {
				sent = clarifyFill(words("sent"), map[string]string{"name": name})
			}
			href := ""
			if strings.HasPrefix(asked.Item.Href, "/workspace/app/chat") && !strings.HasPrefix(asked.Item.Href, "//") {
				href = asked.Item.Href
			}
			dialog.Call("close")
			showProjectToast(sent, href, words("open"))
			slot.Call("removeAttribute", "data-filled-for")
			refreshJourneyClarify(cfg, true)
		}()
	})
	func() {
		defer recoverAt("issue3995-clarify.open-dialog")
		dialog.Call("showModal")
	}()
	question.Call("focus")
}
