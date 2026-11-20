package chatrecipient

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type followedRepo struct {
	repo
	rows         []FollowedThread
	err          error
	host, home   string
	subject      string
	limit, lists int
}

func (r *followedRepo) ListFollowed(_ context.Context, host, home, subject string, limit int) ([]FollowedThread, error) {
	r.lists++
	r.host, r.home, r.subject, r.limit = host, home, subject, limit
	return append([]FollowedThread(nil), r.rows...), r.err
}

func TestIssue3916_FollowedThreads_DropsConversationsTheCallerCannotOpen_Security(t *testing.T) {
	db := &followedRepo{rows: []FollowedThread{
		{ConversationID: "open", RootPostID: "r1", RootBody: "hello"},
		{ConversationID: "secret", RootPostID: "r2", RootBody: "private plans"},
		{ConversationID: "gone", RootPostID: "r3"},
		{ConversationID: "open", RootPostID: "r4", RootBody: strings.Repeat("é", 500)},
	}}
	admission := &refusing{conversations: conversations{allowed: true, denied: map[string]bool{"host/secret": true}}, missing: map[string]bool{"gone": true}}
	s := &Service{Conversations: admission, Repo: db}
	got, err := s.FollowedThreads(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, "host")
	if err != nil || len(got) != 2 || got[0].RootPostID != "r1" || got[1].RootPostID != "r4" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if db.host != "host" || db.home != "home" || db.subject != "alice" || db.limit != FollowedThreadsLimit {
		t.Fatalf("the list was not scoped to the caller: %+v", db)
	}
	if got[0].HostTenantID != "host" || len([]rune(got[1].RootBody)) != FollowedPreviewRunes || !strings.HasSuffix(got[1].RootBody, "…") {
		t.Fatalf("host or preview not set: %+v", got)
	}
	if admission.asked != 3 {
		t.Fatalf("each conversation must be admitted once, asked=%d", admission.asked)
	}
}

func TestIssue3916_FollowedThreads_Refusals(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	if _, err := (&Service{Conversations: conversations{allowed: true}, Repo: &repo{}}).FollowedThreads(ctx, p, "host"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("a store that cannot list: %v", err)
	}
	db := &followedRepo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	for _, bad := range []struct {
		p    chat.Principal
		host string
	}{{chat.Principal{SubjectID: "alice"}, "host"}, {chat.Principal{TenantID: "home"}, "host"}, {p, ""}} {
		if _, err := s.FollowedThreads(ctx, bad.p, bad.host); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	if db.lists != 0 {
		t.Fatalf("the store was read before the request was checked")
	}
	db.err = chat.ErrUnavailable
	if _, err := s.FollowedThreads(ctx, p, "host"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("store failure: %v", err)
	}
	broken := &refusing{conversations: conversations{allowed: true}, broken: "x"}
	s2 := &Service{Conversations: broken, Repo: &followedRepo{rows: []FollowedThread{{ConversationID: "x"}}}}
	if _, err := s2.FollowedThreads(ctx, p, "host"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("an outage must not read as an empty list: %v", err)
	}
}
