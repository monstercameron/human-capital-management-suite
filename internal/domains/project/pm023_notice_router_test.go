package project

import (
	"context"
	"testing"
	"time"
)

func TestTodo_PM_023(t *testing.T) {
	event := ProjectNoticeEvent{EventID: "notice-1", TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", Kind: NoticeAssigned, At: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	var delivered []ProjectNotice
	router := &ProjectNoticeRouter{
		Audience: func(context.Context, ProjectNoticeEvent) (ProjectNoticeAudience, error) {
			return ProjectNoticeAudience{Revision: 4, Recipients: []string{"alice", "bob"}}, nil
		},
		Preference: func(context.Context, string, ProjectNoticeEvent) (ProjectNoticePreference, error) {
			return ProjectNoticePreference{}, nil
		},
		Deliver: func(_ context.Context, notice ProjectNotice) error { delivered = append(delivered, notice); return nil },
	}
	if err := router.Route(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2 || delivered[0].AudienceRevision != 4 || delivered[0].Event.EventID != event.EventID {
		t.Fatalf("delivery=%+v", delivered)
	}
}

func TestTodo_PM_023_Integration(t *testing.T) {
	event := ProjectNoticeEvent{EventID: "notice-1", TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", Kind: NoticeDue, At: time.Now().UTC()}
	delivered := 0
	router := &ProjectNoticeRouter{
		Audience: func(context.Context, ProjectNoticeEvent) (ProjectNoticeAudience, error) {
			return ProjectNoticeAudience{Revision: 9, Recipients: []string{"alice"}}, nil
		},
		Preference: func(context.Context, string, ProjectNoticeEvent) (ProjectNoticePreference, error) {
			return ProjectNoticePreference{}, nil
		},
		Deliver: func(context.Context, ProjectNotice) error { delivered++; return nil },
	}
	if err := router.Route(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := router.Route(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if delivered != 1 {
		t.Fatalf("replayed notice delivered %d times", delivered)
	}
}

func TestTodo_PM_023_Security(t *testing.T) {
	event := ProjectNoticeEvent{EventID: "notice-1", TenantID: "tenant-a", ProjectID: "project-a", TaskID: "task-a", Kind: NoticeMentioned, At: time.Now().UTC()}
	var delivered []string
	router := &ProjectNoticeRouter{
		Audience: func(context.Context, ProjectNoticeEvent) (ProjectNoticeAudience, error) {
			return ProjectNoticeAudience{Revision: 2, Recipients: []string{"alice", "revoked"}, Revoked: map[string]bool{"revoked": true}}, nil
		},
		Preference: func(context.Context, string, ProjectNoticeEvent) (ProjectNoticePreference, error) {
			return ProjectNoticePreference{Muted: true, QuietHours: true}, nil
		},
		Deliver: func(_ context.Context, notice ProjectNotice) error {
			delivered = append(delivered, notice.Recipient)
			return nil
		},
	}
	if err := router.Route(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 0 {
		t.Fatalf("optional project notice bypassed preferences/revocation: %v", delivered)
	}
	event.MandatoryHCM = true
	if err := router.Route(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 || delivered[0] != "alice" {
		t.Fatalf("mandatory HCM precedence/revocation failed: %v", delivered)
	}
}
