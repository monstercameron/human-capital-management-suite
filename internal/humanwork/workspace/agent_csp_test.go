package workspace

import (
	"strings"
	"testing"
)

func TestTodo_AGENTP_018_ProductAgentRoutesCSP(t *testing.T) {
	connect := web033ParseCSP(t, ProductContentSecurityPolicy("localhost:8290"))["connect-src"]
	for _, path := range []string{"/workspace/persona-admin/", "/api/chat/personas/", "/api/agent-controls", "/api/agent-controls/", "/api/agents/"} {
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
