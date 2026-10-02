package chat

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrSavedLimit = errors.New("chat: saved message limit reached")

const SavedLimit = 5000

type SavedState string

const (
	SavedTodo SavedState = "todo"
	SavedDone SavedState = "done"
	SavedAll  SavedState = "all"
)

// SavedItem persists a reference and private metadata only. Post and Channel
// are current, authorized read projections and must never be persisted.
type SavedItem struct {
	TenantID, HomeTenantID, PersonID, ConversationID, PostID string
	State                                                    SavedState
	Note                                                     string
	DueAt                                                    *time.Time
	CreatedAt, UpdatedAt                                     time.Time
	Revision                                                 uint64
	Post                                                     *Post  `json:",omitempty"`
	Channel                                                  string `json:",omitempty"`
	Availability                                             string
}

type SavedRequest struct {
	Principal                        Principal
	TenantID, ConversationID, PostID string
}

type SavedListRequest struct {
	Principal Principal
	TenantID  string
	Tab       SavedState
	Page      Page
}

type SavedPage struct {
	Items      []SavedItem
	NextCursor string
}

type SavedChange struct {
	State  *SavedState
	Note   *string
	SetDue bool
	DueAt  *time.Time
}

// SavedStore is separate from the channel store: saves emit no shared events.
// All methods scope by the caller, never by a caller-supplied target person.
type SavedStore interface {
	SaveItem(context.Context, Principal, SavedItem) (SavedItem, error)
	RemoveSaved(context.Context, Principal, string, string, string) error
	ChangeSaved(context.Context, Principal, string, string, string, SavedChange) (SavedItem, error)
	ListSavedItems(context.Context, Principal, string, SavedState, Page) (SavedPage, error)
	DeliverSavedReminder(context.Context, Principal, SavedItem, SavedReminderSink) error
}

// SavedReminderSink must deliver only to this person and deduplicate the key.
// The durable adapter acknowledges a due time only after successful delivery.
type SavedReminderSink interface {
	NotifySaved(context.Context, Principal, SavedItem, string) error
}

// SavedConversationReader lets a routed service retain its complete policy
// decision, including bilateral grants, without rebuilding authority claims.
type SavedConversationReader interface {
	ReadSavedConversation(context.Context, GetConversationRequest) (Conversation, error)
}

func ValidateSavedOwner(ctx context.Context, p Principal, tenant string) error {
	if err := validatePrincipal(p, tenant); err != nil {
		return err
	}
	if verified, ok := trust.FromContext(ctx); ok {
		if verified.SubjectKind() != trust.SubjectKindHuman || verified.Subject() != p.SubjectID || verified.Tenant().String() != p.TenantID || !time.Now().Before(verified.ExpiresAt()) {
			return ErrPermissionDenied
		}
	}
	return nil
}

func (s *Service) savedStore(ctx context.Context, p Principal, tenant string) (SavedStore, error) {
	if err := ValidateSavedOwner(ctx, p, tenant); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrUnavailable
	}
	store, ok := s.store.(SavedStore)
	if !ok {
		return nil, ErrUnavailable
	}
	return store, nil
}

func (s *Service) savedProjection(ctx context.Context, p Principal, item SavedItem) (SavedItem, error) {
	item.Post, item.Channel, item.Availability = nil, "", "no_access"
	if item.HomeTenantID != p.TenantID || item.PersonID != p.SubjectID {
		return SavedItem{}, ErrPermissionDenied
	}
	req := GetConversationRequest{Principal: p, TenantID: item.TenantID, ConversationID: item.ConversationID}
	var c Conversation
	var err error
	if current, ok := s.authority.(SavedConversationReader); ok {
		c, err = current.ReadSavedConversation(ctx, req)
	} else {
		c, err = s.GetConversation(ctx, req)
	}
	if err != nil {
		if errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrNotFound) {
			return item, nil
		}
		return SavedItem{}, err
	}
	if c.TenantID != item.TenantID || c.ID != item.ConversationID {
		return SavedItem{}, ErrPermissionDenied
	}
	// Public discovery alone is insufficient after leaving a channel.
	m, err := s.store.GetMembership(ctx, item.TenantID, item.ConversationID, p.TenantID, p.SubjectID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return SavedItem{}, err
	}
	if err != nil || m.LeftAt != nil {
		return item, nil
	}
	post, err := s.store.GetPost(ctx, item.TenantID, item.ConversationID, item.PostID)
	if errors.Is(err, ErrNotFound) {
		item.Availability = "removed"
		return item, nil
	}
	if err != nil {
		return SavedItem{}, err
	}
	if post.TenantID != item.TenantID || post.ConversationID != item.ConversationID || post.ID != item.PostID {
		return SavedItem{}, ErrPermissionDenied
	}
	if post.Deleted {
		item.Availability = "deleted"
		if post.Body == RemovedByAdministrator {
			// CHATMOD-004: an administrator's removal reads "This message was removed".
			item.Availability = "removed"
		}
		return item, nil
	}
	if !s.postVisibleTo(ctx, p, c, post) {
		return item, nil
	}
	post = s.projectConversationReferences(ctx, p, []Post{post})[0]
	item.Post, item.Channel, item.Availability = &post, c.Name, "readable"
	return item, nil
}

func (s *Service) SaveForLater(ctx context.Context, r SavedRequest) (SavedItem, error) {
	store, err := s.savedStore(ctx, r.Principal, r.TenantID)
	if err != nil {
		return SavedItem{}, err
	}
	if r.ConversationID == "" || r.PostID == "" {
		return SavedItem{}, ErrInvalidArgument
	}
	item := SavedItem{TenantID: r.TenantID, HomeTenantID: r.Principal.TenantID, PersonID: r.Principal.SubjectID, ConversationID: r.ConversationID, PostID: r.PostID, State: SavedTodo, CreatedAt: s.now(), UpdatedAt: s.now()}
	projected, err := s.savedProjection(ctx, r.Principal, item)
	if err != nil {
		return SavedItem{}, err
	}
	if projected.Post == nil {
		return SavedItem{}, ErrPermissionDenied
	}
	return store.SaveItem(ctx, r.Principal, item)
}

func (s *Service) Unsave(ctx context.Context, r SavedRequest) error {
	store, err := s.savedStore(ctx, r.Principal, r.TenantID)
	if err != nil {
		return err
	}
	if r.ConversationID == "" || r.PostID == "" {
		return ErrInvalidArgument
	}
	return store.RemoveSaved(ctx, r.Principal, r.TenantID, r.ConversationID, r.PostID)
}

func (s *Service) UpdateSaved(ctx context.Context, r SavedRequest, change SavedChange) (SavedItem, error) {
	store, err := s.savedStore(ctx, r.Principal, r.TenantID)
	if err != nil {
		return SavedItem{}, err
	}
	if r.ConversationID == "" || r.PostID == "" || (change.State != nil && *change.State != SavedTodo && *change.State != SavedDone) || (change.Note != nil && len(*change.Note) > 4000) {
		return SavedItem{}, ErrInvalidArgument
	}
	if change.SetDue && change.DueAt != nil && !change.DueAt.After(s.now()) {
		return SavedItem{}, ErrInvalidArgument
	}
	return store.ChangeSaved(ctx, r.Principal, r.TenantID, r.ConversationID, r.PostID, change)
}

func (s *Service) MarkSavedDone(ctx context.Context, r SavedRequest) (SavedItem, error) {
	state := SavedDone
	return s.UpdateSaved(ctx, r, SavedChange{State: &state})
}
func (s *Service) ReopenSaved(ctx context.Context, r SavedRequest) (SavedItem, error) {
	state := SavedTodo
	return s.UpdateSaved(ctx, r, SavedChange{State: &state})
}
func (s *Service) SetSavedNote(ctx context.Context, r SavedRequest, note string) (SavedItem, error) {
	return s.UpdateSaved(ctx, r, SavedChange{Note: &note})
}
func (s *Service) SetSavedDue(ctx context.Context, r SavedRequest, due *time.Time) (SavedItem, error) {
	return s.UpdateSaved(ctx, r, SavedChange{SetDue: true, DueAt: due})
}

func (s *Service) ListSaved(ctx context.Context, r SavedListRequest) (SavedPage, error) {
	store, err := s.savedStore(ctx, r.Principal, r.TenantID)
	if err != nil {
		return SavedPage{}, err
	}
	if r.Tab == "" {
		r.Tab = SavedTodo
	}
	if (r.Tab != SavedTodo && r.Tab != SavedDone && r.Tab != SavedAll) || validatePage(r.Page) != nil {
		return SavedPage{}, ErrInvalidArgument
	}
	page, err := store.ListSavedItems(ctx, r.Principal, r.TenantID, r.Tab, r.Page)
	if err != nil {
		return SavedPage{}, err
	}
	for i, item := range page.Items {
		page.Items[i], err = s.savedProjection(ctx, r.Principal, item)
		if err != nil {
			return SavedPage{}, err
		}
	}
	return page, nil
}

// SearchSaved searches only current readable text and the owner's private note.
func (s *Service) SearchSaved(ctx context.Context, person Principal, query string) ([]SavedItem, error) {
	query = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "in: saved")))
	if len(query) > 4000 {
		return nil, ErrInvalidArgument
	}
	result := []SavedItem{}
	cursor := ""
	for {
		page, err := s.ListSaved(ctx, SavedListRequest{Principal: person, TenantID: person.TenantID, Tab: SavedAll, Page: Page{PageSize: 200, Cursor: cursor}})
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			text := item.Note
			if item.Post != nil {
				text += "\n" + item.Post.Body
			}
			if strings.Contains(strings.ToLower(text), query) {
				result = append(result, item)
			}
		}
		if page.NextCursor == "" {
			return result, nil
		}
		if page.NextCursor == cursor {
			return nil, ErrConflict
		}
		cursor = page.NextCursor
	}
}

func (s *Service) DispatchSavedReminders(ctx context.Context, person Principal, tenant string, sink SavedReminderSink) error {
	if sink == nil {
		return ErrUnavailable
	}
	store, err := s.savedStore(ctx, person, tenant)
	if err != nil {
		return err
	}
	cursor := ""
	for {
		page, err := s.ListSaved(ctx, SavedListRequest{Principal: person, TenantID: tenant, Tab: SavedTodo, Page: Page{Cursor: cursor, PageSize: 200}})
		if err != nil {
			return err
		}
		for _, item := range page.Items {
			if item.DueAt != nil && !item.DueAt.After(s.now()) {
				if err := store.DeliverSavedReminder(ctx, person, item, sink); err != nil {
					return err
				}
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		if page.NextCursor == cursor {
			return ErrConflict
		}
		cursor = page.NextCursor
	}
}
