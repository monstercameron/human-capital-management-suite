package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
)

func TestChatExtensionMissingServicesFailClosed(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "tenant", SubjectID: "owner"}
	calls := map[string]func(*ChatExtensions) error{
		"counts": func(s *ChatExtensions) error { _, err := s.Counts(ctx, p, "tenant", "conv"); return err },
		"follow": func(s *ChatExtensions) error { _, err := s.ThreadFollow(ctx, p, "tenant", "conv", "root"); return err },
		"put follow": func(s *ChatExtensions) error {
			_, err := s.PutThreadFollow(ctx, p, "tenant", "conv", chatrecipient.Follow{}, 1)
			return err
		},
		"sidebar":     func(s *ChatExtensions) error { _, err := s.Sidebar(ctx, p); return err },
		"put sidebar": func(s *ChatExtensions) error { _, err := s.PutSidebar(ctx, p, chatrecipient.Sidebar{}, 1); return err },
		"quiet hours": func(s *ChatExtensions) error { _, err := s.QuietHours(ctx, p); return err },
		"put quiet hours": func(s *ChatExtensions) error {
			_, err := s.PutQuietHours(ctx, p, chatrecipient.QuietHours{}, 1)
			return err
		},
		"propose grant": func(s *ChatExtensions) error {
			_, err := s.ProposeGrant(ctx, "tenant", "guest", "conv", "internal", "US", time.Now().Add(time.Hour))
			return err
		},
		"accept grant": func(s *ChatExtensions) error { return s.AcceptGrant(ctx, "tenant", "guest", "conv", "grant") },
		"revoke grant": func(s *ChatExtensions) error { return s.RevokeGrant(ctx, "tenant", "guest", "conv", "grant") },
		"policy": func(s *ChatExtensions) error {
			return s.SetChannelPolicy(ctx, "tenant", "conv", nil, nil, nil, nil, chatpolicy.RoleMode(0), "internal", "US", 1)
		},
		"install": func(s *ChatExtensions) error {
			_, err := s.Install(ctx, p, "conv", chatapps.Manifest{}, nil)
			return err
		},
		"list": func(s *ChatExtensions) error { _, err := s.ListInstallations(ctx, p, "conv"); return err },
		"status": func(s *ChatExtensions) error {
			_, err := s.ChangeStatus(ctx, p, "conv", "app", chatapps.Suspended)
			return err
		},
		"invoke": func(s *ChatExtensions) error {
			_, err := s.Invoke(ctx, p, "conv", "app", chatapps.Callback{})
			return err
		},
		"agent": func(s *ChatExtensions) error { _, err := s.Agent(ctx, p, "conv", "app"); return err },
		"propose intent": func(s *ChatExtensions) error {
			_, err := s.ProposeIntent(ctx, p, "conv", chatapps.Proposal{})
			return err
		},
		"report": func(s *ChatExtensions) error { return s.Report(ctx, p, chatrecords.Report{}) },
		"moderate": func(s *ChatExtensions) error {
			return s.Moderate(ctx, p, "conv", "case", "remove", "post", "reason", "evidence")
		},
		"cursor": func(s *ChatExtensions) error { _, err := s.IssueEventCursor(ctx, p, "conv", "app", 0); return err },
		"pull":   func(s *ChatExtensions) error { _, _, err := s.PullEvents(ctx, p, "conv", "token", 10); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			for _, s := range []*ChatExtensions{nil, {}} {
				if err := call(s); !errors.Is(err, chat.ErrUnavailable) {
					t.Fatalf("unconfigured operation = %v, want unavailable", err)
				}
			}
		})
	}
}

type extensionIntentCapture struct{ proposal chatapps.Proposal }

func (c *extensionIntentCapture) Propose(_ context.Context, p chatapps.Proposal) (chatapps.ProposalReceipt, error) {
	c.proposal = p
	return chatapps.ProposalReceipt{IntentID: "intent", Status: "PROPOSED"}, nil
}

func TestChatExtensionAgentAndEventIsolation(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "tenant", SubjectID: "owner"}
	repo := chatapps.NewMemoryRepository()
	records := chatrecords.NewMemoryRepository()
	intent := &extensionIntentCapture{}
	s := &ChatExtensions{
		Conversations: extensionConversations{},
		Apps:          &chatapps.Service{Repo: repo, Authority: ChatAppAuthority{Conversations: extensionConversations{}}, Intent: intent, Secret: []byte("extension-test-cursor-secret")},
		Records:       &chatrecords.Service{Repo: records, Auth: ChatRecordAuthority{}},
	}
	installed, err := s.Install(ctx, p, "conv", chatapps.Manifest{AppID: "agent", Version: 1, Agent: &chatapps.AgentManifest{DisplayName: "Assistant"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListInstallations(ctx, p, "conv")
	if err != nil || len(listed) != 1 || listed[0].ID != installed.ID {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	agent, err := s.Agent(ctx, p, "conv", installed.ID)
	if err != nil || agent.DisplayName != "Assistant" || agent.Status != chatapps.Active {
		t.Fatalf("agent = %+v, %v", agent, err)
	}
	if _, err := s.Agent(ctx, p, "different", installed.ID); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("cross-conversation agent = %v", err)
	}
	if _, err := s.Agent(ctx, p, "conv", "missing"); !errors.Is(err, chatapps.ErrNotFound) {
		t.Fatalf("absent agent = %v", err)
	}
	proposal := chatapps.Proposal{Tenant: "forged", Principal: "forged", Conversation: "forged", AgentInstallation: installed.ID, IntentType: "promotion", IdempotencyKey: "once"}
	receipt, err := s.ProposeIntent(ctx, p, "conv", proposal)
	if err != nil || receipt.IntentID != "intent" || intent.proposal.Tenant != p.TenantID || intent.proposal.Principal != p.SubjectID || intent.proposal.Conversation != "conv" {
		t.Fatalf("proposal boundary: %+v %+v %v", receipt, intent.proposal, err)
	}
	token, err := s.IssueEventCursor(ctx, p, "conv", installed.ID, 0)
	if err != nil || token == "" {
		t.Fatalf("cursor = %q %v", token, err)
	}
	events, next, err := s.PullEvents(ctx, p, "conv", token, 10)
	if err != nil || len(events) != 0 || next == "" {
		t.Fatalf("empty event page: %+v %q %v", events, next, err)
	}
	if _, _, err := s.PullEvents(ctx, p, "different", token, 10); !errors.Is(err, chatapps.ErrDenied) {
		t.Fatalf("cross-conversation cursor = %v", err)
	}
	if _, err := s.IssueEventCursor(ctx, p, "different", installed.ID, 0); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("cross-conversation cursor issue = %v", err)
	}
	if _, err := s.Invoke(ctx, p, "different", installed.ID, chatapps.Callback{}); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("cross-conversation callback = %v", err)
	}
	changed, err := s.ChangeStatus(ctx, p, "conv", installed.ID, chatapps.Suspended)
	if err != nil || changed.Status != chatapps.Suspended {
		t.Fatalf("suspend = %+v %v", changed, err)
	}
	if _, err := s.ProposeIntent(ctx, p, "conv", proposal); !errors.Is(err, chatapps.ErrDenied) {
		t.Fatalf("suspended proposal = %v", err)
	}
	if _, err := s.IssueEventCursor(ctx, p, "conv", installed.ID, 0); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("suspended cursor = %v", err)
	}
	audit, err := records.Events(ctx, "tenant")
	if err != nil || len(audit) != 2 || audit[0].Action != "chat.app.install" || audit[1].Action != "chat.app.status" {
		t.Fatalf("app audit = %+v %v", audit, err)
	}
}

func TestChatExtensionReportsBindCurrentIdentity(t *testing.T) {
	ctx := context.Background()
	repo := chatrecords.NewMemoryRepository()
	s := &ChatExtensions{Conversations: extensionConversations{}, Records: &chatrecords.Service{Repo: repo, Auth: ChatRecordAuthority{}}}
	p := chat.Principal{TenantID: "tenant", SubjectID: "member"}
	report := chatrecords.Report{TenantID: "forged", ReporterID: "forged", ConversationID: "conv", ReportID: "report", TargetID: "post", Reason: "review"}
	if err := s.Report(ctx, p, report); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Reports(ctx, "tenant")
	if err != nil || len(got) != 1 || got[0].ReporterID != "member" || got[0].TenantID != "tenant" {
		t.Fatalf("report = %+v %v", got, err)
	}
	if err := s.Moderate(ctx, p, "conv", "case", "remove", "post", "reason", "evidence"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member moderated = %v", err)
	}
	p.SubjectID = "outsider"
	if err := s.Report(ctx, p, report); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("outsider reported = %v", err)
	}
	p.SubjectID = "owner"
	if err := s.Moderate(ctx, p, "conv", "case", "remove", "post", "reason", "evidence"); err != nil {
		t.Fatalf("owner moderation = %v", err)
	}
	if _, err := (ChatRecordAuthority{}).Authorize(ctx, "owner", "tenant", "chat.app.install", "app"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("unbound audit authority = %v", err)
	}
	if got, err := repo.Reports(ctx, "tenant"); err != nil || len(got) != 1 {
		t.Fatalf("denied report mutated storage: %+v %v", got, err)
	}
}
