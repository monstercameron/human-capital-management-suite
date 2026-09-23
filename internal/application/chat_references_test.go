package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
)

type referenceDirectoryStore struct {
	chat.Store
	members       chat.ListMembershipsResponse
	conversations chat.ListConversationsResponse
	err           error
	principal     chat.Principal
	tenant        string
}

func (s *referenceDirectoryStore) ListMemberships(_ context.Context, tenant, _ string, _ chat.Page) (chat.ListMembershipsResponse, error) {
	s.tenant = tenant
	return s.members, s.err
}
func (s *referenceDirectoryStore) ListConversations(_ context.Context, p chat.Principal, tenant string, _ chat.Page, _ chat.ConversationScope) (chat.ListConversationsResponse, error) {
	s.principal, s.tenant = p, tenant
	return s.conversations, s.err
}

func TestChatReferenceDirectoryFiltersCurrentScope(t *testing.T) {
	ctx := context.Background()
	left := time.Now()
	store := &referenceDirectoryStore{
		members: chat.ListMembershipsResponse{Memberships: []chat.Membership{
			{HomeTenantID: "home", SubjectID: "Alice"}, {HomeTenantID: "home", SubjectID: "Alice-left", LeftAt: &left}, {HomeTenantID: "home", SubjectID: "Bob"},
		}},
		conversations: chat.ListConversationsResponse{Conversations: []chat.Conversation{{ID: "sales", TenantID: "host", Name: "Sales Team"}, {ID: "support", TenantID: "host", Name: "Support"}}},
	}
	d := chatReferenceDirectory{store: store}
	p := chat.Principal{TenantID: "home", SubjectID: "viewer"}
	people, err := d.People(ctx, p, "host", "conv", " ALI ")
	if err != nil || len(people) != 1 || people[0].Reference.ID != "Alice" || people[0].Reference.TenantID != "home" || !people[0].Eligible || store.tenant != "host" {
		t.Fatalf("people=%+v tenant=%s err=%v", people, store.tenant, err)
	}
	rooms, err := d.Conversations(ctx, p, "host", "conv", " TEAM ")
	if err != nil || len(rooms) != 1 || rooms[0].Reference.ID != "sales" || !reflect.DeepEqual(store.principal, p) || store.tenant != "host" {
		t.Fatalf("rooms=%+v principal=%+v err=%v", rooms, store.principal, err)
	}
	rooms, err = d.VisibleConversations(ctx, "home", "viewer", "SUP")
	if err != nil || len(rooms) != 1 || rooms[0].Reference.ID != "support" || !reflect.DeepEqual(store.principal, p) || store.tenant != "home" {
		t.Fatalf("visible=%+v principal=%+v err=%v", rooms, store.principal, err)
	}
	fault := errors.New("directory unavailable")
	store.err = fault
	if _, err := d.People(ctx, p, "host", "conv", ""); !errors.Is(err, fault) {
		t.Fatalf("people failure=%v", err)
	}
	if _, err := d.Conversations(ctx, p, "host", "conv", ""); !errors.Is(err, fault) {
		t.Fatalf("rooms failure=%v", err)
	}
	if _, err := d.VisibleConversations(ctx, "home", "viewer", ""); !errors.Is(err, fault) {
		t.Fatalf("visible failure=%v", err)
	}
}

func TestChatReferenceDirectoryAgentsRespectInstallation(t *testing.T) {
	ctx := context.Background()
	at := time.Now().UTC()
	repo := chatapps.NewMemoryRepository()
	for _, row := range []struct {
		id, tenant, conversation, name string
		status                         chatapps.Status
		agent                          bool
	}{
		{"active", "host", "conv", "Payroll Helper", chatapps.Active, true},
		{"paused", "host", "conv", "Payroll Paused", chatapps.Suspended, true},
		{"other", "other", "conv", "Payroll Other", chatapps.Active, true},
		{"elsewhere", "host", "elsewhere", "Payroll Elsewhere", chatapps.Active, true},
		{"app", "host", "conv", "", chatapps.Active, false},
	} {
		v := chatapps.Installation{ID: row.id, Tenant: row.tenant, Conversation: row.conversation, Version: 1, Status: row.status, CreatedAt: at.Add(-time.Hour)}
		if row.agent {
			v.Manifest.Agent = &chatapps.AgentManifest{DisplayName: row.name}
		}
		if err := repo.Put(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	d := chatReferenceDirectory{apps: &chatapps.Service{Repo: repo, Now: func() time.Time { return at }}}
	for _, query := range []string{" PAYROLL ", "active", ""} {
		got, err := d.Agents(ctx, chat.Principal{}, "host", "conv", query)
		if err != nil || len(got) != 1 || got[0].Reference.ID != "active" || !got[0].Eligible {
			t.Fatalf("query=%q agents=%+v err=%v", query, got, err)
		}
	}
	if got, err := d.Agents(ctx, chat.Principal{}, "host", "conv", "missing"); err != nil || len(got) != 0 {
		t.Fatalf("unmatched=%+v %v", got, err)
	}
	for _, id := range []string{"active", "paused", "other", "elsewhere", "app", "missing"} {
		if got := d.AgentEligible(ctx, "host", "conv", id); got != (id == "active") {
			t.Fatalf("eligible(%s)=%v", id, got)
		}
	}
	d.apps = nil
	if got, err := d.Agents(ctx, chat.Principal{}, "host", "conv", ""); err != nil || len(got) != 0 || d.AgentEligible(ctx, "host", "conv", "active") {
		t.Fatalf("unconfigured=%+v %v", got, err)
	}
}
