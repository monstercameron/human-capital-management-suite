package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENTP_008_ServeRefusesConfiguredInvocationWithoutChat(t *testing.T) {
	config := stubServeConfig()
	options := Options{}.Apply(WithStore(&stubStore{}), WithVerifier(stubVerifier{}))
	options.PersonaInvocation = &PersonaInvocationProductionConfig{}
	served, err := ComposeServe(context.Background(), ServeInput{Config: config, Identity: "persona-invocation-composition", Options: options})
	if served != nil {
		_ = served.Stop(context.Background())
	}
	if !errors.Is(err, errPersonaInvocationProductionComposition) || served != nil {
		t.Fatalf("served=%v error=%v; configured persona invocation must not silently disappear when chat is unavailable", served != nil, err)
	}
}

func TestTodo_AGENTP_008_ServedRuntimeBindsOwnedPortsWithoutMutatingOptions(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	writer := &personaChatWriterFake{}
	streaming := &streamingChatService{ConversationService: servedPortChatWriter{writer: writer}}
	refs := &lazyPersonaReferenceSource{}
	refs.bind(servedPortReferenceSource{lookup: &personaReferenceLookupFake{}})
	personas := &personaServeWiring{refs: refs, now: func() time.Time { return now }}
	config := PersonaInvocationProductionConfig{
		Authority: personaAuthorityFake{admission: personaAdmission()}, Grants: &personaGrantFake{}, T0Skills: personaT0PolicyFake{allowed: true},
		Fence: &personaRunWorkerFenceFake{}, Leases: personaRunWorkerLeaseFake{id: "lease"},
		Run: PersonaRunStarterConfig{
			Builder: mustPersonaRunBuilder(t), Authority: personaChatAdmissionAuthorityFake{},
			Model: personaRunWorkerModelFake{}, Work: &personaRunWorkerWorkFake{},
			Output: &personaRunWorkerOutputFake{}, Reply: personaRunWorkerReplyFake{},
			WorkerID: "served-worker", LeaseTTL: time.Minute, Now: func() time.Time { return now },
		},
	}
	database := composedAgentDatabase{store: &agentstore.Store{}, personas: &agentpersonastore.Store{}}
	runtime, err := composeServedPersonaInvocation(streaming, personas, database, &config, &recordingLogger{})
	if err != nil || runtime == nil || runtime.Worker == nil || streaming.personaInvocation != runtime.Wiring {
		t.Fatalf("runtime=%v binding=%v error=%v; want the complete worker installed in served chat", runtime != nil, streaming.personaInvocation != nil, err)
	}
	if config.AgentStore != nil || config.TenantUUID != nil || config.Chat != nil || config.References != nil || config.Failures != nil {
		t.Fatal("serve composition mutated the caller's options")
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow,
		SessionRef: "served-persona-test", CredentialDigest: "test-credential", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := personaSendRequest()
	request.References = nil
	post, err := streaming.sendPost(trust.WithPrincipal(context.Background(), principal), request)
	if err != nil || writer.calls != 1 || post.ID == "" {
		t.Fatalf("post=%+v calls=%d error=%v; binding must retain the authenticated chat writer", post, writer.calls, err)
	}
}

func TestTodo_AGENTP_008_ServedRuntimeLeavesChatUnboundOnIncompleteConfig(t *testing.T) {
	streaming := &streamingChatService{ConversationService: servedPortChatWriter{writer: &personaChatWriterFake{}}}
	refs := &lazyPersonaReferenceSource{}
	refs.bind(servedPortReferenceSource{lookup: &personaReferenceLookupFake{}})
	personas := &personaServeWiring{refs: refs, now: time.Now}
	database := composedAgentDatabase{store: &agentstore.Store{}, personas: &agentpersonastore.Store{}}
	runtime, err := composeServedPersonaInvocation(streaming, personas, database, &PersonaInvocationProductionConfig{}, &recordingLogger{})
	if !errors.Is(err, errPersonaInvocationProductionComposition) || runtime != nil || streaming.personaInvocation != nil {
		t.Fatalf("runtime=%v binding=%v error=%v; incomplete authority must install no handoff", runtime != nil, streaming.personaInvocation != nil, err)
	}
	if runtime, err := composeServedPersonaInvocation(nil, nil, composedAgentDatabase{}, nil, nil); runtime != nil || err != nil {
		t.Fatalf("unconfigured runtime=%v error=%v; ordinary chat remains optional", runtime, err)
	}
}
