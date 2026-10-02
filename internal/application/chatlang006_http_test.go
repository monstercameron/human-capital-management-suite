package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatlang"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatlangCall drives the served assembly's translation administration API.
func (r *chatlangRig) call(subject, method, path, body string) (int, ChatlangView) {
	r.t.Helper()
	admission, bearer := integrate1Admission(r.t, "host", subject, time.Now)
	handler := (&agentServedAssembly{Renderings: r.chat.renderings}).Overlay(http.NotFoundHandler(), admission)
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", bearer)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var view ChatlangView
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			r.t.Fatalf("%s %s: %v: %s", method, path, err, response.Body.String())
		}
	}
	return response.Code, view
}

func TestTodo_CHATLANG_006_Administration(t *testing.T) {
	rig := newChatlangRig(t)
	if _, err := rig.chat.service.AddMembership(rig.as("alice"), chatcore.AddMembershipRequest{Principal: rig.person("alice"), Membership: chatcore.Membership{ConversationID: rig.room, TenantID: "host", HomeTenantID: "host", SubjectID: "manu", Role: chatcore.Manager, HistoryVisibility: chatcore.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	room := "?conversation=" + rig.room

	code, view := rig.call("alice", http.MethodGet, ChatlangPath+"/settings"+room, "")
	if code != 200 || view.Workspace.Enabled || !view.CanManageWorkspace || !view.CanManageChannel || len(view.Supported) != 8 || !view.Engine.Ready || view.BudgetMicros != chatlang.DefaultMonthlyBudgetMicros || view.Effective != chatlang.ReasonWorkspaceOff || view.Paused {
		t.Fatalf("an administrator sees translation off by default: %d %+v", code, view)
	}
	if code, _ = rig.call("bruno", http.MethodGet, ChatlangPath+"/settings"+room, ""); code != http.StatusForbidden {
		t.Fatalf("a member read the administration: %d", code)
	}
	if code, _ = rig.call("bruno", http.MethodPost, ChatlangPath+"/workspace", `{"workspace":{"enabled":true}}`); code != http.StatusForbidden {
		t.Fatalf("a member changed the workspace: %d", code)
	}
	if rig.governance.Allows(rig.as("alice"), "host", rig.room) {
		t.Fatal("a refused change took effect")
	}

	code, view = rig.call("alice", http.MethodPost, ChatlangPath+"/workspace", `{"workspace":{"enabled":true,"external_allowed":true,"languages":["de","fr"],"budget_micros":3000000,"formality":{"de":"formal"}}}`)
	if code != 200 || !view.Workspace.Enabled || view.BudgetMicros != 3000000 || len(view.Workspace.Languages) != 2 || view.Workspace.Formality["de"] != "formal" {
		t.Fatalf("workspace change: %d %+v", code, view)
	}
	if !rig.governance.Allows(rig.as("alice"), "host", rig.room) {
		t.Fatal("translation is not offered after an administrator turned it on")
	}

	// A channel manager runs their own channel but not the workspace.
	code, view = rig.call("manu", http.MethodGet, ChatlangPath+"/settings"+room, "")
	if code != 200 || view.CanManageWorkspace || !view.CanManageChannel || view.Channel == nil || view.BudgetMicros != 0 || len(view.Glossary) != 0 || view.Effective != chatlang.Allowed {
		t.Fatalf("a channel manager's view: %d %+v", code, view)
	}
	code, view = rig.call("manu", http.MethodPost, ChatlangPath+"/channel", `{"conversation":"`+rig.room+`","translation":"inherit","external":"barred"}`)
	if code != 200 || view.Channel == nil || view.Channel.External != chatlang.ExternalBarred || view.Effective != chatlang.ReasonExternalBarred {
		t.Fatalf("a channel manager bars the external engine: %d %+v", code, view)
	}
	if code, _ = rig.call("manu", http.MethodPost, ChatlangPath+"/workspace", `{"workspace":{"enabled":false}}`); code != http.StatusForbidden {
		t.Fatalf("a channel manager changed the workspace: %d", code)
	}
	if code, _ = rig.call("manu", http.MethodPost, ChatlangPath+"/glossary/add", `{"term":{"source":"x"}}`); code != http.StatusForbidden {
		t.Fatalf("a channel manager changed the glossary: %d", code)
	}
	if code, _ = rig.call("bruno", http.MethodPost, ChatlangPath+"/channel", `{"conversation":"`+rig.room+`","translation":"off"}`); code != http.StatusForbidden {
		t.Fatalf("a member changed a channel: %d", code)
	}

	// The glossary: terms with a required translation, and a do-not-translate list.
	code, view = rig.call("alice", http.MethodPost, ChatlangPath+"/glossary/add", `{"term":{"source":"Acme Cloud"}}`)
	if code != 200 || len(view.Glossary) != 1 {
		t.Fatalf("glossary add: %d %+v", code, view)
	}
	code, view = rig.call("alice", http.MethodPost, ChatlangPath+"/glossary/add", `{"term":{"source":"time off","language":"de","target":"Urlaub"}}`)
	if code != 200 || len(view.Glossary) != 2 || view.Glossary[1].Target != "Urlaub" {
		t.Fatalf("glossary add: %d %+v", code, view)
	}
	id := view.Glossary[0].ID
	code, view = rig.call("alice", http.MethodPost, ChatlangPath+"/glossary/remove", `{"id":"`+id+`"}`)
	if code != 200 || len(view.Glossary) != 1 {
		t.Fatalf("glossary remove: %d %+v", code, view)
	}

	for name, tc := range map[string]struct {
		method, path, body string
		want               int
	}{
		"unknown field":      {http.MethodPost, "/workspace", `{"workspace":{"enabled":true},"tenant":"other"}`, 400},
		"unsupported":        {http.MethodPost, "/workspace", `{"workspace":{"languages":["xx"]}}`, 400},
		"negative budget":    {http.MethodPost, "/workspace", `{"workspace":{"budget_micros":-5}}`, 400},
		"bad switch":         {http.MethodPost, "/channel", `{"conversation":"` + rig.room + `","translation":"maybe"}`, 400},
		"missing channel":    {http.MethodPost, "/channel", `{"conversation":"nope","translation":"off"}`, 400},
		"bad term":           {http.MethodPost, "/glossary/add", `{"term":{"source":"a","language":"de"}}`, 400},
		"trailing data":      {http.MethodPost, "/workspace", `{"workspace":{}} {}`, 400},
		"get on a write":     {http.MethodGet, "/workspace", ``, 405},
		"post on a read":     {http.MethodPost, "/settings", `{}`, 405},
		"unknown action":     {http.MethodGet, "/nothing", ``, 404},
		"oversize":           {http.MethodPost, "/workspace", `{"workspace":{"languages":["` + strings.Repeat("x", 20000) + `"]}}`, 400},
		"remove a stranger":  {http.MethodPost, "/glossary/remove", `{"id":"no-such-term"}`, 400},
		"channel not a room": {http.MethodPost, "/channel", `{"conversation":"","translation":"off"}`, 403},
	} {
		if code, _ := rig.call("alice", tc.method, ChatlangPath+tc.path, tc.body); code != tc.want {
			t.Errorf("%s: %d, want %d", name, code, tc.want)
		}
	}
	// A wrong content type is refused before anything is read.
	admission, bearer := integrate1Admission(t, "host", "alice", time.Now)
	request := httptest.NewRequest(http.MethodPost, ChatlangPath+"/workspace", strings.NewReader(`{"workspace":{"enabled":false}}`))
	request.Header.Set("Authorization", bearer)
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	(&agentServedAssembly{Renderings: rig.chat.renderings}).Overlay(http.NotFoundHandler(), admission).ServeHTTP(response, request)
	if _, view = rig.call("alice", http.MethodGet, ChatlangPath+"/settings"+room, ""); response.Code != 400 || !view.Workspace.Enabled {
		t.Fatalf("a wrong content type was accepted: %d", response.Code)
	}
}

func TestTodo_CHATLANG_006_NotComposed(t *testing.T) {
	admission, bearer := integrate1Admission(t, "host", "alice", time.Now)
	handler := (&agentServedAssembly{}).Overlay(http.NotFoundHandler(), admission)
	for method, want := range map[string]int{http.MethodGet: 200, http.MethodPost: 503} {
		request := httptest.NewRequest(method, ChatlangPath+map[string]string{http.MethodGet: "/settings", http.MethodPost: "/workspace"}[method], strings.NewReader(`{}`))
		request.Header.Set("Authorization", bearer)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("%s with nothing composed: %d %s", method, response.Code, response.Body.String())
		}
		if method == http.MethodGet && !strings.Contains(response.Body.String(), "translation_not_composed") {
			t.Fatalf("the typed answer is missing: %s", response.Body.String())
		}
	}
}

func TestTodo_CHATLANG_006_Features(t *testing.T) {
	rig := newChatlangRig(t)
	features := func() chatui.ChatFeatures {
		admission, bearer := integrate1Admission(t, "host", "alice", time.Now)
		request := httptest.NewRequest(http.MethodGet, integrate2FeaturesPath, nil)
		request.Header.Set("Authorization", bearer)
		response := httptest.NewRecorder()
		(&agentServedAssembly{Renderings: rig.chat.renderings}).Overlay(http.NotFoundHandler(), admission).ServeHTTP(response, request)
		var out chatui.ChatFeatures
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &out) != nil {
			t.Fatalf("features %d %s", response.Code, response.Body.String())
		}
		return out
	}
	if got := features(); !got.Translation || got.Translating {
		t.Fatalf("an engine is composed but the workspace has not turned translation on: %+v", got)
	}
	rig.enable(chatlang.Workspace{})
	if got := features(); !got.Translation || !got.Translating {
		t.Fatalf("translation on: %+v", got)
	}
	rig.governance.BindEngine(ChatlangEngineInfo{})
	if got := features(); got.Translation || got.Translating {
		t.Fatalf("no engine composed: %+v", got)
	}
}
