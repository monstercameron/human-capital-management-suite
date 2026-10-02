package application

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type chattoneAdminFixture struct {
	allowed  bool
	identity chatrewrite.Identity
}

func (a *chattoneAdminFixture) ManageWritingStyles(_ context.Context, id chatrewrite.Identity) error {
	a.identity = id
	if !a.allowed {
		return personachat.ErrDenied
	}
	return nil
}
func TestTodo_CHATTONE_004_Administration(t *testing.T) {
	s, ctx, _, _, _, _ := chattoneFixture(t)
	admin := &chattoneAdminFixture{}
	s.Administration = admin
	config := ChattoneAdminConfig{ConversationID: "room", Enabled: true, Styles: []ChattoneAdminStyle{{ID: "house", Label: "House voice", Instruction: "Keep a formal house tone.", Register: "professional"}}}
	if _, err := s.ConfigureWritingStyles(ctx, config); !errors.Is(err, personachat.ErrDenied) {
		t.Fatal("non-admin configured styles", err)
	}
	admin.allowed = true
	reply, err := s.ConfigureWritingStyles(ctx, config)
	if err != nil || len(reply.Styles) != 1 || reply.Styles[0].ID != "house" || admin.identity.Tenant != "tenant" {
		t.Fatal(reply, err)
	}
	other, _ := s.Rewrite.Registry.Styles("other")
	if len(other) != 3 {
		t.Fatal("configuration crossed tenants")
	}
	h := ChattoneHandler{Surface: s}
	w := httptest.NewRecorder()
	body := `{"conversation_id":"room","enabled":false,"styles":[{"id":"house","label":"House voice","instruction":"Keep a formal house tone.","register":"professional"}]}`
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ChattonePath+"/styles", strings.NewReader(body)).WithContext(ctx))
	if w.Code != 200 || strings.Contains(w.Body.String(), "Keep a formal house tone") || strings.Contains(w.Body.String(), "instruction") {
		t.Fatal("admin instructions exposed", w.Code, w.Body)
	}
	if _, enabled := s.Rewrite.Registry.Styles("tenant"); enabled {
		t.Fatal("admin disable failed")
	}
	config.Styles[0].Register = "unknown"
	if _, err := s.ConfigureWritingStyles(ctx, config); !errors.Is(err, chatrewrite.ErrInvalid) {
		t.Fatal("bad registry accepted", err)
	}
}
