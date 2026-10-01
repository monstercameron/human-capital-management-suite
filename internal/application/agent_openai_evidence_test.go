package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENT_020_OpenAISourceRequiresCurrentOwnerBinding(t *testing.T) {
	if _, err := NewPersonaOpenAIModelEvidence(PersonaOpenAIModelOwnerConfig{}); !errors.Is(err, ErrAgentModelGatewayNotConfigured) {
		t.Fatalf("empty owners=%v", err)
	}
	evidence := &PersonaOpenAIModelEvidence{}
	if err := evidence.RecordRoute(context.Background(), agentmodel.RouteRecord{TraceID: "forged"}); !errors.Is(err, ErrAgentModelGatewayTenant) {
		t.Fatalf("unbound route=%v", err)
	}
	if err := evidence.VerifySourceClassification(context.Background(), agentegress.SourceClassificationRequest{Tenant: "tenant", SourceClass: "persona-profile"}); !errors.Is(err, ErrAgentModelGatewayTenant) {
		t.Fatalf("unbound source=%v", err)
	}
}

func TestTodo_AGENT_020_OpenAIPostClassCannotDowngrade(t *testing.T) {
	for _, tc := range []struct {
		declared, actual trustdlp.DataClass
		allowed          bool
	}{
		{trustdlp.ClassInternal, trustdlp.ClassPublic, true},
		{trustdlp.ClassInternal, trustdlp.ClassInternal, true},
		{trustdlp.ClassPublic, trustdlp.ClassInternal, false},
		{trustdlp.ClassInternal, trustdlp.ClassPII, false},
		{trustdlp.ClassInternal, trustdlp.DataClass("CONFIDENTIAL"), false},
		{trustdlp.ClassInternal, trustdlp.ClassMedical, false},
	} {
		if got := personaOpenAISourceClassCovers(tc.declared, tc.actual); got != tc.allowed {
			t.Fatalf("class %s -> %s allowed=%v", tc.actual, tc.declared, got)
		}
	}
}
