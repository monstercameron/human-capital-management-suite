package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type ChannelPollOption struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Count int    `json:"-"`
}

type ChannelPoll struct {
	ConversationID string
	Revision       uint64
	Question       string
	Options        []ChannelPollOption
	MyOptionID     string
	TotalVotes     int
}

type ChannelPollMutation struct {
	Operation string
	Question  string
	Options   []string
	OptionID  string
}

type channelPollPayload struct {
	Question string              `json:"question"`
	Options  []ChannelPollOption `json:"options"`
}

func emptyChannelPoll(conversation string) ChannelPoll {
	return ChannelPoll{ConversationID: conversation, Revision: 1, Options: []ChannelPollOption{}}
}

func validateChannelPollMutation(m ChannelPollMutation) error {
	switch m.Operation {
	case "CREATE":
		if strings.TrimSpace(m.Question) == "" || len(m.Question) > 240 || !validPollText(m.Question) || len(m.Options) < 2 || len(m.Options) > 10 || m.OptionID != "" {
			return chat.ErrInvalidArgument
		}
		seen := map[string]bool{}
		for _, option := range m.Options {
			value := strings.TrimSpace(option)
			if value == "" || len(value) > 100 || !validPollText(option) || seen[strings.ToLower(value)] {
				return chat.ErrInvalidArgument
			}
			seen[strings.ToLower(value)] = true
		}
	case "VOTE":
		if m.OptionID == "" || m.Question != "" || len(m.Options) != 0 {
			return chat.ErrInvalidArgument
		}
	default:
		return chat.ErrInvalidArgument
	}
	return nil
}

func validPollText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}

// ChannelPoll reads the channel poll after validating current membership and
// routed authority. Individual votes are projected only as the caller's own
// selection; voter identities never leave the store.
func (s *Store) ChannelPoll(ctx context.Context, tenantID, homeTenantID, conversationID, subjectID string, authorize func(context.Context) error) (ChannelPoll, error) {
	out := emptyChannelPoll(conversationID)
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
		return readChannelPoll(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, &out)
	})
	return out, err
}

func readChannelPoll(ctx context.Context, tx dbport.Tx, tenantID, homeTenantID, conversationID, subjectID string, out *ChannelPoll) error {
	var payload []byte
	err := tx.QueryRow(ctx, `SELECT revision,question,options_json FROM chat_channel_poll WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID).Scan(&out.Revision, &out.Question, &payload)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, &out.Options); err != nil {
		return err
	}
	out.Options = append([]ChannelPollOption{}, out.Options...)
	for i := range out.Options {
		out.Options[i].Count = 0
	}
	rows, err := tx.Query(ctx, `SELECT option_id,count(*) FROM chat_channel_poll_vote WHERE tenant_id=$1 AND conversation_id=$2 GROUP BY option_id`, tenantID, conversationID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var optionID string
		var count int
		if err := rows.Scan(&optionID, &count); err != nil {
			rows.Close()
			return err
		}
		out.TotalVotes += count
		for i := range out.Options {
			if out.Options[i].ID == optionID {
				out.Options[i].Count = count
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	err = tx.QueryRow(ctx, `SELECT option_id FROM chat_channel_poll_vote WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND subject_id=$4`, tenantID, conversationID, homeTenantID, subjectID).Scan(&out.MyOptionID)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	return nil
}

// MutateChannelPoll serializes create and vote operations on the conversation
// row and records every accepted revision in the immutable widget audit log.
func (s *Store) MutateChannelPoll(ctx context.Context, tenantID, homeTenantID, conversationID, subjectID string, expected uint64, m ChannelPollMutation, authorize func(context.Context) error) (ChannelPoll, error) {
	out := emptyChannelPoll(conversationID)
	if s == nil || tenantID == "" || homeTenantID == "" || conversationID == "" || subjectID == "" || expected == 0 {
		return out, chat.ErrInvalidArgument
	}
	if err := validateChannelPollMutation(m); err != nil {
		return out, err
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := channelTodoMember(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, true); err != nil {
			return err
		}
		if err := channelTodoPolicyFence(ctx, tx, tenantID, conversationID, authorize); err != nil {
			return err
		}
		var revision uint64 = 1
		var priorQuestion string
		var priorOptions []byte
		err := tx.QueryRow(ctx, `SELECT revision,question,options_json FROM chat_channel_poll WHERE tenant_id=$1 AND conversation_id=$2`, tenantID, conversationID).Scan(&revision, &priorQuestion, &priorOptions)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if expected != revision {
			return chat.ErrConflict
		}
		stored := channelPollPayload{Question: priorQuestion, Options: []ChannelPollOption{}}
		if len(priorOptions) != 0 {
			if err := json.Unmarshal(priorOptions, &stored.Options); err != nil {
				return err
			}
		}
		voteDelta := channelPollVoteDelta{}
		if m.Operation == "CREATE" {
			if stored.Question != "" {
				return chat.ErrConflict
			}
			stored.Question = strings.TrimSpace(m.Question)
			for _, text := range m.Options {
				stored.Options = append(stored.Options, ChannelPollOption{ID: uuid.NewString(), Text: strings.TrimSpace(text)})
			}
		} else {
			if stored.Question == "" {
				return chat.ErrNotFound
			}
			valid := false
			for _, option := range stored.Options {
				valid = valid || option.ID == m.OptionID
			}
			if !valid {
				return chat.ErrNotFound
			}
			voteDelta.HomeTenantID, voteDelta.SubjectID, voteDelta.OptionID = homeTenantID, subjectID, m.OptionID
			err := tx.QueryRow(ctx, `SELECT option_id FROM chat_channel_poll_vote WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND subject_id=$4 FOR UPDATE`, tenantID, conversationID, homeTenantID, subjectID).Scan(&voteDelta.PriorOptionID)
			if err != nil && !errors.Is(err, dbport.ErrNoRows) {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_poll_vote(tenant_id,conversation_id,home_tenant_id,subject_id,option_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id,home_tenant_id,subject_id) DO UPDATE SET option_id=EXCLUDED.option_id,voted_at=now()`, tenantID, conversationID, homeTenantID, subjectID, m.OptionID); err != nil {
				return err
			}
		}
		options, err := json.Marshal(stored.Options)
		if err != nil {
			return err
		}
		revision++
		if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_poll(tenant_id,conversation_id,revision,question,options_json) VALUES($1,$2,$3,$4,$5) ON CONFLICT (tenant_id,conversation_id) DO UPDATE SET revision=EXCLUDED.revision,question=EXCLUDED.question,options_json=EXCLUDED.options_json,updated_at=now()`, tenantID, conversationID, revision, stored.Question, options); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_channel_poll_revision(tenant_id,conversation_id,revision,actor_home_tenant_id,actor_id,operation,prior_question,question,prior_options_json,options_json,vote_home_tenant_id,vote_subject_id,prior_option_id,option_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, tenantID, conversationID, revision, homeTenantID, subjectID, m.Operation, priorQuestion, stored.Question, emptyJSON(priorOptions), options, nullableVoteIdentity(m.Operation, voteDelta.HomeTenantID), nullableVoteIdentity(m.Operation, voteDelta.SubjectID), voteDelta.PriorOptionID, voteDelta.OptionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_record_inventory(tenant_id,record_id,conversation_id,kind,source_id,revision,created_at) VALUES($1,$2,$3,'CHANNEL_POLL',$4,$5,now())`, tenantID, "poll:"+conversationID+":"+strconv.FormatUint(revision, 10), conversationID, conversationID, revision); err != nil {
			return err
		}
		return readChannelPoll(ctx, tx, tenantID, homeTenantID, conversationID, subjectID, &out)
	})
	return out, err
}

type channelPollVoteDelta struct {
	HomeTenantID, SubjectID, PriorOptionID, OptionID string
}

func nullableVoteIdentity(operation, value string) any {
	if operation == "CREATE" {
		return nil
	}
	return value
}

func emptyJSON(value []byte) []byte {
	if len(value) == 0 {
		return []byte(`[]`)
	}
	return value
}
