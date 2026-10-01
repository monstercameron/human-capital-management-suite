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
		} `json:"versions"`
		Installations []struct {
			ID             string `json:"id"`
			PersonaID      string `json:"persona_id"`
			ConversationID string `json:"conversation_id"`
			Version        int64  `json:"version"`
			Revision       int64  `json:"revision"`
		} `json:"installations"`
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
				handleRolloutClick(args[0])
			}
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
}

func filterRolloutPersona(event js.Value) {
	target := event.Get("target")
	if target.Truthy() && target.Get("id").String() == "agent-portable-body" {
		body := target.Get("value").String()
		fields, err := portableDefinitionMappingFields([]byte(body))
		if err != nil {
			setRolloutStatus("invalid")
			return
		}
		rolloutPortableBrowser.Lock()
		s := rolloutPortableBrowser.snapshot
		s.Portable.Definition = body
		s.Portable.Destinations = fields
		rolloutPortableBrowser.snapshot = s
		mount := rolloutPortableBrowser.mount
		rolloutPortableBrowser.Unlock()
		if event.Get("type").String() == "change" {
			renderRolloutPortable(mount, s)
			setRolloutStatus("mapping_required")
		}
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
			hidden := persona != "" && n.Get("dataset").Get("personaId").String() != persona
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
		if !versions.Index(i).Get("disabled").Bool() {
			versions.Index(i).Set("selected", true)
			break
		}
	}
}
func findRolloutPortableMount() {
	m := js.Global().Get("document").Call("getElementById", "agent-rollout-portable")
	if !m.Truthy() {
		return
	}
	rolloutPortableBrowser.Lock()
	locale := m.Call("getAttribute", "data-locale").String()
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
		return errAgentRolloutInput
	}
	return json.NewDecoder(res.Body).Decode(out)
}

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
		return nil, errAgentRolloutInput
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
	// Portable authoring is independent of published rollout installations.
	// The server supplies its current action permissions and definition pins.
	_ = postRollout(ctx, cfg, "/api/agents/portable/catalog", struct{}{}, &s.Portable)
	seenPersonas := map[string]bool{}
	for _, v := range reply.Catalog.Versions {
		if !seenPersonas[v.PersonaID] {
			s.Personas = append(s.Personas, productui.AgentRolloutPersona{ID: v.PersonaID, Name: v.Name})
			seenPersonas[v.PersonaID] = true
		}
		s.Versions = append(s.Versions, productui.AgentRolloutVersion{PersonaID: v.PersonaID, Version: v.Version, Digest: v.ProfileDigest, ProfileDigest: v.ProfileDigest})
	}
	for _, i := range reply.Catalog.Installations {
		s.Installations = append(s.Installations, productui.AgentRolloutInstallation{ID: i.ID, PersonaID: i.PersonaID, ConversationID: i.ConversationID, Version: i.Version, Revision: i.Revision, CanaryEligible: true})
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
	loc := productui.ResolveProductLocale(mount.Call("getAttribute", "data-locale").String())
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
			event.Set("target", persona)
			filterRolloutPersona(event)
		}
	}
}

func setRolloutStatus(message string) {
	status := js.Global().Get("document").Call("getElementById", "agent-rollout-status")
	if status.Truthy() {
		locale := js.Global().Get("document").Call("getElementById", "agent-rollout-portable").Call("getAttribute", "data-locale").String()
		status.Set("textContent", productui.AgentRolloutPortableStatus(productui.ResolveProductLocale(locale), message))
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
	ids := form.Call("querySelectorAll", "input[data-rollout-installation]:checked")
	var installs, canaries []string
	for i := 0; i < ids.Get("length").Int(); i++ {
		installs = append(installs, ids.Index(i).Get("value").String())
	}
	cs := form.Call("querySelectorAll", "input[data-rollout-canary]:checked")
	for i := 0; i < cs.Get("length").Int(); i++ {
		canaries = append(canaries, cs.Index(i).Get("value").String())
	}
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
			} else {
				setRolloutStatus("refused")
			}
		})
	}()
}
func handleRolloutClick(event js.Value) {
	button := event.Get("target").Call("closest", "button[data-rollout-action]")
	if !button.Truthy() {
		return
	}
	action := strings.ToUpper(button.Get("dataset").Get("rolloutAction").String())
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
	event.Call("preventDefault")
	rolloutPortableBrowser.Lock()
	cfg := rolloutPortableBrowser.config
	s := rolloutPortableBrowser.snapshot
	rolloutPortableBrowser.Unlock()
	rev, _ := strconv.ParseInt(button.Get("dataset").Get("rolloutRevision").String(), 10, 64)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var out rolloutReceiptReply
		err := postRollout(ctx, cfg, "/api/agents/rollouts", map[string]any{"action": action, "rollout_id": button.Get("dataset").Get("rolloutId").String(), "digest": button.Get("dataset").Get("rolloutDigest").String(), "revision": rev}, &out)
		ui.PostAsync(func() {
			if err != nil {
				setRolloutStatus("refused")
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
		})
	}()
}

func handlePortableSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() {
		return
	}
	action := form.Get("dataset").Get("portableAction").String()
	if action == "" {
		return
	}
	event.Call("preventDefault")
	rolloutPortableBrowser.Lock()
	cfg := rolloutPortableBrowser.config
	rolloutPortableBrowser.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	go func() {
		defer cancel()
		var body []byte
		if action == "read" {
			id := strings.TrimSpace(form.Call("querySelector", "#agent-portable-draft-id").Get("value").String())
			body, _ = json.Marshal(map[string]string{"definition_id": id})
			data, err := postPortable(ctx, cfg, "/api/agents/portable/draft", body)
			if err != nil {
				ui.PostAsync(func() { setRolloutStatus("refused") })
				return
			}
			ui.PostAsync(func() { displayPortableDraft(data) })
			return
		}
		if action == "export" {
			selector := form.Call("querySelector", "#agent-portable-manifest")
			manifest := selector.Get("value").String()
			version, _ := strconv.ParseUint(form.Call("querySelector", "#agent-portable-manifest-version").Get("value").String(), 10, 64)
			if version == 0 && selector.Get("selectedOptions").Get("length").Int() > 0 {
				version, _ = strconv.ParseUint(selector.Get("selectedOptions").Index(0).Get("dataset").Get("manifestVersion").String(), 10, 64)
			}
			body, _ = json.Marshal(map[string]any{"manifest_id": manifest, "manifest_version": version})
			data, err := postPortable(ctx, cfg, "/api/agents/portable/export", body)
			if err != nil {
				ui.PostAsync(func() { setRolloutStatus("refused") })
				return
			}
			fields, err := portableDefinitionMappingFields(data)
			if err != nil {
				ui.PostAsync(func() { setRolloutStatus("invalid") })
				return
			}
			ui.PostAsync(func() {
				rolloutPortableBrowser.Lock()
				s := rolloutPortableBrowser.snapshot
				s.Portable.Definition = string(data)
				s.Portable.Destinations = fields
				rolloutPortableBrowser.snapshot = s
				rolloutPortableBrowser.Unlock()
				renderRolloutPortable(rolloutPortableBrowser.mount, s)
				setRolloutStatus("exported")
			})
			return
		}
		definition := form.Call("querySelector", "#agent-portable-body").Get("value").String()
		if strings.TrimSpace(definition) == "" {
			ui.PostAsync(func() { setRolloutStatus("invalid") })
			return
		}
		var raw json.RawMessage
		if json.Unmarshal([]byte(definition), &raw) != nil {
			ui.PostAsync(func() { setRolloutStatus("invalid") })
			return
		}
		target := form.Call("querySelector", "#agent-portable-target")
		destination := target.Get("value").String()
		if target.Get("selectedOptions").Get("length").Int() == 0 {
			ui.PostAsync(func() { setRolloutStatus("mapping_required") })
			return
		}
		version, _ := strconv.ParseUint(target.Get("selectedOptions").Index(0).Get("dataset").Get("manifestVersion").String(), 10, 64)
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
			ui.PostAsync(func() { setRolloutStatus("mapping_required") })
			return
		}
		wrapper, _ := json.Marshal(map[string]any{"definition": raw, "destination_manifest_id": destination, "destination_manifest_version": version, "mappings": mappings})
		data, err := postPortable(ctx, cfg, "/api/agents/portable/import", wrapper)
		if err != nil {
			ui.PostAsync(func() { setRolloutStatus("refused") })
			return
		}
		ui.PostAsync(func() {
			displayPortableDraft(data)
		})
	}()
}

func displayPortableDraft(data []byte) {
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
		setRolloutStatus("refused")
		return
	}
	rolloutPortableBrowser.Lock()
	s := rolloutPortableBrowser.snapshot
	s.Portable.Draft = &productui.AgentPortableReviewDraft{ID: draft.Manifest.ID, Purpose: draft.Manifest.Purpose, Version: draft.Manifest.Version, Instructions: draft.Instructions}
	rolloutPortableBrowser.snapshot = s
	mount := rolloutPortableBrowser.mount
	rolloutPortableBrowser.Unlock()
	renderRolloutPortable(mount, s)
	setRolloutStatus("imported")
}
