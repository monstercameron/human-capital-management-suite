package application

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// TestTodo_CHATBUG_028 pins the server half of the mention menu: the agent
// directory of a conversation that has no agent installed answers 200 with an
// empty list, never an error, so the browser has nothing to report.
func TestTodo_CHATBUG_028(t *testing.T) {
	for name, prepare := range map[string]func(s *PersonaChatSurface){
		"no reference candidates": func(s *PersonaChatSurface) {
			s.References.(*personaSurfaceReferencesFixture).candidates = nil
		},
		"a candidate whose installation is not current here": func(s *PersonaChatSurface) {
			refs := s.References.(*personaSurfaceReferencesFixture)
			refs.byReference = map[string]personaReferenceFacts{}
		},
		"a candidate that is not eligible": func(s *PersonaChatSurface) {
			refs := s.References.(*personaSurfaceReferencesFixture)
			refs.candidates = []chat.ReferenceCandidate{{Reference: refs.candidates[0].Reference, Eligible: false}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, ctx, _, _, _ := personaSurfaceFixture(t)
			prepare(s)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, personachat.Path+"?conversation_id=channel-a", nil).WithContext(ctx)
			personachat.Handler{Surface: s}.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("directory of a conversation with no agents answered %d %s, want 200", recorder.Code, recorder.Body.String())
			}
			if body := strings.TrimSpace(recorder.Body.String()); body != `{"personas":[],"post_actors":[]}` {
				t.Fatalf("directory body = %s, want empty lists", body)
			}
		})
	}

	// The conversation that does have an agent still lists it.
	s, ctx, _, _, _ := personaSurfaceFixture(t)
	directory, err := s.Directory(ctx, "channel-a")
	if err != nil || len(directory.Personas) != 1 {
		t.Fatalf("directory of a conversation with an agent = %+v, %v", directory, err)
	}
}
