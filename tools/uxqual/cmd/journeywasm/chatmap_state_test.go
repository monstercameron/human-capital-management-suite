package main

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type chatmapFakeRecorder struct {
	calls    int
	err      error
	accuracy float64
}

func (r *chatmapFakeRecorder) ReadPosition(context.Context) (chat.LocationPosition, error) {
	r.calls++
	return chat.LocationPosition{Latitude: 40, Longitude: 10, Accuracy: r.accuracy}, r.err
}

type chatmapFakeSender struct{ calls int }

func (s *chatmapFakeSender) SendLocation(context.Context, *ChatmapPending) error {
	s.calls++
	return nil
}
func TestTodo_CHATMAP_004(t *testing.T) {
	now := time.Now()
	r := &chatmapFakeRecorder{accuracy: 200}
	d := ChatmapDraft{}
	if r.calls != 0 {
		t.Fatal("background read")
	}
	if err := d.PressRead(context.Background(), r, now); err != nil || r.calls != 1 || d.Status != "rough" || d.Place.Precision != "approximate" {
		t.Fatal(d, err)
	}
	r.err = errChatmapRefused
	if err := d.PressRead(context.Background(), r, now); err != errChatmapRefused || d.Status != "refused" {
		t.Fatal(err, d)
	}
}
func TestTodo_CHATMAP_004_Queue(t *testing.T) {
	now := time.Now()
	end := now.Add(time.Hour)
	s := &chatmapFakeSender{}
	p := ChatmapPending{Tenant: "t", Subject: "u", Conversation: "c", Draft: ChatmapDraft{Place: chat.LocationPlace{Source: chat.LocationPin, CapturedAt: now}, ExpiresAt: &end}}
	if err := p.Deliver(context.Background(), s, false, now, "t", "u"); err != nil || !p.Queued || s.calls != 0 {
		t.Fatal(p, err)
	}
	if err := p.Deliver(context.Background(), s, true, now, "t", "u"); err != nil || !p.Sent || s.calls != 1 {
		t.Fatal(p, err)
	}
	if err := p.Deliver(context.Background(), s, true, now, "t", "u"); err != nil || s.calls != 1 {
		t.Fatal("sent twice")
	}
	p.Sent = false
	p.Draft.ExpiresAt = &end
	if err := p.Deliver(context.Background(), s, true, end, "t", "u"); err != chat.ErrNotFound || p.Draft.Place.Position != nil || s.calls != 1 {
		t.Fatal("expired sent", err)
	}
}
func TestTodo_CHATMAP_004_Security(t *testing.T) {
	now := time.Now()
	s := &chatmapFakeSender{}
	p := ChatmapPending{Tenant: "t", Subject: "u", Draft: ChatmapDraft{Place: chat.LocationPlace{Source: chat.LocationPin, CapturedAt: now}}}
	if err := p.Deliver(context.Background(), s, true, now, "t", "other"); err != chat.ErrPermissionDenied || s.calls != 0 {
		t.Fatal("identity leak", err)
	}
	if ChatmapExpiry("keep", now) != nil || ChatmapExpiry("hour", now).Sub(now) != time.Hour {
		t.Fatal("duration")
	}
}

func TestTodo_CHATMAP_004_Nearest(t *testing.T) {
	origin := chat.LocationPosition{Latitude: 80, Longitude: 179.9}
	across := chat.LocationPosition{Latitude: 80, Longitude: -179.9}
	south := chat.LocationPosition{Latitude: 79.9, Longitude: 179.9}
	if ChatmapDistance(origin, across) >= ChatmapDistance(origin, south) || ChatmapDistance(origin, origin) != 0 {
		t.Fatal("nearest sites must use spherical distance across the date line")
	}
}
