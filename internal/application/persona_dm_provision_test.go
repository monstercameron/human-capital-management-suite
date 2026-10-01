package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
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
	if creator.request.ConversationID != wantID || creator.request.IdempotencyKey == "" || creator.request.Kind != chatcore.Direct || len(creator.request.Members) != 1 || creator.request.Members[0] != p.Persona {
		t.Fatalf("unsafe create request=%+v", creator.request)
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
