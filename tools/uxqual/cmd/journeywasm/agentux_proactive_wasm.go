//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"
	_ "time/tzdata"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var agentAnnouncementsBrowser struct {
	sync.Mutex
	config               journeyclient.Config
	mount                js.Value
	observer             js.Value
	click, input, submit js.Func
	observe              js.Func
	bound, busy          bool
	snapshot             productui.AgentAnnouncementsSnapshot
}

func configureAgentAnnouncements(cfg journeyclient.Config) {
	agentAnnouncementsBrowser.Lock()
	agentAnnouncementsBrowser.config = cfg
	bind := !agentAnnouncementsBrowser.bound
	agentAnnouncementsBrowser.bound = true
	agentAnnouncementsBrowser.Unlock()
	if bind {
		document := js.Global().Get("document")
		agentAnnouncementsBrowser.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentAnnouncementClick(args[0])
			}
			return nil
		})
		agentAnnouncementsBrowser.input = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				updateAgentAnnouncementCounter(args[0])
			}
			return nil
		})
		agentAnnouncementsBrowser.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentAnnouncementSubmit(args[0])
			}
			return nil
		})
		document.Call("addEventListener", "click", agentAnnouncementsBrowser.click)
		document.Call("addEventListener", "input", agentAnnouncementsBrowser.input)
		document.Call("addEventListener", "change", agentAnnouncementsBrowser.input)
		document.Call("addEventListener", "submit", agentAnnouncementsBrowser.submit)
		agentAnnouncementsBrowser.observe = js.FuncOf(func(js.Value, []js.Value) any { findAgentAnnouncementsMount(); return nil })
		agentAnnouncementsBrowser.observer = js.Global().Get("MutationObserver").New(agentAnnouncementsBrowser.observe)
		agentAnnouncementsBrowser.observer.Call("observe", document.Get("body"), map[string]any{"childList": true, "subtree": true})
	}
	findAgentAnnouncementsMount()
}

func findAgentAnnouncementsMount() {
	if !agentOperationsRoute(js.Global().Get("location").Get("pathname").String()) {
		return
	}
	mount := js.Global().Get("document").Call("getElementById", "agent-announcements")
	if !mount.Truthy() {
		return
	}
	selectAgentOperationsTab()
	agentAnnouncementsBrowser.Lock()
	if agentAnnouncementsBrowser.mount.Truthy() && agentAnnouncementsBrowser.mount.Equal(mount) {
		agentAnnouncementsBrowser.Unlock()
		return
	}
	agentAnnouncementsBrowser.mount = mount
	agentAnnouncementsBrowser.Unlock()
	go runAgentAnnouncementRequest(mount, "", nil)
}

func runAgentAnnouncementRequest(mount js.Value, action string, input any) {
	agentAnnouncementsBrowser.Lock()
	if agentAnnouncementsBrowser.busy {
		agentAnnouncementsBrowser.Unlock()
		return
	}
	agentAnnouncementsBrowser.busy = true
	cfg := agentAnnouncementsBrowser.config
	agentAnnouncementsBrowser.Unlock()
	ui.PostAsync(func() { announcementRequestBusy(mount, true) })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	reply, err := agentAnnouncementRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), action, input)
	ui.PostAsync(func() {
		agentAnnouncementsBrowser.Lock()
		agentAnnouncementsBrowser.busy = false
		if !agentAnnouncementsBrowser.mount.Equal(mount) {
			agentAnnouncementsBrowser.Unlock()
			return
		}
		if err == nil && reply.Preview == nil {
			agentAnnouncementsBrowser.snapshot = reply.Snapshot
		}
		snapshot := agentAnnouncementsBrowser.snapshot
		agentAnnouncementsBrowser.Unlock()
		announcementRequestBusy(mount, false)
		if err != nil {
			key := "failed"
			if action != "" {
				key = "request_failed"
			}
			if err == errAgentAnnouncementInvalid {
				key = "input_invalid"
			}
			if err == errAgentAnnouncementConflict {
				key = "conflict"
			}
			if err == errAgentAnnouncementDenied {
				key = "denied"
			}
			if plain, ok := err.(agentAnnouncementReasonError); ok {
				if status := mount.Call("querySelector", "[data-announcement-status]"); status.Truthy() {
					status.Set("textContent", productui.AgentAnnouncementResultText(productui.ResolveProductLocale(domAttribute(mount, "data-locale")), "FAILED", plain.Reason))
					status.Call("focus")
				}
			} else {
				announcementBrowserStatus(mount, key)
			}
			return
		}
		if reply.Preview != nil {
			request, ok := input.(agentcontrols.AnnouncementDraft)
			form := mount.Call("querySelector", "[data-announcement-editor]")
			if !ok || !form.Truthy() || domDataset(form, "announcementPreviewRequest") != request.IdempotencyKey {
				return
			}
			if form := mount.Call("querySelector", "[data-announcement-editor]"); form.Truthy() {
				form.Call("setAttribute", "data-announcement-preview-digest", reply.Preview.Digest)
			}
			form.Call("querySelector", "[data-announcement-action=post]").Set("disabled", !reply.Preview.Public)
			if request.ID != "" {
				form.Call("querySelector", "[data-announcement-action=post]").Set("hidden", false)
				form.Call("querySelector", "[data-announcement-action=save]").Set("hidden", true)
			}
			preview := mount.Call("querySelector", "[data-announcement-preview]")
			if preview.Truthy() {
				preview.Set("hidden", false)
				preview.Set("textContent", "")
				doc := js.Global().Get("document")
				heading := doc.Call("createElement", "h4")
				heading.Set("textContent", announcementBrowserText(mount, "preview_title"))
				preview.Call("append", heading)
				agent := mount.Call("querySelector", "[data-announcement-agent]").Get("selectedOptions").Index(0)
				author := doc.Call("createElement", "strong")
				author.Set("textContent", agent.Get("textContent").String())
				preview.Call("append", author)
				text := doc.Call("createElement", "p")
				text.Set("textContent", reply.Preview.Text)
				text.Set("dir", "auto")
				preview.Call("appendChild", text)
				sourceLabel := doc.Call("createElement", "p")
				sourceLabel.Set("textContent", announcementBrowserText(mount, "sources"))
				preview.Call("append", sourceLabel)
				list := doc.Call("createElement", "ul")
				for _, source := range reply.Preview.Sources {
					item := doc.Call("createElement", "li")
					link := doc.Call("createElement", "a")
					link.Set("textContent", source.Title)
					link.Set("href", source.Href)
					item.Call("appendChild", link)
					list.Call("appendChild", item)
				}
				preview.Call("appendChild", list)
				key := "private_preview"
				if reply.Preview.Public {
					key = "public_preview"
				}
				status := doc.Call("createElement", "p")
				status.Set("textContent", announcementBrowserText(mount, key))
				preview.Call("appendChild", status)
				announcementBrowserStatus(mount, key)
			}
			return
		}
		locale := domAttribute(mount, "data-locale")
		for i := range snapshot.Rows {
			for j := range snapshot.Rows[i].Attempts {
				attempt := &snapshot.Rows[i].Attempts[j]
				attempt.TimeLabel = js.Global().Get("Intl").Get("DateTimeFormat").New(locale, map[string]any{"dateStyle": "medium", "timeStyle": "short"}).Call("format", js.Global().Get("Date").New(attempt.At)).String()
			}
			if snapshot.Rows[i].LastRunAt != "" {
				snapshot.Rows[i].LastRun = js.Global().Get("Intl").Get("DateTimeFormat").New(locale, map[string]any{"dateStyle": "medium", "timeStyle": "short"}).Call("format", js.Global().Get("Date").New(snapshot.Rows[i].LastRunAt)).String()
			}
			if snapshot.Rows[i].NextRunAt != "" {
				snapshot.Rows[i].NextRun = js.Global().Get("Intl").Get("DateTimeFormat").New(locale, map[string]any{"dateStyle": "medium", "timeStyle": "short"}).Call("format", js.Global().Get("Date").New(snapshot.Rows[i].NextRunAt)).String()
			}
		}
		markup, renderErr := ui.RenderToString(productui.RenderAgentAnnouncements(productui.ResolveProductLocale(locale), snapshot))
		if renderErr == nil {
			mount.Set("innerHTML", markup)
		}
		if action != "" {
			status := mount.Call("querySelector", "[data-announcement-status]")
			if status.Truthy() {
				status.Call("setAttribute", "tabindex", "-1")
				status.Call("focus")
			}
		}
	})
}

func handleAgentAnnouncementClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	if picker := target.Call("closest", "[data-agentdoc-picker]"); picker.Truthy() {
		if form := picker.Call("closest", "[data-announcement-editor]"); form.Truthy() {
			clearAnnouncementPreview(form)
			showAnnouncementFieldError(form, "")
		}
	}
	if button := target.Call("closest", "button[data-announcement-retry]"); button.Truthy() {
		event.Call("preventDefault")
		go runAgentAnnouncementRequest(agentAnnouncementsBrowser.mount, "", nil)
		return
	}
	if button := target.Call("closest", "button[data-announcement-new]"); button.Truthy() {
		event.Call("preventDefault")
		form := js.Global().Get("document").Call("querySelector", "[data-announcement-editor]")
		if form.Truthy() {
			form.Call("removeAttribute", "data-announcement-command-key")
			form.Call("removeAttribute", "data-announcement-id")
			form.Call("removeAttribute", "data-announcement-revision")
			form.Call("removeAttribute", "data-announcement-preview-digest")
			form.Call("reset")
			clearAnnouncementPreview(form)
			if picker := form.Call("querySelector", "[data-agentdoc-picker]"); picker.Truthy() {
				refs := picker.Call("querySelectorAll", "[data-agentdoc-reference]")
				for i := 0; i < refs.Length(); i++ {
					refs.Index(i).Call("remove")
				}
				personaDocumentPickerUpdateCount(picker)
			}
			form.Set("hidden", false)
			form.Call("querySelector", "[data-announcement-editor-title]").Set("textContent", announcementBrowserText(agentAnnouncementsBrowser.mount, "new"))
			initializeAnnouncementForm(form)
			form.Call("querySelector", "select").Call("focus")
		}
		return
	}
	button := target.Call("closest", "button[data-announcement-action]")
	if !button.Truthy() || button.Get("disabled").Bool() {
		return
	}
	action := domDataset(button, "announcementAction")
	row := button.Call("closest", "[data-announcement-id]")
	if action == "confirm-delete" || action == "cancel-delete" {
		event.Call("preventDefault")
		confirmation := row.Call("querySelector", "[data-announcement-delete-confirmation]")
		if confirmation.Truthy() {
			confirmation.Set("hidden", action == "cancel-delete")
		}
		return
	}
	if action == "preview" || action == "post" && button.Call("closest", "[data-announcement-editor]").Truthy() {
		form := button.Call("closest", "[data-announcement-editor]")
		event.Call("preventDefault")
		input, ok := agentAnnouncementDraftFromDOM(form)
		if !ok {
			return
		}
		event.Call("preventDefault")
		requestAction := "preview"
		if action == "preview" && input.ID != "" {
			agentAnnouncementsBrowser.Lock()
			rows := append([]productui.AgentAnnouncementRow(nil), agentAnnouncementsBrowser.snapshot.Rows...)
			agentAnnouncementsBrowser.Unlock()
			if !agentAnnouncementPreviewMatchesSaved(input, rows) {
				announcementBrowserStatus(agentAnnouncementsBrowser.mount, "save_before_preview")
				return
			}
		}
		if action == "post" {
			if input.ID != "" {
				command := agentcontrols.AnnouncementCommand{ID: input.ID, Action: "POST_NOW", ExpectedRevision: input.ExpectedRevision, IdempotencyKey: input.IdempotencyKey, PreviewDigest: input.PreviewDigest}
				go runAgentAnnouncementRequest(agentAnnouncementsBrowser.mount, "control", command)
				return
			}
			input.Cadence = "NOW"
			input.Weekdays, input.MonthDay = nil, 0
			input.ExpectedRevision = 0
			requestAction = "create"
		}
		if requestAction == "preview" {
			form.Call("setAttribute", "data-announcement-preview-request", input.IdempotencyKey)
		}
		go runAgentAnnouncementRequest(agentAnnouncementsBrowser.mount, requestAction, input)
		return
	}
	if !row.Truthy() {
		return
	}
	if action == "edit" || action == "preview-again" {
		event.Call("preventDefault")
		id := domDataset(row, "announcementId")
		agentAnnouncementsBrowser.Lock()
		rows := append([]productui.AgentAnnouncementRow(nil), agentAnnouncementsBrowser.snapshot.Rows...)
		agentAnnouncementsBrowser.Unlock()
		for _, saved := range rows {
			if saved.ID == id {
				editAnnouncementForm(agentAnnouncementsBrowser.mount, saved)
				if action == "preview-again" {
					form := agentAnnouncementsBrowser.mount.Call("querySelector", "[data-announcement-editor]")
					if input, ok := agentAnnouncementDraftFromDOM(form); ok {
						form.Call("setAttribute", "data-announcement-preview-request", input.IdempotencyKey)
						go runAgentAnnouncementRequest(agentAnnouncementsBrowser.mount, "preview", input)
					}
				}
				break
			}
		}
		return
	}
	revision, err := strconv.ParseUint(domDataset(row, "announcementRevision"), 10, 64)
	if err != nil {
		return
	}
	commandAction := strings.ToUpper(action)
	if action == "post" {
		commandAction = "POST_NOW"
	}
	command := agentcontrols.AnnouncementCommand{ID: domDataset(row, "announcementId"), Action: commandAction, ExpectedRevision: revision, IdempotencyKey: js.Global().Get("crypto").Call("randomUUID").String(), RetryOccurrence: domDataset(row, "announcementRetryOccurrence")}
	event.Call("preventDefault")
	go runAgentAnnouncementRequest(agentAnnouncementsBrowser.mount, "control", command)
}

func handleAgentAnnouncementSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() || domDataset(form, "announcementEditor") == "" {
		return
	}
	event.Call("preventDefault")
	input, ok := agentAnnouncementDraftFromDOM(form)
	if !ok {
		return
	}
	action := "create"
	if input.ID != "" {
		action = "update"
	}
	go runAgentAnnouncementRequest(agentAnnouncementsBrowser.mount, action, input)
}

func updateAgentAnnouncementCounter(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() {
		return
	}
	form := target.Call("closest", "[data-announcement-editor]")
	if form.Truthy() {
		clearAnnouncementPreview(form)
		showAnnouncementFieldError(form, "")
		updateAnnouncementChoices(form)
	}
	if domDataset(target, "announcementInstruction") == "" {
		return
	}
	counter := js.Global().Get("document").Call("querySelector", "[data-announcement-counter]")
	if counter.Truthy() {
		counter.Set("textContent", strconv.Itoa(len([]rune(target.Get("value").String())))+" / 1000")
	}
}

func agentAnnouncementDraftFromDOM(form js.Value) (agentcontrols.AnnouncementDraft, bool) {
	if !form.Truthy() {
		return agentcontrols.AnnouncementDraft{}, false
	}
	value := func(selector string) string {
		node := form.Call("querySelector", selector)
		if !node.Truthy() {
			return ""
		}
		return node.Get("value").String()
	}

	draft := agentcontrols.AnnouncementDraft{InstallationID: value("[data-announcement-agent]"), PersonaID: value("[data-announcement-agent]"), ConversationID: value("[data-announcement-conversation]"), Instruction: value("[data-announcement-instruction]"), Time: value("[data-announcement-time]"), Zone: value("[data-announcement-zone]"), IdempotencyKey: js.Global().Get("crypto").Call("randomUUID").String()}
	key := domDataset(form, "announcementCommandKey")
	if key == "" {
		key = draft.IdempotencyKey
		form.Call("setAttribute", "data-announcement-command-key", key)
	}
	draft.IdempotencyKey = key
	draft.ID = domDataset(form, "announcementId")
	draft.PreviewDigest = domDataset(form, "announcementPreviewDigest")
	draft.ExpectedRevision, _ = strconv.ParseUint(domDataset(form, "announcementRevision"), 10, 64)
	if cadence := form.Call("querySelector", `input[name="announcement-when"]:checked`); cadence.Truthy() {
		draft.Cadence = cadence.Get("value").String()
	}
	if draft.Cadence == "WEEKLY" {
		days := form.Call("querySelectorAll", `input[name="announcement-weekday"]:checked`)
		for i := 0; i < days.Length(); i++ {
			day, _ := strconv.Atoi(days.Index(i).Get("value").String())
			draft.Weekdays = append(draft.Weekdays, day)
		}
	}
	if draft.Cadence == "MONTHLY" {
		draft.MonthDay, _ = strconv.Atoi(value("[data-announcement-month-day]"))
	}
	conversation := form.Call("querySelector", "[data-announcement-conversation]").Get("selectedOptions").Index(0)
	if conversation.Truthy() {
		draft.InstallationID = domDataset(conversation, "installationId")
	}
	references := form.Call("querySelectorAll", "[data-agentdoc-reference]")
	for i := 0; i < references.Length(); i++ {
		row := references.Index(i)
		mode := agentdocref.VersionMode(domDataset(row, "versionMode"))
		version, _ := strconv.ParseUint(domDataset(row, "pinnedVersion"), 10, 64)
		draft.Documents = append(draft.Documents, agentAnnouncementDocumentReference(domDataset(row, "documentId"), domDataset(row, "documentLabel"), string(mode), domDataset(row, "sectionAnchor"), version))
	}
	field := agentAnnouncementInvalidField(draft)
	showAnnouncementFieldError(form, field)
	return draft, field == ""
}

func announcementBrowserText(mount js.Value, key string) string {
	return productui.AgentAnnouncementText(productui.ResolveProductLocale(domAttribute(mount, "data-locale")), key)
}
func announcementBrowserStatus(mount js.Value, key string) {
	if status := mount.Call("querySelector", "[data-announcement-status]"); status.Truthy() {
		status.Set("textContent", announcementBrowserText(mount, key))
		status.Call("setAttribute", "tabindex", "-1")
		status.Call("focus")
	}
}

func initializeAnnouncementForm(form js.Value) {
	zone := js.Global().Get("Intl").Get("DateTimeFormat").New().Call("resolvedOptions").Get("timeZone").String()
	selectZone := form.Call("querySelector", "[data-announcement-zone]")
	selectZone.Set("textContent", "")
	zones := []string{zone, "UTC"}
	if supported := js.Global().Get("Intl").Get("supportedValuesOf"); supported.Type() == js.TypeFunction {
		values := supported.Invoke("timeZone")
		for i := 0; i < values.Length(); i++ {
			if values.Index(i).String() != zone && values.Index(i).String() != "UTC" {
				zones = append(zones, values.Index(i).String())
			}
		}
	}
	for _, id := range zones {
		option := js.Global().Get("document").Call("createElement", "option")
		option.Set("value", id)
		option.Set("textContent", id)
		selectZone.Call("appendChild", option)
	}
	selectZone.Set("value", zone)
	now := js.Global().Get("Date").New()
	form.Call("querySelector", "[data-announcement-time]").Set("value", fmtAnnouncementTime(now.Call("getHours").Int(), now.Call("getMinutes").Int()))
	form.Call("querySelector", `input[name="announcement-weekday"][value="1"]`).Set("checked", true)
	form.Call("querySelector", "[data-announcement-month-day]").Set("value", "1")
	updateAnnouncementChoices(form)
}

func updateAnnouncementChoices(form js.Value) {
	agent := form.Call("querySelector", "[data-announcement-agent]")
	selected := agent.Get("value").String()
	conversations := form.Call("querySelector", "[data-announcement-conversation]")
	chosen := conversations.Get("value").String()
	conversations.Set("textContent", "")
	document := js.Global().Get("document")
	placeholder := document.Call("createElement", "option")
	placeholder.Set("value", "")
	placeholder.Set("textContent", announcementBrowserText(agentAnnouncementsBrowser.mount, "choose_conversation"))
	conversations.Call("append", placeholder)
	var choices []productui.AgentAnnouncementConversation
	option := agent.Get("selectedOptions").Index(0)
	if selected != "" && option.Truthy() {
		_ = json.Unmarshal([]byte(domAttribute(option, "data-announcement-conversations")), &choices)
	}
	for _, choice := range choices {
		option := document.Call("createElement", "option")
		option.Set("value", choice.ID)
		option.Set("textContent", choice.Name)
		option.Call("setAttribute", "data-installation-id", choice.InstallationID)
		conversations.Call("append", option)
	}
	conversations.Set("value", chosen)
	if conversations.Get("selectedIndex").Int() < 0 {
		conversations.Set("value", "")
	}
	when := form.Call("querySelector", `input[name="announcement-when"]:checked`).Get("value").String()
	editing := domDataset(form, "announcementId") != ""
	action, label := agentAnnouncementPrimaryAction(when, editing)
	save := form.Call("querySelector", "[data-announcement-action=save]")
	post := form.Call("querySelector", "[data-announcement-action=post]")
	save.Set("hidden", action != "save")
	save.Set("disabled", false)
	save.Set("className", "button primary")
	save.Set("textContent", announcementBrowserText(agentAnnouncementsBrowser.mount, label))
	post.Set("hidden", action != "post")
	form.Call("querySelector", "#announcement-save-help").Set("hidden", true)
	form.Call("querySelector", "[data-announcement-weekdays]").Set("hidden", when != "WEEKLY")
	form.Call("querySelector", ".agent-announcement-time").Set("hidden", when == "NOW" || when == "ONCE")
	for _, selector := range []string{"[data-announcement-month-day]", "label[for=announcement-month-day]", "#announcement-month-help"} {
		form.Call("querySelector", selector).Set("hidden", when != "MONTHLY")
	}
}

func editAnnouncementForm(mount js.Value, row productui.AgentAnnouncementRow) {
	form := mount.Call("querySelector", "[data-announcement-editor]")
	form.Call("removeAttribute", "data-announcement-command-key")
	form.Call("removeAttribute", "data-announcement-preview-digest")
	form.Set("hidden", false)
	initializeAnnouncementForm(form)
	form.Call("setAttribute", "data-announcement-id", row.ID)
	form.Call("setAttribute", "data-announcement-revision", strconv.FormatUint(row.Revision, 10))
	form.Call("querySelector", "[data-announcement-editor-title]").Set("textContent", announcementBrowserText(mount, "edit_title"))
	value := row.Editor
	form.Call("querySelector", "[data-announcement-agent]").Set("value", value.PersonaID)
	updateAnnouncementChoices(form)
	if value.Cadence == "ONCE" {
		value.Cadence = "NOW"
	}
	for selector, content := range map[string]string{"[data-announcement-agent]": value.PersonaID, "[data-announcement-conversation]": value.ConversationID, "[data-announcement-instruction]": value.Instruction, "[data-announcement-time]": value.Time, "[data-announcement-zone]": value.Zone, "[data-announcement-month-day]": strconv.Itoa(value.MonthDay)} {
		form.Call("querySelector", selector).Set("value", content)
	}
	form.Call("querySelector", `input[name="announcement-when"][value="`+value.Cadence+`"]`).Set("checked", true)
	days := form.Call("querySelectorAll", `input[name="announcement-weekday"]`)
	for i := 0; i < days.Length(); i++ {
		day, _ := strconv.Atoi(days.Index(i).Get("value").String())
		checked := false
		for _, chosen := range value.Weekdays {
			checked = checked || chosen == day
		}
		days.Index(i).Set("checked", checked)
	}
	var refs []productui.PersonaAdminDocumentReference
	for _, doc := range value.Documents {
		refs = append(refs, productui.PersonaAdminDocumentReference{DocumentID: doc.DocumentID, Label: doc.Label, Title: doc.Label, VersionMode: string(doc.VersionMode), PinnedVersion: doc.PinnedVersion, SectionAnchor: doc.SectionAnchor, Readable: true})
	}
	markup, err := ui.RenderToString(productui.AgentDocumentReferencePicker(productui.ResolveProductLocale(domAttribute(mount, "data-locale")), productui.AgentDocumentReferencePickerModel{ID: "announcement-documents", Available: true, ReferenceLimit: 5, DefaultVersionMode: "LATEST_PUBLISHED", References: refs}))
	if err == nil {
		form.Call("querySelector", "[data-agentdoc-picker]").Set("outerHTML", markup)
	}
	updateAnnouncementChoices(form)
	form.Call("querySelector", "[data-announcement-instruction]").Call("focus")
}

func clearAnnouncementPreview(form js.Value) {
	form.Call("querySelector", "[data-announcement-action=post]").Set("disabled", false)
	form.Call("removeAttribute", "data-announcement-preview-digest")
	form.Call("removeAttribute", "data-announcement-preview-request")
	if preview := form.Call("querySelector", "[data-announcement-preview]"); preview.Truthy() {
		preview.Set("hidden", true)
	}
}

func showAnnouncementFieldError(form js.Value, field string) {
	errors := form.Call("querySelectorAll", "[data-announcement-error]")
	invalid := form.Call("querySelectorAll", "[aria-invalid]")
	for i := 0; i < invalid.Length(); i++ {
		invalid.Index(i).Call("removeAttribute", "aria-invalid")
	}
	for i := 0; i < errors.Length(); i++ {
		node := errors.Index(i)
		active := domAttribute(node, "data-announcement-error") == field
		node.Set("hidden", !active)
		if active {
			node.Set("textContent", announcementBrowserText(agentAnnouncementsBrowser.mount, "error_"+field))
		}
	}
	if field == "" {
		return
	}
	selector := "[data-announcement-" + field + "]"
	if field == "documents" {
		selector = "[data-agentdoc-search]"
	}
	control := form.Call("querySelector", selector)
	if control.Truthy() {
		control.Call("setAttribute", "aria-invalid", "true")
		id := "announcement-" + field + "-error"
		described := domAttribute(control, "aria-describedby")
		if !strings.Contains(described, id) {
			control.Call("setAttribute", "aria-describedby", strings.TrimSpace(described+" "+id))
		}
		control.Call("focus")
	}
}

func announcementRequestBusy(mount js.Value, busy bool) {
	mount.Call("setAttribute", "aria-busy", strconv.FormatBool(busy))
	buttons := mount.Call("querySelectorAll", "button[data-announcement-action],button[data-announcement-new]")
	for i := 0; i < buttons.Length(); i++ {
		button := buttons.Index(i)
		if busy {
			button.Call("setAttribute", "data-announcement-was-disabled", strconv.FormatBool(button.Get("disabled").Bool()))
			button.Set("disabled", true)
		} else if disabled := domDataset(button, "announcementWasDisabled"); disabled != "" {
			button.Set("disabled", disabled == "true")
			button.Call("removeAttribute", "data-announcement-was-disabled")
		}
	}
	if status := mount.Call("querySelector", "[data-announcement-status]"); status.Truthy() {
		text := ""
		if busy {
			text = announcementBrowserText(mount, "working")
		}
		status.Set("textContent", text)
	}
}
