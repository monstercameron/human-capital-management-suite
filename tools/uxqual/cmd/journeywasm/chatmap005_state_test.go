package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type chatmapLiveSenderFixture struct {
	calls  int
	places []chat.LocationPlace
	err    error
}

func (s *chatmapLiveSenderFixture) SendUpdate(_ context.Context, _ *ChatmapLiveSession, p chat.LocationPlace) error {
	s.calls++
	s.places = append(s.places, p)
	return s.err
}

func chatmapSession(now time.Time) *ChatmapLiveSession {
	return &ChatmapLiveSession{Tenant: "t", Subject: "u", Conversation: "c", PostID: "m", ShareID: "s", ExpiresAt: now.Add(15 * time.Minute), Interval: 30 * time.Second, Precision: "approximate", Radius: 500}
}

func TestTodo_CHATMAP_005_Session(t *testing.T) {
	now := time.Now()
	r := &chatmapFakeRecorder{accuracy: 20}
	s := &chatmapLiveSenderFixture{}
	session := chatmapSession(now)
	wait, done := session.Tick(context.Background(), r, s, now.Add(time.Minute), "t", "u")
	if done || wait != 30*time.Second || s.calls != 1 || r.calls != 1 {
		t.Fatal("tick", wait, done, s.calls)
	}
	// The share's own precision travels with each update; the device cannot raise it.
	if got := s.places[0]; got.Precision != "approximate" || got.ApproximateRadius != 500 || got.Source != chat.LocationDevice {
		t.Fatal("update precision", got)
	}
	// The end time stops the loop with no further read or send.
	if _, done = session.Tick(context.Background(), r, s, now.Add(16*time.Minute), "t", "u"); !done || session.Reason != chat.LiveEndedExpired || r.calls != 1 {
		t.Fatal("expiry", session.Reason, r.calls)
	}
	if _, done = session.Tick(context.Background(), r, s, now, "t", "u"); !done || r.calls != 1 {
		t.Fatal("a stopped session read the position again")
	}
}

func TestTodo_CHATMAP_005_Session_Security(t *testing.T) {
	now := time.Now()
	r := &chatmapFakeRecorder{accuracy: 20}
	s := &chatmapLiveSenderFixture{}
	// Another person signed in: nothing is read, nothing is sent.
	session := chatmapSession(now)
	if _, done := session.Tick(context.Background(), r, s, now, "t", "someone-else"); !done || session.Reason != chat.LiveEndedSignOut || r.calls != 0 || s.calls != 0 {
		t.Fatal("sign-out did not stop the loop", session.Reason)
	}
	// Permission refused: stop, do not keep asking.
	session = chatmapSession(now)
	r.err = errChatmapRefused
	if _, done := session.Tick(context.Background(), r, s, now, "t", "u"); !done || session.Reason != "refused" {
		t.Fatal("refusal")
	}
	// The server says the share is gone: stop.
	session = chatmapSession(now)
	r.err = nil
	s.err = chat.ErrNotFound
	if _, done := session.Tick(context.Background(), r, s, now, "t", "u"); !done {
		t.Fatal("ended share kept updating")
	}
	// A weak signal or a dropped connection keeps the loop going without
	// moving the end time.
	session = chatmapSession(now)
	r.err = errChatmapNoFix
	if _, done := session.Tick(context.Background(), r, s, now, "t", "u"); done || session.Failures != 1 {
		t.Fatal("no fix ended the share")
	}
	r.err, s.err = nil, chat.ErrUnavailable
	if _, done := session.Tick(context.Background(), r, s, now, "t", "u"); done || session.Failures != 2 {
		t.Fatal("lost connection ended the share", errors.Is(s.err, chat.ErrUnavailable))
	}
	if !session.ExpiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatal("the device moved the end time")
	}
}

func TestTodo_CHATMAP_005_Choices(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	for choice, want := range map[string]time.Duration{"live15": 15 * time.Minute, "live60": time.Hour, "live480": 8 * time.Hour} {
		d, ok := ChatmapLiveDuration(choice)
		if !ok || d != want || ChatmapExpiry(choice, now).Sub(now) != want {
			t.Fatal(choice, d)
		}
	}
	if _, ok := ChatmapLiveDuration("keep"); ok {
		t.Fatal("keep is not live")
	}
	if ChatmapMinutesLeft(now.Add(90*time.Second), now) != 2 || ChatmapMinutesLeft(now, now) != 0 {
		t.Fatal("minutes left")
	}
	// An administrator's refusal is told apart from a failure.
	p := ChatmapPending{Tenant: "t", Subject: "u", Draft: ChatmapDraft{Live: true, Place: chat.LocationPlace{Source: chat.LocationDevice, CapturedAt: now}}}
	end := now.Add(time.Hour)
	p.Draft.ExpiresAt = &end
	if err := p.Deliver(context.Background(), chatmapDenySender{}, true, now, "t", "u"); err == nil || p.Draft.Status != "live_off" {
		t.Fatal("refusal status", p.Draft.Status, err)
	}
}

type chatmapDenySender struct{}

func (chatmapDenySender) SendLocation(context.Context, *ChatmapPending) error {
	return chat.ErrPermissionDenied
}
