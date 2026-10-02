package application

import (
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"testing"
	"time"
)

func TestIntegrate2RemovalUsesTargetGrants(t *testing.T) {
	at := time.Now()
	r := chatstateHTTPRequest(t, "GET", ChannelStatusPath+"room", "")
	facts := chatstateDirectoryFacts{chatstateFacts: &chatstateFacts{roles: []string{chatpolicy.WorkspaceAdministratorRole}}, candidates: []chatpolicy.Principal{{ID: "target", Tenant: "tenant-a", Active: true, Roles: []string{"reopener"}, AuthorityRevision: 1}}}
	members := chatstateRoleMembers{chatstateMembers: &chatstateMembers{joined: at.Add(-time.Hour)}}
	a := chatCurrentAuthority{source: newChatAuthoritySource(facts), members: members}
	c := chat.Conversation{ID: "room", TenantID: "tenant-a", Kind: chat.PublicChannel}
	permission, err := a.ChannelStatusPermissionsForRemoval(r.Context(), chat.Principal{TenantID: "tenant-a", SubjectID: "target"}, c, at)
	if err != nil || !permission.ChangeRestricted || !permission.ChannelAdmin || permission.WorkspaceAdmin {
		t.Fatalf("target grants = %+v %v", permission, err)
	}
	if _, err = a.ChannelStatusPermissionsForRemoval(r.Context(), chat.Principal{TenantID: "foreign", SubjectID: "target"}, c, at); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("foreign removal", err)
	}
	facts.chatstateFacts.revoked = true
	if _, err = a.ChannelStatusPermissionsForRemoval(r.Context(), chat.Principal{TenantID: "tenant-a", SubjectID: "target"}, c, at); err == nil {
		t.Fatal("revoked acting principal admitted")
	}
}
