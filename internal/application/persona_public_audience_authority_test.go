package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaPublicAuthorityStoreFake struct {
	snapshot                                                                                         chatstore.PublicAudienceSnapshot
	policy                                                                                           chatstore.PersonaChannelPolicySnapshot
	err                                                                                              error
	disclosureCalls                                                                                  int
	lastHost, lastRoom, lastHome, lastSubject, lastPost, lastDigest, lastField, lastTitle, lastClass string
}

func (s *personaPublicAuthorityStoreFake) CapturePublicAudienceSnapshot(context.Context, string, string) (chatstore.PublicAudienceSnapshot, error) {
	return s.snapshot, s.err
}

func (s *personaPublicAuthorityStoreFake) PublicChatDisclosureClass(context.Context, string, string, string, string) (dlp.DataClass, error) {
	return dlp.ClassInternal, s.err
}
func (s *personaPublicAuthorityStoreFake) ReadableChatDisclosureClass(_ context.Context, tenant, home, subject, _, _, _ string) (dlp.DataClass, error) {
	if tenant != "tenant-a" || home != "tenant-a" || subject != "alice" {
		return "", chatstore.ErrNotMember
	}
	return dlp.ClassInternal, s.err
}
func (s *personaPublicAuthorityStoreFake) CapturePersonaChannelPolicy(_ context.Context, _, _, manager string) (chatstore.PersonaChannelPolicySnapshot, error) {
	out := s.policy
	out.ManagerID = manager
	return out, s.err
}
func (s *personaPublicAuthorityStoreFake) AuthorizePublicChatDisclosure(_ context.Context, host, room, home, subject, post, digest, field, title, class string) error {
	s.disclosureCalls++
	s.lastHost, s.lastRoom, s.lastHome, s.lastSubject, s.lastPost, s.lastField, s.lastTitle, s.lastClass = host, room, home, subject, post, field, title, class
	s.lastDigest = digest
	return s.err
}

func publicAuthorityApplicationFixture(t *testing.T) (*PersonaPublicAudienceAuthority, *personaPublicAuthorityStoreFake, context.Context, chat.Conversation) {
	t.Helper()
	conversation := chat.Conversation{TenantID: "tenant-a", ID: "room", Kind: chat.PublicChannel, Revision: 1}
	store := &personaPublicAuthorityStoreFake{snapshot: chatstore.PublicAudienceSnapshot{TenantID: "tenant-a", ConversationID: "room", Revision: 7, PolicyRevision: 1, Classification: "INTERNAL", Current: []chatstore.PublicAudiencePrincipal{{HomeTenantID: "tenant-a", SubjectID: "alice"}, {HomeTenantID: "tenant-b", SubjectID: "guest", Guest: true}}, Eligible: []chatstore.PublicAudiencePrincipal{{HomeTenantID: "tenant-a", SubjectID: "alice"}, {HomeTenantID: "tenant-b", SubjectID: "guest", Guest: true}, {HomeTenantID: "tenant-a", SubjectID: "future"}}}, policy: chatstore.PersonaChannelPolicySnapshot{TenantID: "tenant-a", ConversationID: "room", Kind: string(chat.PublicChannel), Revision: 7, PolicyRevision: 1, Policy: chatstore.PersonaChannelPolicy{PlacementClass: "ANY_INTERNAL", MaxTier: "T2", AllowedDataClasses: []string{"PUBLIC", "INTERNAL"}, AllowedChannelClasses: []string{"PUBLIC"}}}}
	ctx := personaRunAudienceContext(t, "tenant-a", "alice")
	return &PersonaPublicAudienceAuthority{Store: store}, store, ctx, conversation
}

func TestTodo_AGENTP_012_PublicAuthority(t *testing.T) {
	authority, store, ctx, conversation := publicAuthorityApplicationFixture(t)
	snapshot, err := authority.ReadPersonaAudienceFloorSnapshot(ctx, conversation)
	if err != nil || snapshot.Revision != 7 || !snapshot.FenceComplete || len(snapshot.CurrentMembers) != 2 || len(snapshot.EligibleFutureMembers) != 3 || !snapshot.CurrentMembers[1].Guest || !snapshot.CurrentMembers[1].External {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	disclosure := chatrecipient.Disclosure{SourceID: "chat:post", RecordID: "post", Field: "body", DataClass: dlp.ClassInternal}
	if err = authority.AuthorizePersonaAudienceDisclosure(ctx, snapshot.CurrentMembers[0], disclosure); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) || store.disclosureCalls != 0 {
		t.Fatalf("unbound room err=%v calls=%d", err, store.disclosureCalls)
	}
	bound := WithPersonaPublicAudienceSourceDigests(WithPersonaPublicAudienceConversation(ctx, "tenant-a", "room"), map[string]string{"post": "sha256:body"})
	if class, err := authority.PersonaPublicChatDisclosureClass(ctx, "tenant-a", "room", "post", "sha256:body"); err != nil || class != dlp.ClassInternal {
		t.Fatalf("source classification=%s %v", class, err)
	}
	if err = authority.AuthorizePersonaAudienceDisclosure(bound, snapshot.CurrentMembers[0], disclosure); err != nil || store.disclosureCalls != 1 || store.lastRoom != "room" || store.lastHost != "tenant-a" || store.lastHome != "tenant-a" || store.lastSubject != "alice" || store.lastPost != "post" || store.lastDigest != "sha256:body" || store.lastField != "body" || store.lastClass != "INTERNAL" {
		t.Fatalf("disclosure=%+v err=%v", store, err)
	}
	if err = authority.AllowPersonaAudienceDataClass(ctx, conversation, dlp.ClassInternal); err != nil {
		t.Fatal(err)
	}
	if err = authority.AllowPersonaAudienceDataClass(ctx, conversation, dlp.ClassCompensation); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("compensation=%v", err)
	}
	store.policy.Policy.AlwaysPrivate = true
	if _, err = authority.ReadPersonaAudienceFloorSnapshot(ctx, conversation); err != nil {
		t.Fatalf("private-route invocation audience unavailable=%v", err)
	}
	if err = authority.AllowPersonaAudienceDataClass(ctx, conversation, dlp.ClassInternal); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("always-private public disclosure=%v", err)
	}
	store.policy.Policy.AlwaysPrivate = false
	principal, _ := trust.FromContext(ctx)
	facts, err := authority.ResolvePersonaAdminPlacement(ctx, PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}, "room")
	if err != nil || facts.Revision != 7 || facts.ManagerID != "alice" || facts.Policy.MaxTier != "T2" {
		t.Fatalf("placement=%+v err=%v", facts, err)
	}
	if NewPersonaPublicAudienceAuthority(nil).Store == nil {
		t.Fatal("constructor lost typed store port")
	}
}

func TestTodo_AGENTP_012_PublicAuthority_Security(t *testing.T) {
	for _, mutate := range []func(*personaPublicAuthorityStoreFake){
		func(s *personaPublicAuthorityStoreFake) { s.err = errors.New("unavailable") },
		func(s *personaPublicAuthorityStoreFake) { s.snapshot.Current = nil },
		func(s *personaPublicAuthorityStoreFake) { s.snapshot.Eligible = nil },
		func(s *personaPublicAuthorityStoreFake) { s.snapshot.TenantID = "foreign" },
		func(s *personaPublicAuthorityStoreFake) { s.snapshot.Revision = 0 },
		func(s *personaPublicAuthorityStoreFake) { s.policy.Revision++ },
		func(s *personaPublicAuthorityStoreFake) { s.policy.Policy.AllowedChannelClasses = []string{"PRIVATE"} },
		func(s *personaPublicAuthorityStoreFake) { s.snapshot.Eligible[1].Guest = false },
	} {
		authority, store, ctx, conversation := publicAuthorityApplicationFixture(t)
		mutate(store)
		if _, err := authority.ReadPersonaAudienceFloorSnapshot(ctx, conversation); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
			t.Fatalf("incomplete or raced authority err=%v snapshot=%+v", err, store.snapshot)
		}
	}
	if _, err := NewPersonaPublicAudienceAuthority(nil).ReadPersonaAudienceFloorSnapshot(context.Background(), chat.Conversation{}); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("nil source err=%v", err)
	}
	authority, store, ctx, conversation := publicAuthorityApplicationFixture(t)
	store.err = errors.New("down")
	if err := authority.AllowPersonaAudienceDataClass(ctx, conversation, dlp.ClassPublic); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("policy outage=%v", err)
	}
	store.err = nil
	for _, bound := range []context.Context{context.Background(), WithPersonaPublicAudienceConversation(ctx, "foreign", "room"), WithPersonaPublicAudienceConversation(ctx, "tenant-a", "")} {
		if err := authority.AuthorizePersonaAudienceDisclosure(bound, chatrecipient.AudiencePrincipal{TenantID: "tenant-a", SubjectID: "alice"}, chatrecipient.Disclosure{SourceID: "chat:post", RecordID: "post", Field: "body", DataClass: dlp.ClassPublic}); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
			t.Fatalf("forged context=%v", err)
		}
	}
	bad := chatrecipient.Disclosure{SourceID: "private-document:post", RecordID: "post", Field: "body", DataClass: dlp.ClassPublic}
	if err := authority.AuthorizePersonaAudienceDisclosure(WithPersonaPublicAudienceConversation(ctx, "tenant-a", "room"), chatrecipient.AudiencePrincipal{TenantID: "tenant-a", SubjectID: "alice"}, bad); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("unknown source=%v", err)
	}
}
