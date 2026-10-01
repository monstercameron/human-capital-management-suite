package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

type bindingModelDispatcher struct{ calls int }

func (d *bindingModelDispatcher) DispatchTypedModel(_ context.Context, req agentmodel.Request, _ agentmodel.TypedModelInput) (agentmodel.ModelResult, error) {
	d.calls++
	return agentmodel.ModelResult{Text: req.Actor.TaskID}, nil
}

func TestTodo_AGENT2_026_RuntimeTypedModelBinding(t *testing.T) {
	binding := &AgentTypedModelDispatchBinding{}
	model := agentModel{Kind: agentModelConfigured, Typed: binding}
	if binding.Available() || model.Available() {
		t.Fatal("unbound model was presented as ready")
	}
	if _, err := binding.DispatchTypedModel(context.Background(), agentmodel.Request{}, agentmodel.TypedModelInput{}); !errors.Is(err, ErrAgentModelUnavailable) {
		t.Fatalf("unbound=%v", err)
	}
	dispatcher := &bindingModelDispatcher{}
	if err := BindAgentRuntimeTypedModel(&agentRuntime{TypedModels: binding}, dispatcher); err != nil {
		t.Fatal(err)
	}
	if !binding.Available() || !model.Available() {
		t.Fatal("bound dispatcher stayed unavailable")
	}
	if err := binding.Bind(&bindingModelDispatcher{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("rebind=%v", err)
	}
	got, err := binding.DispatchTypedModel(context.Background(), agentmodel.Request{Actor: agentmodel.ActorChain{TaskID: "task-current"}}, agentmodel.TypedModelInput{})
	if err != nil || got.Text != "task-current" || dispatcher.calls != 1 {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, dispatcher.calls)
	}
	if err := BindAgentRuntimeTypedModel(nil, dispatcher); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("nil runtime=%v", err)
	}
}

type refusingTypedModelSource struct{ calls int }

func (s *refusingTypedModelSource) BuildTypedAgentModelRequest(context.Context, agentmodel.Request, agentmodel.TypedModelInput) (AgentModelGatewayRequest, error) {
	s.calls++
	return AgentModelGatewayRequest{}, ErrAgentModelExecutorBinding
}
func TestTodo_AGENT2_026_PlatformTypedDispatchRefuses(t *testing.T) {
	if _, err := NewOpenAIPlatformTypedModelDispatcher(nil, &refusingTypedModelSource{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("missing gateway=%v", err)
	}
	source := &refusingTypedModelSource{}
	dispatcher, err := NewOpenAIPlatformTypedModelDispatcher(&AgentModelGateway{}, source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.DispatchTypedModel(context.Background(), agentmodel.Request{}, agentmodel.TypedModelInput{WebSearch: true}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) || source.calls != 0 {
		t.Fatalf("websearch=%v calls=%d", err, source.calls)
	}
	if _, err := dispatcher.DispatchTypedModel(context.Background(), agentmodel.Request{}, agentmodel.TypedModelInput{}); !errors.Is(err, ErrAgentModelExecutorBinding) || source.calls != 1 {
		t.Fatalf("source=%v calls=%d", err, source.calls)
	}
}
