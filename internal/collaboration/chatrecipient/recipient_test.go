package chatrecipient

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type conversations struct {
	chat.ConversationService
	allowed bool
	denied  map[string]bool
	prefs   chat.NotificationPreferences
}

func (c conversations) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	if !c.allowed || c.denied[r.TenantID+"/"+r.ConversationID] {
		return chat.Conversation{}, chat.ErrPermissionDenied
	}
	return chat.Conversation{ID: r.ConversationID, TenantID: r.TenantID}, nil
}
func (c conversations) GetPreferences(context.Context, chat.GetPreferencesRequest) (chat.NotificationPreferences, error) {
	return c.prefs, nil
}

type repo struct {
	counts  Counts
	follow  Follow
	sidebar Sidebar
	saved   Sidebar
	quiet   QuietHours
	calls   int
}

func (r *repo) Counts(context.Context, Identity) (Counts, error) { r.calls++; return r.counts, nil }
func (r *repo) Follow(_ context.Context, _ Identity, root string) (Follow, error) {
	r.calls++
	return Follow{RootPostID: root, Revision: 1}, nil
}
func (r *repo) PutFollow(_ context.Context, _ Identity, f Follow, _ uint64) (Follow, error) {
	r.calls++
	f.Revision = 2
	return f, nil
}
func (r *repo) Sidebar(context.Context, string, string) (Sidebar, error) {
	r.calls++
	return r.sidebar, nil
}
func (r *repo) PutSidebar(_ context.Context, _, _ string, x Sidebar, _ uint64) (Sidebar, error) {
	r.calls++
	r.saved = x
	x.Revision = 2
	return x, nil
}
func (r *repo) QuietHours(context.Context, string, string) (QuietHours, error) {
	r.calls++
	return r.quiet, nil
}
func (r *repo) PutQuietHours(_ context.Context, _, _ string, x QuietHours, _ uint64) (QuietHours, error) {
	r.calls++
	x.Revision = 2
	return x, nil
}

func TestTodo_CHAT_022(t *testing.T) {
	db := &repo{counts: Counts{Unread: 3, Mentions: 1}}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	got, err := s.Counts(context.Background(), p, "host", "chat")
	if err != nil || got.Unread != 3 || got.Mentions != 1 || db.calls != 1 {
		t.Fatalf("counts=%+v calls=%d err=%v", got, db.calls, err)
	}
	s.Conversations = conversations{allowed: false}
	if _, err = s.Counts(context.Background(), p, "host", "chat"); !errors.Is(err, chat.ErrPermissionDenied) || db.calls != 1 {
		t.Fatalf("revoked access reached repository: %v calls=%d", err, db.calls)
	}
}
func TestTodo_CHAT_023(t *testing.T) {
	db := &repo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	f, err := s.PutFollow(context.Background(), p, "host", "chat", Follow{RootPostID: "root", Followed: true}, 1)
	if err != nil || !f.Followed || f.Revision != 2 {
		t.Fatalf("follow=%+v err=%v", f, err)
	}
	if _, err = s.PutFollow(context.Background(), p, "host", "chat", Follow{}, 1); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("empty root: %v", err)
	}
}
func TestTodo_CHAT_032(t *testing.T) {
	db := &repo{}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	layout := Sidebar{Layout: []byte(`{"sections":[{"id":"team","chats":[{"hostTenantId":"host","conversationId":"chat"}]}]}`)}
	x, err := s.PutSidebar(context.Background(), p, layout, 1)
	if err != nil || x.Revision != 2 || db.calls != 1 {
		t.Fatalf("sidebar=%+v calls=%d err=%v", x, db.calls, err)
	}
	s.Conversations = conversations{allowed: false}
	if _, err = s.PutSidebar(context.Background(), p, layout, 2); !errors.Is(err, chat.ErrPermissionDenied) || db.calls != 1 {
		t.Fatalf("foreign layout accepted: %v calls=%d", err, db.calls)
	}
}

func TestTodo_CHAT_035_SecurityRevokedDraftIsNotReturnedOrSaved(t *testing.T) {
	stored := Sidebar{Layout: []byte(`{"sections":[{"id":"direct","chats":[{"hostTenantId":"host","conversationId":"open"},{"hostTenantId":"host","conversationId":"revoked"}]}],"drafts":{"open":"keep me","revoked":"private text"},"filters":{"direct":"all"}}`), Revision: 7}
	db := &repo{sidebar: stored}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	s := &Service{Conversations: conversations{allowed: true, denied: map[string]bool{"host/revoked": true}}, Repo: db}
	got, err := s.Sidebar(context.Background(), p)
	if err != nil || got.Revision != 7 {
		t.Fatalf("sidebar read = %+v, %v", got, err)
	}
	var layout struct {
		Drafts  map[string]string `json:"drafts"`
		Filters map[string]string `json:"filters"`
	}
	if err := json.Unmarshal(got.Layout, &layout); err != nil || len(layout.Drafts) != 1 || layout.Drafts["open"] != "keep me" || layout.Filters["direct"] != "all" {
		t.Fatalf("revoked draft leaked or allowed layout changed: %+v, %v", layout, err)
	}
	if _, err := s.PutSidebar(context.Background(), p, stored, 7); !errors.Is(err, chat.ErrPermissionDenied) || db.calls != 1 {
		t.Fatalf("revoked room was saved: %v, calls=%d", err, db.calls)
	}
	valid := Sidebar{Layout: []byte(`{"sections":[{"id":"direct","chats":[{"hostTenantId":"host","conversationId":"open"}]}],"drafts":{"open":"keep me","revoked":"private text"}}`)}
	if _, err := s.PutSidebar(context.Background(), p, valid, 7); !errors.Is(err, chat.ErrInvalidArgument) || db.calls != 1 {
		t.Fatalf("orphan draft was saved: %v, calls=%d", err, db.calls)
	}
	valid.Layout = []byte(`{"sections":[{"id":"direct","chats":[{"hostTenantId":"host","conversationId":"open"}]}],"drafts":{"open":"keep me"}}`)
	if _, err := s.PutSidebar(context.Background(), p, valid, 7); err != nil || db.calls != 2 || string(db.saved.Layout) != string(valid.Layout) {
		t.Fatalf("authorized draft did not save: %v, calls=%d saved=%s", err, db.calls, db.saved.Layout)
	}
	db.sidebar = valid
	got, err = s.Sidebar(context.Background(), p)
	if err != nil || string(got.Layout) != string(valid.Layout) {
		t.Fatalf("authorized draft changed on reload: %s, %v", got.Layout, err)
	}
}

func TestTodo_CHAT_035_SecurityAmbiguousHostDraftIsDiscarded(t *testing.T) {
	layout := Sidebar{Layout: []byte(`{"sections":[{"id":"direct","chats":[{"hostTenantId":"host-a","conversationId":"same"},{"hostTenantId":"host-b","conversationId":"same"}]}],"drafts":{"same":"private text"}}`)}
	db := &repo{sidebar: layout}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	got, err := s.Sidebar(context.Background(), p)
	if err != nil || json.Valid(got.Layout) == false || string(got.Layout) == string(layout.Layout) {
		t.Fatalf("ambiguous draft was returned: %s, %v", got.Layout, err)
	}
	var x sidebarLayout
	if err := json.Unmarshal(got.Layout, &x); err != nil || len(x.Drafts) != 0 {
		t.Fatalf("ambiguous draft leaked: %+v, %v", x.Drafts, err)
	}
	if _, err := s.PutSidebar(context.Background(), p, layout, 1); !errors.Is(err, chat.ErrInvalidArgument) || db.calls != 1 {
		t.Fatalf("ambiguous draft was stored: %v, calls=%d", err, db.calls)
	}
}
func TestTodo_CHAT_034(t *testing.T) {
	q := QuietHours{Timezone: "America/New_York", StartMinute: 22 * 60, EndMinute: 7 * 60, Enabled: true}
	at := time.Date(2026, 9, 21, 23, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	for _, tc := range []struct {
		kind NoticeKind
		mode DeliveryMode
		want bool
	}{{OptionalMessage, AllMessages, false}, {OptionalMention, MentionsOnly, false}, {GovernedHCM, Muted, true}, {OptionalMessage, Muted, false}} {
		got, err := ShouldNotify(tc.kind, tc.mode, q, at)
		if err != nil || got != tc.want {
			t.Errorf("kind=%s mode=%s got=%v err=%v", tc.kind, tc.mode, got, err)
		}
	}
	q.Enabled = false
	if got, err := ShouldNotify(OptionalMention, MentionsOnly, q, at); err != nil || !got {
		t.Fatalf("mention enabled=%v err=%v", got, err)
	}
	s := &Service{Repo: &repo{}}
	if _, err := s.PutQuietHours(context.Background(), chat.Principal{TenantID: "home", SubjectID: "alice"}, QuietHours{Timezone: "invalid"}, 1); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("timezone accepted: %v", err)
	}
}

func TestRecipientServiceReadAndValidation(t *testing.T) {
	db := &repo{sidebar: Sidebar{Layout: []byte(`{"sections":[]}`), Revision: 3}, quiet: QuietHours{Timezone: "UTC", Revision: 4}}
	s := &Service{Conversations: conversations{allowed: true}, Repo: db}
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	f, err := s.Follow(context.Background(), p, "host", "chat", "root")
	if err != nil || f.RootPostID != "root" {
		t.Fatalf("follow=%+v err=%v", f, err)
	}
	if _, err = s.Follow(context.Background(), p, "host", "chat", ""); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("empty root: %v", err)
	}
	side, err := s.Sidebar(context.Background(), p)
	if err != nil || side.Revision != 3 {
		t.Fatalf("sidebar=%+v err=%v", side, err)
	}
	quiet, err := s.QuietHours(context.Background(), p)
	if err != nil || quiet.Revision != 4 {
		t.Fatalf("quiet=%+v err=%v", quiet, err)
	}
	saved, err := s.PutQuietHours(context.Background(), p, QuietHours{Timezone: "UTC", StartMinute: 10, EndMinute: 20, Enabled: true}, 1)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	if _, err = s.PutSidebar(context.Background(), p, Sidebar{Layout: []byte(`{"sections":[{"id":"x"},{"id":"x"}]}`)}, 1); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("duplicate section: %v", err)
	}
	if _, err = s.Counts(context.Background(), chat.Principal{}, "host", "chat"); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("empty principal: %v", err)
	}
}

func TestRecipientQuietHoursBoundaries(t *testing.T) {
	q := QuietHours{Timezone: "UTC", StartMinute: 9 * 60, EndMinute: 17 * 60, Enabled: true}
	for _, tc := range []struct {
		hour int
		want bool
	}{{8, true}, {9, false}, {16, false}, {17, true}} {
		at := time.Date(2026, 9, 21, tc.hour, 0, 0, 0, time.UTC)
		got, err := ShouldNotify(OptionalMessage, AllMessages, q, at)
		if err != nil || got != tc.want {
			t.Errorf("hour %d got=%v err=%v", tc.hour, got, err)
		}
	}
	if _, err := ShouldNotify("bogus", AllMessages, q, time.Now()); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("unknown kind: %v", err)
	}
	if _, err := ShouldNotify(OptionalMessage, "bogus", q, time.Now()); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("unknown mode: %v", err)
	}
}

func TestRecipientOptionalDeliveryUsesCurrentPreferences(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	s := &Service{Conversations: conversations{allowed: true, prefs: chat.NotificationPreferences{MentionsOnly: true}}, Repo: &repo{quiet: QuietHours{Timezone: "UTC"}}}
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	got, err := s.OptionalDelivery(context.Background(), p, "host", "chat", OptionalMessage, at)
	if err != nil || got {
		t.Fatalf("all-message alert passed mentions-only: %v %v", got, err)
	}
	got, err = s.OptionalDelivery(context.Background(), p, "host", "chat", OptionalMention, at)
	if err != nil || !got {
		t.Fatalf("mention blocked: %v %v", got, err)
	}
	s.Conversations = conversations{allowed: false}
	got, err = s.OptionalDelivery(context.Background(), p, "host", "chat", OptionalMention, at)
	if got || !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("revoked recipient notified: %v %v", got, err)
	}
	got, err = s.OptionalDelivery(context.Background(), p, "host", "chat", GovernedHCM, at)
	if err != nil || !got {
		t.Fatalf("governed notice suppressed: %v %v", got, err)
	}
}
