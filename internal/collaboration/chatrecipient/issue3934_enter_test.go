package chatrecipient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// ISSUE-3934: the Enter-key choice rides in the person's own sidebar document.
// It round-trips, survives a read that filters a revoked room, and only the two
// known values are stored.
func TestIssue3934_SidebarKeepsTheEnterChoice(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	layout := []byte(`{"sections":[{"id":"a","chats":[{"hostTenantId":"host","conversationId":"open"},{"hostTenantId":"host","conversationId":"revoked"}]}],"composerEnter":"newline"}`)
	db := &repo{}
	s := &Service{Conversations: conversations{allowed: true, denied: map[string]bool{"host/revoked": true}}, Repo: db}

	open := strings.Replace(string(layout), `,{"hostTenantId":"host","conversationId":"revoked"}`, "", 1)
	if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: []byte(open)}, 1); err != nil {
		t.Fatalf("PutSidebar: %v", err)
	}
	var saved struct {
		ComposerEnter string `json:"composerEnter"`
	}
	if err := json.Unmarshal(db.saved.Layout, &saved); err != nil || saved.ComposerEnter != "newline" {
		t.Fatalf("stored layout lost the choice: %s (%v)", db.saved.Layout, err)
	}

	db.sidebar = Sidebar{Layout: layout, Revision: 5}
	got, err := s.Sidebar(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got.Layout, &saved); err != nil || saved.ComposerEnter != "newline" {
		t.Fatalf("filtered read lost the choice: %s (%v)", got.Layout, err)
	}

	for _, bad := range []string{"enter", "NEWLINE", strings.Repeat("x", 64)} {
		doc, _ := json.Marshal(map[string]any{"sections": []any{}, "composerEnter": bad})
		if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: doc}, 1); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("composerEnter %q = %v, want ErrInvalidArgument", bad, err)
		}
	}
	for _, ok := range []string{"", "send", "newline"} {
		doc, _ := json.Marshal(map[string]any{"sections": []any{}, "composerEnter": ok})
		if _, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: doc}, 1); err != nil {
			t.Errorf("composerEnter %q refused: %v", ok, err)
		}
	}
}
