//go:build js && wasm

package main

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	documentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/document/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

var personaDocumentPicker struct {
	sync.Mutex
	client documentv1.DocumentServiceClient
	epoch  uint64
	timer  *time.Timer
}

var personaDocumentPickerListeners sync.Once

// configurePersonaAdminDocumentService is the product bootstrap's narrow
// adapter to the same authorized DocumentService client used by the hub.
func configurePersonaAdminDocumentService(client documentv1.DocumentServiceClient) {
	personaDocumentPicker.Lock()
	personaDocumentPicker.client = client
	personaDocumentPicker.Unlock()
}

func personaAdminDocumentServiceAvailable() bool {
	personaDocumentPicker.Lock()
	defer personaDocumentPicker.Unlock()
	return personaDocumentPicker.client != nil
}

func installPersonaDocumentPickerHandlers() {
	personaDocumentPickerListeners.Do(func() {
		document := js.Global().Get("document")
		if !document.Truthy() {
			return
		}
		input := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			target := args[0].Get("target")
			if target.Truthy() && target.Get("dataset").Get("agentdocSearch").Type() == js.TypeString {
				personaDocumentPickerSchedule(target)
			} else if target.Truthy() && target.Get("dataset").Get("agentdocInstructions").Type() == js.TypeString {
				personaDocumentPickerReconcileInstructions(target)
				personaDocumentMentionSchedule(target)
			}
			return nil
		})
		document.Call("addEventListener", "input", input)
		keydown := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 || !args[0].Truthy() {
				return nil
			}
			event, target := args[0], args[0].Get("target")
			if !target.Truthy() {
				return nil
			}
			if target.Get("dataset").Get("agentdocInstructions").Type() == js.TypeString {
				if personaDocumentMentionKey(target, event.Get("key").String()) {
					event.Call("preventDefault")
				}
				return nil
			}
			if target.Get("dataset").Get("agentdocSearch").Type() != js.TypeString {
				return nil
			}
			if personaDocumentPickerKey(target, event.Get("key").String()) {
				event.Call("preventDefault")
			}
			return nil
		})
		document.Call("addEventListener", "keydown", keydown)
		click := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 || !args[0].Truthy() {
				return nil
			}
			event, target := args[0], args[0].Get("target")
			if !target.Truthy() {
				return nil
			}
			retry := target.Call("closest", personaDocumentPickerRetrySelector)
			remove := target.Call("closest", personaDocumentPickerRemoveSelector)
			option := target.Call("closest", "[data-agentdoc-option]")
			optionID, removeTag, retryTag := "", "", ""
			if option.Truthy() {
				optionID = domDataset(option, "agentdocOption")
			}
			if remove.Truthy() {
				removeTag = remove.Get("tagName").String()
			}
			if retry.Truthy() {
				retryTag = retry.Get("tagName").String()
			}
			action := personaDocumentPickerClickAction(optionID, removeTag, retryTag)
			if action == "retry" {
				event.Call("preventDefault")
				picker := retry.Call("closest", "[data-agentdoc-picker]")
				personaDocumentPickerSchedule(picker.Call("querySelector", "[data-agentdoc-search]"))
				return nil
			}
			if action == "remove" {
				event.Call("preventDefault")
				picker := remove.Call("closest", "[data-agentdoc-picker]")
				row := remove.Call("closest", "[data-agentdoc-reference]")
				if row.Truthy() {
					personaDocumentPickerRemoveInstructionToken(picker, domDataset(row, "documentId"))
					row.Call("remove")
					personaDocumentPickerUpdateCount(picker)
				}
				return nil
			}
			if action == "add" {
				event.Call("preventDefault")
				picker := option.Call("closest", "[data-agentdoc-picker]")
				if !picker.Truthy() {
					if form := option.Call("closest", "form"); form.Truthy() {
						picker = form.Call("querySelector", "[data-agentdoc-picker]")
					}
				}
				personaDocumentPickerAdd(picker, option)
			}
			return nil
		})
		document.Call("addEventListener", "click", click)
		change := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			target := args[0].Get("target")
			if target.Truthy() && target.Get("dataset").Get("agentdocMode").Type() == js.TypeString {
				row := target.Call("closest", "[data-agentdoc-reference]")
				row.Get("dataset").Set("versionMode", target.Get("value").String())
			}
			return nil
		})
		document.Call("addEventListener", "change", change)
	})
}

func personaDocumentPickerSchedule(input js.Value) {
	if !input.Truthy() {
		return
	}
	picker := input.Call("closest", "[data-agentdoc-picker]")
	if !picker.Truthy() || domDataset(picker, "agentdocAvailable") != "true" || input.Get("disabled").Bool() {
		return
	}
	query := strings.TrimSpace(input.Get("value").String())
	personaDocumentPickerSetState(picker, "loading", domDataset(picker, "agentdocLoading"))
	personaDocumentPickerScheduleQuery(picker, query, "")
}

func personaDocumentMentionSchedule(textarea js.Value) {
	state := personaInstructionMentionAt(textarea.Get("value").String(), textarea.Get("selectionStart").Int())
	menu := textarea.Get("parentElement").Call("querySelector", "[data-agentdoc-mention-menu]")
	if !state.Open {
		personaDocumentMentionClose(textarea)
		return
	}
	form := textarea.Call("closest", "form")
	picker := form.Call("querySelector", "[data-agentdoc-picker]")
	if !picker.Truthy() || domDataset(picker, "agentdocAvailable") != "true" {
		personaDocumentMentionClose(textarea)
		return
	}
	menu.Set("hidden", false)
	menu.Get("dataset").Set("agentdocMentionStart", strconv.Itoa(state.Start))
	menu.Get("dataset").Set("agentdocActiveIndex", "-1")
	textarea.Call("setAttribute", "aria-expanded", "true")
	menu.Call("querySelector", "[data-agentdoc-mention-status]").Set("textContent", domDataset(picker, "agentdocLoading"))
	personaDocumentPickerScheduleQuery(picker, strings.TrimSpace(state.Query), textarea.Get("id").String())
}

func personaDocumentPickerScheduleQuery(picker js.Value, query, mentionTarget string) {
	personaDocumentPicker.Lock()
	personaDocumentPicker.epoch++
	epoch := personaDocumentPicker.epoch
	if personaDocumentPicker.timer != nil {
		personaDocumentPicker.timer.Stop()
	}
	personaDocumentPicker.timer = time.AfterFunc(250*time.Millisecond, func() {
		go personaDocumentPickerSearch(epoch, domDataset(picker, "agentdocPicker"), query, mentionTarget)
	})
	personaDocumentPicker.Unlock()
}

func personaDocumentPickerSearch(epoch uint64, pickerID, query, mentionTarget string) {
	personaDocumentPicker.Lock()
	client := personaDocumentPicker.client
	personaDocumentPicker.Unlock()
	personaAdminBrowser.Lock()
	cfg := personaAdminBrowser.cfg
	personaAdminBrowser.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	items, err := searchPersonaAdminDocuments(chatRPCContext(ctx, cfg), client, query, productui.ResolveProductLocale(cfg.Locale), time.Now())
	cancel()
	ui.PostAsync(func() {
		personaDocumentPicker.Lock()
		current := personaDocumentPicker.epoch
		personaDocumentPicker.Unlock()
		if epoch != current {
			return
		}
		picker := js.Global().Get("document").Call("querySelector", "[data-agentdoc-picker='"+pickerID+"']")
		if !picker.Truthy() {
			return
		}
		if mentionTarget != "" {
			textarea := js.Global().Get("document").Call("getElementById", mentionTarget)
			if !textarea.Truthy() {
				return
			}
			if err != nil {
				personaDocumentMentionRenderResults(textarea, nil)
				personaDocumentMentionStatus(textarea, domDataset(picker, "agentdocFailed"))
				return
			}
			personaDocumentMentionRenderResults(textarea, items)
			if len(items) == 0 {
				personaDocumentMentionStatus(textarea, domDataset(picker, "agentdocEmpty"))
			} else {
				personaDocumentMentionStatus(textarea, "")
			}
			return
		}
		if err != nil {
			personaDocumentPickerRenderResults(picker, nil)
			personaDocumentPickerSetState(picker, "failed", domDataset(picker, "agentdocFailed"))
			return
		}
		personaDocumentPickerRenderResults(picker, items)
		if len(items) == 0 {
			personaDocumentPickerSetState(picker, "empty", domDataset(picker, "agentdocEmpty"))
		} else {
			personaDocumentPickerSetState(picker, "ready", "")
		}
	})
}

func personaDocumentPickerRenderResults(picker js.Value, items []productui.AgentDocumentSuggestion) {
	document := js.Global().Get("document")
	list := picker.Call("querySelector", "[data-agentdoc-results]")
	list.Set("textContent", "")
	labels := personaDocumentSuggestionLabels(items)
	for index, item := range items {
		option := document.Call("createElement", "li")
		option.Set("id", domDataset(picker, "agentdocPicker")+"-option-"+strconv.Itoa(index))
		option.Set("className", "agentdoc-picker-option")
		option.Set("role", "option")
		option.Call("setAttribute", "aria-selected", "false")
		option.Get("dataset").Set("agentdocOption", item.DocumentID)
		option.Get("dataset").Set("agentdocTitle", item.Title)
		option.Get("dataset").Set("agentdocLocation", item.Location)
		option.Get("dataset").Set("agentdocLabel", labels[item.DocumentID])
		option.Get("dataset").Set("agentdocUpdated", item.Updated)
		option.Get("dataset").Set("agentdocVersion", strconv.FormatUint(item.PublishedVersion, 10))
		strong := document.Call("createElement", "strong")
		strong.Set("textContent", item.Title)
		strong.Set("dir", "auto")
		detail := document.Call("createElement", "small")
		detail.Set("className", "muted")
		detail.Set("textContent", strings.Trim(strings.Join([]string{item.Location, item.Updated}, " · "), " ·"))
		option.Call("append", strong, detail)
		list.Call("append", option)
	}
	list.Set("hidden", len(items) == 0)
	input := picker.Call("querySelector", "[data-agentdoc-search]")
	input.Call("setAttribute", "aria-expanded", strconv.FormatBool(len(items) > 0))
	input.Call("removeAttribute", "aria-activedescendant")
	picker.Get("dataset").Set("activeIndex", "-1")
}

func personaDocumentMentionRenderResults(textarea js.Value, items []productui.AgentDocumentSuggestion) {
	menu := textarea.Get("parentElement").Call("querySelector", "[data-agentdoc-mention-menu]")
	list := menu.Call("querySelector", "[data-agentdoc-mention-results]")
	list.Set("textContent", "")
	document := js.Global().Get("document")
	labels := personaDocumentSuggestionLabels(items)
	for index, item := range items {
		option := document.Call("createElement", "li")
		option.Set("id", textarea.Get("id").String()+"-mention-option-"+strconv.Itoa(index))
		option.Set("className", "agentdoc-picker-option")
		option.Set("role", "option")
		option.Call("setAttribute", "aria-selected", "false")
		option.Get("dataset").Set("agentdocOption", item.DocumentID)
		option.Get("dataset").Set("agentdocTitle", item.Title)
		option.Get("dataset").Set("agentdocLocation", item.Location)
		option.Get("dataset").Set("agentdocLabel", labels[item.DocumentID])
		option.Get("dataset").Set("agentdocUpdated", item.Updated)
		option.Get("dataset").Set("agentdocVersion", strconv.FormatUint(item.PublishedVersion, 10))
		strong := document.Call("createElement", "strong")
		strong.Set("textContent", labels[item.DocumentID])
		strong.Set("dir", "auto")
		detail := document.Call("createElement", "small")
		detail.Set("className", "muted")
		detail.Set("textContent", strings.Trim(strings.Join([]string{item.Location, item.Updated}, " · "), " ·"))
		option.Call("append", strong, detail)
		list.Call("append", option)
	}
	menu.Set("hidden", false)
	menu.Get("dataset").Set("agentdocActiveIndex", "-1")
	textarea.Call("setAttribute", "aria-expanded", "true")
}

func personaDocumentMentionStatus(textarea js.Value, message string) {
	menu := textarea.Get("parentElement").Call("querySelector", "[data-agentdoc-mention-menu]")
	if menu.Truthy() {
		menu.Call("querySelector", "[data-agentdoc-mention-status]").Set("textContent", message)
	}
}

func personaDocumentMentionClose(textarea js.Value) {
	menu := textarea.Get("parentElement").Call("querySelector", "[data-agentdoc-mention-menu]")
	if menu.Truthy() {
		menu.Set("hidden", true)
		menu.Get("dataset").Set("agentdocActiveIndex", "-1")
		menu.Call("querySelector", "[data-agentdoc-mention-results]").Set("textContent", "")
	}
	textarea.Call("setAttribute", "aria-expanded", "false")
	textarea.Call("removeAttribute", "aria-activedescendant")
}

func personaDocumentMentionKey(textarea js.Value, key string) bool {
	menu := textarea.Get("parentElement").Call("querySelector", "[data-agentdoc-mention-menu]")
	if !menu.Truthy() || menu.Get("hidden").Bool() {
		return false
	}
	if key == "Escape" {
		personaDocumentMentionClose(textarea)
		return true
	}
	options := menu.Call("querySelectorAll", "[data-agentdoc-option]")
	count := options.Get("length").Int()
	if count == 0 {
		return false
	}
	active, _ := strconv.Atoi(domDataset(menu, "agentdocActiveIndex"))
	switch key {
	case "ArrowDown", "ArrowUp":
		delta := 1
		if key == "ArrowUp" {
			delta = -1
		}
		active = (active + delta + count) % count
		for index := 0; index < count; index++ {
			options.Call("item", index).Call("setAttribute", "aria-selected", strconv.FormatBool(index == active))
		}
		menu.Get("dataset").Set("agentdocActiveIndex", strconv.Itoa(active))
		textarea.Call("setAttribute", "aria-activedescendant", options.Call("item", active).Get("id").String())
		return true
	case "Enter", "Tab":
		if active < 0 {
			active = 0
		}
		if active < count {
			form := textarea.Call("closest", "form")
			personaDocumentPickerAdd(form.Call("querySelector", "[data-agentdoc-picker]"), options.Call("item", active))
			return true
		}
	}
	return false
}

func personaDocumentPickerSetState(picker js.Value, state, message string) {
	picker.Get("dataset").Set("agentdocState", state)
	picker.Call("querySelector", "[data-agentdoc-status]").Set("textContent", message)
	retry := picker.Call("querySelector", personaDocumentPickerRetrySelector)
	if retry.Truthy() {
		retry.Set("hidden", state != "failed")
	}
}

func personaDocumentPickerKey(input js.Value, key string) bool {
	picker := input.Call("closest", "[data-agentdoc-picker]")
	options := picker.Call("querySelectorAll", "[data-agentdoc-option]")
	count := options.Get("length").Int()
	if key == "Escape" {
		picker.Call("querySelector", "[data-agentdoc-results]").Set("hidden", true)
		input.Call("setAttribute", "aria-expanded", "false")
		return true
	}
	if count == 0 {
		return false
	}
	active, _ := strconv.Atoi(domDataset(picker, "activeIndex"))
	switch key {
	case "ArrowDown", "ArrowUp":
		delta := 1
		if key == "ArrowUp" {
			delta = -1
		}
		active = (active + delta + count) % count
		for index := 0; index < count; index++ {
			options.Call("item", index).Call("setAttribute", "aria-selected", strconv.FormatBool(index == active))
		}
		picker.Get("dataset").Set("activeIndex", strconv.Itoa(active))
		input.Call("setAttribute", "aria-activedescendant", options.Call("item", active).Get("id").String())
		return true
	case "Enter":
		if active >= 0 && active < count {
			personaDocumentPickerAdd(picker, options.Call("item", active))
			return true
		}
	}
	return false
}

func personaDocumentPickerAdd(picker, option js.Value) {
	if !picker.Truthy() || !option.Truthy() || picker.Call("querySelectorAll", "[data-agentdoc-reference]").Get("length").Int() >= personaDocumentPickerLimit(picker) {
		personaDocumentPickerUpdateCount(picker)
		return
	}
	documentID := domDataset(option, "agentdocOption")
	documentTitle := domDataset(option, "agentdocTitle")
	documentLabel := domDataset(option, "agentdocLabel")
	if documentLabel == "" {
		documentLabel = documentTitle
	}
	rows := picker.Call("querySelectorAll", "[data-agentdoc-reference]")
	for index := 0; index < rows.Get("length").Int(); index++ {
		if domDataset(rows.Call("item", index), "documentId") == documentID {
			return
		}
	}
	document := js.Global().Get("document")
	row := document.Call("createElement", "li")
	row.Set("className", "agentdoc-picker-row")
	row.Get("dataset").Set("agentdocReference", "true")
	row.Get("dataset").Set("documentId", documentID)
	row.Get("dataset").Set("documentLabel", documentLabel)
	row.Get("dataset").Set("documentTitle", documentTitle)
	row.Get("dataset").Set("documentLocation", domDataset(option, "agentdocLocation"))
	mode := domDataset(picker, "agentdocDefaultMode")
	if mode != string(agentdocref.ModeLatestPublished) {
		mode = string(agentdocref.ModePinned)
	}
	row.Get("dataset").Set("versionMode", mode)
	row.Get("dataset").Set("pinnedVersion", domDataset(option, "agentdocVersion"))
	row.Get("dataset").Set("sectionAnchor", "")
	row.Get("dataset").Set("readable", "true")
	link := document.Call("createElement", "a")
	link.Set("href", "/workspace/app/docs?document="+js.Global().Get("encodeURIComponent").Invoke(documentID).String())
	link.Set("target", "_blank")
	link.Set("rel", "noopener noreferrer")
	link.Set("textContent", documentLabel)
	versionControl := document.Call("createElement", "select")
	if domDataset(picker, "agentdocLockedMode") == "true" {
		versionControl = document.Call("createElement", "span")
		versionControl.Set("className", "muted")
		versionControl.Get("dataset").Set("agentdocModeLocked", "true")
		versionControl.Set("textContent", map[bool]string{true: domDataset(picker, "agentdocLatest"), false: strings.ReplaceAll(domDataset(picker, "agentdocPinned"), "{version}", domDataset(option, "agentdocVersion"))}[mode == string(agentdocref.ModeLatestPublished)])
	} else {
		versionControl.Get("dataset").Set("agentdocMode", "true")
		pinned := document.Call("createElement", "option")
		pinned.Set("value", "PINNED")
		pinned.Set("textContent", strings.ReplaceAll(domDataset(picker, "agentdocPinned"), "{version}", domDataset(option, "agentdocVersion")))
		latest := document.Call("createElement", "option")
		latest.Set("value", "LATEST_PUBLISHED")
		latest.Set("textContent", domDataset(picker, "agentdocLatest"))
		versionControl.Call("append", pinned, latest)
		versionControl.Set("value", mode)
	}
	remove := document.Call("createElement", "button")
	remove.Set("type", "button")
	remove.Set("className", "button secondary compact")
	remove.Get("dataset").Set("agentdocRemove", "true")
	remove.Set("textContent", domDataset(picker, "agentdocRemove"))
	remove.Call("setAttribute", "aria-label", remove.Get("textContent").String()+" "+documentLabel)
	row.Call("append", link, versionControl, remove)
	picker.Call("querySelector", "[data-agentdoc-selected]").Call("append", row)
	personaDocumentPickerInsertInstructionToken(picker, documentID, documentTitle, documentLabel, domDataset(option, "agentdocLocation"))
	picker.Call("querySelector", "[data-agentdoc-search]").Set("value", "")
	personaDocumentPickerRenderResults(picker, nil)
	personaDocumentPickerUpdateCount(picker)
}

func personaDocumentPickerInsertInstructionToken(picker js.Value, documentID, title, label, location string) {
	form := picker.Call("closest", "form")
	textarea := form.Call("querySelector", "[data-agentdoc-instructions]")
	if !textarea.Truthy() {
		return
	}
	start := textarea.Get("selectionStart").Int()
	text, caret, _ := personaInstructionInsertDocument(textarea.Get("value").String(), start, nil, personaInstructionDocumentChoice{DocumentID: documentID, Title: title, Location: location, VisibleLabel: label, VersionMode: agentdocref.VersionMode(domDataset(picker, "agentdocDefaultMode"))})
	textarea.Set("value", text)
	textarea.Call("setSelectionRange", caret, caret)
	textarea.Call("focus")
	personaDocumentMentionClose(textarea)
	personaAdminUpdateInstructionCounter(textarea)
}

func personaDocumentPickerRemoveInstructionToken(picker js.Value, documentID string) {
	form := picker.Call("closest", "form")
	textarea := form.Call("querySelector", "[data-agentdoc-instructions]")
	if !textarea.Truthy() {
		return
	}
	text, _ := personaInstructionRemoveReference(textarea.Get("value").String(), documentID, personaDocumentReferencesFromForm(form))
	textarea.Set("value", text)
	personaAdminUpdateInstructionCounter(textarea)
}

func personaDocumentPickerUpdateCount(picker js.Value) {
	if !picker.Truthy() {
		return
	}
	count := picker.Call("querySelectorAll", "[data-agentdoc-reference]").Get("length").Int()
	countNode := picker.Call("querySelector", "[data-agentdoc-count]")
	if countNode.Truthy() {
		locale := productui.ResolveProductLocale(js.Global().Get("document").Get("documentElement").Get("lang").String())
		template := domDataset(picker, "agentdocCountTemplate")
		text := strings.NewReplacer("{count}", locale.FormatNumber(strconv.Itoa(count), 0), "{limit}", locale.FormatNumber(strconv.Itoa(personaDocumentPickerLimit(picker)), 0)).Replace(template)
		countNode.Set("textContent", text)
		countNode.Set("hidden", count == 0)
	}
	input := picker.Call("querySelector", "[data-agentdoc-search]")
	limit := personaDocumentPickerLimit(picker)
	input.Set("disabled", count >= limit)
	if count >= limit {
		personaDocumentPickerSetState(picker, "limit", domDataset(picker, "agentdocLimit"))
	} else {
		personaDocumentPickerSetState(picker, "idle", domDataset(picker, "agentdocIdle"))
	}
}

func personaDocumentPickerReconcileInstructions(textarea js.Value) {
	form := textarea.Call("closest", "form")
	if !form.Truthy() {
		return
	}
	picker := form.Call("querySelector", "[data-agentdoc-picker]")
	if !picker.Truthy() {
		return
	}
	text := textarea.Get("value").String()
	rows := picker.Call("querySelectorAll", "[data-agentdoc-reference]")
	for index := rows.Get("length").Int() - 1; index >= 0; index-- {
		row := rows.Call("item", index)
		label := domDataset(row, "documentLabel")
		if label != "" && !strings.Contains(text, "@["+label+"]") {
			row.Call("remove")
		}
	}
	personaDocumentPickerUpdateCount(picker)
}

func personaDocumentPickerLimit(picker js.Value) int {
	if picker.Truthy() {
		if limit, err := strconv.Atoi(domDataset(picker, "agentdocLimitValue")); err == nil && limit > 0 {
			return limit
		}
	}
	return agentdocref.MaxPersonaReferences
}

func personaDocumentReferencesFromForm(form js.Value) []agentdocref.Reference {
	rows := form.Call("querySelectorAll", "[data-agentdoc-reference]")
	refs := make([]agentdocref.Reference, 0, rows.Get("length").Int())
	for index := 0; index < rows.Get("length").Int(); index++ {
		row := rows.Call("item", index)
		mode := domDataset(row, "versionMode")
		if selectNode := row.Call("querySelector", "[data-agentdoc-mode]"); selectNode.Truthy() {
			mode = selectNode.Get("value").String()
		}
		pinned, _ := strconv.ParseUint(domDataset(row, "pinnedVersion"), 10, 64)
		if mode == string(agentdocref.ModeLatestPublished) {
			pinned = 0
		}
		refs = append(refs, agentdocref.Reference{DocumentID: domDataset(row, "documentId"), VersionMode: agentdocref.VersionMode(mode), PinnedVersion: pinned, SectionAnchor: domDataset(row, "sectionAnchor"), Label: domDataset(row, "documentLabel")})
	}
	return refs
}
