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

type ChannelTeamMember struct {
	HomeTenantID string
	SubjectID    string
	Role         string
	RoleLabel    string
	joinedAt     time.Time
}
type ChannelTeamWidget struct {
	ConversationID string
	Revision       uint64
	Pinned         bool
	Purpose        string
	Members        []ChannelTeamMember
	CanPin         bool
}
type ChannelProjectMilestone struct {
	ID                string                    `json:"id"`
	Text              string                    `json:"text"`
	Status            string                    `json:"status"`
	OwnerHomeTenantID string                    `json:"owner_home_tenant_id,omitempty"`
	OwnerSubjectID    string                    `json:"owner_subject_id,omitempty"`
	DueDate           string                    `json:"due_date,omitempty"`
	OwnerBinding      ChannelTodoSelectedMember `json:"owner_binding,omitempty"`
}
type ChannelProjectWidget struct {
	ConversationID string
	Revision       uint64
	Pinned         bool
	Title          string
	Summary        string
	Milestones     []ChannelProjectMilestone
	CanPin         bool
}
type ChannelWidgets struct {
	Team    ChannelTeamWidget
	Project ChannelProjectWidget
}
type ChannelWidgetMutation struct {
	Kind               string
	Operation          string
	Pinned             bool
	Purpose            string
	MemberHomeTenantID string
	MemberSubjectID    string
	RoleLabel          string
	Title              string
	Summary            string
	Milestone          ChannelProjectMilestone
	MilestoneID        string
}
type teamLabel struct {
	HomeTenantID     string                    `json:"home_tenant_id"`
	SubjectID        string                    `json:"subject_id"`
	JoinedAtUnixNano int64                     `json:"joined_at_unix_nano"`
	Label            string                    `json:"label"`
	Binding          ChannelTodoSelectedMember `json:"binding"`
}
type teamPayload struct {
	Purpose string      `json:"purpose"`
	Labels  []teamLabel `json:"labels"`
}
type projectPayload struct {
	Title      string                    `json:"title"`
	Summary    string                    `json:"summary"`
	Milestones []ChannelProjectMilestone `json:"milestones"`
}

func emptyChannelWidgets(conversation string) ChannelWidgets {
	return ChannelWidgets{Team: ChannelTeamWidget{ConversationID: conversation, Revision: 1, Members: []ChannelTeamMember{}}, Project: ChannelProjectWidget{ConversationID: conversation, Revision: 1, Milestones: []ChannelProjectMilestone{}}}
}

func (s *Store) ChannelWidgets(ctx context.Context, host, home, conversation, subject string, authorize func(context.Context) error) (ChannelWidgets, error) {
	out := emptyChannelWidgets(conversation)
	if s == nil || host == "" || home == "" || conversation == "" || subject == "" {
		return out, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		manager, err := channelTodoMember(ctx, tx, host, home, conversation, subject, true)
		if err != nil {
			return err
		}
		if err := channelTodoPolicyFence(ctx, tx, host, conversation, authorize); err != nil {
			return err
		}
		return loadChannelWidgets(ctx, tx, host, conversation, manager, &out)
	})
	return out, err
}

func loadChannelWidgets(ctx context.Context, tx dbport.Tx, host, conversation string, manager bool, out *ChannelWidgets) error {
	out.Team.CanPin, out.Project.CanPin = manager, manager
	members, err := channelWidgetMembers(ctx, tx, host, conversation)
	if err != nil {
		return err
	}
	out.Team.Members = members
	for _, kind := range []string{"TEAM", "PROJECT"} {
		var revision uint64
		var pinned bool
		var payload []byte
		err := tx.QueryRow(ctx, `SELECT revision,pinned,payload_json FROM chat_channel_widget WHERE tenant_id=$1 AND conversation_id=$2 AND kind=$3`, host, conversation, kind).Scan(&revision, &pinned, &payload)
		if errors.Is(err, dbport.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if kind == "TEAM" {
			var v teamPayload
			if err := json.Unmarshal(payload, &v); err != nil {
				return err
			}
			out.Team.Revision, out.Team.Pinned, out.Team.Purpose = revision, pinned, v.Purpose
			for i := range out.Team.Members {
				for _, label := range v.Labels {
					if label.HomeTenantID == out.Team.Members[i].HomeTenantID && label.SubjectID == out.Team.Members[i].SubjectID && label.JoinedAtUnixNano == out.Team.Members[i].joinedAt.UnixNano() {
						current, err := currentChannelTodoSelection(ctx, tx, host, label.HomeTenantID, conversation, label.SubjectID)
						if err != nil && !errors.Is(err, chat.ErrPermissionDenied) {
							return err
						}
						if err == nil && sameChannelTodoSelection(current, label.Binding) {
							out.Team.Members[i].RoleLabel = label.Label
						}
						break
					}
				}
			}
		} else {
			var v projectPayload
			if err := json.Unmarshal(payload, &v); err != nil {
				return err
			}
			out.Project.Revision, out.Project.Pinned, out.Project.Title, out.Project.Summary = revision, pinned, v.Title, v.Summary
			out.Project.Milestones = v.Milestones
			if out.Project.Milestones == nil {
				out.Project.Milestones = []ChannelProjectMilestone{}
			}
			for i := range out.Project.Milestones {
				if out.Project.Milestones[i].OwnerSubjectID != "" {
					owner := &out.Project.Milestones[i]
					current, err := currentChannelTodoSelection(ctx, tx, host, owner.OwnerHomeTenantID, conversation, owner.OwnerSubjectID)
					if err != nil && !errors.Is(err, chat.ErrPermissionDenied) {
						return err
					}
					if !channelWidgetHasMember(members, owner.OwnerHomeTenantID, owner.OwnerSubjectID) || err != nil || !sameChannelTodoSelection(current, owner.OwnerBinding) {
						owner.OwnerHomeTenantID, owner.OwnerSubjectID = "", ""
					}
					owner.OwnerBinding = ChannelTodoSelectedMember{}
				}
			}
		}
	}
	return nil
}

func (s *Store) MutateChannelWidget(ctx context.Context, host, home, conversation, subject string, expected uint64, m ChannelWidgetMutation, authorize func(context.Context) error) (ChannelWidgets, error) {
	out := emptyChannelWidgets(conversation)
	if s == nil || host == "" || home == "" || conversation == "" || subject == "" || expected == 0 {
		return out, chat.ErrInvalidArgument
	}
	if err := validateChannelWidgetMutation(m); err != nil {
		return out, err
	}
	err := s.RunTenantTx(ctx, host, func(tx dbport.Tx) error {
		manager, err := channelTodoMember(ctx, tx, host, home, conversation, subject, true)
		if err != nil {
			return err
		}
		if err := channelTodoPolicyFence(ctx, tx, host, conversation, authorize); err != nil {
			return err
		}
		if m.Operation == "SET_PINNED" && !manager {
			return chat.ErrPermissionDenied
		}
		members, err := channelWidgetMembers(ctx, tx, host, conversation)
		if err != nil {
			return err
		}
		var revision uint64 = 1
		var pinned bool
		var payload []byte
		err = tx.QueryRow(ctx, `SELECT revision,pinned,payload_json FROM chat_channel_widget WHERE tenant_id=$1 AND conversation_id=$2 AND kind=$3`, host, conversation, m.Kind).Scan(&revision, &pinned, &payload)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if expected != revision {
			return chat.ErrConflict
		}
		if payload == nil {
			payload = []byte(`{}`)
		}
		priorPayload, priorPinned := payload, pinned
		if m.Kind == "TEAM" {
			var v teamPayload
			if err := json.Unmarshal(payload, &v); err != nil {
				return err
			}
			switch m.Operation {
			case "SET_PINNED":
				pinned = m.Pinned
			case "SET_PURPOSE":
				v.Purpose = strings.TrimSpace(m.Purpose)
			case "SET_ROLE_LABEL":
				if !channelWidgetHasMember(members, m.MemberHomeTenantID, m.MemberSubjectID) {
					return chat.ErrNotFound
				}
				for _, member := range members {
					if member.HomeTenantID == m.MemberHomeTenantID && member.SubjectID == m.MemberSubjectID {
						labels := v.Labels[:0]
						for _, label := range v.Labels {
							if label.HomeTenantID != member.HomeTenantID || label.SubjectID != member.SubjectID {
								labels = append(labels, label)
							}
						}
						v.Labels = labels
						if strings.TrimSpace(m.RoleLabel) != "" {
							binding, err := currentChannelTodoSelection(ctx, tx, host, member.HomeTenantID, conversation, member.SubjectID)
							if err != nil {
								return err
							}
							v.Labels = append(v.Labels, teamLabel{member.HomeTenantID, member.SubjectID, member.joinedAt.UnixNano(), strings.TrimSpace(m.RoleLabel), binding})
						}
						break
					}
				}
			}
			payload, err = json.Marshal(v)
		} else {
			var v projectPayload
			if err := json.Unmarshal(payload, &v); err != nil {
				return err
			}
			switch m.Operation {
			case "SET_PINNED":
				pinned = m.Pinned
			case "SET_DETAILS":
				v.Title, v.Summary = strings.TrimSpace(m.Title), strings.TrimSpace(m.Summary)
			case "ADD_MILESTONE":
				if len(v.Milestones) >= 50 {
					return chat.ErrInvalidArgument
				}
				if m.Milestone.OwnerSubjectID != "" && !channelWidgetHasMember(members, m.Milestone.OwnerHomeTenantID, m.Milestone.OwnerSubjectID) {
					return chat.ErrNotFound
				}
				if m.Milestone.OwnerSubjectID != "" {
					binding, e := currentChannelTodoSelection(ctx, tx, host, m.Milestone.OwnerHomeTenantID, conversation, m.Milestone.OwnerSubjectID)
					if e != nil {
						return e
					}
					m.Milestone.OwnerBinding = binding
				}
				m.Milestone.ID = uuid.NewString()
				v.Milestones = append(v.Milestones, m.Milestone)
			case "UPDATE_MILESTONE", "DELETE_MILESTONE":
				found := false
				for i := range v.Milestones {
					if v.Milestones[i].ID == m.MilestoneID {
						found = true
						if m.Operation == "DELETE_MILESTONE" {
							v.Milestones = append(v.Milestones[:i], v.Milestones[i+1:]...)
						} else {
							if m.Milestone.OwnerSubjectID != "" && !channelWidgetHasMember(members, m.Milestone.OwnerHomeTenantID, m.Milestone.OwnerSubjectID) {
								return chat.ErrNotFound
							}
							if m.Milestone.OwnerSubjectID != "" {
								binding, e := currentChannelTodoSelection(ctx, tx, host, m.Milestone.OwnerHomeTenantID, conversation, m.Milestone.OwnerSubjectID)
								if e != nil {
									return e
								}
								m.Milestone.OwnerBinding = binding
							}
							m.Milestone.ID = m.MilestoneID
							v.Milestones[i] = m.Milestone
						}
						break
					}
				}
				if !found {
					return chat.ErrNotFound
				}
			}
			payload, err = json.Marshal(v)
		}
		if err != nil {
			return err
		}
		revision++
		if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_widget(tenant_id,conversation_id,kind,revision,pinned,payload_json) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (tenant_id,conversation_id,kind) DO UPDATE SET revision=EXCLUDED.revision,pinned=EXCLUDED.pinned,payload_json=EXCLUDED.payload_json,updated_at=now()`, host, conversation, m.Kind, revision, pinned, payload); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_widget_revision(tenant_id,conversation_id,kind,revision,actor_home_tenant_id,actor_id,operation,prior_pinned,pinned,prior_payload_json,payload_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, host, conversation, m.Kind, revision, home, subject, m.Operation, priorPinned, pinned, priorPayload, payload); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at) VALUES($1,$2,$3,'CHANNEL_WIDGET',$4,$5,now())`, host, "widget:"+conversation+":"+m.Kind+":"+strconv.FormatUint(revision, 10), conversation, m.Kind, revision); err != nil {
			return err
		}
		return loadChannelWidgets(ctx, tx, host, conversation, manager, &out)
	})
	return out, err
}

func channelWidgetMembers(ctx context.Context, tx dbport.Tx, host, conversation string) ([]ChannelTeamMember, error) {
	rows, err := tx.Query(ctx, `SELECT m.home_tenant_id,m.member_id,m.role,m.joined_at FROM chat_membership m WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.state='active' AND m.left_at IS NULL AND (m.home_tenant_id=$1 OR EXISTS (SELECT 1 FROM chat_share_grant g WHERE g.tenant_id=m.tenant_id AND g.conversation_id=m.conversation_id AND g.consumer_tenant=m.home_tenant_id AND g.accepted_at IS NOT NULL AND g.revoked_at IS NULL AND g.expires_at>now())) ORDER BY m.home_tenant_id,m.member_id LIMIT 501`, host, conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]ChannelTeamMember, 0)
	for rows.Next() {
		var m ChannelTeamMember
		if err := rows.Scan(&m.HomeTenantID, &m.SubjectID, &m.Role, &m.joinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(members) > 500 {
		return nil, chat.ErrUnavailable
	}
	return members, nil
}

func channelWidgetHasMember(members []ChannelTeamMember, home, subject string) bool {
	if home == "" || subject == "" {
		return home == "" && subject == ""
	}
	for _, m := range members {
		if m.HomeTenantID == home && m.SubjectID == subject {
			return true
		}
	}
	return false
}

func validWidgetText(v string, max int) bool {
	if len(v) > max || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}
func validateChannelWidgetMutation(m ChannelWidgetMutation) error {
	if m.Kind != "TEAM" && m.Kind != "PROJECT" {
		return chat.ErrInvalidArgument
	}
	switch m.Operation {
	case "SET_PINNED":
		return nil
	case "SET_PURPOSE":
		if m.Kind == "TEAM" && validWidgetText(m.Purpose, 1000) {
			return nil
		}
	case "SET_ROLE_LABEL":
		if m.Kind == "TEAM" && m.MemberHomeTenantID != "" && m.MemberSubjectID != "" && validWidgetText(m.RoleLabel, 80) {
			return nil
		}
	case "SET_DETAILS":
		if m.Kind == "PROJECT" && validWidgetText(m.Title, 120) && validWidgetText(m.Summary, 1000) {
			return nil
		}
	case "DELETE_MILESTONE":
		if m.Kind == "PROJECT" && m.MilestoneID != "" {
			return nil
		}
	case "ADD_MILESTONE", "UPDATE_MILESTONE":
		if m.Kind != "PROJECT" || (m.Operation == "UPDATE_MILESTONE" && m.MilestoneID == "") || strings.TrimSpace(m.Milestone.Text) == "" || !validWidgetText(m.Milestone.Text, 240) || (m.Milestone.OwnerSubjectID == "") != (m.Milestone.OwnerHomeTenantID == "") {
			break
		}
		switch m.Milestone.Status {
		case "PLANNED", "IN_PROGRESS", "BLOCKED", "DONE":
		default:
			return chat.ErrInvalidArgument
		}
		if m.Milestone.DueDate != "" {
			d, err := time.Parse("2006-01-02", m.Milestone.DueDate)
			if err != nil || d.Format("2006-01-02") != m.Milestone.DueDate {
				return chat.ErrInvalidArgument
			}
		}
		return nil
	}
	return chat.ErrInvalidArgument
}
