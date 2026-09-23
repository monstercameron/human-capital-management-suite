package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

const (
	channelTodoMaxItems     = 100
	channelTodoMaxTextBytes = 500
	channelTodoMaxSelected  = 20
)

type ChannelTodoItem struct {
	ID                        string                      `json:"id"`
	Text                      string                      `json:"text"`
	Completed                 bool                        `json:"completed"`
	CreatedBy                 string                      `json:"created_by"`
	CreatedByHomeTenantID     string                      `json:"created_by_home_tenant_id,omitempty"`
	CreatedAtUnix             int64                       `json:"created_at_unix"`
	SourcePostID              string                      `json:"source_post_id,omitempty"`
	CompletedBySubjectID      string                      `json:"completed_by_subject_id,omitempty"`
	CompletedByHomeTenantID   string                      `json:"completed_by_home_tenant_id,omitempty"`
	CompletedAtUnix           int64                       `json:"completed_at_unix,omitempty"`
	CompletionMode            string                      `json:"completion_mode,omitempty"`
	SelectedCompleters        []ChannelTodoSelectedMember `json:"selected_completers,omitempty"`
	CanToggle                 bool                        `json:"-"`
	CanManageCompletionPolicy bool                        `json:"-"`
}

type ChannelTodoSelectedMember struct {
	HomeTenantID       string `json:"home_tenant_id"`
	SubjectID          string `json:"subject_id"`
	MembershipRevision uint64 `json:"membership_revision"`
	JoinedAtUnixNano   int64  `json:"joined_at_unix_nano"`
	GrantID            string `json:"grant_id,omitempty"`
	GrantVersion       uint64 `json:"grant_version,omitempty"`
}

type ChannelTodoList struct {
	ConversationID string
	Revision       uint64
	Pinned         bool
	Items          []ChannelTodoItem
}

type ChannelTodoMutation struct {
	Operation          string
	ItemID             string
	Text               string
	Completed          bool
	Pinned             bool
	SourcePostID       string
	CompletionMode     string
	SelectedCompleters []ChannelTodoSelectedMember
}

// ChannelTodo reads a shared list only after checking a current human channel
// membership. The same check runs within every write transaction.
func (s *Store) ChannelTodo(ctx context.Context, tenantID, homeTenantID, conversationID, subjectID string, authorize func(context.Context) error) (ChannelTodoList, error) {
	out := ChannelTodoList{ConversationID: conversationID, Revision: 1, Items: []ChannelTodoItem{}}
	if s == nil || tenantID == "" || homeTenantID == "" || conversationID == "" || subjectID == "" {
		return out, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := channelTodoMember(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, true); err != nil {
			return err
		}
		if err := channelTodoPolicyFence(ctx, tx, tenantID, conversationID, authorize); err != nil {
			return err
		}
		var payload []byte
		err := tx.QueryRow(ctx, `SELECT revision,pinned,items_json FROM chat_channel_todo WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID).Scan(&out.Revision, &out.Pinned, &payload)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(payload, &out.Items); err != nil {
			return err
		}
		return projectChannelTodoLinks(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, &out)
	})
	return out, err
}

// MutateChannelTodo uses the conversation lock as the serialization point,
// including when no widget row exists yet. Revocation updates to membership
// cannot cross the FOR SHARE check without waiting for this transaction.
func (s *Store) MutateChannelTodo(ctx context.Context, tenantID, homeTenantID, conversationID, subjectID string, expected uint64, m ChannelTodoMutation, authorize func(context.Context) error) (ChannelTodoList, error) {
	out := ChannelTodoList{ConversationID: conversationID, Revision: 1, Items: []ChannelTodoItem{}}
	if s == nil || tenantID == "" || homeTenantID == "" || conversationID == "" || subjectID == "" || expected == 0 {
		return out, chat.ErrInvalidArgument
	}
	if err := validateChannelTodoMutation(m); err != nil {
		return out, err
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		manager, err := channelTodoMember(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, true)
		if err != nil {
			return err
		}
		if err := channelTodoPolicyFence(ctx, tx, tenantID, conversationID, authorize); err != nil {
			return err
		}
		if m.Operation == "SET_PINNED" && !manager {
			return chat.ErrPermissionDenied
		}
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT revision,pinned,items_json FROM chat_channel_todo WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID).Scan(&out.Revision, &out.Pinned, &payload)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if err == nil {
			if err := json.Unmarshal(payload, &out.Items); err != nil {
				return err
			}
		}
		if expected != out.Revision {
			return chat.ErrConflict
		}
		if m.Operation == "ADD" && m.SourcePostID != "" {
			if err := channelTodoPinnedPost(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, m.SourcePostID); err != nil {
				return err
			}
		}
		if m.Operation == "ADD" && m.CompletionMode != "" {
			actor, err := currentChannelTodoSelection(ctx, tx, tenantID, homeTenantID, conversationID, subjectID)
			if err != nil {
				return err
			}
			bound, err := bindChannelTodoSelections(ctx, tx, tenantID, conversationID, actor, m)
			if err != nil {
				return err
			}
			m.SelectedCompleters = bound
		}
		if m.Operation == "SET_COMPLETED" || m.Operation == "SET_COMPLETION_POLICY" {
			item := channelTodoItemByID(out.Items, m.ItemID)
			if item == nil {
				return chat.ErrNotFound
			}
			actor, err := currentChannelTodoSelection(ctx, tx, tenantID, homeTenantID, conversationID, subjectID)
			if err != nil {
				return err
			}
			if m.Operation == "SET_COMPLETED" && !canToggleChannelTodo(*item, tenantID, actor) {
				return chat.ErrPermissionDenied
			}
			if m.Operation == "SET_COMPLETION_POLICY" {
				if !channelTodoCreator(*item, tenantID, actor) {
					return chat.ErrPermissionDenied
				}
				bound, err := bindChannelTodoSelections(ctx, tx, tenantID, conversationID, actor, m)
				if err != nil {
					return err
				}
				m.SelectedCompleters = bound
			}
		}
		priorPayload, err := json.Marshal(out.Items)
		if err != nil {
			return err
		}
		priorPinned := out.Pinned
		if err := applyChannelTodoMutation(&out, homeTenantID, subjectID, m); err != nil {
			return err
		}
		out.Revision++
		payload, err = json.Marshal(out.Items)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_channel_todo(tenant_id,conversation_id,revision,pinned,items_json)
			VALUES($1,$2,$3,$4,$5) ON CONFLICT (tenant_id,conversation_id) DO UPDATE SET
			revision=EXCLUDED.revision,pinned=EXCLUDED.pinned,items_json=EXCLUDED.items_json,updated_at=now()`, tenantID, conversationID, out.Revision, out.Pinned, payload)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_channel_todo_revision(tenant_id,conversation_id,revision,actor_home_tenant_id,actor_id,operation,prior_pinned,pinned,prior_items_json,items_json)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenantID, conversationID, out.Revision, homeTenantID, subjectID, m.Operation, priorPinned, out.Pinned, priorPayload, payload)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at)
			VALUES($1,$2,$3,'CHANNEL_TODO',$4,$5,now())`, tenantID, "todo:"+conversationID+":"+strconv.FormatUint(out.Revision, 10), conversationID, conversationID, out.Revision)
		if err != nil {
			return err
		}
		return projectChannelTodoLinks(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, &out)
	})
	return out, err
}

// Hold the current channel policy row while rechecking the routed authority.
// The conversation lock above also fences first-time policy creation.
func channelTodoPolicyFence(ctx context.Context, tx dbport.Tx, tenantID, conversationID string, authorize func(context.Context) error) error {
	if authorize == nil {
		return chat.ErrPermissionDenied
	}
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM chat_channel_policy WHERE tenant_id=$1 AND conversation_id=$2 FOR SHARE`, tenantID, conversationID).Scan(&revision)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	return authorize(ctx)
}

func projectChannelTodoLinks(ctx context.Context, tx dbport.Tx, tenantID, homeTenantID, conversationID, subjectID string, out *ChannelTodoList) error {
	actor, err := currentChannelTodoSelection(ctx, tx, tenantID, homeTenantID, conversationID, subjectID)
	if err != nil {
		return err
	}
	for i := range out.Items {
		if out.Items[i].CreatedByHomeTenantID == "" {
			// Rows written before creator tenant attribution were host-only.
			out.Items[i].CreatedByHomeTenantID = tenantID
		}
		if out.Items[i].CompletionMode == "" {
			out.Items[i].CompletionMode = "EVERYONE"
		}
		out.Items[i].CanManageCompletionPolicy = channelTodoCreator(out.Items[i], tenantID, actor)
		out.Items[i].CanToggle = canToggleChannelTodo(out.Items[i], tenantID, actor)
		if !out.Items[i].CanManageCompletionPolicy {
			out.Items[i].SelectedCompleters = nil
		} else if len(out.Items[i].SelectedCompleters) != 0 {
			visible := make([]ChannelTodoSelectedMember, 0, len(out.Items[i].SelectedCompleters))
			for _, selected := range out.Items[i].SelectedCompleters {
				current, err := currentChannelTodoSelection(ctx, tx, tenantID, selected.HomeTenantID, conversationID, selected.SubjectID)
				if errors.Is(err, chat.ErrPermissionDenied) {
					continue
				}
				if err != nil {
					return err
				}
				if sameChannelTodoSelection(selected, current) {
					visible = append(visible, selected)
				}
			}
			out.Items[i].SelectedCompleters = visible
		}
		if out.Items[i].SourcePostID == "" {
			continue
		}
		if err := channelTodoPinnedPost(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, out.Items[i].SourcePostID); err != nil {
			if !errors.Is(err, chat.ErrNotFound) {
				return err
			}
			out.Items[i].SourcePostID = ""
		}
	}
	return nil
}

func channelTodoMember(ctx context.Context, tx dbport.Tx, tenantID, homeTenantID, conversationID, subjectID string, lock bool) (bool, error) {
	lockSQL := ""
	if lock {
		lockSQL = " FOR UPDATE"
	}
	var kind, owner string
	err := tx.QueryRow(ctx, `SELECT kind,owner_id FROM chat_conversation WHERE tenant_id=$1 AND id=$2 AND lifecycle='ACTIVE'`+lockSQL, tenantID, conversationID).Scan(&kind, &owner)
	if errors.Is(err, dbport.ErrNoRows) {
		return false, chat.ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if kind != string(chat.PublicChannel) && kind != string(chat.PrivateChannel) {
		return false, chat.ErrInvalidArgument
	}
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' AND left_at IS NULL FOR SHARE`, tenantID, conversationID, homeTenantID, subjectID).Scan(&role)
	if errors.Is(err, dbport.ErrNoRows) {
		return false, chat.ErrPermissionDenied
	}
	if err != nil {
		return false, err
	}
	if tenantID != homeTenantID {
		var grantID string
		err = tx.QueryRow(ctx, `SELECT id FROM chat_share_grant WHERE tenant_id=$1 AND conversation_id=$2 AND consumer_tenant=$3 AND accepted_at IS NOT NULL AND revoked_at IS NULL AND expires_at>now() FOR SHARE`, tenantID, conversationID, homeTenantID).Scan(&grantID)
		if errors.Is(err, dbport.ErrNoRows) {
			return false, chat.ErrPermissionDenied
		}
		if err != nil {
			return false, err
		}
	}
	return homeTenantID == tenantID && subjectID == owner || role == "manager", nil
}

func channelTodoPinnedPost(ctx context.Context, tx dbport.Tx, tenantID, homeTenantID, conversationID, subjectID, postID string) error {
	var found string
	err := tx.QueryRow(ctx, `SELECT p.id FROM chat_post p JOIN chat_pin pin ON pin.tenant_id=p.tenant_id AND pin.conversation_id=p.conversation_id AND pin.post_id=p.id
		JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' AND m.left_at IS NULL
		WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$5 AND NOT p.tombstoned
		AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))
		LIMIT 1 FOR SHARE OF p,pin,m`, tenantID, conversationID, homeTenantID, subjectID, postID).Scan(&found)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrNotFound
	}
	return err
}

func validateChannelTodoMutation(m ChannelTodoMutation) error {
	switch m.Operation {
	case "ADD":
		if m.ItemID != "" || (m.CompletionMode != "" && m.CompletionMode != "EVERYONE" && m.CompletionMode != "ME" && m.CompletionMode != "ME_AND_SELECTED") || len(m.SelectedCompleters) > channelTodoMaxSelected || (m.CompletionMode != "ME_AND_SELECTED" && len(m.SelectedCompleters) != 0) || strings.TrimSpace(m.Text) == "" || len(m.Text) > channelTodoMaxTextBytes || !utf8.ValidString(m.Text) {
			return chat.ErrInvalidArgument
		}
		for _, r := range m.Text {
			if r == 0 || (unicode.IsControl(r) && r != '\n' && r != '\t') {
				return chat.ErrInvalidArgument
			}
		}
	case "SET_COMPLETED", "DELETE":
		if m.ItemID == "" || m.Text != "" || m.SourcePostID != "" || m.CompletionMode != "" || len(m.SelectedCompleters) != 0 {
			return chat.ErrInvalidArgument
		}
	case "SET_PINNED":
		if m.ItemID != "" || m.Text != "" || m.SourcePostID != "" || m.CompletionMode != "" || len(m.SelectedCompleters) != 0 {
			return chat.ErrInvalidArgument
		}
	case "SET_COMPLETION_POLICY":
		if m.ItemID == "" || m.Text != "" || m.SourcePostID != "" || (m.CompletionMode != "EVERYONE" && m.CompletionMode != "ME" && m.CompletionMode != "ME_AND_SELECTED") || len(m.SelectedCompleters) > channelTodoMaxSelected || (m.CompletionMode != "ME_AND_SELECTED" && len(m.SelectedCompleters) != 0) {
			return chat.ErrInvalidArgument
		}
	default:
		return chat.ErrInvalidArgument
	}
	return nil
}

func applyChannelTodoMutation(out *ChannelTodoList, homeTenantID, subjectID string, m ChannelTodoMutation) error {
	switch m.Operation {
	case "ADD":
		if len(out.Items) >= channelTodoMaxItems {
			return chat.ErrConflict
		}
		mode := m.CompletionMode
		if mode == "" {
			mode = "EVERYONE"
		}
		out.Items = append(out.Items, ChannelTodoItem{ID: uuid.NewString(), Text: strings.TrimSpace(m.Text), CreatedBy: subjectID, CreatedByHomeTenantID: homeTenantID, CreatedAtUnix: time.Now().UTC().Unix(), SourcePostID: m.SourcePostID, CompletionMode: mode, SelectedCompleters: m.SelectedCompleters})
	case "SET_COMPLETED", "DELETE":
		for i := range out.Items {
			if out.Items[i].ID == m.ItemID {
				if m.Operation == "SET_COMPLETED" {
					out.Items[i].Completed = m.Completed
					if m.Completed {
						out.Items[i].CompletedBySubjectID = subjectID
						out.Items[i].CompletedByHomeTenantID = homeTenantID
						out.Items[i].CompletedAtUnix = time.Now().UTC().Unix()
					} else {
						out.Items[i].CompletedBySubjectID = ""
						out.Items[i].CompletedByHomeTenantID = ""
						out.Items[i].CompletedAtUnix = 0
					}
				} else {
					out.Items = append(out.Items[:i], out.Items[i+1:]...)
				}
				return nil
			}
		}
		return chat.ErrNotFound
	case "SET_PINNED":
		out.Pinned = m.Pinned
	case "SET_COMPLETION_POLICY":
		item := channelTodoItemByID(out.Items, m.ItemID)
		if item == nil {
			return chat.ErrNotFound
		}
		item.CompletionMode = m.CompletionMode
		item.SelectedCompleters = m.SelectedCompleters
	}
	return nil
}
