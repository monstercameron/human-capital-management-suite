package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// PrivateSavedNotice is the notification path's reference-only envelope. It
// contains neither a copied message nor the person's private note.
type PrivateSavedNotice struct {
	RecipientHomeTenantID, RecipientSubjectID            string
	HostTenantID, ConversationID, PostID, IdempotencyKey string
	DueAt                                                time.Time
}

type PrivateSavedNoticeDelivery interface {
	DeliverPrivateSavedNotice(context.Context, PrivateSavedNotice) error
}

// SavedReminderNotifications adapts the existing recipient notification path.
// A composition without that path returns unavailable and keeps the due time
// pending so a later successful tick can deliver it.
type SavedReminderNotifications struct{ Delivery PrivateSavedNoticeDelivery }

func (s SavedReminderNotifications) NotifySaved(ctx context.Context, p chat.Principal, item chat.SavedItem, key string) error {
	if err := chat.ValidateSavedOwner(ctx, p, item.TenantID); err != nil {
		return err
	}
	if item.PersonID != p.SubjectID || item.HomeTenantID != p.TenantID {
		return chat.ErrPermissionDenied
	}
	if item.DueAt == nil || key == "" || item.ConversationID == "" || item.PostID == "" {
		return chat.ErrInvalidArgument
	}
	if s.Delivery == nil {
		return chat.ErrUnavailable
	}
	return s.Delivery.DeliverPrivateSavedNotice(ctx, PrivateSavedNotice{RecipientHomeTenantID: p.TenantID, RecipientSubjectID: p.SubjectID, HostTenantID: item.TenantID, ConversationID: item.ConversationID, PostID: item.PostID, IdempotencyKey: key, DueAt: *item.DueAt})
}
