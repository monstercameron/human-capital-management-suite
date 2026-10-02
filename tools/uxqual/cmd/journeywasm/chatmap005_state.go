package main

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Live location, device side. The page reads the position only while a share
// the person started is running and the product is open: closing the page
// stops the loop, the server then shows the share as paused, and its end time
// (kept by the server) is never moved by anything here.

const chatmapLiveInterval = 30 * time.Second

// ChatmapLiveDuration reads the duration choices that start a live share.
func ChatmapLiveDuration(choice string) (time.Duration, bool) {
	switch choice {
	case "live15":
		return 15 * time.Minute, true
	case "live60":
		return time.Hour, true
	case "live480":
		return 8 * time.Hour, true
	}
	return 0, false
}

// ChatmapLiveSender sends one position update for a running share.
type ChatmapLiveSender interface {
	SendUpdate(context.Context, *ChatmapLiveSession, chat.LocationPlace) error
}

// ChatmapLiveSession is one running live share on this device.
type ChatmapLiveSession struct {
	Tenant, Subject, Conversation, PostID, ShareID string
	ExpiresAt                                      time.Time
	Interval                                       time.Duration
	Precision                                      string
	Radius                                         float64
	Failures                                       int
	Stopped                                        bool
	Reason                                         string
}

// Tick runs one step of the loop: stop when the end time has passed, the
// signed-in person is no longer the sharer, or permission was refused; otherwise
// read the position, send it, and say how long to wait. A failed read or send
// leaves the share to be shown as paused by the server; it never changes the end.
func (s *ChatmapLiveSession) Tick(ctx context.Context, recorder ChatmapRecorder, sender ChatmapLiveSender, now time.Time, tenant, subject string) (time.Duration, bool) {
	if s.Stopped {
		return 0, true
	}
	stop := func(reason string) (time.Duration, bool) {
		s.Stopped, s.Reason = true, reason
		return 0, true
	}
	if tenant != s.Tenant || subject != s.Subject {
		return stop(chat.LiveEndedSignOut)
	}
	if !now.Before(s.ExpiresAt) {
		return stop(chat.LiveEndedExpired)
	}
	wait := s.Interval
	if wait <= 0 {
		wait = chatmapLiveInterval
	}
	if recorder == nil {
		return wait, false
	}
	p, err := recorder.ReadPosition(ctx)
	if err != nil {
		if errors.Is(err, errChatmapRefused) {
			return stop("refused")
		}
		s.Failures++
		return wait, false
	}
	place := chat.LocationPlace{Position: &p, Source: chat.LocationDevice, Precision: s.Precision, ApproximateRadius: s.Radius, CapturedAt: now}
	if err = sender.SendUpdate(ctx, s, place); err != nil {
		if errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrPermissionDenied) {
			return stop(chat.LiveEndedStopped)
		}
		s.Failures++
		return wait, false
	}
	s.Failures = 0
	return wait, false
}

// ChatmapMinutesLeft is the figure the banner shows.
func ChatmapMinutesLeft(expires, now time.Time) int {
	if !now.Before(expires) {
		return 0
	}
	return int((expires.Sub(now) + time.Minute - 1) / time.Minute)
}
