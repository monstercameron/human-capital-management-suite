package workspace

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

type agentUXSetup3InstallationTransport struct {
	active  map[string]bool
	retired string
}

func (t *agentUXSetup3InstallationTransport) ExecutePersonaAdminCommand(_ context.Context, request productui.PersonaAdminCommandRequest) error {
	if request.Action != "UNINSTALL" {
		return errors.New("unexpected action")
	}
	key := request.PersonaID + "/" + request.ConversationID
	if !t.active[key] {
		return errors.New("installation is not active")
	}
	t.active[key] = false
	t.retired = key
	return nil
}

func TestAgentUXSetup3_F5HTTPUninstallTargetsExactPlacement(t *testing.T) {
	handler, token := personaAdminCommandAuthorizedHandler(t)
	installations := &agentUXSetup3InstallationTransport{active: map[string]bool{
		"policy-helper/general":       true,
		"policy-helper/direct-policy": true,
	}}
	handler.personaAdminCommands = installations
	request := httptest.NewRequest(http.MethodPost, PathPersonaAdminCommand, strings.NewReader(`{"action":"UNINSTALL","persona_id":"policy-helper","conversation_id":"general"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || installations.retired != "policy-helper/general" || installations.active["policy-helper/general"] || !installations.active["policy-helper/direct-policy"] {
		t.Fatalf("status=%d retired=%q active=%v body=%s", response.Code, installations.retired, installations.active, response.Body.String())
	}
}
