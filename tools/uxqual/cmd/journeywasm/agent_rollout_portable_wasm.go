//go:build js && wasm

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type rolloutCatalogReply struct {
	Catalog struct {
		Versions []struct {
			PersonaID       string `json:"persona_id"`
			Name            string `json:"name"`
			Version         int64  `json:"version"`
			ProfileDigest   string `json:"profile_digest"`
			ManifestID      string `json:"manifest_id"`
			ManifestVersion uint64 `json:"manifest_version"`
			PublishedAt     string `json:"published_at"`
			Current         bool   `json:"current"`
		} `json:"versions"`
		Installations []struct {
			ID             string `json:"id"`
			PersonaID      string `json:"persona_id"`
			ConversationID string `json:"conversation_id"`
			Version        int64  `json:"version"`
			Revision       int64  `json:"revision"`
			Name           string `json:"name"`
			Kind           string `json:"kind"`
			MemberCount    uint32 `json:"member_count"`
			Visible        bool   `json:"visible"`
			UpdateBlocked  bool   `json:"update_blocked"`
		} `json:"installations"`
		NotListedCount int `json:"not_listed_count"`
	} `json:"catalog"`
}
type rolloutReceiptReply struct {
	Plan struct {
		ID            string `json:"id"`
		Digest        string `json:"digest"`
		ProfileDigest string `json:"profile_digest"`
		Version       int64  `json:"version"`
		CanaryCount   int    `json:"canary_count"`
		Candidates    []struct {
			InstallationID    string `json:"installation_id"`
			ConversationID    string `json:"conversation_id"`
			PolicyDigest      string `json:"policy_digest"`
			Version           int64  `json:"version"`
			Revision          int64  `json:"revision"`
			RevocationEpoch   int64  `json:"revocation_epoch"`
			AuthorityRevision int64  `json:"authority_revision"`
		} `json:"candidates"`
	} `json:"plan"`
	Progress struct {
		Revision   int64  `json:"revision"`
		Cursor     int64  `json:"cursor"`
		Stage      string `json:"stage"`
		ApproverID string `json:"approver_id"`
	} `json:"progress"`
}

var rolloutPortableBrowser struct {
	sync.Mutex
	config   journeyclient.Config
	bound    bool
	busy     bool
	mount    js.Value
	observer js.Value
	observe  js.Func
	locale   string
	snapshot productui.AgentRolloutSnapshot
}

func configureAgentRolloutPortable(cfg journeyclient.Config) {
	cfg = personaChatHTTPConfig(cfg)
	rolloutPortableBrowser.Lock()
	changed := rolloutPortableBrowser.config.Bearer != cfg.Bearer
	previous := rolloutPortableBrowser.mount
	if changed {
		rolloutPortableBrowser.snapshot = productui.AgentRolloutSnapshot{}
		rolloutPortableBrowser.mount = js.Undefined()
	}
	rolloutPortableBrowser.config = cfg
	first := !rolloutPortableBrowser.bound
	rolloutPortableBrowser.bound = true
	rolloutPortableBrowser.Unlock()
	if changed && previous.Truthy() {
		renderRolloutPortable(previous, productui.AgentRolloutSnapshot{Loading: true})
		rolloutPortableBrowser.Lock()
		rolloutPortableBrowser.mount = js.Undefined()
		rolloutPortableBrowser.Unlock()
	}
	if first {
		rolloutPortableBrowser.observe = js.FuncOf(func(_ js.Value, _ []js.Value) any { findRolloutPortableMount(); return nil })
		rolloutPortableBrowser.observer = js.Global().Get("MutationObserver").New(rolloutPortableBrowser.observe)
		rolloutPortableBrowser.observer.Call("observe", js.Global().Get("document").Get("body"), map[string]any{"childList": true, "subtree": true, "attributes": true, "attributeFilter": []any{"data-locale"}})
		js.Global().Get("document").Call("addEventListener", "submit", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleRolloutSubmit(args[0])
				handlePortableSubmit(args[0])
			}
			return nil
		}))
		js.Global().Get("document").Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentOperationsTabClick(args[0])
				handleRolloutClick(args[0])
			}
			return nil
		}))
		js.Global().Get("document").Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentOperationsTabKeydown(args[0])
			}
			return nil
		}))
		js.Global().Call("addEventListener", "popstate", js.FuncOf(func(_ js.Value, _ []js.Value) any {
			selectAgentOperationsTab()
			return nil
		}))
		js.Global().Get("document").Call("addEventListener", "change", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				filterRolloutPersona(args[0])
			}
			return nil
		}))
		js.Global().Get("document").Call("addEventListener", "input", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				filterRolloutPersona(args[0])
			}
			return nil
		}))
	}
	findRolloutPortableMount()
	selectAgentOperationsTab()
}

func filterRolloutPersona(event js.Value) {
	target := event.Get("target")
	if target.Truthy() && target.Get("id").String() == "agent-portable-file" {
		files := target.Get("files")
		if files.Get("length").Int() == 0 {
			return
		}
		fileName := js.Global().Get("document").Call("getElementById", "agent-portable-file-name")
		if fileName.Truthy() {
			fileName.Set("textContent", files.Index(0).Get("name").String())
		}
		if files.Index(0).Get("size").Int() > 2<<20 {
			setPortableStatus("invalid")
			return
		}
		reader := js.Global().Get("FileReader").New()
		var loaded js.Func
		loaded = js.FuncOf(func(_ js.Value, _ []js.Value) any {
			body := reader.Get("result").String()
			loaded.Release()
			updatePortableDefinition(body, true)
			return nil
		})
		reader.Set("onload", loaded)
		reader.Call("readAsText", files.Index(0))
		return
	}
	if target.Truthy() && target.Get("id").String() == "agent-portable-body" {
		updatePortableDefinition(target.Get("value").String(), event.Get("type").String() == "change")
		return
	}
	if target.Truthy() && target.Call("closest", "#agent-portable-import").Truthy() {
		updatePortableImportAvailability()
	}
	if target.Truthy() && target.Get("dataset").Get("rolloutInstallation").Type() == js.TypeString {
		syncRolloutCanaries()
		return
	}
	if target.Truthy() && target.Get("id").String() == "agent-rollout-version" {
		updateAgentRolloutTarget()
		return
	}
	if !target.Truthy() || target.Get("id").String() != "agent-rollout-persona" {
		return
	}
	persona := target.Get("value").String()
	for _, selector := range []string{"#agent-rollout-version option", "#agent-rollout-installations input[data-rollout-installation]", "#agent-rollout-canaries input[data-rollout-canary]"} {
		nodes := js.Global().Get("document").Call("querySelectorAll", selector)
		for i := 0; i < nodes.Get("length").Int(); i++ {
			n := nodes.Index(i)
			hidden := persona != "" && domDataset(n, "personaId") != persona
			if n.Get("tagName").String() == "INPUT" {
				n.Get("parentElement").Set("hidden", hidden)
				if hidden {
					n.Set("checked", false)
				}
			} else {
				n.Set("hidden", hidden)
				n.Set("disabled", hidden)
			}
		}
	}
	versions := js.Global().Get("document").Call("querySelectorAll", "#agent-rollout-version option")
	for i := 0; i < versions.Get("length").Int(); i++ {
		if !versions.Index(i).Get("disabled").Bool() && domDataset(versions.Index(i), "current") == "true" {
			versions.Index(i).Set("selected", true)
			break
		}
	}
	updateAgentRolloutTarget()
}

func syncRolloutCanaries() {
	selectedRows := js.Global().Get("document").Call("querySelectorAll", "input[data-rollout-installation]")
	selectedIDs := make(map[string]bool, selectedRows.Get("length").Int())
	selectedCount := 0
	for index := 0; index < selectedRows.Get("length").Int(); index++ {
		input := selectedRows.Index(index)
		selected := input.Get("checked").Bool() && !input.Get("parentElement").Get("hidden").Bool()
		selectedIDs[input.Get("value").String()] = selected
		if selected {
			selectedCount++
		}
	}
	rows := js.Global().Get("document").Call("querySelectorAll", "[data-rollout-canary-row]")
	visible := false
	anyChecked := false
	firstShown := js.Undefined()
	for index := 0; index < rows.Get("length").Int(); index++ {
		row := rows.Index(index)
		id := domDataset(row, "rolloutCanaryRow")
		input := row.Call("querySelector", "input[data-rollout-canary]")
		show := selectedCount >= 2 && selectedIDs[id]
		visible = visible || show
		row.Set("hidden", !show)
		if input.Truthy() {
			input.Set("disabled", !show)
			if !show {
				input.Set("checked", false)
			} else {
				if firstShown.IsUndefined() {
					firstShown = input
				}
				anyChecked = anyChecked || input.Get("checked").Bool()
			}
		}
	}
	// A staged rollout always has a conversation that is updated first. The
	// first selected one is ticked by default so the choice is visible and a
	// preview is never sent without it.
	if !anyChecked && !firstShown.IsUndefined() {
		firstShown.Set("checked", true)
	}
	empty := js.Global().Get("document").Call("querySelector", "[data-rollout-canary-empty]")
	if empty.Truthy() {
		empty.Set("hidden", selectedCount >= 2 && visible)
	}
	containers := js.Global().Get("document").Call("querySelectorAll", "[data-rollout-canary-container]")
	for index := 0; index < containers.Get("length").Int(); index++ {
		containers.Index(index).Set("hidden", selectedCount < 2)
	}
}

func handleAgentOperationsTabClick(event js.Value) {
	tab := event.Get("target").Call("closest", "[data-agent-operations-tab]")
	if !tab.Truthy() {
		return
	}
	event.Call("preventDefault")
	activateAgentOperationsTab(tab, true)
}

func handleAgentOperationsTabKeydown(event js.Value) {
	tab := event.Get("target").Call("closest", "[data-agent-operations-tab]")
	if !tab.Truthy() {
		return
	}
	key := event.Get("key").String()
	tabs := js.Global().Get("document").Call("querySelectorAll", "[data-agent-operations-tab]")
	length := tabs.Get("length").Int()
	if length == 0 {
		return
	}
	current := 0
	for index := 0; index < length; index++ {
		if tabs.Index(index).Equal(tab) {
			current = index
			break
		}
	}
	next := current
	switch key {
	case "ArrowRight", "ArrowDown":
		next = (current + 1) % length
	case "ArrowLeft", "ArrowUp":
		next = (current + length - 1) % length
	case "Home":
		next = 0
	case "End":
		next = length - 1
	default:
		return
	}
	event.Call("preventDefault")
	activateAgentOperationsTab(tabs.Index(next), true)
	tabs.Index(next).Call("focus")
}

func activateAgentOperationsTab(tab js.Value, push bool) {
	if !tab.Truthy() {
		return
	}
	href := tab.Get("href").String()
	if push {
		js.Global().Get("history").Call("pushState", nil, "", href)
	}
	selectAgentOperationsTab()
}

func selectAgentOperationsTab() {
	if !agentOperationsRoute(js.Global().Get("location").Get("pathname").String()) {
		return
	}
	selected := agentOperationsSelectedTab(strings.TrimPrefix(js.Global().Get("location").Get("search").String(), "?"))
	tabs := js.Global().Get("document").Call("querySelectorAll", "[data-agent-operations-tab]")
	for index := 0; index < tabs.Get("length").Int(); index++ {
		tab := tabs.Index(index)
		active := domDataset(tab, "agentOperationsTab") == selected
		tab.Call("setAttribute", "aria-selected", strconv.FormatBool(active))
		tab.Call("setAttribute", "tabindex", map[bool]string{true: "0", false: "-1"}[active])
	}
	panels := js.Global().Get("document").Call("querySelectorAll", "[data-agent-operations-panel]")
	for index := 0; index < panels.Get("length").Int(); index++ {
		panel := panels.Index(index)
		panel.Set("hidden", domDataset(panel, "agentOperationsPanel") != selected)
	}
}

func updatePortableDefinition(body string, rerender bool) {
	fields, err := portableDefinitionMappingFields([]byte(body))
	if err != nil {
		setPortableStatus("invalid")
		updatePortableImportAvailability()
		return
	}
	rolloutPortableBrowser.Lock()
	s := rolloutPortableBrowser.snapshot
	s.Portable.Definition = body
	s.Portable.Destinations = fields
	rolloutPortableBrowser.snapshot = s
	mount := rolloutPortableBrowser.mount
	rolloutPortableBrowser.Unlock()
	if rerender {
		renderRolloutPortable(mount, s)
		setPortableStatus("mapping_required")
	}
	updatePortableImportAvailability()
}

func updatePortableImportAvailability() {
	form := js.Global().Get("document").Call("getElementById", "agent-portable-import")
	if !form.Truthy() {
		return
	}
	target := form.Call("querySelector", "#agent-portable-target")
	valid := target.Truthy() && target.Get("selectedOptions").Get("length").Int() > 0
	mappings := form.Call("querySelectorAll", "[data-destination-id]")
	for i := 0; valid && i < mappings.Get("length").Int(); i++ {
		valid = strings.TrimSpace(mappings.Index(i).Get("value").String()) != ""
	}
	button := form.Call("querySelector", `button[type="submit"]`)
	if button.Truthy() {
		button.Set("disabled", !valid)
		button.Get("classList").Call("toggle", "primary", valid)
		button.Get("classList").Call("toggle", "secondary", !valid)
	}
}
func findRolloutPortableMount() {
	if !agentOperationsRoute(js.Global().Get("location").Get("pathname").String()) {
		rolloutPortableBrowser.Lock()
		rolloutPortableBrowser.mount = js.Undefined()
		rolloutPortableBrowser.Unlock()
		return
	}
	m := js.Global().Get("document").Call("getElementById", "agent-rollout-portable")
	if !m.Truthy() {
		return
	}
	rolloutPortableBrowser.Lock()
	locale := domAttribute(m, "data-locale")
	if rolloutPortableBrowser.mount.Truthy() && rolloutPortableBrowser.mount.Equal(m) && rolloutPortableBrowser.locale == locale {
		rolloutPortableBrowser.Unlock()
		return
	}
	rolloutPortableBrowser.mount = m
	rolloutPortableBrowser.locale = locale
	s := rolloutPortableBrowser.snapshot
	rolloutPortableBrowser.Unlock()
	if s.Available {
		renderRolloutPortable(m, s)
		return
	}
	go fetchRolloutCatalog(m)
}
func rolloutEndpoint(cfg journeyclient.Config, path string) (string, error) {
	u, e := url.Parse(cfg.TunnelURL)
	if e != nil || u.Host == "" || cfg.Bearer == "" {
		return "", errAgentRolloutInput
	}
	if u.Scheme == "ws" {
		u.Scheme = "http"
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	}
	u.Path = path
	u.RawQuery = ""
	return u.String(), nil
}
func postRollout(ctx context.Context, cfg journeyclient.Config, path string, body any, out any) error {
	endpoint, e := rolloutEndpoint(cfg, path)
	if e != nil {
		return e
	}
	b, e := json.Marshal(body)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return rolloutHTTPError{status: res.StatusCode}
	}
	return json.NewDecoder(res.Body).Decode(out)
}

type rolloutHTTPError struct{ status int }

func (e rolloutHTTPError) Error() string { return "rollout http status " + strconv.Itoa(e.status) }

func postPortable(ctx context.Context, cfg journeyclient.Config, path string, body []byte) ([]byte, error) {
	endpoint, err := rolloutEndpoint(cfg, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Bearer)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, rolloutHTTPError{status: res.StatusCode}
	}
	var raw json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func fetchRolloutCatalog(mount js.Value) {
	rolloutPortableBrowser.Lock()
	cfg := rolloutPortableBrowser.config
	rolloutPortableBrowser.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var reply rolloutCatalogReply
	err := postRollout(ctx, cfg, "/api/agents/rollouts", map[string]any{"action": "CATALOG"}, &reply)
	s := productui.AgentRolloutSnapshot{Available: err == nil, CanPreview: err == nil}
	query := js.Global().Get("URLSearchParams").New(js.Global().Get("location").Get("search"))
	if agent := query.Call("get", "agent"); !agent.IsNull() {
		s.PersonaID = agent.String()
	}
	if version := query.Call("get", "version"); !version.IsNull() {
		s.TargetVersion, _ = strconv.ParseInt(version.String(), 10, 64)
	}
	// Portable authoring is independent of published rollout installations.
	// The server supplies its current action permissions and definition pins.
	_ = postRollout(ctx, cfg, "/api/agents/portable/catalog", struct{}{}, &s.Portable)
	seenPersonas := map[string]bool{}
	for _, v := range reply.Catalog.Versions {
		if !seenPersonas[v.PersonaID] {
			s.Personas = append(s.Personas, productui.AgentRolloutPersona{ID: v.PersonaID, Name: v.Name})
			seenPersonas[v.PersonaID] = true
		}
		s.Versions = append(s.Versions, productui.AgentRolloutVersion{PersonaID: v.PersonaID, Version: v.Version, PublishedAt: v.PublishedAt, Current: v.Current, Digest: v.ProfileDigest, ProfileDigest: v.ProfileDigest})
	}
	for i := range s.Portable.Manifests {
		manifest := &s.Portable.Manifests[i]
		for _, version := range reply.Catalog.Versions {
			if manifest.ID == version.ManifestID && manifest.Version == strconv.FormatUint(version.ManifestVersion, 10) && version.Current {
				manifest.Name = version.Name
				manifest.PersonaVersion = strconv.FormatInt(version.Version, 10)
				manifest.Live = true
				break
			}
		}
	}
	s.NotListedCount = reply.Catalog.NotListedCount
	for _, i := range reply.Catalog.Installations {
		s.Installations = append(s.Installations, productui.AgentRolloutInstallation{ID: i.ID, PersonaID: i.PersonaID, ConversationID: i.ConversationID, Name: i.Name, ConversationKind: i.Kind, MemberCount: i.MemberCount, Visible: i.Visible, Version: i.Version, Revision: i.Revision, UpdateBlocked: i.UpdateBlocked, CanaryEligible: true})
	}
	rolloutPortableBrowser.Lock()
	if rolloutPortableBrowser.config.Bearer != cfg.Bearer {
		rolloutPortableBrowser.Unlock()
		return
	}
	rolloutPortableBrowser.snapshot = s
	rolloutPortableBrowser.Unlock()
	ui.PostAsync(func() { renderRolloutPortable(mount, s) })
}
func renderRolloutPortable(mount js.Value, s productui.AgentRolloutSnapshot) {
	if !mount.Truthy() || !mount.Get("isConnected").Bool() {
		return
	}
	loc := productui.ResolveProductLocale(domAttribute(mount, "data-locale")).WithTimeZone(agentViewerTimeZone())
	markup, e := ui.RenderToString(productui.AgentRolloutPortableMount(loc, s, s.Portable))
	if e != nil {
		return
	}
	tmp := js.Global().Get("document").Call("createElement", "div")
	tmp.Set("innerHTML", markup)
	child := tmp.Get("firstElementChild")
	if child.Truthy() {
		mount.Call("replaceWith", child)
		rolloutPortableBrowser.Lock()
		rolloutPortableBrowser.mount = child
		rolloutPortableBrowser.Unlock()
		persona := child.Call("querySelector", "#agent-rollout-persona")
		if persona.Truthy() {
			event := js.Global().Get("Object").New()
			if s.TargetVersion == 0 {
				event.Set("target", persona)
				filterRolloutPersona(event)
			} else {
				hideOtherRolloutPersonas(persona.Get("value").String())
			}
		}
		selectAgentOperationsTab()
	}
}

func setRolloutStatus(message string) {
	status := js.Global().Get("document").Call("getElementById", "agent-rollout-status")
	if status.Truthy() {
		locale := domAttribute(js.Global().Get("document").Call("getElementById", "agent-rollout-portable"), "data-locale")
		status.Set("textContent", productui.AgentRolloutPortableStatus(productui.ResolveProductLocale(locale), message))
	}
}

func setPortableStatus(message string) {
	status := js.Global().Get("document").Call("getElementById", "agent-portable-status")
	if status.Truthy() {
		locale := domAttribute(js.Global().Get("document").Call("getElementById", "agent-rollout-portable"), "data-locale")
		status.Set("textContent", productui.AgentRolloutPortableStatus(productui.ResolveProductLocale(locale), message))
	}
}

func rolloutStatusForError(err error) string {
	if httpErr, ok := err.(rolloutHTTPError); ok {
		switch httpErr.status {
		case http.StatusBadRequest:
			return "evidence_missing"
		case http.StatusForbidden:
			return "not_allowed"
		case http.StatusConflict:
			return "stale_preview"
		case http.StatusNotFound, http.StatusServiceUnavailable:
			return "unavailable"
		}
	}
	return "refused"
}

func restoreBusyButton(button js.Value, label string, focus bool) {
	if !button.Truthy() || !button.Get("isConnected").Bool() {
		return
	}
	button.Set("disabled", false)
	button.Call("removeAttribute", "aria-busy")
	if label != "" {
		button.Set("textContent", label)
	}
	if focus {
		button.Call("focus")
	}
}

func handleRolloutSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() || form.Get("id").String() != "agent-rollout-preview" {
		return
	}
	event.Call("preventDefault")
	rolloutPortableBrowser.Lock()
	if rolloutPortableBrowser.busy {
		rolloutPortableBrowser.Unlock()
		return
	}
	cfg := rolloutPortableBrowser.config
	rolloutPortableBrowser.busy = true
	rolloutPortableBrowser.Unlock()
	submit := form.Call("querySelector", `button[type="submit"]`)
	submitLabel := ""
	if submit.Truthy() {
		submitLabel = submit.Get("textContent").String()
		submit.Set("disabled", true)
		submit.Call("setAttribute", "aria-busy", "true")
		if busyLabel := submit.Get("dataset").Get("busyLabel"); busyLabel.Type() == js.TypeString {
			submit.Set("textContent", busyLabel.String())
		}
	}
	setRolloutStatus("working")
	ids := form.Call("querySelectorAll", "input[data-rollout-installation]:checked")
	var installs, canaries []string
	for i := 0; i < ids.Get("length").Int(); i++ {
		installs = append(installs, ids.Index(i).Get("value").String())
	}
	cs := form.Call("querySelectorAll", "input[data-rollout-canary]:checked")
	for i := 0; i < cs.Get("length").Int(); i++ {
		canaries = append(canaries, cs.Index(i).Get("value").String())
	}
	canaries = rolloutPreviewFirstConversations(installs, canaries)
	batch, _ := strconv.Atoi(form.Call("querySelector", "#agent-rollout-batch").Get("value").String())
	ver, _ := strconv.ParseInt(form.Call("querySelector", "#agent-rollout-version").Get("value").String(), 10, 64)
	persona := form.Call("querySelector", "#agent-rollout-persona").Get("value").String()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var out rolloutReceiptReply
		err := postRollout(ctx, cfg, "/api/agents/rollouts", map[string]any{"action": "PREVIEW", "persona_id": persona, "target_version": ver, "installation_ids": installs, "canary_ids": canaries, "batch_limit": batch}, &out)
		ui.PostAsync(func() {
			rolloutPortableBrowser.Lock()
			rolloutPortableBrowser.busy = false
			s := rolloutPortableBrowser.snapshot
			if err == nil {
				s.PersonaID = persona
				s.Active = &productui.AgentRolloutPlan{ID: out.Plan.ID, Digest: out.Plan.Digest, Version: out.Plan.Version, ProfileDigest: out.Plan.ProfileDigest, CanaryCount: out.Plan.CanaryCount}
				for _, c := range out.Plan.Candidates {
					s.Active.Candidates = append(s.Active.Candidates, productui.AgentRolloutCandidate{InstallationID: c.InstallationID, ConversationID: c.ConversationID, Version: c.Version, Revision: c.Revision, RevocationEpoch: c.RevocationEpoch, AuthorityRevision: c.AuthorityRevision, PolicyDigest: c.PolicyDigest})
				}
				s.Progress = &productui.AgentRolloutProgress{Revision: out.Progress.Revision, Cursor: out.Progress.Cursor, Stage: out.Progress.Stage, ApproverID: out.Progress.ApproverID}
				s.CanApprove = out.Progress.Stage == "PREVIEWED"
				s.CanPromote = out.Progress.Stage == "CANARY_COMPLETE"
				s.CanAdvance = out.Progress.Stage == "APPROVED"
				rolloutPortableBrowser.snapshot = s
			}
			rolloutPortableBrowser.Unlock()
			if err == nil {
				renderRolloutPortable(rolloutPortableBrowser.mount, s)
				setRolloutStatus("rollout_previewed")
			} else {
				// A refused preview leaves the form in place; without this the
				// button stayed disabled on "Working…" and nothing could be retried.
				restoreBusyButton(submit, submitLabel, true)
				setRolloutStatus(rolloutStatusForError(err))
			}
		})
	}()
}
func handleRolloutClick(event js.Value) {
	button := event.Get("target").Call("closest", "button[data-rollout-action],button[data-portable-copy]")
	if !button.Truthy() {
		return
	}
	if button.Get("dataset").Get("portableCopy").Type() == js.TypeString {
		event.Call("preventDefault")
		rolloutPortableBrowser.Lock()
		definition := rolloutPortableBrowser.snapshot.Portable.Definition
		rolloutPortableBrowser.Unlock()
		area := js.Global().Get("document").Call("createElement", "textarea")
		area.Set("value", definition)
		area.Get("style").Set("position", "fixed")
		area.Get("style").Set("opacity", "0")
		js.Global().Get("document").Get("body").Call("appendChild", area)
		area.Call("select")
		copied := js.Global().Get("document").Call("execCommand", "copy").Bool()
		area.Call("remove")
		if copied {
			setPortableStatus("copied")
		} else {
			setPortableStatus("copy_failed")
		}
		return
	}
	action := strings.ToUpper(domDataset(button, "rolloutAction"))
	if action == "REFRESH" {
		event.Call("preventDefault")
		rolloutPortableBrowser.Lock()
		mount := rolloutPortableBrowser.mount
		rolloutPortableBrowser.Unlock()
		setRolloutStatus("loading")
		go fetchRolloutCatalog(mount)
		return
	}
	if action == "PREVIEW" {
		return
	}
	if confirmation := button.Get("dataset").Get("confirm"); confirmation.Type() == js.TypeString && !rolloutConfirmedInPage(confirmation.String(), button) {
		event.Call("preventDefault")
		return
	}
	event.Call("preventDefault")
	buttonLabel := button.Get("textContent").String()
	button.Set("disabled", true)
	button.Call("setAttribute", "aria-busy", "true")
	if busyLabel := button.Get("dataset").Get("busyLabel"); busyLabel.Type() == js.TypeString {
		button.Set("textContent", busyLabel.String())
	}
	setRolloutStatus("working")
	rolloutPortableBrowser.Lock()
	cfg := rolloutPortableBrowser.config
	s := rolloutPortableBrowser.snapshot
	rolloutPortableBrowser.Unlock()
	rev, _ := strconv.ParseInt(domDataset(button, "rolloutRevision"), 10, 64)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var out rolloutReceiptReply
		err := postRollout(ctx, cfg, "/api/agents/rollouts", map[string]any{"action": action, "rollout_id": domDataset(button, "rolloutId"), "digest": domDataset(button, "rolloutDigest"), "revision": rev}, &out)
		ui.PostAsync(func() {
			if err != nil {
				restoreBusyButton(button, buttonLabel, true)
				setRolloutStatus(rolloutStatusForError(err))
				return
			}
			s.Progress = &productui.AgentRolloutProgress{Revision: out.Progress.Revision, Cursor: out.Progress.Cursor, Stage: out.Progress.Stage, ApproverID: out.Progress.ApproverID}
			s.CanApprove = out.Progress.Stage == "PREVIEWED"
			s.CanPromote = out.Progress.Stage == "CANARY_COMPLETE"
			s.CanAdvance = out.Progress.Stage == "APPROVED"
			rolloutPortableBrowser.Lock()
			rolloutPortableBrowser.snapshot = s
			rolloutPortableBrowser.Unlock()
			renderRolloutPortable(rolloutPortableBrowser.mount, s)
			setRolloutStatus("rollout_updated")
		})
	}()
}

func handlePortableSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() {
		return
	}
	action := domDataset(form, "portableAction")
	if action == "" {
		return
	}
	event.Call("preventDefault")
	submit := form.Call("querySelector", `button[type="submit"]`)
	if confirmation := form.Get("dataset").Get("confirm"); confirmation.Type() == js.TypeString && !rolloutConfirmedInPage(confirmation.String(), submit) {
		return
	}
	submitLabel := ""
	if submit.Truthy() {
		submitLabel = submit.Get("textContent").String()
		submit.Set("disabled", true)
		submit.Call("setAttribute", "aria-busy", "true")
		if busyLabel := submit.Get("dataset").Get("busyLabel"); busyLabel.Type() == js.TypeString {
			submit.Set("textContent", busyLabel.String())
		}
	}
	setPortableStatus("working")
	rolloutPortableBrowser.Lock()
	cfg := rolloutPortableBrowser.config
	rolloutPortableBrowser.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	go func() {
		defer cancel()
		var body []byte
		if action == "read" {
			id := strings.TrimSpace(domDataset(submit, "definitionId"))
			body, _ = json.Marshal(map[string]string{"definition_id": id})
			data, err := postPortable(ctx, cfg, "/api/agents/portable/draft", body)
			if err != nil {
				ui.PostAsync(func() {
					restoreBusyButton(submit, submitLabel, true)
					setPortableStatus(rolloutStatusForError(err))
				})
				return
			}
			ui.PostAsync(func() { displayPortableDraft(data, "draft_loaded") })
			return
		}
		if action == "export" {
			selector := form.Call("querySelector", "#agent-portable-manifest")
			manifest := selector.Get("value").String()
			version, _ := strconv.ParseUint(form.Call("querySelector", "#agent-portable-manifest-version").Get("value").String(), 10, 64)
			if version == 0 && selector.Get("selectedOptions").Get("length").Int() > 0 {
				version, _ = strconv.ParseUint(domDataset(selector.Get("selectedOptions").Index(0), "manifestVersion"), 10, 64)
			}
			body, _ = json.Marshal(map[string]any{"manifest_id": manifest, "manifest_version": version})
			data, err := postPortable(ctx, cfg, "/api/agents/portable/export", body)
			if err != nil {
				ui.PostAsync(func() {
					restoreBusyButton(submit, submitLabel, true)
					setPortableStatus(rolloutStatusForError(err))
				})
				return
			}
			fields, err := portableDefinitionMappingFields(data)
			if err != nil {
				ui.PostAsync(func() {
					restoreBusyButton(submit, submitLabel, true)
					setPortableStatus("invalid")
				})
				return
			}
			ui.PostAsync(func() {
				downloadPortableDefinition(data)
				rolloutPortableBrowser.Lock()
				s := rolloutPortableBrowser.snapshot
				s.Portable.Definition = string(data)
				s.Portable.Destinations = fields
				rolloutPortableBrowser.snapshot = s
				rolloutPortableBrowser.Unlock()
				renderRolloutPortable(rolloutPortableBrowser.mount, s)
				setPortableStatus("exported")
			})
			return
		}
		definition := form.Call("querySelector", "#agent-portable-body").Get("value").String()
		if strings.TrimSpace(definition) == "" {
			ui.PostAsync(func() {
				restoreBusyButton(submit, submitLabel, true)
				setPortableStatus("invalid")
			})
			return
		}
		var raw json.RawMessage
		if json.Unmarshal([]byte(definition), &raw) != nil {
			ui.PostAsync(func() {
				restoreBusyButton(submit, submitLabel, true)
				setPortableStatus("invalid")
			})
			return
		}
		target := form.Call("querySelector", "#agent-portable-target")
		destination := target.Get("value").String()
		if target.Get("selectedOptions").Get("length").Int() == 0 {
			ui.PostAsync(func() {
				restoreBusyButton(submit, submitLabel, true)
				setPortableStatus("mapping_required")
			})
			return
		}
		version, _ := strconv.ParseUint(domDataset(target.Get("selectedOptions").Index(0), "manifestVersion"), 10, 64)
		mappingNodes := form.Call("querySelectorAll", "[data-destination-id]")
		mappings := make([]map[string]string, 0, mappingNodes.Get("length").Int())
		for i := 0; i < mappingNodes.Get("length").Int(); i++ {
			node := mappingNodes.Index(i)
			value := strings.TrimSpace(node.Get("value").String())
			if value == "" {
				continue
			}
			ds := node.Get("dataset")
			mappings = append(mappings, map[string]string{"kind": ds.Get("mappingKind").String(), "source_id": ds.Get("sourceId").String(), "destination_id": value})
		}
		if mappingNodes.Get("length").Int() > 0 && len(mappings) != mappingNodes.Get("length").Int() {
			ui.PostAsync(func() {
				restoreBusyButton(submit, submitLabel, true)
				setPortableStatus("mapping_required")
			})
			return
		}
		wrapper, _ := json.Marshal(map[string]any{"definition": raw, "destination_manifest_id": destination, "destination_manifest_version": version, "mappings": mappings})
		data, err := postPortable(ctx, cfg, "/api/agents/portable/import", wrapper)
		if err != nil {
			ui.PostAsync(func() {
				restoreBusyButton(submit, submitLabel, true)
				setPortableStatus(rolloutStatusForError(err))
			})
			return
		}
		ui.PostAsync(func() {
			displayPortableDraft(data, "imported")
		})
	}()
}

func downloadPortableDefinition(content []byte) {
	blob := js.Global().Get("Blob").New([]any{string(content)}, map[string]any{"type": "application/json"})
	url := js.Global().Get("URL").Call("createObjectURL", blob)
	anchor := js.Global().Get("document").Call("createElement", "a")
	anchor.Set("href", url)
	anchor.Set("download", "agent.json")
	anchor.Call("click")
	js.Global().Get("URL").Call("revokeObjectURL", url)
}

func displayPortableDraft(data []byte, message string) {
	var draft struct {
		State        string `json:"state"`
		Instructions string `json:"instructions"`
		Manifest     struct {
			ID      string `json:"id"`
			Purpose string `json:"purpose"`
			Version uint64 `json:"version"`
		} `json:"manifest"`
	}
	if json.Unmarshal(data, &draft) != nil || draft.State != "DRAFT" || draft.Manifest.ID == "" || draft.Instructions == "" {
		setPortableStatus("refused")
		return
	}
	rolloutPortableBrowser.Lock()
	cfg := rolloutPortableBrowser.config
	rolloutPortableBrowser.Unlock()
	personaAdminBrowser.Lock()
	importedBy := ""
	if personaAdminBrowser.cfg.Tenant == cfg.Tenant && personaAdminBrowser.cfg.Subject == cfg.Subject {
		importedBy = agentUXR7ImportedBy(personaAdminBrowser.snapshot, cfg.Subject)
	}
	personaAdminBrowser.Unlock()
	rolloutPortableBrowser.Lock()
	s := rolloutPortableBrowser.snapshot
	item := productui.AgentPortableReviewDraft{ID: draft.Manifest.ID, Name: "", Purpose: draft.Manifest.Purpose, Version: draft.Manifest.Version, Instructions: draft.Instructions}
	if message == "imported" {
		item.ImportedAt = js.Global().Get("Date").New().Call("toISOString").String()
		item.ImportedBy = importedBy
		if selectNode := js.Global().Get("document").Call("getElementById", "agent-portable-target"); selectNode.Truthy() && selectNode.Get("selectedOptions").Get("length").Int() > 0 {
			item.Name = strings.Split(selectNode.Get("selectedOptions").Index(0).Get("textContent").String(), " · ")[0]
		}
	}
	s.Portable.Draft = &item
	found := false
	for i := range s.Portable.Drafts {
		if s.Portable.Drafts[i].ID == item.ID {
			item = agentUXR7PreserveDraftMetadata(s.Portable.Drafts[i], item)
			s.Portable.Drafts[i] = item
			found = true
		}
	}
	if !found {
		s.Portable.Drafts = append(s.Portable.Drafts, item)
	}
	rolloutPortableBrowser.snapshot = s
	mount := rolloutPortableBrowser.mount
	rolloutPortableBrowser.Unlock()
	renderRolloutPortable(mount, s)
	setPortableStatus(message)
}

func hideOtherRolloutPersonas(persona string) {
	nodes := js.Global().Get("document").Call("querySelectorAll", "#agent-rollout-version option,input[data-rollout-installation],input[data-rollout-canary]")
	for i := 0; i < nodes.Get("length").Int(); i++ {
		n := nodes.Index(i)
		hidden := domDataset(n, "personaId") != persona
		if n.Get("tagName").String() == "INPUT" {
			n.Get("parentElement").Set("hidden", hidden)
		} else {
			n.Set("hidden", hidden)
			n.Set("disabled", hidden)
		}
	}
}
func updateAgentRolloutTarget() {
	document := js.Global().Get("document")
	form := document.Call("getElementById", "agent-rollout-preview")
	if !form.Truthy() {
		return
	}
	persona := form.Call("querySelector", "#agent-rollout-persona").Get("value").String()
	version, _ := strconv.ParseInt(form.Call("querySelector", "#agent-rollout-version").Get("value").String(), 10, 64)
	rolloutPortableBrowser.Lock()
	snapshot := rolloutPortableBrowser.snapshot
	snapshot.PersonaID = persona
	snapshot.TargetVersion = version
	rolloutPortableBrowser.snapshot = snapshot
	rolloutPortableBrowser.Unlock()
	locale := productui.ResolveProductLocale(domAttribute(document.Call("getElementById", "agent-rollout-portable"), "data-locale")).WithTimeZone(agentViewerTimeZone())
	markup, err := ui.RenderToString(productui.RenderAgentRolloutForm(locale, snapshot))
	if err == nil {
		form.Set("outerHTML", markup)
		hideOtherRolloutPersonas(persona)
		syncRolloutCanaries()
	}
}
