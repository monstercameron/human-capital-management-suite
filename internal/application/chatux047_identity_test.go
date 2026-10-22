package application

import (
	"bytes"
	"context"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/proto"
)

type chatIdentityCloneTestPersonas struct{}

func (chatIdentityCloneTestPersonas) ListPersonaChatIdentities(context.Context, string) ([]agentpersonastore.PersonaChatIdentity, error) {
	return nil, nil
}

func (chatIdentityCloneTestPersonas) Scoped(values.TenantId) (*agentpersonastore.TenantStore, error) {
	return nil, nil
}

func TestChatIdentityIdentifyClonesAgentBeforeSettingSubject(t *testing.T) {
	now := time.Date(2026, time.October, 21, 13, 0, 0, 0, time.UTC)
	source := &chatv1.Identity{
		SubjectId:         "canonical-agent-subject",
		DisplayName:       "Policy Helper",
		Agent:             true,
		AgentId:           "agent:policy-helper",
		AgentIcon:         &chatv1.AgentIcon{Glyph: "shield", Shape: "circle", Foreground: "#fff", Background: "#123"},
		AgentIconRevision: 7,
	}
	unknown := []byte{0x98, 0x06, 0x07} // field 99, varint 7
	source.ProtoReflect().SetUnknown(append([]byte(nil), unknown...))
	original := proto.Clone(source)

	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "harborcare-demo", Subject: "qa-reviewer", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "chatux047-identity-clone", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "sha256:chatux047-identity-clone",
	})
	if err != nil {
		t.Fatalf("create principal: %v", err)
	}
	tenant := principal.Tenant().String()
	directory := &chatIdentityDirectory{
		personas: chatIdentityCloneTestPersonas{},
		now:      func() time.Time { return now },
		agents: map[string]chatIdentityAgents{
			tenant: {at: now, byID: map[string]*chatv1.Identity{"agent:policy-helper": source}},
		},
	}

	got := directory.identify(trust.WithPrincipal(context.Background(), principal), tenant, []string{"agent:policy-helper"})["agent:policy-helper"]
	if got == nil {
		t.Fatal("agent identity was not returned")
	}
	if got == source {
		t.Fatal("returned identity aliases the cached protobuf")
	}
	if got.SubjectId != "agent:policy-helper" || got.DisplayName != "Policy Helper" || !got.Agent || got.AgentId != "agent:policy-helper" || got.AgentIconRevision != 7 {
		t.Fatalf("cloned identity lost intended subject or fields: %+v", got)
	}
	gotUnknown := got.ProtoReflect().GetUnknown()
	if got.AgentIcon == nil || got.AgentIcon.Glyph != "shield" || !bytes.Equal(gotUnknown, unknown) {
		t.Fatalf("cloned nested/unknown protobuf fields were not preserved: %+v unknown=%x", got.AgentIcon, gotUnknown)
	}
	got.AgentIcon.Glyph = "changed"
	if !proto.Equal(source, original) {
		t.Fatalf("cached source identity was mutated: got=%+v want=%+v", source, original)
	}
	if source.AgentIcon.Glyph != "shield" {
		t.Fatalf("nested icon aliases source: %+v", source.AgentIcon)
	}
}
