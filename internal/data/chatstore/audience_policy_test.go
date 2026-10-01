package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_AGENT_015_ExplicitAudiencePolicyProvisioningAndCAS(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "private", TenantID: "tenant-a", Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "owner", HomeTenantID: "tenant-a", Role: "manager", State: "active"}})
	if _, err := s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "private"); !errors.Is(err, ErrAudienceEligibilityUnavailable) {
		t.Fatalf("snapshot without persisted policy = %v, want fail closed", err)
	}
	policy := AudiencePolicy{RequiredRoles: []string{"manager"}, RoleMode: 1, Classification: "T2", Residency: "us"}
	revision, err := s.PutAudiencePolicy(context.Background(), "tenant-a", "private", 0, policy)
	if err != nil || revision != 1 {
		t.Fatalf("provision policy = revision %d, err %v; want revision 1", revision, err)
	}
	snapshot, err := s.CaptureAudienceSnapshot(context.Background(), "tenant-a", "private")
	if err != nil {
		t.Fatal(err)
	}
	if err = chatauthority.New(s).PutPolicy(context.Background(), "tenant-a", "private", []string{"manager"}, nil, nil, nil, chatpolicy.RolesAny, "T3", "us", 1, "tenant-admin"); err != nil {
		t.Fatalf("alternate authority policy update = %v", err)
	}
	if err := s.WithAudienceFence(context.Background(), snapshot, func(dbport.Tx) error { return nil }); !errors.Is(err, ErrAudienceChanged) {
		t.Fatalf("old snapshot fence after alternate writer = %v, want ErrAudienceChanged", err)
	}
	policy.Classification = "T4"
	if _, err := s.PutAudiencePolicy(context.Background(), "tenant-a", "private", 1, policy); !errors.Is(err, ErrAudiencePolicyConflict) {
		t.Fatalf("stale policy update = %v, want ErrAudiencePolicyConflict", err)
	}
}

func TestTodo_AGENT_015_AudiencePolicyProvisioningIsTenantAndKindScoped(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversationRow(t, s, Conversation{ID: "direct", TenantID: "tenant-a", Kind: "DIRECT_MESSAGE", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	policy := AudiencePolicy{RoleMode: 2, Classification: "T1"}
	if _, err := s.PutAudiencePolicy(context.Background(), "tenant-b", "direct", 0, policy); err == nil {
		t.Fatal("foreign tenant provisioned a chat policy")
	}
	seedConversationRow(t, s, Conversation{ID: "public", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	if _, err := s.PutAudiencePolicy(context.Background(), "tenant-a", "public", 0, policy); err == nil {
		t.Fatal("private/group/direct policy path accepted a public channel")
	}
}
