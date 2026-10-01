//go:build js && wasm

package main

import (
	"net/url"
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestTodo_AGENTP_018_PreviewSelectionSurvivesCatalogRefresh(t *testing.T) {
	snapshot := productui.PersonaAdminSnapshot{Personas: []productui.PersonaAdminPersona{{ID: "first", Lifecycle: productui.PersonaPublished}, {ID: "chosen", Lifecycle: productui.PersonaDraft}}}
	if got := personaAdminCurrentSelection(snapshot, "chosen"); got != "chosen" {
		t.Fatalf("selection reset to %s", got)
	}
	if got := personaAdminCurrentSelection(snapshot, "removed"); got != "first" {
		t.Fatalf("removed selection retained: %s", got)
	}
	if got := personaAdminCurrentSelection(productui.PersonaAdminSnapshot{}, "chosen"); got != "" {
		t.Fatalf("empty catalog retained %s", got)
	}
}

func TestTodo_AGENTP_018_PreviewRefreshRequiresAllTargets(t *testing.T) {
	for _, targets := range [][3]string{{"persona", "", ""}, {"persona", "user", ""}, {"", "user", "room"}} {
		if got := personaAdminSnapshotEndpoint(targets[0], targets[1], targets[2]); got != "/workspace/persona-admin/" {
			t.Fatalf("partial selection requested a preview: %s", got)
		}
	}
	endpoint, err := url.Parse(personaAdminSnapshotEndpoint("persona one", "user two", "room three"))
	if err != nil {
		t.Fatal(err)
	}
	query := endpoint.Query()
	if query.Get("persona_id") != "persona one" || query.Get("subject_id") != "user two" || query.Get("conversation_id") != "room three" {
		t.Fatalf("preview targets lost: %s", endpoint)
	}
}

func TestTodo_AGENTP_018_LocalSetupPostsWithBrowserProofAndFailsClosed(t *testing.T) {
	global := js.Global()
	priorFetch, priorDocument := global.Get("fetch"), global.Get("document")
	defer global.Set("fetch", priorFetch)
	defer global.Set("document", priorDocument)
	object := global.Get("Object")
	document := object.New()
	get := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
	defer get.Release()
	document.Set("getElementById", get)
	global.Set("document", document)
	button := object.New()
	button.Set("disabled", false)
	thenable := object.New()
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(map[string]any{"ok": false}); return nil })
	defer resolve.Release()
	thenable.Set("then", resolve)
	calls := 0
	fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		calls++
		if args[1].Get("headers").Get("authorization").String() != "Bearer signed-session" {
			t.Error("setup omitted current verified credential")
		}
		if args[0].String() != "/workspace/persona-admin/local-setup" || args[1].Get("method").String() != "POST" || args[1].Get("credentials").String() != "same-origin" {
			t.Errorf("incorrect setup request: %v %v", args[0], args[1])
		}
		if !button.Get("disabled").Bool() {
			t.Error("setup did not prevent duplicate submissions")
		}
		return thenable
	})
	defer fetch.Release()
	global.Set("fetch", fetch)
	personaAdminBrowser.Lock()
	prior := personaAdminBrowser.snapshot
	priorCfg := personaAdminBrowser.cfg
	personaAdminBrowser.cfg = journeyclient.Config{Bearer: "signed-session", Tenant: "tenant-a", Subject: "admin-a"}
	personaAdminBrowser.snapshot = &productui.PersonaAdminSnapshot{}
	personaAdminBrowser.Unlock()
	defer func() {
		personaAdminBrowser.Lock()
		personaAdminBrowser.snapshot = prior
		personaAdminBrowser.cfg = priorCfg
		personaAdminBrowser.Unlock()
	}()
	personaAdminSetupLocalDraft(button)
	if calls != 0 {
		t.Fatal("unavailable server projection reached bootstrap")
	}
	personaAdminBrowser.Lock()
	personaAdminBrowser.snapshot = &productui.PersonaAdminSnapshot{LocalDevBootstrapAvailable: true}
	personaAdminBrowser.Unlock()
	personaAdminSetupLocalDraft(button)
	if calls != 1 || button.Get("disabled").Bool() {
		t.Fatalf("calls=%d button disabled=%v", calls, button.Get("disabled"))
	}
}
