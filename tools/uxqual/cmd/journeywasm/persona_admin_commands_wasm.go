//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"syscall/js"

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
		action := form.Get("dataset").Get("personaAdminCommandForm").String()
		starter := form.Call("querySelector", "[name='starter_id']")
		version := 1
		starterID := ""
		if starter.Truthy() && starter.Get("tagName").String() == "SELECT" {
			selected := starter.Get("selectedOptions").Call("item", 0)
			starterID = starter.Get("value").String()
			if selected.Truthy() {
				if raw := selected.Get("dataset").Get("starterVersion").String(); raw != "" {
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
			request.AvatarRef = personaAdminFormValue(form, "avatar_ref")
			request.ManifestID = personaAdminFormValue(form, "manifest_id")
			request.BusinessOwnerID = personaAdminFormValue(form, "business_owner_id")
			request.TechnicalStewardID = personaAdminFormValue(form, "technical_steward_id")
			request.OrganizationScopes = personaAdminSplitList(personaAdminFormValue(form, "organization_scopes"))
		case "CREATE_VERSION":
			request.Handle = personaAdminFormValue(form, "handle")
			request.DisplayName = personaAdminFormValue(form, "display_name")
			request.Purpose = personaAdminFormValue(form, "purpose")
			request.AllowedChannels = personaAdminCheckedValues(form, "allowed_channels")
			if len(request.AllowedChannels) == 0 {
				personaAdminSetCommandStatus("invalid")
				return nil
			}
		default:
			personaAdminSetCommandStatus("invalid")
			return nil
		}
		personaAdminSubmitCommand(request)
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
		decision := ""
		if value := button.Get("dataset").Get("personaReviewDecision"); value.Type() == js.TypeString {
			decision = value.String()
		}
		personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{
			Action:    button.Get("dataset").Get("personaCommand").String(),
			PersonaID: card.Get("dataset").Get("personaId").String(),
			Decision:  decision,
		})
		return nil
	})
	document.Call("addEventListener", "click", click, true)

	change := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() {
			return nil
		}
		target := args[0].Get("target")
		if target.Truthy() && target.Get("id").String() == "persona-admin-starter" {
			personaAdminApplyStarter(target)
		}
		return nil
	})
	document.Call("addEventListener", "change", change)
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
	allowed := map[string]bool{}
	for _, channel := range strings.Split(dataset.Get("starterChannels").String(), ",") {
		allowed[strings.TrimSpace(channel)] = true
	}
	choices := document.Call("querySelectorAll", "[data-persona-channel]")
	for index := 0; index < choices.Get("length").Int(); index++ {
		choice := choices.Call("item", index)
		channel := choice.Get("dataset").Get("personaChannel").String()
		choice.Set("checked", allowed[channel])
		choice.Set("disabled", !allowed[channel])
	}
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

func personaAdminSubmitCommand(request productui.PersonaAdminCommandRequest) {
	personaAdminBrowser.Lock()
	cfg := personaAdminBrowser.cfg
	valid := cfg.Bearer != "" && cfg.Tenant != "" && cfg.Subject != ""
	personaAdminBrowser.Unlock()
	if !valid {
		personaAdminSetCommandStatus("unavailable")
		return
	}
	fetch := js.Global().Get("fetch")
	if fetch.Type() != js.TypeFunction {
		personaAdminSetCommandStatus("unavailable")
		return
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		personaAdminSetCommandStatus("invalid")
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
		personaAdminSetCommandStatus("unavailable")
		onReject.Release()
		return nil
	})
	onResponse = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() {
			personaAdminSetCommandStatus("unavailable")
			onResponse.Release()
			return nil
		}
		response := args[0]
		textPromise := response.Call("text")
		onText = js.FuncOf(func(_ js.Value, textArgs []js.Value) any {
			code := "unavailable"
			if len(textArgs) > 0 {
				var payload struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if json.Unmarshal([]byte(textArgs[0].String()), &payload) == nil && payload.Error.Code != "" {
					code = payload.Error.Code
				} else if response.Get("ok").Bool() {
					code = "success"
				}
			}
			personaAdminSetCommandStatus(code)
			onText.Release()
			if code == "success" {
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

func personaAdminSetCommandStatus(code string) {
	personaAdminBrowser.Lock()
	personaAdminBrowser.commandStatus = code
	personaAdminBrowser.Unlock()
	document := js.Global().Get("document")
	node := document.Call("getElementById", "persona-admin-editor-status")
	if !node.Truthy() {
		return
	}
	locale := productui.ResolveProductLocale(document.Get("documentElement").Get("lang").String())
	node.Set("textContent", productui.PersonaAdminCommandStatusText(locale, code))
	node.Call("setAttribute", "role", map[bool]string{true: "status", false: "alert"}[code == "success"])
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
		personaAdminBrowser.snapshot = &snapshot
		revalidate := personaAdminBrowser.revalidate
		personaAdminBrowser.Unlock()
		if revalidate != nil {
			revalidate()
		}
	}()
}
