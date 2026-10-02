package main

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type ChatmapRecorder interface {
	ReadPosition(context.Context) (chat.LocationPosition, error)
}

func ChatmapDistance(a, b chat.LocationPosition) float64 {
	lat := (b.Latitude - a.Latitude) * math.Pi / 180
	lon := (b.Longitude - a.Longitude) * math.Pi / 180
	h := math.Pow(math.Sin(lat/2), 2) + math.Cos(a.Latitude*math.Pi/180)*math.Cos(b.Latitude*math.Pi/180)*math.Pow(math.Sin(lon/2), 2)
	return 12742000 * math.Asin(math.Sqrt(min(1, max(0, h))))
}

var (
	errChatmapRefused = errors.New("location permission refused")
	errChatmapNoFix   = errors.New("location fix unavailable")
)

type ChatmapDraft struct {
	Place     chat.LocationPlace
	Note      string
	ExpiresAt *time.Time
	Captured  bool
	Status    string
	// Live asks the server for a bounded share the device keeps updating.
	Live bool
}

func (d *ChatmapDraft) PressRead(ctx context.Context, recorder ChatmapRecorder, now time.Time) error {
	if recorder == nil {
		d.Status = "no_fix"
		return errChatmapNoFix
	}
	p, err := recorder.ReadPosition(ctx)
	if err != nil {
		d.Status = "no_fix"
		if errors.Is(err, errChatmapRefused) {
			d.Status = "refused"
		}
		return err
	}
	d.Place = chat.LocationPlace{Position: &p, Source: chat.LocationDevice, Precision: "approximate", ApproximateRadius: 500, CapturedAt: now}
	d.Captured = true
	d.Status = ""
	if p.Accuracy > 100 {
		d.Status = "rough"
	}
	return nil
}
func ChatmapExpiry(duration string, now time.Time) *time.Time {
	if duration == "keep" {
		return nil
	}
	if live, ok := ChatmapLiveDuration(duration); ok {
		at := now.Add(live)
		return &at
	}
	at := now.Add(time.Hour)
	if duration == "day" {
		at = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	}
	return &at
}

type ChatmapPending struct {
	Draft                                      ChatmapDraft
	Tenant, Subject, Conversation, Key, PostID string
	Revision                                   uint64
	Queued, Sent                               bool
	// ShareID is the server's id for the share once it is attached.
	ShareID string
}
type ChatmapSender interface {
	SendLocation(context.Context, *ChatmapPending) error
}

func (p *ChatmapPending) Deliver(ctx context.Context, sender ChatmapSender, online bool, now time.Time, tenant, subject string) error {
	if p.Sent {
		return nil
	}
	if tenant != p.Tenant || subject != p.Subject {
		p.Draft = ChatmapDraft{Status: "discarded"}
		p.Queued = false
		return chat.ErrPermissionDenied
	}
	if (p.Draft.ExpiresAt != nil && !now.Before(*p.Draft.ExpiresAt)) || (p.Draft.Place.Source == chat.LocationDevice && now.Sub(p.Draft.Place.CapturedAt) > 5*time.Minute) {
		p.Draft = ChatmapDraft{Status: "discarded"}
		p.Queued = false
		return chat.ErrNotFound
	}
	if !online {
		p.Queued = true
		p.Draft.Status = "offline"
		return nil
	}
	if sender == nil {
		return chat.ErrUnavailable
	}
	if err := sender.SendLocation(ctx, p); err != nil {
		p.Draft.Status = "failed"
		if errors.Is(err, chat.ErrPermissionDenied) {
			// The administrator has switched sharing (or live sharing) off here.
			p.Draft.Status = "sharing_off"
			if p.Draft.Live {
				p.Draft.Status = "live_off"
			}
		}
		p.Queued = errors.Is(err, chat.ErrUnavailable)
		return err
	}
	p.Sent = true
	p.Queued = false
	p.Draft = ChatmapDraft{Status: "sent"}
	return nil
}
