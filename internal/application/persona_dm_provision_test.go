package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type personaDMProvisionResolver struct {
	id         string
	createMiss bool
	calls      int
}

func (r *personaDMProvisionResolver) ResolvePersonaDM(context.Context, chatcore.Principal, string) (string, error) {
	r.calls++
	if r.createMiss && r.calls == 1 {
		return "", chatcore.ErrPermissionDenied
	}
	if r.id == "" {
		return "", chatcore.ErrPermissionDenied
	}
	return r.id, nil
}

type personaDMProvisionCreator struct {
	request chatcore.CreateConversationRequest
	err     error
}

type personaDMPolicyFake struct {
	audienceWrites int
	channelWrites  int
	snapshot       *chatstore.PersonaChannelPolicySnapshot
}

func (f *personaDMPolicyFake) PutAudiencePolicy(context.Context, string, string, int64, chatstore.AudiencePolicy) (int64, error) {
	f.audienceWrites++
	return 1, nil
}
func (f *personaDMPolicyFake) CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error) {
	if f.snapshot == nil {
		return chatstore.PersonaChannelPolicySnapshot{}, dbport.ErrNoRows
	}
	return *f.snapshot, nil
}
func (f *personaDMPolicyFake) PutPersonaChannelPolicy(_ context.Context, _, _ string, _ int64, policy chatstore.PersonaChannelPolicy) (int64, error) {
	f.channelWrites++
	f.snapshot = &chatstore.PersonaChannelPolicySnapshot{Policy: policy, PolicyRevision: int64(f.channelWrites)}
	return int64(f.channelWrites), nil
}

func (c *personaDMProvisionCreator) CreateConversation(_ context.Context, request chatcore.CreateConversationRequest) (chatcore.Conversation, error) {
	c.request = request
	if c.err != nil {
		return chatcore.Conversation{}, c.err
	}
	return chatcore.Conversation{ID: request.ConversationID, TenantID: request.TenantID, Kind: request.Kind}, nil
}

func newPersonaDMProvisionerFixture(t *testing.T, resolver *personaDMProvisionResolver, creator *personaDMProvisionCreator) PersonaDMProvisioner {
	t.Helper()
	provisioner, err := NewPersonaDMProvisioner(creator, resolver, chatcore.MemberRef{TenantID: "tenant-a", SubjectID: "persona-1"})
	if err != nil {
		t.Fatal(err)
	}
	return provisioner
}

func TestTodo_AGENTP_011_PersonaDMProvisionerReusesExistingDM(t *testing.T) {
	resolver := &personaDMProvisionResolver{id: "existing"}
	creator := &personaDMProvisionCreator{}
	got, err := newPersonaDMProvisionerFixture(t, resolver, creator).EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
	if err != nil || got != "existing" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if creator.request.ConversationID != "" {
		t.Fatal("existing DM unexpectedly created")
	}
}

func TestTodo_AGENTP_011_PersonaDMProvisionerCreatesAndVerifiesDeterministicPair(t *testing.T) {
	wantID, _ := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "human-1"}, {TenantID: "tenant-a", SubjectID: "persona-1"}})
	resolver := &personaDMProvisionResolver{id: wantID, createMiss: true}
	creator := &personaDMProvisionCreator{}
	p := newPersonaDMProvisionerFixture(t, resolver, creator)
	got, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
	if err != nil || got != wantID {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if creator.request.ConversationID != wantID || creator.request.IdempotencyKey == "" || creator.request.Kind != chatcore.Direct || creator.request.Name != "Persona 1" || len(creator.request.Members) != 1 || creator.request.Members[0] != p.Persona {
		t.Fatalf("unsafe create request=%+v", creator.request)
	}
}

func TestTodo_AGENTUX_025_PersonaDMProvisionerCreatesAndRepairsPolicies(t *testing.T) {
	wantID, _ := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "human-1"}, {TenantID: "tenant-a", SubjectID: "persona-1"}})
	resolver := &personaDMProvisionResolver{id: wantID, createMiss: true}
	policies := &personaDMPolicyFake{}
	p := newPersonaDMProvisionerFixture(t, resolver, &personaDMProvisionCreator{})
	p.Policies = policies
	got, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
	if err != nil || got != wantID {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if policies.audienceWrites != 1 || policies.channelWrites != 1 || policies.snapshot == nil || !policies.snapshot.Policy.ConversationSearchAllowed || !policies.snapshot.Policy.AlwaysPrivate {
		t.Fatalf("policy writes audience=%d channel=%d snapshot=%+v", policies.audienceWrites, policies.channelWrites, policies.snapshot)
	}

	existing := &personaDMPolicyFake{}
	p = newPersonaDMProvisionerFixture(t, &personaDMProvisionResolver{id: wantID}, &personaDMProvisionCreator{})
	p.Policies = existing
	if _, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if existing.audienceWrites != 1 || existing.channelWrites != 1 {
		t.Fatalf("existing conversation without policies was not repaired: %+v", existing)
	}
}

func TestTodo_AGENTUX_025_PersonaDMProvisionerLeavesAdministratorPolicyAlone(t *testing.T) {
	policies := &personaDMPolicyFake{snapshot: &chatstore.PersonaChannelPolicySnapshot{Policy: chatstore.PersonaChannelPolicy{MaxTier: "T1", PlacementClass: "ONE_TO_ONE_DM", AllowedDataClasses: []string{"PUBLIC"}, AllowedChannelClasses: []string{"ONE_TO_ONE"}, AlwaysPrivate: true}}}
	p := newPersonaDMProvisionerFixture(t, &personaDMProvisionResolver{id: "existing"}, &personaDMProvisionCreator{})
	p.Policies = policies
	if _, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if policies.channelWrites != 0 {
		t.Fatalf("administrator-authored channel policy was overwritten")
	}
}

func TestTodo_AGENTUX_030_PersonaDMUsesAgentDisplayName(t *testing.T) {
	wantID, _ := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: "human-1"}, {TenantID: "tenant-a", SubjectID: "policy-helper"}})
	resolver := &personaDMProvisionResolver{id: wantID, createMiss: true}
	creator := &personaDMProvisionCreator{}
	provisioner, err := NewPersonaDMProvisioner(creator, resolver, chatcore.MemberRef{TenantID: "tenant-a", SubjectID: "policy-helper"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provisioner.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); err != nil {
		t.Fatal(err)
	}
	if creator.request.Name != "Policy Helper" {
		t.Fatalf("conversation name=%q, want Policy Helper", creator.request.Name)
	}
}

func TestTodo_AGENTP_011_PersonaDMProvisionerFailsClosed(t *testing.T) {
	resolver := &personaDMProvisionResolver{id: "", createMiss: false}
	creator := &personaDMProvisionCreator{}
	p := newPersonaDMProvisionerFixture(t, resolver, creator)
	if _, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-b"); !errors.Is(err, errPersonaDMProvisionInvalid) {
		t.Fatalf("wrong tenant error=%v", err)
	}
	resolver.id = ""
	creator.err = chatcore.ErrUnavailable
	if _, err := p.EnsurePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); !errors.Is(err, errPersonaDMProvisionUnavailable) {
		t.Fatalf("create failure error=%v", err)
	}
}
