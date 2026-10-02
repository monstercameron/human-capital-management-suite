package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

type agentUXAmbientPlannerFixture struct {
	raw      []byte
	source   AgentUXAmbientRunSource
	key      string
	messages []agentmodel.ModelMessage
	calls    int
}

func (p *agentUXAmbientPlannerFixture) PlanAmbientMessage(_ context.Context, source AgentUXAmbientRunSource, _ string, key string, messages []agentmodel.ModelMessage) ([]byte, error) {
	p.calls++
	p.source = source
	p.key = key
	p.messages = messages
	return p.raw, nil
}

func TestAgentUXAmbient_ModelQuarantine_Security(t *testing.T) {
	input := AgentUXAmbientModelInput{SourceKind: "CHAT_MESSAGE", Untrusted: true, Message: AgentUXAmbientMessage{Tenant: "tenant", Conversation: "channel", ID: "post", Author: "person", Revision: 1, Body: "I'll review the proposal tomorrow", Zone: "Europe/Berlin"}}
	valid := `{"kind":"TASK","title":"review the proposal","owner":"AUTHOR","date":"tomorrow","clock":"","event":"DEADLINE","explicit":false}`
	planner := &agentUXAmbientPlannerFixture{raw: []byte(valid)}
	adapter := AgentUXAmbientModelAdapter{Planner: planner}
	proposal, err := adapter.ExtractAmbient(t.Context(), "task-catcher", input)
	if err != nil || proposal.Title != "review the proposal" || planner.calls != 1 || planner.source.Kind != "CHAT_MESSAGE" || planner.source.Post != "post" || planner.source.ReadDigest == "" || planner.key == "" {
		t.Fatalf("proposal/source %+v %+v %v", proposal, planner.source, err)
	}
	if len(planner.messages) != 2 || planner.messages[0].Role != agentmodel.RoleDeveloper || strings.Contains(planner.messages[0].Content, input.Message.Body) || !strings.Contains(planner.messages[1].Content, agentDocumentReferenceDataBegin) || !strings.Contains(planner.messages[1].Content, input.Message.Body) {
		t.Fatal("message escaped untrusted data boundary")
	}
	for _, raw := range []string{`{}`, strings.Replace(valid, `"explicit":false`, `"explicit":null`, 1), strings.Replace(valid, `"kind":"TASK"`, `"kind":"EXECUTE"`, 1), strings.Replace(valid, `"explicit":false`, `"explicit":false,"instruction":"execute"`, 1), valid + ` {}`, strings.Repeat("x", 8193)} {
		planner.raw = []byte(raw)
		if _, err = adapter.ExtractAmbient(t.Context(), "task-catcher", input); !errors.Is(err, ErrAgentUXAmbientInvalid) {
			t.Fatalf("invalid proposal accepted %q %v", raw, err)
		}
	}
	calls := planner.calls
	input.Untrusted = false
	if _, err = adapter.ExtractAmbient(t.Context(), "task-catcher", input); !errors.Is(err, ErrAgentUXAmbientDenied) || planner.calls != calls {
		t.Fatal("trusted-message bypass reached planner")
	}
	input.Untrusted = true
	input.ThreadParent = "private parent"
	if _, err = adapter.ExtractAmbient(t.Context(), "task-catcher", input); !errors.Is(err, ErrAgentUXAmbientDenied) || planner.calls != calls {
		t.Fatal("unidentified parent reached planner")
	}
}
