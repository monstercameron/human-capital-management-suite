package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

func TestPersonaPrivateReplyAuthorityCompositionRequiresEveryTrustedSource(t *testing.T) {
	worker := privateChatGatewayVerifiedWorker(t, time.Now().UTC())
	valid := personaPrivateReplyAuthorityCompositionFixture(worker)
	tests := []struct {
		name   string
		change func(*PersonaPrivateReplyAuthorityComposition)
	}{
		{name: "missing base schema and grounding source", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.Base = nil }},
		{name: "missing current projection gate", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.Gate = nil }},
		{name: "missing trusted scope builder", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.ScopeBuilder = nil }},
		{name: "missing private chat authorizer", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.ScopeAuthorizer = nil }},
		{name: "missing durable grant store", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.Grants = nil }},
		{name: "missing installation store", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.Installations = nil }},
		{name: "missing workload verifier", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.WorkloadVerifier = nil }},
		{name: "missing workload credential source", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.WorkloadCredential = nil }},
		{name: "missing clock", change: func(c *PersonaPrivateReplyAuthorityComposition) { c.Now = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.change(&cfg)
			if _, err := NewPersonaPrivateChatReplyAuthoritySource(cfg); !errors.Is(err, errPersonaPrivateReplyAuthorityComposition) {
				t.Fatalf("composition error=%v, want fail-closed unavailable", err)
			}
		})
	}
}

func TestPersonaPrivateReplyRuntimeComposesCurrentPrivateChatAuthority(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	worker := privateChatGatewayVerifiedWorker(t, now)
	composition := personaPrivateReplyAuthorityCompositionFixture(worker)
	composition.Now = func() time.Time { return now }
	runtime, err := NewPersonaPrivateReplyRuntimeWithCurrentAuthority(PersonaPrivateReplyRuntimeConfig{
		OutputPersister: privateReplyRuntimePersisterFake{}, Chat: privateReplyRuntimeChatFake{},
		ReplyCommitter: privateReplyRuntimeCommitterFake{}, OutputPolicy: PersonaReplyOutputPolicy{TenantOrigin: "https://tenant.example"},
	}, composition)
	if err != nil {
		t.Fatalf("compose private reply runtime: %v", err)
	}
	output, ok := runtime.Output.(*SealedPersonaRunOutputValidator)
	if !ok {
		t.Fatalf("output validator type=%T", runtime.Output)
	}
	identitySource, ok := output.authority.(*PersonaPrivateChatReplyAuthoritySource)
	if !ok || identitySource.Base == nil || identitySource.Projection == nil || identitySource.Identity == nil {
		t.Fatalf("output authority did not compose base, current projection and durable identity: %#v", output.authority)
	}
	if _, ok := identitySource.Identity.(*DatabasePersonaPrivateChatGatewayIdentityResolver); !ok {
		t.Fatalf("identity resolver type=%T", identitySource.Identity)
	}
	if _, err := NewPersonaPrivateReplyRuntimeWithCurrentAuthority(PersonaPrivateReplyRuntimeConfig{
		OutputAuthority: privateReplyRuntimeAuthorityFake{},
	}, composition); !errors.Is(err, errPersonaPrivateReplyRuntimeUnavailable) {
		t.Fatalf("caller-supplied output authority was accepted alongside production composition: %v", err)
	}
}

type privateReplyAuthorityScopeBuilderFake struct{}

func (privateReplyAuthorityScopeBuilderFake) BuildPrivateChatScopeRequest(context.Context, agentrun.Record, runstate.Run) (agentgate.PrivateChatScopeRequest, error) {
	return agentgate.PrivateChatScopeRequest{}, errors.New("not used by composition test")
}

type privateReplyAuthorityChatAuthorizerFake struct{}

func (privateReplyAuthorityChatAuthorizerFake) AuthorizePrivateChat(context.Context, agentgate.PrivateChatScopeRequest) (agentgate.PrivateChatScopeEvidence, error) {
	return agentgate.PrivateChatScopeEvidence{}, errors.New("not used by composition test")
}

type privateReplyAuthorityBaseFake struct{}

func (privateReplyAuthorityBaseFake) ResolvePersonaRunChatReplyAuthority(context.Context, agentrun.Record, runstate.Run) (PersonaRunChatReplyAuthority, error) {
	return PersonaRunChatReplyAuthority{}, errors.New("not used by composition test")
}

func personaPrivateReplyAuthorityCompositionFixture(worker *VerifiedPersonaPrivateChatWorkloadIdentitySource) PersonaPrivateReplyAuthorityComposition {
	return PersonaPrivateReplyAuthorityComposition{
		Base: privateReplyAuthorityBaseFake{}, Gate: &agentgate.Gate{}, ScopeBuilder: privateReplyAuthorityScopeBuilderFake{},
		ScopeAuthorizer: privateReplyAuthorityChatAuthorizerFake{}, Grants: &agentdelegationstore.Store{},
		Installations: &agentpersonastore.Store{}, WorkloadVerifier: worker.Verifier,
		WorkloadCredential: worker.Credentials, Now: func() time.Time { return time.Now().UTC() },
	}
}

var _ PersonaPrivateChatScopeRequestBuilder = privateReplyAuthorityScopeBuilderFake{}
var _ agentgate.PrivateChatScopeAuthorizer = privateReplyAuthorityChatAuthorizerFake{}
var _ PersonaRunChatReplyAuthoritySource = privateReplyAuthorityBaseFake{}
