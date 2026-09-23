package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

type extensionRecipientRepo struct {
	t     *testing.T
	calls int
	chatrecipient.Repository
}

func (r *extensionRecipientRepo) Counts(_ context.Context, id chatrecipient.Identity) (chatrecipient.Counts, error) {
	r.t.Helper()
	if id != (chatrecipient.Identity{HostTenantID: "host", HomeTenantID: "home", SubjectID: "member", ConversationID: "conv"}) {
		r.t.Fatalf("counts identity = %+v", id)
	}
	r.calls++
	return chatrecipient.Counts{Unread: 3, Mentions: 1}, nil
}
func (r *extensionRecipientRepo) Follow(ctx context.Context, id chatrecipient.Identity, root string) (chatrecipient.Follow, error) {
	if _, err := r.Counts(ctx, id); err != nil {
		return chatrecipient.Follow{}, err
	}
	if root != "root" {
		r.t.Fatalf("root = %q", root)
	}
	return chatrecipient.Follow{RootPostID: root, Revision: 2, Followed: true}, nil
}
func (r *extensionRecipientRepo) PutFollow(ctx context.Context, id chatrecipient.Identity, f chatrecipient.Follow, expected uint64) (chatrecipient.Follow, error) {
	if expected != 2 || !f.Followed {
		r.t.Fatalf("follow input = %+v expected=%d", f, expected)
	}
	return r.Follow(ctx, id, f.RootPostID)
}
func (r *extensionRecipientRepo) personal(tenant, subject string) {
	r.t.Helper()
	if tenant != "home" || subject != "member" {
		r.t.Fatalf("personal identity = %s/%s", tenant, subject)
	}
	r.calls++
}
func (r *extensionRecipientRepo) Sidebar(_ context.Context, tenant, subject string) (chatrecipient.Sidebar, error) {
	r.personal(tenant, subject)
	return chatrecipient.Sidebar{Layout: []byte(`{"sections":[]}`), Revision: 2}, nil
}
func (r *extensionRecipientRepo) PutSidebar(ctx context.Context, tenant, subject string, x chatrecipient.Sidebar, expected uint64) (chatrecipient.Sidebar, error) {
	if string(x.Layout) != `{"sections":[]}` || expected != 2 {
		r.t.Fatalf("sidebar input = %+v expected=%d", x, expected)
	}
	return r.Sidebar(ctx, tenant, subject)
}
func (r *extensionRecipientRepo) QuietHours(_ context.Context, tenant, subject string) (chatrecipient.QuietHours, error) {
	r.personal(tenant, subject)
	return chatrecipient.QuietHours{Timezone: "UTC", StartMinute: 600, EndMinute: 720, Enabled: true, Revision: 2}, nil
}
func (r *extensionRecipientRepo) PutQuietHours(ctx context.Context, tenant, subject string, x chatrecipient.QuietHours, expected uint64) (chatrecipient.QuietHours, error) {
	want, err := r.QuietHours(ctx, tenant, subject)
	if x != want || expected != 2 {
		r.t.Fatalf("quiet input = %+v expected=%d", x, expected)
	}
	return want, err
}

func TestChatExtensionRecipientIdentityAndPayload(t *testing.T) {
	ctx := context.Background()
	p := chat.Principal{TenantID: "home", SubjectID: "member"}
	repo := &extensionRecipientRepo{t: t}
	s := &ChatExtensions{Recipients: &chatrecipient.Service{Repo: repo, Conversations: extensionConversations{}}}
	counts, err := s.Counts(ctx, p, "host", "conv")
	if err != nil || counts != (chatrecipient.Counts{Unread: 3, Mentions: 1}) {
		t.Fatalf("counts=%+v %v", counts, err)
	}
	follow, err := s.ThreadFollow(ctx, p, "host", "conv", "root")
	if err != nil || !follow.Followed || follow.Revision != 2 {
		t.Fatalf("follow=%+v %v", follow, err)
	}
	if got, err := s.PutThreadFollow(ctx, p, "host", "conv", follow, 2); err != nil || got != follow {
		t.Fatalf("put follow=%+v %v", got, err)
	}
	sidebar, err := s.Sidebar(ctx, p)
	if err != nil || sidebar.Revision != 2 {
		t.Fatalf("sidebar=%+v %v", sidebar, err)
	}
	if got, err := s.PutSidebar(ctx, p, sidebar, 2); err != nil || !reflect.DeepEqual(got, sidebar) {
		t.Fatalf("put sidebar=%+v %v", got, err)
	}
	quiet, err := s.QuietHours(ctx, p)
	if err != nil || !quiet.Enabled || quiet.Timezone != "UTC" {
		t.Fatalf("quiet=%+v %v", quiet, err)
	}
	if got, err := s.PutQuietHours(ctx, p, quiet, 2); err != nil || got != quiet {
		t.Fatalf("put quiet=%+v %v", got, err)
	}
	if repo.calls != 7 {
		t.Fatalf("repository calls=%d, want 7", repo.calls)
	}
	p.SubjectID = "outsider"
	if _, err := s.Counts(ctx, p, "host", "conv"); !errors.Is(err, chat.ErrPermissionDenied) || repo.calls != 7 {
		t.Fatalf("denied counts reached store: calls=%d err=%v", repo.calls, err)
	}
}
