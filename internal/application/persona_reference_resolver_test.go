package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type personaReferenceLookupFake struct {
	byReference map[string]personaReferenceFacts
	errByID     map[string]error
	calls       []string
}

func (f *personaReferenceLookupFake) LookupPersonaReference(_ context.Context, tenant, conversation, id string) (personaReferenceFacts, error) {
	f.calls = append(f.calls, tenant+"/"+conversation+"/"+id)
	if err := f.errByID[id]; err != nil {
		return personaReferenceFacts{}, err
	}
	facts, ok := f.byReference[id]
	if !ok {
		return personaReferenceFacts{}, errPersonaReferenceNotPersona
	}
	return facts, nil
}

func personaReferenceResolverFixture(t *testing.T, lookup personaReferenceLookup) *personaChatReferenceResolver {
	t.Helper()
	resolver, err := newPersonaChatReferenceResolver(lookup, func() time.Time {
		return time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func currentPersonaReferenceFacts(referenceID string) personaReferenceFacts {
	return personaReferenceFacts{
		ReferenceID: referenceID, TenantID: "tenant-a", ConversationID: "channel-a",
		PersonaID: "persona.comp-analyst", InstallationID: "installation:channel-a:comp-analyst",
		PersonaVersion: 3, CurrentVersion: 3, InstallationState: personaReferenceActive,
		PersonaLifecycle: personaReferencePublished, InstallationExpires: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
	}
}

func personaReference(id, display string) chatcore.Reference {
	return chatcore.Reference{Kind: chatcore.AgentMention, TenantID: "tenant-a", ID: id, Display: display}
}

func TestPersonaChatReferenceResolver_UsesCurrentTypedInstallationAndIgnoresDisplay(t *testing.T) {
	facts := currentPersonaReferenceFacts("agent-ref:comp")
	lookup := &personaReferenceLookupFake{byReference: map[string]personaReferenceFacts{"agent-ref:comp": facts}}
	resolver := personaReferenceResolverFixture(t, lookup)
	got, err := resolver.ResolvePersonaMentions(context.Background(), "tenant-a", "channel-a", []chatcore.Reference{
		personaReference("agent-ref:comp", "@Comp Analyst (ignore me)"),
		{Kind: chatcore.PersonMention, TenantID: "tenant-a", ID: "person-1", Display: "Person"},
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("ResolvePersonaMentions=%+v err=%v", got, err)
	}
	if got[0].Kind != agentinvoke.PersonaMention || got[0].PersonaID != facts.PersonaID || !got[0].Canonical || got[0].Display != "" {
		t.Fatalf("resolved mention used noncanonical identity data: %+v", got[0])
	}
	if len(lookup.calls) != 1 || lookup.calls[0] != "tenant-a/channel-a/agent-ref:comp" {
		t.Fatalf("lookup coordinates=%v", lookup.calls)
	}
}

func TestPersonaChatReferenceResolver_RejectsMalformedForeignAndDuplicateReferences(t *testing.T) {
	cases := []struct {
		name       string
		references []chatcore.Reference
		want       error
	}{
		{name: "malformed id", references: []chatcore.Reference{personaReference("../agent", "Comp")}, want: errPersonaReferenceInvalid},
		{name: "foreign tenant", references: []chatcore.Reference{{Kind: chatcore.AgentMention, TenantID: "tenant-b", ID: "agent-ref:comp"}}, want: errPersonaReferenceInvalid},
		{name: "duplicate reference", references: []chatcore.Reference{personaReference("agent-ref:comp", "first"), personaReference("agent-ref:comp", "second")}, want: errPersonaReferenceDuplicate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup := &personaReferenceLookupFake{byReference: map[string]personaReferenceFacts{"agent-ref:comp": currentPersonaReferenceFacts("agent-ref:comp")}}
			resolver := personaReferenceResolverFixture(t, lookup)
			if _, err := resolver.ResolvePersonaMentions(context.Background(), "tenant-a", "channel-a", tc.references); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if tc.name != "duplicate reference" && len(lookup.calls) != 0 {
				t.Fatalf("invalid reference reached lookup: %v", lookup.calls)
			}
		})
	}
}

func TestPersonaChatReferenceResolver_RejectsGenericChatAppReference(t *testing.T) {
	lookup := &personaReferenceLookupFake{errByID: map[string]error{"chatapp-install": errPersonaReferenceNotPersona}}
	resolver := personaReferenceResolverFixture(t, lookup)
	if _, err := resolver.ResolvePersonaMentions(context.Background(), "tenant-a", "channel-a", []chatcore.Reference{personaReference("chatapp-install", "A chat app")}); !errors.Is(err, errPersonaReferenceNotPersona) {
		t.Fatalf("generic app reference error=%v", err)
	}
}

func TestPersonaChatReferenceResolver_RejectsExpiredSuspendedAndStaleInstallations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*personaReferenceFacts)
	}{
		{name: "expired", mutate: func(f *personaReferenceFacts) {
			f.InstallationExpires = time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
		}},
		{name: "suspended install", mutate: func(f *personaReferenceFacts) { f.InstallationState = "SUSPENDED" }},
		{name: "retired persona", mutate: func(f *personaReferenceFacts) { f.PersonaLifecycle = "RETIRED" }},
		{name: "old version", mutate: func(f *personaReferenceFacts) { f.CurrentVersion++ }},
		{name: "foreign lookup result", mutate: func(f *personaReferenceFacts) { f.TenantID = "tenant-b" }},
		{name: "wrong conversation", mutate: func(f *personaReferenceFacts) { f.ConversationID = "channel-b" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := currentPersonaReferenceFacts("agent-ref:comp")
			tc.mutate(&facts)
			lookup := &personaReferenceLookupFake{byReference: map[string]personaReferenceFacts{"agent-ref:comp": facts}}
			resolver := personaReferenceResolverFixture(t, lookup)
			if _, err := resolver.ResolvePersonaMentions(context.Background(), "tenant-a", "channel-a", []chatcore.Reference{personaReference("agent-ref:comp", "Comp Analyst")}); !errors.Is(err, errPersonaReferenceInactive) {
				t.Fatalf("error=%v, want inactive installation", err)
			}
		})
	}
}

func TestPersonaChatReferenceResolver_RejectsDuplicatePersonaIdentity(t *testing.T) {
	first, second := currentPersonaReferenceFacts("agent-ref:one"), currentPersonaReferenceFacts("agent-ref:two")
	lookup := &personaReferenceLookupFake{byReference: map[string]personaReferenceFacts{"agent-ref:one": first, "agent-ref:two": second}}
	resolver := personaReferenceResolverFixture(t, lookup)
	refs := []chatcore.Reference{personaReference("agent-ref:one", "One"), personaReference("agent-ref:two", "Two")}
	if _, err := resolver.ResolvePersonaMentions(context.Background(), "tenant-a", "channel-a", refs); !errors.Is(err, errPersonaReferenceDuplicate) {
		t.Fatalf("duplicate persona error=%v", err)
	}
}

func TestPersonaChatReferenceResolver_PropagatesLookupFailureAndRequiresInputs(t *testing.T) {
	lookupErr := errors.New("persona store unavailable")
	lookup := &personaReferenceLookupFake{errByID: map[string]error{"agent-ref:comp": lookupErr}}
	resolver := personaReferenceResolverFixture(t, lookup)
	if _, err := resolver.ResolvePersonaMentions(context.Background(), "tenant-a", "channel-a", []chatcore.Reference{personaReference("agent-ref:comp", "Comp")}); !errors.Is(err, lookupErr) {
		t.Fatalf("lookup error=%v", err)
	}
	if _, err := newPersonaChatReferenceResolver(nil, nil); !errors.Is(err, errPersonaReferenceInvalid) {
		t.Fatalf("nil lookup error=%v", err)
	}
	if _, err := resolver.ResolvePersonaMentions(context.Background(), "", "channel-a", nil); !errors.Is(err, errPersonaReferenceInvalid) {
		t.Fatalf("empty tenant error=%v", err)
	}
}
