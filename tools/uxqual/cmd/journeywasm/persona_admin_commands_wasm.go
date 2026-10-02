//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

var personaAdminCommandListeners sync.Once

func installPersonaAdminCommandHandlers() {
	personaAdminCommandListeners.Do(installPersonaAdminCommandHandlersOnce)
}

func installPersonaAdminCommandHandlersOnce() {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	submit := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() {
			return nil
		}
		event := args[0]
		target := event.Get("target")
		if !target.Truthy() || !target.Call("matches", "form[data-persona-admin-command-form]").Bool() {
			return nil
		}
		event.Call("preventDefault")
		event.Call("stopImmediatePropagation")
		form := target
		action := domDataset(form, "personaAdminCommandForm")
		starter := form.Call("querySelector", "[name='starter_id']")
		version := 0
		starterID := ""
		if starter.Truthy() && starter.Get("tagName").String() == "SELECT" {
			selected := starter.Get("selectedOptions").Call("item", 0)
			starterID = starter.Get("value").String()
			if selected.Truthy() {
				if raw := domDataset(selected, "starterVersion"); raw != "" {
					if parsed := js.Global().Get("Number").Invoke(raw).Int(); parsed > 0 {
						version = parsed
					}
				}
			}
		} else {
			starterID = personaAdminFormValue(form, "starter_id")
			if raw := personaAdminFormValue(form, "starter_version"); raw != "" {
				if parsed := js.Global().Get("Number").Invoke(raw).Int(); parsed > 0 {
					version = parsed
				}
			}
		}
		request := productui.PersonaAdminCommandRequest{Action: action, StarterID: starterID, StarterVersion: uint32(version), PersonaID: personaAdminFormValue(form, "persona_id")}
		switch action {
		case "CREATE_DRAFT":
			instructions, references, valid := personaAdminInstructionsForSubmit(form)
			if !valid {
				return nil
			}
			request.Instructions = &instructions
			request.AvatarRef = personaAdminFormValue(form, "avatar_ref")
			request.ManifestID = personaAdminFormValue(form, "manifest_id")
			request.BusinessOwnerID = personaAdminFormValue(form, "business_owner_id")
			request.TechnicalStewardID = personaAdminFormValue(form, "technical_steward_id")
			request.OrganizationScopes = personaAdminSplitList(personaAdminFormValue(form, "organization_scopes"))
			if picker := form.Call("querySelector", "[data-agentdoc-picker]"); picker.Truthy() && domDataset(picker, "agentdocAvailable") == "true" {
				request.DocumentReferences = &references
			}
		case "CREATE_VERSION":
			instructions, references, valid := personaAdminInstructionsForSubmit(form)
			if !valid {
				return nil
			}
			request.Instructions = &instructions
			request.Handle = personaAdminFormValue(form, "handle")
			request.DisplayName = personaAdminFormValue(form, "display_name")
			request.Purpose = personaAdminFormValue(form, "purpose")
			request.AllowedChannels = personaAdminCheckedValues(form, "allowed_channels")
			if picker := form.Call("querySelector", "[data-agentdoc-picker]"); picker.Truthy() && domDataset(picker, "agentdocAvailable") == "true" {
				request.DocumentReferences = &references
			}
			if len(request.AllowedChannels) == 0 {
				personaAdminSetCommandStatus("invalid")
				return nil
			}
		case "RUN_EVALUATION":
			// The authenticated command endpoint remains the only authority. The
			// UI sends no client-supplied evaluation evidence.
		case "INSTALL", "UNINSTALL", "REINSTALL":
			// REINSTALL is "Start again" on a stopped placement.
			request.ConversationID = personaAdminFormValue(form, "conversation_id")
			if request.ConversationID == "" {
				personaAdminSetCommandStatusForPersona("invalid", request.PersonaID)
				return nil
			}
		default:
			personaAdminSetCommandStatus("invalid")
			return nil
		}
		if action == "RUN_EVALUATION" {
			personaAdminSubmitCommand(request, domDataset(form.Call("querySelector", "[data-evaluation-version]"), "evaluationVersion"))
		} else {
			personaAdminSubmitCommand(request)
		}
		return nil
	})
	document.Call("addEventListener", "submit", submit, true)

	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() {
			return nil
		}
		event := args[0]
		target := event.Get("target")
		if !target.Truthy() {
			return nil
		}
		retry := target.Call("closest", "[data-persona-admin-retry]")
		if retry.Truthy() {
			event.Call("preventDefault")
			personaAdminRefreshCatalog()
			return nil
		}
		newAgent := target.Call("closest", "[data-persona-new-agent]")
		if newAgent.Truthy() {
			event.Call("preventDefault")
			controlled := document.Call("getElementById", domDataset(newAgent, "personaNewAgent"))
			if controlled.Truthy() {
				open := controlled.Get("hidden").Bool()
				controlled.Set("hidden", !open)
				newAgent.Call("setAttribute", "aria-expanded", map[bool]string{true: "true", false: "false"}[open])
				if open {
					if focus := controlled.Call("querySelector", "input,select,textarea,button"); focus.Truthy() {
						focus.Call("focus")
					}
				}
			}
			return nil
		}
		toggle := target.Call("closest", "[data-persona-editor-toggle]")
		if toggle.Truthy() {
			event.Call("preventDefault")
			form := js.Global().Get("document").Call("getElementById", domDataset(toggle, "personaEditorToggle"))
			if form.Truthy() {
				// Edit opens the editor with focus in its first field; Edit again
				// or Cancel closes it, asking first when something was changed.
				if form.Get("hidden").Bool() {
					personaEditorOpen(form)
				} else {
					personaEditorRequestClose(form)
				}
			}
			return nil
		}
		setup := target.Call("closest", "[data-persona-local-setup]")
		if setup.Truthy() {
			event.Call("preventDefault")
			event.Call("stopImmediatePropagation")
			personaAdminSetupLocalDraft(setup)
			return nil
		}
		button := target.Call("closest", "[data-persona-command]")
		if !button.Truthy() || button.Get("disabled").Bool() {
			return nil
		}
		event.Call("preventDefault")
		event.Call("stopImmediatePropagation")
		card := button.Call("closest", "[data-persona-id]")
		if !card.Truthy() {
			personaAdminSetCommandStatus("invalid")
			return nil
		}
		// The button becomes the question and a second press goes ahead (AGENTUX-050).
		if question := domDataset(button, "personaConfirm"); question != "" && !inPageConfirmed(question, button, personaCommandDestructive(domDataset(button, "personaCommand"))) {
			return nil
		}
		decision := ""
		reason := ""
		if value := button.Get("dataset").Get("personaReviewDecision"); value.Type() == js.TypeString {
			decision = value.String()
		}
		if decision == "" {
			wrapper := button.Call("closest", "[data-review-decision-wrapper]")
			if wrapper.Truthy() {
				decision = domDataset(wrapper, "reviewDecisionWrapper")
			}
		}
		if confirmationID := domDataset(button, "reviewUnreadableSubmit"); confirmationID != "" {
			confirmation := document.Call("getElementById", confirmationID)
			if !confirmation.Truthy() || !confirmation.Get("checked").Bool() {
				return nil
			}
			reason = "approved_without_reading_documents"
		}
		if decision == "REJECT" {
			wrapper := button.Call("closest", "[data-review-decision-wrapper]")
			// The reason is typed into a field beside the button, not a browser prompt.
			typed, ready := inPageReason(wrapper, button, domDataset(wrapper, "reviewReasonPrompt"))
			if !ready {
				return nil
			}
			reason = typed
		}
		personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{
			Action:    domDataset(button, "personaCommand"),
			PersonaID: domDataset(card, "personaId"),
			Decision:  decision,
			Reason:    reason,
		})
		return nil
	})
	document.Call("addEventListener", "click", click, true)

	change := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() {
			return nil
		}
		target := args[0].Get("target")
		if target.Truthy() && target.Get("dataset").Get("reviewUnreadableConfirm").Type() == js.TypeString {
			id := domDataset(target, "reviewUnreadableConfirm")
			button := document.Call("querySelector", "[data-review-unreadable-submit='"+id+"']")
			if button.Truthy() {
				button.Set("disabled", !target.Get("checked").Bool())
			}
		}
		if target.Truthy() && target.Get("id").String() == "persona-admin-starter" {
			personaAdminApplyStarter(target)
		}
		// AGENTUX-075: the agent's "React to questions with an emoji" checkbox saves
		// the owner's choice as soon as it is changed.
		if target.Truthy() && target.Get("dataset").Get("personaReactions").Type() == js.TypeString {
			react := target.Get("checked").Bool()
			personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{Action: "SET_REACTIONS", PersonaID: domDataset(target, "personaReactions"), ReactToQuestions: &react})
		}
		return nil
	})
	document.Call("addEventListener", "change", change)

	input := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() {
			return nil
		}
		target := args[0].Get("target")
		if target.Truthy() && domDataset(target, "agentPurposeAutosize") == "true" {
			target.Get("style").Set("height", "auto")
			target.Get("style").Set("height", strconv.Itoa(target.Get("scrollHeight").Int())+"px")
			count := document.Call("getElementById", target.Get("id").String()+"-count")
			if count.Truthy() {
				locale := productui.ResolveProductLocale(document.Get("documentElement").Get("lang").String())
				count.Set("textContent", strings.NewReplacer("{count}", locale.FormatNumber(strconv.Itoa(len([]rune(target.Get("value").String()))), 0), "{limit}", locale.FormatNumber("240", 0)).Replace(domDataset(count, "purposeCounterTemplate")))
			}
		}
		if !target.Truthy() || target.Get("dataset").Get("personaFilter").Type() != js.TypeString {
			if target.Truthy() && target.Get("dataset").Get("agentdocInstructions").Type() == js.TypeString {
				personaAdminUpdateInstructionCounter(target)
			}
			return nil
		}
		selectNode := document.Call("getElementById", domDataset(target, "personaFilter"))
		if !selectNode.Truthy() {
			return nil
		}
		query := strings.ToLower(strings.TrimSpace(target.Get("value").String()))
		options := selectNode.Get("options")
		for index := 0; index < options.Get("length").Int(); index++ {
			option := options.Call("item", index)
			matches := query == "" || strings.Contains(strings.ToLower(option.Get("textContent").String()), query)
			option.Set("hidden", !matches)
		}
		return nil
	})
	document.Call("addEventListener", "input", input)
}

func personaAdminInstructionsForSubmit(form js.Value) (string, []agentdocref.Reference, bool) {
	textarea := form.Call("querySelector", "[data-agentdoc-instructions]")
	if !textarea.Truthy() {
		return "", nil, true
	}
	references := personaDocumentReferencesFromForm(form)
	stored, kept, unknown := personaInstructionStoredForSubmit(textarea.Get("value").String(), references)
	validation := form.Call("querySelector", "[data-agentdoc-instruction-validation]")
	if len(unknown) > 0 {
		locale := productui.ResolveProductLocale(js.Global().Get("document").Get("documentElement").Get("lang").String())
		if validation.Truthy() {
			validation.Set("textContent", productui.AgentDocInstructionValidationText(locale, unknown[0]))
			validation.Set("hidden", false)
		}
		textarea.Call("setAttribute", "aria-invalid", "true")
		textarea.Call("focus")
		return "", nil, false
	}
	if validation.Truthy() {
		validation.Set("textContent", "")
		validation.Set("hidden", true)
	}
	textarea.Call("removeAttribute", "aria-invalid")
	return stored, kept, true
}

func personaAdminApplyStarter(selectNode js.Value) {
	option := selectNode.Get("selectedOptions").Call("item", 0)
	if !option.Truthy() {
		return
	}
	dataset := option.Get("dataset")
	document := js.Global().Get("document")
	for _, field := range []struct{ id, key string }{{"persona-admin-handle", "starterHandle"}, {"persona-admin-name", "starterName"}, {"persona-admin-purpose-input", "starterPurpose"}} {
		node := document.Call("getElementById", field.id)
		if node.Truthy() {
			node.Set("value", dataset.Get(field.key).String())
		}
	}
	manifest := document.Call("getElementById", "persona-admin-manifest")
	if manifest.Truthy() {
		manifest.Set("value", dataset.Get("starterManifest").String())
	}
	builtIn := document.Call("getElementById", "persona-admin-built-in-instructions")
	if builtIn.Truthy() {
		builtIn.Set("textContent", dataset.Get("starterInstructions").String())
	}
	allowed := map[string]bool{}
	for _, channel := range strings.Split(dataset.Get("starterChannels").String(), ",") {
		allowed[strings.TrimSpace(channel)] = true
	}
	choices := document.Call("querySelectorAll", "[data-persona-channel]")
	for index := 0; index < choices.Get("length").Int(); index++ {
		choice := choices.Call("item", index)
		channel := domDataset(choice, "personaChannel")
		choice.Set("checked", allowed[channel])
		choice.Set("disabled", !allowed[channel])
	}
}

func personaAdminUpdateInstructionCounter(textarea js.Value) {
	counter := js.Global().Get("document").Call("querySelector", "[data-agentdoc-instruction-counter='"+textarea.Get("id").String()+"']")
	if !counter.Truthy() {
		return
	}
	template := domDataset(counter, "agentdocCounterTemplate")
	locale := productui.ResolveProductLocale(js.Global().Get("document").Get("documentElement").Get("lang").String())
	text := strings.NewReplacer("{count}", locale.FormatNumber(strconv.Itoa(len([]rune(textarea.Get("value").String()))), 0), "{limit}", locale.FormatNumber("8000", 0)).Replace(template)
	counter.Set("textContent", text)
}

func personaAdminFormValue(form js.Value, name string) string {
	node := form.Call("querySelector", "[name='"+name+"']")
	if !node.Truthy() {
		return ""
	}
	return strings.TrimSpace(node.Get("value").String())
}

func personaAdminCheckedValues(form js.Value, name string) []string {
	items := form.Call("querySelectorAll", "[name='"+name+"']:checked")
	values := make([]string, 0, items.Get("length").Int())
	for index := 0; index < items.Get("length").Int(); index++ {
		values = append(values, items.Call("item", index).Get("value").String())
	}
	return values
}

func personaAdminSplitList(value string) []string {
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func personaAdminSetupLocalDraft(button js.Value) {
	personaAdminBrowser.Lock()
	available := personaAdminBrowser.snapshot != nil && personaAdminBrowser.snapshot.LocalDevBootstrapAvailable
	cfg := personaAdminBrowser.cfg
	personaAdminBrowser.Unlock()
	fetch := js.Global().Get("fetch")
	if !available || cfg.Bearer == "" || cfg.Tenant == "" || cfg.Subject == "" || button.Get("disabled").Bool() || fetch.Type() != js.TypeFunction {
		js.Global().Get("console").Call("warn", "Persona setup not ready", available, cfg.Bearer != "", cfg.Tenant != "", cfg.Subject != "")
		personaAdminSetCommandStatus("unavailable")
		return
	}
	button.Set("disabled", true)
	// Same-origin credentials include the edge-issued SameSite CSRF proof and
	// authenticated browser session; the server rechecks local admin authority.
	headers := js.Global().Get("Object").New()
	headers.Set("authorization", "Bearer "+cfg.Bearer)
	promise := fetch.Invoke("/workspace/persona-admin/local-setup", map[string]any{"method": "POST", "credentials": "same-origin", "headers": headers})
	var response, reject js.Func
	response = js.FuncOf(func(_ js.Value, args []js.Value) any {
		button.Set("disabled", false)
		if len(args) > 0 && args[0].Truthy() && args[0].Get("ok").Bool() {
			personaAdminSetCommandStatus("success")
			personaAdminRefreshCatalog()
		} else {
			if len(args) > 0 && args[0].Truthy() {
				js.Global().Get("console").Call("warn", "Persona setup request refused", args[0].Get("status"))
			}
			personaAdminSetCommandStatus("unavailable")
		}
		response.Release()
		reject.Release()
		return nil
	})
	reject = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Truthy() {
			js.Global().Get("console").Call("warn", "Persona setup transport failed", args[0].Get("name"), args[0].Get("message"))
		}
		button.Set("disabled", false)
		personaAdminSetCommandStatus("unavailable")
		response.Release()
		reject.Release()
		return nil
	})
	promise.Call("then", response, reject)
}

func personaAdminSubmitCommand(request productui.PersonaAdminCommandRequest, evaluationVersion ...string) {
	personaAdminBrowser.Lock()
	cfg := personaAdminBrowser.cfg
	personaAdminBrowser.commandAction = request.Action
	valid := cfg.Bearer != "" && cfg.Tenant != "" && cfg.Subject != ""
	personaAdminBrowser.Unlock()
	if !valid {
		personaAdminSetCommandStatusForPersona("unavailable", request.PersonaID)
		return
	}
	if request.Action == "RUN_EVALUATION" {
		personaAdminSetEvaluationResult(request.PersonaID, productui.PersonaAdminEvaluationResult{Status: "RUNNING"})
	}
	fetch := js.Global().Get("fetch")
	if fetch.Type() != js.TypeFunction {
		if request.Action == "RUN_EVALUATION" {
			personaAdminSetEvaluationUnavailable(request.PersonaID, personaAdminEvaluationVersion(evaluationVersion))
		} else {
			personaAdminSetCommandStatusForPersona("unavailable", request.PersonaID)
		}
		return
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		personaAdminSetCommandStatusForPersona("invalid", request.PersonaID)
		return
	}
	headers := js.Global().Get("Object").New()
	headers.Set("authorization", "Bearer "+cfg.Bearer)
	headers.Set("content-type", "application/json")
	promise := fetch.Invoke("/workspace/persona-admin/commands", map[string]any{
		"method": "POST", "credentials": "same-origin", "headers": headers, "body": string(encoded),
	})
	var onResponse, onText, onReject js.Func
	onReject = js.FuncOf(func(js.Value, []js.Value) any {
		if request.Action == "RUN_EVALUATION" {
			personaAdminSetEvaluationUnavailable(request.PersonaID, personaAdminEvaluationVersion(evaluationVersion))
		} else {
			personaAdminSetCommandStatusForPersona("unavailable", request.PersonaID)
		}
		onReject.Release()
		return nil
	})
	onResponse = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() {
			if request.Action == "RUN_EVALUATION" {
				personaAdminSetEvaluationUnavailable(request.PersonaID, personaAdminEvaluationVersion(evaluationVersion))
			} else {
				personaAdminSetCommandStatusForPersona("unavailable", request.PersonaID)
			}
			onResponse.Release()
			return nil
		}
		response := args[0]
		textPromise := response.Call("text")
		onText = js.FuncOf(func(_ js.Value, textArgs []js.Value) any {
			code := "unavailable"
			if len(textArgs) > 0 {
				var payload struct {
					Evaluation *productui.PersonaAdminEvaluationResult `json:"evaluation"`
					Error      struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if json.Unmarshal([]byte(textArgs[0].String()), &payload) == nil {
					if payload.Error.Code != "" {
						code = payload.Error.Code
					} else if response.Get("ok").Bool() {
						code = "success"
						if request.Action == "RUN_EVALUATION" && payload.Evaluation != nil {
							personaAdminSetEvaluationResult(request.PersonaID, *payload.Evaluation)
						}
					}
				}
			}
			if request.Action == "RUN_EVALUATION" && (code == "evaluation_unavailable" || code == "unavailable" || code == "invalid") {
				personaAdminSetEvaluationUnavailable(request.PersonaID, personaAdminEvaluationVersion(evaluationVersion))
			} else {
				personaAdminSetCommandStatusForPersona(code, request.PersonaID)
			}
			onText.Release()
			if code == "success" {
				personaAdminBrowser.Lock()
				personaAdminBrowser.commandAction = personaAdminLifecycleOutcomeAction(personaAdminBrowser.snapshot, request.PersonaID, request.Action, request.Decision)
				personaAdminBrowser.Unlock()
				personaAdminRefreshCatalog()
			}
			return nil
		})
		textPromise.Call("then", onText).Call("catch", onReject)
		onResponse.Release()
		return nil
	})
	promise.Call("then", onResponse).Call("catch", onReject)
}

func personaAdminEvaluationVersion(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func personaAdminSetEvaluationResult(personaID string, result productui.PersonaAdminEvaluationResult) {
	personaAdminBrowser.Lock()
	if personaAdminBrowser.evaluationResults == nil {
		personaAdminBrowser.evaluationResults = make(map[string]productui.PersonaAdminEvaluationResult)
	}
	result.FailingCases = append([]string(nil), result.FailingCases...)
	personaAdminBrowser.evaluationResults[personaID] = result
	revalidate := personaAdminBrowser.revalidate
	personaAdminBrowser.Unlock()
	if revalidate != nil {
		revalidate()
	}
}

func personaAdminSetEvaluationUnavailable(personaID, version string) {
	personaAdminBrowser.Lock()
	delete(personaAdminBrowser.evaluationResults, personaID)
	revalidate := personaAdminBrowser.revalidate
	personaAdminBrowser.Unlock()
	if revalidate != nil {
		revalidate()
	}
	document := js.Global().Get("document")
	node := document.Call("querySelector", "[data-persona-command-status='"+personaID+"']")
	if !node.Truthy() {
		return
	}
	locale := productui.ResolveProductLocale(document.Get("documentElement").Get("lang").String())
	node.Set("textContent", productui.PersonaAdminEvaluationUnavailableText(locale, version))
	node.Call("setAttribute", "role", "alert")
	fallback := document.Call("querySelector", "[data-evaluation-fallback='"+personaID+"']")
	if fallback.Truthy() {
		fallback.Set("hidden", false)
	}
}

func personaAdminSetCommandStatus(code string) {
	personaAdminSetCommandStatusForPersona(code, "")
}

func personaAdminSetCommandStatusForPersona(code, personaID string) {
	personaAdminBrowser.Lock()
	personaAdminBrowser.commandStatus = code
	personaAdminBrowser.commandPersona = personaID
	personaAdminBrowser.Unlock()
	document := js.Global().Get("document")
	node := document.Call("getElementById", "persona-admin-editor-status")
	if personaID != "" {
		node = document.Call("querySelector", "[data-persona-command-status='"+personaID+"']")
	}
	if !node.Truthy() {
		return
	}
	locale := productui.ResolveProductLocale(document.Get("documentElement").Get("lang").String())
	node.Set("textContent", productui.PersonaAdminCommandStatusText(locale, code))
	node.Call("setAttribute", "role", map[bool]string{true: "status", false: "alert"}[code == "success"])
	if personaID != "" {
		card := node.Call("closest", "[data-persona-id]")
		picker := card.Call("querySelector", "[data-agentdoc-picker]")
		if picker.Truthy() {
			if code == "success" {
				personaDocumentPickerSetState(picker, "idle", domDataset(picker, "agentdocIdle"))
			} else {
				message := productui.PersonaAdminCommandStatusText(locale, code)
				if code == "document_unreadable" {
					labels := make([]string, 0)
					rows := picker.Call("querySelectorAll", "[data-agentdoc-reference]")
					for index := 0; index < rows.Get("length").Int(); index++ {
						if label := domDataset(rows.Call("item", index), "documentLabel"); label != "" {
							labels = append(labels, label)
						}
					}
					message = strings.ReplaceAll(domDataset(picker, "agentdocDocumentUnreadable"), "{document}", strings.Join(labels, ", "))
				}
				personaDocumentPickerSetState(picker, "command-error", message)
			}
		}
	}
}

func personaAdminRefreshCatalog() {
	personaAdminBrowser.Lock()
	cfg := personaAdminBrowser.cfg
	personaAdminBrowser.Unlock()
	go func() {
		snapshot, err := fetchPersonaAdminSnapshot(context.Background(), cfg, "", "", "")
		if err != nil || !snapshot.Available {
			return
		}
		personaAdminBrowser.Lock()
		if cfg.Tenant != personaAdminBrowser.tenant || cfg.Subject != personaAdminBrowser.subject || cfg.Bearer != personaAdminBrowser.bearer {
			personaAdminBrowser.Unlock()
			return
		}
		if personaAdminBrowser.snapshot != nil {
			snapshot = personaAdminPreserveRunHistory(*personaAdminBrowser.snapshot, snapshot)
		}
		personaAdminBrowser.snapshot = &snapshot
		revalidate := personaAdminBrowser.revalidate
		personaAdminBrowser.Unlock()
		if revalidate != nil {
			revalidate()
		}
		// Activity draws its agent rows from this snapshot; when an agent was
		// paused or resumed from that page, its row must say so.
		refreshAgentControlsAfterAgentChange()
		go refreshPersonaAdminRunHistory(cfg)
	}()
}
