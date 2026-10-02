package application

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIntegrate2ServedWaveRoutes(t *testing.T) {
	admission, bearer := integrate1Admission(t, "tenant-a", "alice", time.Now)
	gates := &chatgateHTTPFixture{}
	renderings := &chatrenderHTTPFixture{settings: chatrender.DefaultPreference("de-DE"), rendering: chatrender.Rendering{Message: "post", Revision: 1, Text: "reader text"}}
	status := &chatstateHTTPFixture{}
	search := &chatsearchHTTPFixture{}
	locations := &chatmapSurfaceFixture{}
	assembly := &agentServedAssembly{Gates: gates, Renderings: renderings, ChannelStatus: status, ChatSearch: ChatSearchHTTP{Port: search, History: chatsearch.NewRecent()}, Locations: locations}
	handler := assembly.Overlay(http.NotFoundHandler(), admission)
	tests := []struct{ method, path, body string }{
		{"GET", ChatgatePath + "?conversation=room", ""},
		{"GET", ChatRenderingPath + "/settings", ""},
		{"GET", ChatRenderingPath + "/reader?conversation=room&message=post", ""},
		{"GET", ChannelStatusPath + "room", ""},
		{"POST", ChatSearchPath, `{"query":"reader","mode":"keyword"}`},
		{"POST", ChatmapPath + "/attach", `{"TenantID":"tenant-a","ConversationID":"room","PostID":"post","PostRevision":1}`},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			req.Header.Set("Authorization", bearer)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("served route: %d %s", w.Code, w.Body)
			}
			req = httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 401 {
				t.Fatalf("unadmitted route: %d %s", w.Code, w.Body)
			}
		})
	}
	if gates.calls != 1 || renderings.called != 2 || status.calls != 1 || search.calls != 1 || locations.calls != 1 {
		t.Fatalf("ports not reached: %d %d %d %d %d", gates.calls, renderings.called, status.calls, search.calls, locations.calls)
	}
}

func TestIntegrate2UnavailableFeatures(t *testing.T) {
	admission, bearer := integrate1Admission(t, "tenant-a", "alice", time.Now)
	var style *ChattoneService
	assembly := &agentServedAssembly{WritingStyles: style}
	handler := assembly.Overlay(nil, admission)
	for _, path := range []string{ChatRenderingPath + "/settings", ChattonePath + "/suggestion?conversation=room", ChatgatePath + "?conversation=room", ChannelStatusPath + "room", ChatSearchPath + "/recent", ChatFiltersPath + "/definitions"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if !((w.Code == 503 && strings.Contains(w.Body.String(), "unavailable")) || (w.Code == 200 && strings.Contains(w.Body.String(), `"available":false`))) || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("unavailable %s: %d %s", path, w.Code, w.Body)
		}
	}
	req := httptest.NewRequest("GET", integrate2FeaturesPath, nil)
	req.Header.Set("Authorization", bearer)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var features chatui.ChatFeatures
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &features) != nil || features != (chatui.ChatFeatures{}) {
		t.Fatalf("unavailable capability leaked: %d %s", w.Code, w.Body)
	}
	req = httptest.NewRequest("GET", integrate2FeaturesPath, nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("feature discovery bypassed admission", w.Code)
	}
}

func TestIntegrate2LocationWritesRequireRoutes(t *testing.T) {
	surface := &integrate2Location{}
	if _, err := surface.Attach(context.Background(), chat.AttachLocationRequest{}); err == nil {
		t.Fatal("unfenced location attach")
	}
	if err := surface.End(context.Background(), chat.Principal{}, chat.LocationKey{}); err == nil {
		t.Fatal("unfenced location end")
	}
}

func TestIntegrate2MaintenanceTypedNil(t *testing.T) {
	var store *chatstore.Store
	assembly := &agentServedAssembly{ChatMaintenance: store}
	if err := assembly.TickTenant(context.Background(), "tenant"); err != nil {
		t.Fatal(err)
	}
}

type integrate2StatusDirectoryFixture struct {
	*chatstateHTTPFixture
	principal chat.Principal
}

func (f *integrate2StatusDirectoryFixture) SearchChannelStatuses(_ context.Context, p chat.Principal, tenant, query string, archived bool) ([]chat.ChannelStatus, error) {
	f.principal = p
	return []chat.ChannelStatus{{ConversationID: "archive", Name: "Past work", TenantID: tenant, Status: "ARCHIVED"}}, nil
}
func TestIntegrate2ServedArchivedDirectory(t *testing.T) {
	admission, bearer := integrate1Admission(t, "tenant-a", "alice", time.Now)
	f := &integrate2StatusDirectoryFixture{chatstateHTTPFixture: &chatstateHTTPFixture{}}
	h := (&agentServedAssembly{ChannelStatus: f}).Overlay(nil, admission)
	req := httptest.NewRequest("GET", ChannelStatusPath+"?archived=true", nil)
	req.Header.Set("Authorization", bearer)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Past work") || f.principal.SubjectID != "alice" || f.principal.TenantID != "tenant-a" {
		t.Fatal(w.Code, w.Body, f.principal)
	}
}

type integrate2RenderingSearchFixture struct{ *chatrenderHTTPFixture }

func (f *integrate2RenderingSearchFixture) SearchRenderings(context.Context, chatstore.RenderingScope, string) ([]chatrender.Rendering, error) {
	f.called++
	return []chatrender.Rendering{{Text: "Selected paragraph"}}, nil
}
func (f *integrate2RenderingSearchFixture) SearchMessageLanguages(context.Context, chatstore.RenderingScope, string) ([]chatstore.MessageLanguage, error) {
	f.called++
	return []chatstore.MessageLanguage{{Message: "post", Revision: 1}}, nil
}
func TestIntegrate2ServedRenderingSearch(t *testing.T) {
	admission, bearer := integrate1Admission(t, "tenant-a", "alice", time.Now)
	f := &integrate2RenderingSearchFixture{chatrenderHTTPFixture: &chatrenderHTTPFixture{}}
	h := (&agentServedAssembly{Renderings: f}).Overlay(nil, admission)
	for _, path := range []string{"/search?conversation=room&query=paragraph", "/search-language?conversation=room&language=de"} {
		req := httptest.NewRequest("GET", ChatRenderingPath+path, nil)
		req.Header.Set("Authorization", bearer)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 || (!strings.Contains(w.Body.String(), "Selected paragraph") && !strings.Contains(w.Body.String(), "post")) {
			t.Fatal(w.Code, w.Body)
		}
	}
	if f.called != 2 || f.authorized != 2 {
		t.Fatal("search bypassed authority or projection", f)
	}
	f.deny = true
	req := httptest.NewRequest("GET", ChatRenderingPath+"/search?conversation=room", nil)
	req.Header.Set("Authorization", bearer)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 || f.called != 2 {
		t.Fatal("denied search reached source", w.Code, f.called)
	}
}

type integrate2MaintenanceFixture struct {
	locations, status int
	tenant            string
}

func (f *integrate2MaintenanceFixture) SweepLocations(_ context.Context, tenant string, _ time.Time) (int64, error) {
	f.locations++
	f.tenant = tenant
	return 1, nil
}
func (f *integrate2MaintenanceFixture) SweepChannelStatuses(_ context.Context, tenant string, _ time.Time) (int, error) {
	f.status++
	f.tenant = tenant
	return 1, nil
}
func TestIntegrate2MaintenanceWithoutModelWorker(t *testing.T) {
	f := &integrate2MaintenanceFixture{}
	if err := (&agentServedAssembly{ChatMaintenance: f}).TickTenant(t.Context(), "tenant-a"); err != nil || f.locations != 1 || f.status != 1 || f.tenant != "tenant-a" {
		t.Fatal("chat expiry required a model worker", f, err)
	}
}
