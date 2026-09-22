// Package chatrecipient owns personal chat state after current conversation admission.
package chatrecipient

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type Identity struct{ HostTenantID, HomeTenantID, SubjectID, ConversationID string }
type Counts struct{ Unread, Mentions uint64 }
type Follow struct {
	RootPostID string
	Followed   bool
	Revision   uint64
}
type Sidebar struct {
	Layout   json.RawMessage
	Revision uint64
}
type QuietHours struct {
	Timezone               string
	StartMinute, EndMinute int
	Enabled                bool
	Revision               uint64
}

type Repository interface {
	Counts(context.Context, Identity) (Counts, error)
	Follow(context.Context, Identity, string) (Follow, error)
	PutFollow(context.Context, Identity, Follow, uint64) (Follow, error)
	Sidebar(context.Context, string, string) (Sidebar, error)
	PutSidebar(context.Context, string, string, Sidebar, uint64) (Sidebar, error)
	QuietHours(context.Context, string, string) (QuietHours, error)
	PutQuietHours(context.Context, string, string, QuietHours, uint64) (QuietHours, error)
}

// Service authorizes every conversation-scoped operation before touching personal state.
type Service struct {
	Conversations chat.ConversationService
	Repo          Repository
}

func (s *Service) admit(ctx context.Context, p chat.Principal, host, conversation string) (Identity, error) {
	if s == nil || s.Conversations == nil || s.Repo == nil {
		return Identity{}, chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" || host == "" || conversation == "" {
		return Identity{}, chat.ErrInvalidArgument
	}
	if _, err := s.Conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: host, ConversationID: conversation}); err != nil {
		return Identity{}, err
	}
	return Identity{HostTenantID: host, HomeTenantID: p.TenantID, SubjectID: p.SubjectID, ConversationID: conversation}, nil
}

func (s *Service) Counts(ctx context.Context, p chat.Principal, host, conversation string) (Counts, error) {
	id, err := s.admit(ctx, p, host, conversation)
	if err != nil {
		return Counts{}, err
	}
	return s.Repo.Counts(ctx, id)
}
func (s *Service) Follow(ctx context.Context, p chat.Principal, host, conversation, root string) (Follow, error) {
	id, err := s.admit(ctx, p, host, conversation)
	if err != nil {
		return Follow{}, err
	}
	if root == "" {
		return Follow{}, chat.ErrInvalidArgument
	}
	return s.Repo.Follow(ctx, id, root)
}
func (s *Service) PutFollow(ctx context.Context, p chat.Principal, host, conversation string, f Follow, expected uint64) (Follow, error) {
	id, err := s.admit(ctx, p, host, conversation)
	if err != nil {
		return Follow{}, err
	}
	if f.RootPostID == "" || expected == 0 {
		return Follow{}, chat.ErrInvalidArgument
	}
	return s.Repo.PutFollow(ctx, id, f, expected)
}
func (s *Service) Sidebar(ctx context.Context, p chat.Principal) (Sidebar, error) {
	if s == nil || s.Repo == nil {
		return Sidebar{}, chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return Sidebar{}, chat.ErrInvalidArgument
	}
	return s.Repo.Sidebar(ctx, p.TenantID, p.SubjectID)
}
func (s *Service) PutSidebar(ctx context.Context, p chat.Principal, x Sidebar, expected uint64) (Sidebar, error) {
	if s == nil || s.Repo == nil || s.Conversations == nil {
		return Sidebar{}, chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" || expected == 0 || !json.Valid(x.Layout) {
		return Sidebar{}, chat.ErrInvalidArgument
	}
	var layout struct {
		Sections []struct {
			ID    string `json:"id"`
			Chats []struct {
				HostTenantID   string `json:"hostTenantId"`
				ConversationID string `json:"conversationId"`
			} `json:"chats"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(x.Layout, &layout); err != nil {
		return Sidebar{}, chat.ErrInvalidArgument
	}
	seen := map[string]bool{}
	for _, section := range layout.Sections {
		if section.ID == "" || seen[section.ID] || len(section.Chats) > 200 {
			return Sidebar{}, chat.ErrInvalidArgument
		}
		seen[section.ID] = true
		for _, item := range section.Chats {
			if _, err := s.admit(ctx, p, item.HostTenantID, item.ConversationID); err != nil {
				return Sidebar{}, err
			}
		}
	}
	if len(layout.Sections) > 30 {
		return Sidebar{}, chat.ErrInvalidArgument
	}
	return s.Repo.PutSidebar(ctx, p.TenantID, p.SubjectID, x, expected)
}
func (s *Service) QuietHours(ctx context.Context, p chat.Principal) (QuietHours, error) {
	if s == nil || s.Repo == nil {
		return QuietHours{}, chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" {
		return QuietHours{}, chat.ErrInvalidArgument
	}
	return s.Repo.QuietHours(ctx, p.TenantID, p.SubjectID)
}
func (s *Service) PutQuietHours(ctx context.Context, p chat.Principal, x QuietHours, expected uint64) (QuietHours, error) {
	if s == nil || s.Repo == nil {
		return QuietHours{}, chat.ErrUnavailable
	}
	if p.TenantID == "" || p.SubjectID == "" || expected == 0 || x.StartMinute < 0 || x.StartMinute > 1439 || x.EndMinute < 0 || x.EndMinute > 1439 {
		return QuietHours{}, chat.ErrInvalidArgument
	}
	if _, err := time.LoadLocation(x.Timezone); err != nil {
		return QuietHours{}, chat.ErrInvalidArgument
	}
	return s.Repo.PutQuietHours(ctx, p.TenantID, p.SubjectID, x, expected)
}

type NoticeKind string

const (
	OptionalMessage NoticeKind = "OPTIONAL_MESSAGE"
	OptionalMention NoticeKind = "OPTIONAL_MENTION"
	GovernedHCM     NoticeKind = "GOVERNED_HCM"
)

type DeliveryMode string

const (
	AllMessages  DeliveryMode = "ALL"
	MentionsOnly DeliveryMode = "MENTIONS"
	Muted        DeliveryMode = "MUTED"
)

// OptionalDelivery resolves current conversation and personal preferences at
// decision time. A governed HCM notice is never passed through chat settings.
func (s *Service) OptionalDelivery(ctx context.Context, p chat.Principal, host, conversation string, kind NoticeKind, at time.Time) (bool, error) {
	if kind == GovernedHCM {
		return true, nil
	}
	if _, err := s.admit(ctx, p, host, conversation); err != nil {
		return false, err
	}
	prefs, err := s.Conversations.GetPreferences(ctx, chat.GetPreferencesRequest{Principal: p, TenantID: host, ConversationID: conversation})
	if err != nil {
		return false, err
	}
	quiet, err := s.QuietHours(ctx, p)
	if err != nil {
		return false, err
	}
	mode := AllMessages
	if prefs.MentionsOnly {
		mode = MentionsOnly
	}
	if prefs.Muted {
		mode = Muted
	}
	return ShouldNotify(kind, mode, quiet, at)
}

// ShouldNotify applies only to optional chat delivery. Governed HCM notices
// bypass these personal chat preferences and remain on their own plane.
func ShouldNotify(kind NoticeKind, mode DeliveryMode, quiet QuietHours, at time.Time) (bool, error) {
	if kind == GovernedHCM {
		return true, nil
	}
	if kind != OptionalMessage && kind != OptionalMention {
		return false, chat.ErrInvalidArgument
	}
	if mode != AllMessages && mode != MentionsOnly && mode != Muted {
		return false, chat.ErrInvalidArgument
	}
	if mode == Muted || (mode == MentionsOnly && kind != OptionalMention) {
		return false, nil
	}
	if !quiet.Enabled {
		return true, nil
	}
	loc, err := time.LoadLocation(quiet.Timezone)
	if err != nil {
		return false, errors.Join(chat.ErrInvalidArgument, err)
	}
	local := at.In(loc)
	minute := local.Hour()*60 + local.Minute()
	if quiet.StartMinute == quiet.EndMinute {
		return false, nil
	}
	if quiet.StartMinute < quiet.EndMinute {
		return minute < quiet.StartMinute || minute >= quiet.EndMinute, nil
	}
	return minute >= quiet.EndMinute && minute < quiet.StartMinute, nil
}
