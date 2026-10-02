//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

var personaAdminBrowser struct {
	sync.Mutex
	tenant              string
	subject             string
	bearer              string
	cfg                 journeyclient.Config
	snapshot            *productui.PersonaAdminSnapshot
	selected            string
	previewSubject      string
	previewConversation string
	previewValidation   []string
	previewUnavailable  bool
	previewSeq          uint64
	revalidate          func()
	installed           bool
	historyRequested    bool
	commandStatus       string
	commandPersona      string
	commandAction       string
	evaluationResults   map[string]productui.PersonaAdminEvaluationResult
}

// personaAdminBrowserClient is a hydrated, metadata-only browser projection.
// The server has already authorized and produced the snapshot for the current
// bearer, so rendering never invents catalog state or trusts browser tenant
// fields. Lifecycle writes use the authenticated server command transport,
// which reauthorizes each action against current persisted state.
type personaAdminBrowserClient struct {
	snapshot productui.PersonaAdminSnapshot
}

func (c personaAdminBrowserClient) Snapshot(context.Context, productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	return c.snapshot, nil
}

func (c personaAdminBrowserClient) Preview(context.Context, productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	return c.snapshot.Preview, nil
}

func (personaAdminBrowserClient) RequestReview(id string) error {
	personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{Action: "REQUEST_REVIEW", PersonaID: id})
	return nil
}
func (personaAdminBrowserClient) ReviewPersona(id, decision string) error {
	personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{Action: "REVIEW", PersonaID: id, Decision: decision})
	return nil
}
func (personaAdminBrowserClient) PublishPersona(id string) error {
	personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{Action: "PUBLISH", PersonaID: id})
	return nil
}
func (personaAdminBrowserClient) RollbackPersona(id string) error {
	personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{Action: "ROLLBACK", PersonaID: id})
	return nil
}
func (personaAdminBrowserClient) SuspendPersona(id string) error {
	personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{Action: "SUSPEND", PersonaID: id})
	return nil
}
func (personaAdminBrowserClient) RetirePersona(id string) error {
	personaAdminSubmitCommand(productui.PersonaAdminCommandRequest{Action: "RETIRE", PersonaID: id})
	return nil
}

var _ productui.PersonaAdminClient = personaAdminBrowserClient{}

// hydratePersonaAdminView attaches the server-authorized projection to the
// resolved browser view. A missing projection preserves the honest unavailable
// state and never fabricates a client from cfg tenant or role display facts.
func hydratePersonaAdminView(view *productui.View, cfg journeyclient.Config) {
	if view == nil {
		return
	}
	snapshot := cfg.PersonaAdminSnapshot
	personaAdminBrowser.Lock()
	if personaAdminBrowser.tenant == cfg.Tenant && personaAdminBrowser.subject == cfg.Subject && personaAdminBrowser.bearer == cfg.Bearer && personaAdminBrowser.snapshot != nil {
		snapshot = personaAdminBrowser.snapshot
	}
	if snapshot != nil {
		copy := *snapshot
		view.Locale = view.Locale.WithTimeZone(agentViewerTimeZone())
		copy.DocumentServiceAvailable = copy.DocumentServiceAvailable || personaAdminDocumentServiceAvailable()
		copy.CommandStatus = personaAdminBrowser.commandStatus
		copy.CommandPersonaID = personaAdminBrowser.commandPersona
		copy.CommandAction = personaAdminBrowser.commandAction
		copy.PreviewPersonaID = personaAdminBrowser.selected
		copy.PreviewSubjectID = personaAdminBrowser.previewSubject
		copy.PreviewConversationID = personaAdminBrowser.previewConversation
		copy.PreviewUnavailable = personaAdminBrowser.previewUnavailable
		copy.PreviewValidationFields = append([]string(nil), personaAdminBrowser.previewValidation...)
		copy.Personas = append([]productui.PersonaAdminPersona(nil), copy.Personas...)
		for index := range copy.Personas {
			if result, ok := personaAdminBrowser.evaluationResults[copy.Personas[index].ID]; ok {
				copy.Personas[index].EvaluationStatus = result.Status
				copy.Personas[index].EvaluationPassed = result.Passed
				copy.Personas[index].EvaluationFailed = result.Failed
				copy.Personas[index].EvaluationCaseCount = result.Passed + result.Failed
				copy.Personas[index].EvaluationFailureNames = append([]string(nil), result.FailingCases...)
				if result.Status == "PASSED" && copy.Personas[index].EvaluationRef == "" {
					copy.Personas[index].EvaluationRef = "recorded"
				}
			}
		}
		snapshot = &copy
	}
	personaAdminBrowser.Unlock()
	if view == nil || snapshot == nil || !snapshot.Available {
		return
	}
	view.PersonaAdminClient = personaAdminBrowserClient{snapshot: *snapshot}
}

func configurePersonaAdminBrowser(cfg journeyclient.Config, revalidate func()) {
	installPersonaAdminCommandHandlers()
	installPersonaDocumentPickerHandlers()
	personaAdminBrowser.Lock()
	if personaAdminBrowser.tenant != cfg.Tenant || personaAdminBrowser.subject != cfg.Subject {
		personaAdminBrowser.evaluationResults = make(map[string]productui.PersonaAdminEvaluationResult)
	}
	identityChanged := personaAdminBrowser.tenant != cfg.Tenant || personaAdminBrowser.subject != cfg.Subject || personaAdminBrowser.bearer != cfg.Bearer
	if identityChanged {
		personaAdminBrowser.historyRequested = false
		personaAdminBrowser.commandStatus = ""
		personaAdminBrowser.commandPersona = ""
		personaAdminBrowser.commandAction = ""
		personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation = "", ""
		personaAdminBrowser.selected = ""
		personaAdminBrowser.previewUnavailable = false
		personaAdminBrowser.previewValidation = nil
	}
	personaAdminBrowser.tenant, personaAdminBrowser.subject, personaAdminBrowser.bearer, personaAdminBrowser.cfg = cfg.Tenant, cfg.Subject, cfg.Bearer, cfg
	if identityChanged {
		personaAdminBrowser.previewSeq++
	}
	if identityChanged || personaAdminBrowser.snapshot == nil {
		personaAdminBrowser.snapshot = cfg.PersonaAdminSnapshot
	}
	personaAdminBrowser.revalidate = revalidate
	if identityChanged && personaAdminBrowser.snapshot != nil {
		personaAdminBrowser.selected = personaAdminBrowser.snapshot.PreviewPersonaID
	}
	loadHistory := personaAdminShouldLoadHistory(personaAdminBrowser.snapshot != nil, personaAdminBrowser.historyRequested)
	if loadHistory {
		personaAdminBrowser.historyRequested = true
	}
	install := !personaAdminBrowser.installed
	personaAdminBrowser.installed = true
	personaAdminBrowser.Unlock()
	if loadHistory {
		go refreshPersonaAdminRunHistory(cfg)
	}
	if install {
		listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 || !args[0].Truthy() {
				return nil
			}
			target := args[0].Get("target")
			if !target.Truthy() {
				return nil
			}
			id := target.Get("id").String()
			if id == "persona-admin-preview-persona" || id == "persona-admin-preview-subject" || id == "persona-admin-preview-conversation" {
				personaAdminResolveBrowserCombobox(target)
				return nil
			}
			card := target.Call("closest", "[data-persona-id]")
			if card.Truthy() {
				personaAdminBrowser.Lock()
				personaAdminBrowser.selected = domDataset(card, "personaId")
				personaAdminBrowser.Unlock()
			}
			return nil
		})
		document := js.Global().Get("document")
		document.Call("addEventListener", "change", listener)
		keyup := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 || !args[0].Truthy() || args[0].Get("key").String() != "Enter" {
				return nil
			}
			target := args[0].Get("target")
			if target.Truthy() && target.Get("dataset").Get("personaCombobox").Type() == js.TypeString {
				personaAdminResolveBrowserCombobox(target)
			}
			return nil
		})
		document.Call("addEventListener", "keyup", keyup)
		submit := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 || !args[0].Truthy() {
				return nil
			}
			event := args[0]
			form := event.Get("target")
			if !form.Truthy() || !form.Call("matches", "form[data-persona-preview-check]").Bool() {
				return nil
			}
			event.Call("preventDefault")
			personaAdminCheckPreview()
			return nil
		})
		document.Call("addEventListener", "submit", submit)
	}
}

func personaAdminCheckPreview() {
	document := js.Global().Get("document")
	persona := personaAdminComboboxValue(document.Call("getElementById", "persona-admin-preview-persona"))
	subject := personaAdminComboboxValue(document.Call("getElementById", "persona-admin-preview-subject"))
	conversation := personaAdminComboboxValue(document.Call("getElementById", "persona-admin-preview-conversation"))
	personaAdminBrowser.Lock()
	personaAdminBrowser.selected, personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation = persona, subject, conversation
	personaAdminBrowser.previewValidation = personaAdminMissingPreviewFields(persona, subject, conversation)
	currentCfg, revalidate := personaAdminBrowser.cfg, personaAdminBrowser.revalidate
	if personaAdminBrowser.snapshot != nil && (persona == "" || subject == "" || conversation == "") {
		cleared := *personaAdminBrowser.snapshot
		cleared.Preview = productui.PersonaAdminPreview{}
		personaAdminBrowser.snapshot = &cleared
	}
	personaAdminBrowser.Unlock()
	if persona == "" || subject == "" || conversation == "" {
		if revalidate != nil {
			revalidate()
		}
		fields := personaAdminMissingPreviewFields(persona, subject, conversation)
		if len(fields) > 0 {
			ui.PostAsync(func() {
				field := document.Call("getElementById", "persona-admin-preview-"+fields[0])
				if field.Truthy() {
					field.Call("focus")
				}
			})
		}
		return
	}
	go refreshPersonaAdminPreview(currentCfg, persona, subject, conversation)
}

func personaAdminComboboxValue(input js.Value) string {
	if !input.Truthy() {
		return ""
	}
	if input.Get("tagName").String() == "SELECT" {
		return strings.TrimSpace(input.Get("value").String())
	}
	value, _, ok := personaAdminBrowserComboboxSelection(input)
	if ok {
		input.Get("dataset").Set("selectedValue", value)
		return value
	}
	input.Get("dataset").Set("selectedValue", "")
	return ""
}

func personaAdminResolveBrowserCombobox(input js.Value) {
	value, label, ok := personaAdminBrowserComboboxSelection(input)
	if !ok {
		input.Get("dataset").Set("selectedValue", "")
		return
	}
	input.Set("value", label)
	input.Get("dataset").Set("selectedValue", value)
}

func personaAdminBrowserComboboxSelection(input js.Value) (string, string, bool) {
	label := strings.TrimSpace(input.Get("value").String())
	listID := domAttribute(input, "list")
	list := js.Global().Get("document").Call("getElementById", listID)
	if !list.Truthy() {
		return "", label, false
	}
	options := list.Get("options")
	values := make([]personaAdminSelectionOption, 0, options.Get("length").Int())
	for index := 0; index < options.Get("length").Int(); index++ {
		option := options.Call("item", index)
		values = append(values, personaAdminSelectionOption{Value: domDataset(option, "value"), Label: option.Get("value").String()})
	}
	return personaAdminResolveSelection(label, domDataset(input, "selectedValue"), values)
}

func personaAdminPersonaID(snapshot productui.PersonaAdminSnapshot) string {
	for _, persona := range snapshot.Personas {
		if persona.ID != "" && persona.Lifecycle == productui.PersonaPublished {
			return persona.ID
		}
	}
	for _, persona := range snapshot.Personas {
		if persona.ID != "" {
			return persona.ID
		}
	}
	return ""
}

func personaAdminCurrentSelection(snapshot productui.PersonaAdminSnapshot, selected string) string {
	for _, persona := range snapshot.Personas {
		if persona.ID != "" && persona.ID == selected {
			return selected
		}
	}
	return personaAdminPersonaID(snapshot)
}

func refreshPersonaAdminPreview(cfg journeyclient.Config, persona, subject, conversation string) {
	personaAdminBrowser.Lock()
	if cfg.Tenant != personaAdminBrowser.tenant || cfg.Subject != personaAdminBrowser.subject || cfg.Bearer != personaAdminBrowser.bearer {
		personaAdminBrowser.Unlock()
		return
	}
	personaAdminBrowser.previewSeq++
	seq := personaAdminBrowser.previewSeq
	personaAdminBrowser.previewSubject, personaAdminBrowser.previewConversation = subject, conversation
	personaAdminBrowser.previewValidation = nil
	personaAdminBrowser.Unlock()
	snapshot, err := fetchPersonaAdminSnapshot(context.Background(), cfg, persona, subject, conversation)
	if err != nil {
		personaAdminBrowser.Lock()
		if seq == personaAdminBrowser.previewSeq && personaAdminBrowser.snapshot != nil {
			personaAdminBrowser.previewUnavailable = true
			cleared := *personaAdminBrowser.snapshot
			cleared.Preview = productui.PersonaAdminPreview{}
			personaAdminBrowser.snapshot = &cleared
			revalidate := personaAdminBrowser.revalidate
			personaAdminBrowser.Unlock()
			if revalidate != nil {
				revalidate()
			}
		} else {
			personaAdminBrowser.Unlock()
		}
		return
	}
	personaAdminBrowser.Lock()
	if seq != personaAdminBrowser.previewSeq || cfg.Tenant != personaAdminBrowser.tenant || cfg.Subject != personaAdminBrowser.subject || cfg.Bearer != personaAdminBrowser.bearer {
		personaAdminBrowser.Unlock()
		return
	}
	snapshot = personaAdminApplyPreviewSelection(snapshot, persona, subject, conversation)
	if personaAdminBrowser.snapshot != nil {
		snapshot = personaAdminPreserveRunHistory(*personaAdminBrowser.snapshot, snapshot)
	}
	personaAdminBrowser.snapshot = &snapshot
	personaAdminBrowser.previewUnavailable = false
	personaAdminBrowser.selected = persona
	revalidate := personaAdminBrowser.revalidate
	personaAdminBrowser.Unlock()
	if revalidate != nil {
		revalidate()
	}
	ui.PostAsync(func() {
		result := js.Global().Get("document").Call("querySelector", "[data-persona-preview-result]")
		if result.Truthy() {
			result.Call("setAttribute", "tabindex", "-1")
			result.Call("focus")
		}
	})
}

const personaAdminFetchTimeout = 15 * time.Second

func personaAdminSnapshotEndpoint(persona, subject, conversation string) string {
	query := url.Values{}
	if persona != "" && subject != "" && conversation != "" {
		query.Set("persona_id", persona)
		query.Set("subject_id", subject)
		query.Set("conversation_id", conversation)
	}
	endpoint := "/workspace/persona-admin/"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	return endpoint
}

func fetchPersonaAdminSnapshot(ctx context.Context, cfg journeyclient.Config, persona, subject, conversation string) (productui.PersonaAdminSnapshot, error) {
	endpoint := personaAdminSnapshotEndpoint(persona, subject, conversation)
	fetch := js.Global().Get("fetch")
	if fetch.Type() != js.TypeFunction {
		return productui.PersonaAdminSnapshot{}, errors.New("persona admin transport unavailable")
	}
	controllerCtor := js.Global().Get("AbortController")
	if controllerCtor.Type() != js.TypeFunction {
		return productui.PersonaAdminSnapshot{}, errors.New("persona admin cancellation unavailable")
	}
	controller := controllerCtor.New()
	headers := js.Global().Get("Object").New()
	headers.Set("authorization", "Bearer "+cfg.Bearer)
	responsePromise := fetch.Invoke(endpoint, map[string]any{"credentials": "same-origin", "headers": headers, "signal": controller.Get("signal")})
	result := make(chan struct {
		snapshot productui.PersonaAdminSnapshot
		err      error
	}, 1)
	var once sync.Once
	var then, textThen, textCatch, catch, timer js.Func
	release := func(fn *js.Func) {
		if fn.Value.Type() != js.TypeUndefined {
			fn.Release()
			*fn = js.Func{}
		}
	}
	var timerID js.Value
	timerScheduled := false
	var fetchCallbacks sync.Once
	var textCallbacks sync.Once
	var timerCallback sync.Once
	releaseFetchCallbacks := func() {
		fetchCallbacks.Do(func() {
			release(&then)
			release(&catch)
		})
	}
	releaseTextCallbacks := func() {
		textCallbacks.Do(func() {
			release(&textThen)
			release(&textCatch)
		})
	}
	stopTimer := func() {
		timerCallback.Do(func() {
			if timerScheduled {
				js.Global().Call("clearTimeout", timerID)
				timerScheduled = false
			}
			release(&timer)
		})
	}
	finish := func(item struct {
		snapshot productui.PersonaAdminSnapshot
		err      error
	}) {
		once.Do(func() { result <- item })
	}
	then = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !args[0].Truthy() || !args[0].Get("ok").Bool() {
			releaseFetchCallbacks()
			stopTimer()
			finish(struct {
				snapshot productui.PersonaAdminSnapshot
				err      error
			}{err: errors.New("persona admin request refused")})
			return nil
		}
		textPromise := args[0].Call("text")
		textThen = js.FuncOf(func(_ js.Value, textArgs []js.Value) any {
			releaseTextCallbacks()
			releaseFetchCallbacks()
			stopTimer()
			var payload personaAdminDataWire
			if len(textArgs) == 0 || json.Unmarshal([]byte(textArgs[0].String()), &payload) != nil {
				finish(struct {
					snapshot productui.PersonaAdminSnapshot
					err      error
				}{err: errors.New("persona admin response malformed")})
				return nil
			}
			finish(struct {
				snapshot productui.PersonaAdminSnapshot
				err      error
			}{snapshot: payload.Snapshot})
			return nil
		})
		textCatch = js.FuncOf(func(js.Value, []js.Value) any {
			releaseTextCallbacks()
			releaseFetchCallbacks()
			stopTimer()
			finish(struct {
				snapshot productui.PersonaAdminSnapshot
				err      error
			}{err: errors.New("persona admin response unavailable")})
			return nil
		})
		textPromise.Call("then", textThen).Call("catch", textCatch)
		releaseFetchCallbacks()
		return nil
	})
	catch = js.FuncOf(func(js.Value, []js.Value) any {
		releaseFetchCallbacks()
		stopTimer()
		finish(struct {
			snapshot productui.PersonaAdminSnapshot
			err      error
		}{err: errors.New("persona admin request unavailable")})
		return nil
	})
	responsePromise.Call("then", then).Call("catch", catch)
	timer = js.FuncOf(func(js.Value, []js.Value) any {
		controller.Call("abort")
		stopTimer()
		finish(struct {
			snapshot productui.PersonaAdminSnapshot
			err      error
		}{err: errors.New("persona admin request timed out")})
		return nil
	})
	timerID = js.Global().Call("setTimeout", timer, personaAdminFetchTimeout.Milliseconds())
	timerScheduled = true
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case item := <-result:
		return item.snapshot, item.err
	case <-ctx.Done():
		controller.Call("abort")
		stopTimer()
		return productui.PersonaAdminSnapshot{}, context.Canceled
	}
}

type personaAdminDataWire struct {
	Snapshot productui.PersonaAdminSnapshot `json:"snapshot"`
}

func refreshPersonaAdminRunHistory(cfg journeyclient.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reply, err := agentControlsRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "", nil)
	ui.PostAsync(func() {
		personaAdminBrowser.Lock()
		if personaAdminBrowser.bearer != cfg.Bearer || personaAdminBrowser.snapshot == nil {
			personaAdminBrowser.Unlock()
			return
		}
		snapshot := *personaAdminBrowser.snapshot
		snapshot.Personas = append([]productui.PersonaAdminPersona(nil), snapshot.Personas...)
		for i := range snapshot.Personas {
			snapshot.Personas[i].RecentRuns = reply.Snapshot.Runs
			snapshot.Personas[i].RecentRunsUnavailable = err != nil
		}
		personaAdminBrowser.snapshot = &snapshot
		revalidate := personaAdminBrowser.revalidate
		personaAdminBrowser.Unlock()
		if revalidate != nil {
			revalidate()
		}
	})
}
