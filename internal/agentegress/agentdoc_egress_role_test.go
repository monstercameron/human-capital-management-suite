package agentegress

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

type agentDocEgressRoleVerifier struct {
	want  agentmodel.MessageRole
	calls int
}

func (v *agentDocEgressRoleVerifier) VerifySourceClassification(_ context.Context, source SourceClassificationRequest) error {
	v.calls++
	if source.FieldName != "model.message.0" || source.MessageRole != v.want {
		return ErrRefused
	}
	return nil
}

func TestAgentDocEgress_ProviderRoleBinding(t *testing.T) {
	for _, want := range []agentmodel.MessageRole{agentmodel.RoleUser, agentmodel.RoleDeveloper} {
		t.Run(string(want), func(t *testing.T) {
			dispatcher, leases, adapter, request := providerDispatchFixture(t)
			verifier := &agentDocEgressRoleVerifier{want: want}
			dispatcher.sources = verifier
			_, err := dispatcher.Dispatch(context.Background(), request, adapter)
			if verifier.calls != 1 {
				t.Fatalf("source checks=%d want 1", verifier.calls)
			}
			if want == agentmodel.RoleUser {
				if err != nil || adapter.calls != 1 || leases.uses != 1 {
					t.Fatalf("verified role dispatch err=%v calls=%d leases=%d", err, adapter.calls, leases.uses)
				}
			} else {
				var refusal *Refusal
				if !errors.As(err, &refusal) || refusal.Code != RefusalProviderSource || refusal.Field != "model.message.0" || adapter.calls != 0 || leases.uses != 0 {
					t.Fatalf("mismatched role reached provider: err=%v calls=%d leases=%d", err, adapter.calls, leases.uses)
				}
			}
		})
	}
}

func TestAgentDocEgress_InvalidRole(t *testing.T) {
	for _, role := range []agentmodel.MessageRole{"", "forged-role"} {
		t.Run(string(role), func(t *testing.T) {
			dispatcher, leases, adapter, request := providerDispatchFixture(t)
			verifier := &agentDocEgressRoleVerifier{want: role}
			dispatcher.sources = verifier
			request.Model.Messages[0].Role = role
			_, err := dispatcher.Dispatch(context.Background(), request, adapter)
			var refusal *Refusal
			if !errors.As(err, &refusal) || refusal.Code != RefusalProviderContract || refusal.Field != "model_request" || verifier.calls != 0 || adapter.calls != 0 || leases.uses != 0 {
				t.Fatalf("invalid role escaped binding: err=%v checks=%d calls=%d leases=%d", err, verifier.calls, adapter.calls, leases.uses)
			}
		})
	}
}
