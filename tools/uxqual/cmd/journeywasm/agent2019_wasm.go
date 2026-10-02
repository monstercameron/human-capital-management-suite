//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"strings"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// ---- the administrator's connection console (Agent operations, Connections) ----

type agentAdminSurface struct {
	mount    js.Value
	locale   string
	loaded   bool
	state    productui.AgentAccessLoadState
	snapshot productui.AgentAdminAccessSnapshot
	message  string
	busy     bool
}

func agentAdminTabActive() bool {
	location := js.Global().Get("location")
	return agentOperationsRoute(location.Get("pathname").String()) && agentOperationsSelectedTab(strings.TrimPrefix(location.Get("search").String(), "?")) == "connections"
}

func findAgentAdminMount() {
	if !agentAdminTabActive() {
		agentAccessBrowser.Lock()
		agentAccessBrowser.admin = agentAdminSurface{}
		agentAccessBrowser.Unlock()
		return
	}
	mount := js.Global().Get("document").Call("getElementById", "agent-admin-access")
	if !mount.Truthy() {
		return
	}
	locale := domAttribute(mount, "data-locale")
	agentAccessBrowser.Lock()
	surface := &agentAccessBrowser.admin
	if surface.mount.Truthy() && surface.mount.Equal(mount) && surface.locale == locale {
		agentAccessBrowser.Unlock()
		return
	}
	*surface = agentAdminSurface{mount: mount, locale: locale, state: productui.AgentAccessStateLoading}
	agentAccessBrowser.Unlock()
	go runAgentAdmin(mount, http.MethodGet, "", nil, "")
}

func renderAgentAdmin(mount js.Value) {
	agentAccessBrowser.Lock()
	surface := agentAccessBrowser.admin
	agentAccessBrowser.Unlock()
	renderAgentMarkup(mount, productui.AgentAdminAccessPage(productui.AgentAdminAccessPageProps{
		I18nProps: productui.I18nProps{Locale: agentMountLocale(mount)}, State: surface.state, Snapshot: surface.snapshot, Client: agentAccessNoop{}, Message: surface.message, Embedded: true,
	}))
}

// runAgentAdmin makes one console request (a read, or an action whose answer is
// the console's new state) and draws the result. A failed request keeps what the
// administrator was looking at and says what happened.
func runAgentAdmin(mount js.Value, method, route string, input any, success string) {
	cfg := agentAccessConfig()
	ctx, cancel := context.WithTimeout(context.Background(), agentAccessCallTimeout)
	defer cancel()
	var snapshot productui.AgentAdminAccessSnapshot
	err := agentAccessCall(ctx, http.DefaultClient, cfg, method, agentAccessAPI+"/console"+route, input, &snapshot)
	ui.PostAsync(func() {
		agentAccessBrowser.Lock()
		surface := &agentAccessBrowser.admin
		if !surface.mount.Truthy() || !surface.mount.Equal(mount) {
			agentAccessBrowser.Unlock()
			return
		}
		surface.busy = false
		switch {
		case err == nil:
			surface.state, surface.snapshot, surface.message, surface.loaded = productui.AgentAccessStateReady, snapshot, success, true
		case surface.loaded:
			surface.message = agentAdminMessageKey(err)
		default:
			surface.state = productui.AgentAccessStateUnavailable
		}
		agentAccessBrowser.Unlock()
		renderAgentAdmin(mount)
	})
}

func agentAdminMountFor(element js.Value) (js.Value, bool) {
	agentAccessBrowser.Lock()
	mount, busy := agentAccessBrowser.admin.mount, agentAccessBrowser.admin.busy
	agentAccessBrowser.Unlock()
	if !mount.Truthy() || !element.Truthy() || !mount.Call("contains", element).Bool() || busy {
		return js.Undefined(), false
	}
	agentAccessBrowser.Lock()
	agentAccessBrowser.admin.busy = true
	agentAccessBrowser.Unlock()
	return mount, true
}

func agentAdminRevision(id string) (productui.AgentConnectionRevision, bool) {
	agentAccessBrowser.Lock()
	defer agentAccessBrowser.Unlock()
	for _, revision := range agentAccessBrowser.admin.snapshot.Revisions {
		if revision.ID == id {
			return revision, true
		}
	}
	return productui.AgentConnectionRevision{}, false
}

func handleAgentAdminClick(event js.Value) {
	button := agentClosest(event, "[data-agent-admin-action]")
	if !button.Truthy() {
		return
	}
	mount, ok := agentAdminMountFor(button)
	if !ok {
		return
	}
	action, revisionID := domAttribute(button, "data-agent-admin-action"), domAttribute(button, "data-revision-id")
	button.Set("disabled", true)
	request := agentAdminRevisionRequest{RevisionID: revisionID}
	switch action {
	case "retry":
		go runAgentAdmin(mount, http.MethodGet, "", nil, "")
	case "request_approval":
		go runAgentAdmin(mount, http.MethodPost, "/request", request, "requested")
	case "approve":
		go runAgentAdmin(mount, http.MethodPost, "/approve", request, "approved")
	case "publish":
		go runAgentAdmin(mount, http.MethodPost, "/publish", request, "published")
	case "roll_back":
		go runAgentAdmin(mount, http.MethodPost, "/rollback", request, "rolled_back")
	case "copy":
		revision, found := agentAdminRevision(revisionID)
		if !found {
			go runAgentAdmin(mount, http.MethodGet, "", nil, "")
			return
		}
		draft := productui.AgentConnectionRevision{ID: agentAdminConnection(revisionID), Provider: revision.Provider, CredentialMode: revision.CredentialMode, Grants: revision.Grants}
		go runAgentAdmin(mount, http.MethodPost, "/create", agentAdminCreateRequest{Revision: draft}, "created")
	case "import":
		go runAgentAdmin(mount, http.MethodPost, "/import", agentAdminImportRequest{ConnectionID: domAttribute(button, "data-connection-id"), SnapshotID: domAttribute(button, "data-snapshot-id")}, "updated")
	case "remove-grant":
		revision, found := agentAdminRevision(revisionID)
		if !found {
			go runAgentAdmin(mount, http.MethodGet, "", nil, "")
			return
		}
		go runAgentAdmin(mount, http.MethodPost, "/grants", agentAdminGrantsRequest{RevisionID: revisionID, Grants: agentGrantRowsAfterRemoving(revision.GrantRows, domAttribute(button, "data-grant-id"))}, "updated")
	default:
		agentAccessBrowser.Lock()
		agentAccessBrowser.admin.busy = false
		agentAccessBrowser.Unlock()
		button.Set("disabled", false)
	}
}

func agentAdminConnection(revisionID string) string {
	if cut := strings.IndexByte(revisionID, '#'); cut > 0 {
		return revisionID[:cut]
	}
	return revisionID
}

func handleAgentAdminSubmit(event js.Value) {
	form := agentClosest(event, "form[data-agent-admin-form]")
	if !form.Truthy() {
		return
	}
	event.Call("preventDefault")
	mount, ok := agentAdminMountFor(form)
	if !ok {
		return
	}
	switch domAttribute(form, "data-agent-admin-form") {
	case "create":
		connection := strings.TrimSpace(agentFormValue(form, "connection"))
		skills := parseAgentSkillLines(agentFormValue(form, "skills"))
		if connection == "" || len(skills) == 0 {
			agentAdminRefuse(mount, "invalid")
			return
		}
		draft := productui.AgentConnectionRevision{ID: connection, Provider: strings.TrimSpace(agentFormValue(form, "provider")), CredentialMode: agentFormValue(form, "mode"), Grants: skills}
		if draft.Provider == "" {
			draft.Provider = connection
		}
		go runAgentAdmin(mount, http.MethodPost, "/create", agentAdminCreateRequest{Revision: draft}, "created")
	case "grants":
		revisionID := domAttribute(form, "data-revision-id")
		revision, found := agentAdminRevision(revisionID)
		skills := agentFormChecked(form, "skill")
		roles, population, scopes := agentFormValue(form, "roles"), agentFormValue(form, "population"), agentFormValue(form, "scopes")
		if !found || len(skills) == 0 || len(splitAgentList(roles)) == 0 || strings.TrimSpace(population) == "" || len(splitAgentList(scopes)) == 0 {
			agentAdminRefuse(mount, "invalid")
			return
		}
		go runAgentAdmin(mount, http.MethodPost, "/grants", agentAdminGrantsRequest{RevisionID: revisionID, Grants: agentGrantRowsWith(revision.GrantRows, roles, population, scopes, skills)}, "updated")
	case "import":
		snapshot := strings.TrimSpace(agentFormValue(form, "snapshot"))
		if snapshot == "" {
			agentAdminRefuse(mount, "invalid")
			return
		}
		go runAgentAdmin(mount, http.MethodPost, "/import", agentAdminImportRequest{ConnectionID: domAttribute(form, "data-connection-id"), SnapshotID: snapshot}, "updated")
	case "preview":
		user := strings.TrimSpace(agentFormValue(form, "user"))
		if user == "" {
			agentAdminRefuse(mount, "invalid")
			return
		}
		go runAgentAdmin(mount, http.MethodPost, "/preview", agentAdminPreviewRequest{RevisionID: agentFormValue(form, "revision"), UserID: user, Label: user}, "previewed")
	default:
		agentAccessBrowser.Lock()
		agentAccessBrowser.admin.busy = false
		agentAccessBrowser.Unlock()
	}
}

// agentAdminRefuse says a form was not accepted before anything is sent.
func agentAdminRefuse(mount js.Value, message string) {
	agentAccessBrowser.Lock()
	agentAccessBrowser.admin.busy = false
	agentAccessBrowser.admin.message = message
	agentAccessBrowser.Unlock()
	renderAgentAdmin(mount)
}

func handleAgentAdminChange(event js.Value) {
	selectBox := agentClosest(event, "select[data-agent-admin-tier]")
	if !selectBox.Truthy() {
		return
	}
	mount, ok := agentAdminMountFor(selectBox)
	if !ok {
		return
	}
	selectBox.Set("disabled", true)
	go runAgentAdmin(mount, http.MethodPost, "/tier", agentAdminTierRequest{RevisionID: domAttribute(selectBox, "data-revision-id"), SkillID: domAttribute(selectBox, "data-skill-id"), Tier: selectBox.Get("value").String()}, "updated")
}
