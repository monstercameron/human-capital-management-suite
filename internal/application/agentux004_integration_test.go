package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// The persona lookup the chat page calls, through the served edge (the real
// overlay, a signed bearer, the real directory projection over the surface's own
// ports), returns only the agents the signed-in person may invoke in that
// conversation: an agent that is installed but whose skill the person no longer
// holds is not returned, an agent from another conversation is not returned, a
// request with no admission or the wrong method is refused, and nothing in the
// answer carries instructions or another agent's name.
func TestTodo_AGENTUX_004_Integration(t *testing.T) {
	base, _, _, _, _ := personaSurfaceFixture(t)
	admission, bearer := integrate1Admission(t, "tenant-a", "user-a", base.Now)
	handler := OverlayPersonaChatSurface(http.NotFoundHandler(), base, admission)
	get := func(query, authorization string) (*httptest.ResponseRecorder, personachat.Directory) {
		request := httptest.NewRequest(http.MethodGet, personachat.Path+query, nil)
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		var directory personachat.Directory
		_ = json.Unmarshal(w.Body.Bytes(), &directory)
		return w, directory
	}

	w, directory := get("?conversation_id=channel-a", bearer)
	if w.Code != http.StatusOK || len(directory.Personas) != 1 || directory.Personas[0].Reference.ID != "agent:coach" {
		t.Fatalf("invocable lookup: %d %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); strings.Contains(body, "Secret Comp Analyst") || strings.Contains(body, "instructions") || w.Header().Get("Cache-Control") == "" {
		t.Fatalf("the answer carries what it must not: %s %v", body, w.Header())
	}
	if scope := directory.Personas[0].DocumentScope; scope != "CHANNEL_DOCUMENTS" && scope != "WORKSPACE_DOCUMENTS" && scope != "NO_DOCUMENTS" {
		t.Fatalf("document scope %q", scope)
	}

	// The person loses the skill: the agent is no longer one they may invoke.
	discovery := base.Skills
	base.Skills = &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{}}
	if w, directory = get("?conversation_id=channel-a", bearer); w.Code != http.StatusOK || len(directory.Personas) != 0 {
		t.Fatalf("a revoked skill still lists an agent: %d %s", w.Code, w.Body.String())
	}
	base.Skills = discovery

	// Not admitted, or not a read.
	if w, _ = get("?conversation_id=channel-a", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no admission answered %d", w.Code)
	}
	post := httptest.NewRequest(http.MethodPost, personachat.Path+"?conversation_id=channel-a", strings.NewReader("{}"))
	post.Header.Set("Authorization", bearer)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, post)
	if denied.Code == http.StatusOK {
		t.Fatalf("a POST to the lookup answered 200: %s", denied.Body.String())
	}
}
