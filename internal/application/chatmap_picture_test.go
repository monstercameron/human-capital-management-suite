package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type chatmapReader struct {
	deny bool
	post chat.Post
}

func (r *chatmapReader) ReadAuthorizedReference(_ context.Context, _ chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	if r.deny {
		return chat.Conversation{}, nil, chat.ErrPermissionDenied
	}
	return chat.Conversation{TenantID: tenant, ID: conversation}, &r.post, nil
}

type chatmapPictureRepo struct{ v chat.LocationShare }

func (r *chatmapPictureRepo) AttachLocation(_ context.Context, v chat.LocationShare) (chat.LocationShare, error) {
	r.v = v
	return v, nil
}
func (r *chatmapPictureRepo) ReadLocation(context.Context, chat.LocationKey, time.Time) (chat.LocationShare, error) {
	return r.v, nil
}
func (r *chatmapPictureRepo) EndLocation(context.Context, chat.LocationKey) error {
	r.v.Ended = true
	r.v.Place = chat.LocationPlace{}
	return nil
}
func (r *chatmapPictureRepo) SweepLocations(context.Context, string, time.Time) (int64, error) {
	return 0, nil
}

type chatmapRenderer struct{ calls int }

func (r *chatmapRenderer) Render(p chat.LocationPlace, zoom int, size chat.MapSize, theme chat.MapTheme) (chat.MapPicture, error) {
	r.calls++
	return (chat.SchematicMap{}).Render(p, zoom, size, theme)
}

type chatmapUsage struct{ digests []string }

func (u *chatmapUsage) RecordLocationUsage(_ context.Context, tenant, subject, operation, digest string) error {
	u.digests = append(u.digests, digest)
	return nil
}
func TestTodo_CHATMAP_003_Security(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(time.Hour)
	k := chat.LocationKey{TenantID: "t", ConversationID: "c", PostID: "m", ID: "s"}
	reader := &chatmapReader{post: chat.Post{TenantID: "t", ConversationID: "c", ID: "m", References: []chat.Reference{{Kind: chat.LocationReference, TenantID: "t", ID: "s"}}}}
	repo := &chatmapPictureRepo{v: chat.LocationShare{Version: 1, ID: "s", TenantID: "t", ConversationID: "c", PostID: "m", ExpiresAt: &expiry, Place: chat.LocationPlace{Position: &chat.LocationPosition{Latitude: 42.123456, Longitude: 10, Accuracy: 20}, Label: "secret", CapturedAt: now}}}
	renderer := &chatmapRenderer{}
	usage := &chatmapUsage{}
	svc := &chat.LocationService{Chat: reader, Repo: repo, Now: func() time.Time { return now }}
	cache := &ChatmapPictureCache{Locations: svc, Renderer: renderer, Usage: usage}
	p := chat.Principal{TenantID: "t", SubjectID: "alice"}
	first, err := cache.Picture(context.Background(), p, k, 16, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cache.Picture(context.Background(), p, k, 16, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{}); err != nil || renderer.calls != 1 {
		t.Fatal("not cached", err, renderer.calls)
	}
	first.Image[0] = 'X'
	next, err := cache.Picture(context.Background(), p, k, 16, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{})
	if err != nil || next.Image[0] != '<' {
		t.Fatal("cache aliased", err)
	}
	if len(usage.digests) != 1 || len(usage.digests[0]) != 64 || strings.Contains(usage.digests[0], "secret") {
		t.Fatal("usage leaked place", usage)
	}
	reader.deny = true
	if _, err = cache.Picture(context.Background(), p, k, 16, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{}); err == nil {
		t.Fatal("cached auth bypass")
	}
	reader.deny = false
	for range 117 {
		if _, err = cache.Picture(context.Background(), p, k, 16, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = cache.Picture(context.Background(), p, k, 16, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{}); err != chat.ErrUnavailable {
		t.Fatal("rate limit", err)
	}
	now = expiry
	if _, err = cache.Picture(context.Background(), p, k, 16, chat.MapSize{Width: 400, Height: 200}, chat.MapTheme{}); err != chat.ErrNotFound || repo.v.Place.Position != nil {
		t.Fatal("expiry served cache", err)
	}
	if err = (ChatmapNoopUsage{}).RecordLocationUsage(context.Background(), "t", "alice", "map_picture", "digest"); err != nil {
		t.Fatal(err)
	}
}
