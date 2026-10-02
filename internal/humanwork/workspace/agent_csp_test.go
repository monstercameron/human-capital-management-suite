package workspace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

func TestTodo_AGENTP_018_ProductAgentRoutesCSP(t *testing.T) {
	connect := web033ParseCSP(t, ProductContentSecurityPolicy("localhost:8290"))["connect-src"]
	for _, path := range []string{"/workspace/persona-admin/", personachat.Path, personachat.Path + "/", "/api/agent-controls", "/api/agent-controls/", "/api/agents/"} {
		if !web033HasToken(connect, "localhost:8290"+path) {
			t.Errorf("product client route missing from CSP: %s", path)
		}
	}
	for _, source := range connect {
		if source == "'self'" || source == "localhost:8290" || strings.Contains(source, "*") {
			t.Errorf("agent route allowance expanded to the whole origin: %s", source)
		}
	}
	journey := JourneyContentSecurityPolicy("localhost:8290")
	if strings.Contains(journey, "/api/agents/") || strings.Contains(journey, "/workspace/persona-admin/") {
		t.Fatal("non-product shell gained agent route allowances")
	}
}

func TestTodo_AGENTUX_004_Security(t *testing.T) {
	const host = "cell.test:8290"
	want := strings.Join([]string{
		host + PathAssetPrefix,
		host + PathChatMediaPrefix,
		host + PathDocumentMediaPrefix,
		"ws://" + host + PathTunnel,
		"wss://" + host + PathTunnel,
		host + PathBrandAssets,
		host + PathBrandAssetLifecycle,
		host + PathPersonaAdminData + "/",
		host + personachat.Path,
		host + personachat.Path + "/",
		host + "/api/agent-controls",
		host + "/api/agent-controls/",
		host + "/api/agents/",
		host + "/api/chat/",
		host + "/api/chat-writing-style",
		host + "/api/chat-writing-style/",
	}, " ")

	// Every product route is a client-side navigation target under one shell.
	// Pin both the directly loaded Chat document and a Home-first document:
	// the first document's policy remains active after navigation to Chat.
	handler, token := newShellHandler(t, false)
	for _, document := range []struct {
		name, path string
	}{{"chat", PathProductPrefix + "chat"}, {"home-first", PathProductHome}} {
		t.Run(document.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://"+host+document.path, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s = %d: %s", document.path, response.Code, response.Body.String())
			}
			connect := strings.Join(web033ParseCSP(t, response.Header().Get("Content-Security-Policy"))["connect-src"], " ")
			if connect != want {
				t.Fatalf("connect-src widened or drifted\ngot:  %s\nwant: %s", connect, want)
			}
		})
	}
	if strings.Contains(want, "'self'") || strings.Contains(want, "*") {
		t.Fatalf("persona lookup allowance is not exact: %s", want)
	}
}
