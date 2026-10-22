package chatstore

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CHATGATE-013: the people already in a channel, for the gate to leave alone and
// to ask. A gate cannot know them from its own state: it holds only those it
// admitted.

func (t *chatgateTransaction) Members(ctx context.Context) ([]string, error) {
	rows, err := t.tx.Query(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND state='active' AND left_at IS NULL ORDER BY member_id`, t.scope.Tenant, t.scope.Conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (t *chatgateTransaction) IsMember(ctx context.Context, person string) (bool, error) {
	var in bool
	err := t.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3 AND state='active' AND left_at IS NULL)`, t.scope.Tenant, t.scope.Conversation, person).Scan(&in)
	return in, err
}

var _ chatgate.Roster = (*chatgateTransaction)(nil)

// readState is a gate's state without the lock, the answers and the write path a
// change takes. A list of submissions and the segments a post is addressed to
// are read this way.
func (r *GateRepository) readState(ctx context.Context, scope chatgate.Scope) (chatgate.State, error) {
	var state chatgate.State
	if r == nil || r.Store == nil || scope.Tenant == "" || scope.Conversation == "" {
		return state, chatgate.ErrInvalid
	}
	err := r.Store.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		var payload []byte
		if e := tx.QueryRow(ctx, `SELECT state_json FROM chat_gate_state WHERE tenant_id=$1 AND conversation_id=$2`, scope.Tenant, scope.Conversation).Scan(&payload); e != nil {
			if errors.Is(e, dbport.ErrNoRows) {
				return nil
			}
			return e
		}
		return json.Unmarshal(payload, &state)
	})
	return state, err
}

// ReadState implements chatgate.StateReader.
func (r *GateRepository) ReadState(ctx context.Context, scope chatgate.Scope, fn func(chatgate.State) error) error {
	state, err := r.readState(ctx, scope)
	if err != nil {
		return err
	}
	return fn(state)
}

// SegmentState is a channel's labels and segments, for the paths that address
// content to them. It carries no answer. A person who is no longer in the channel
// holds none of them here, whether or not the gate has been told they left, so
// an audience is never made of people who are not in the channel.
func (r *GateRepository) SegmentState(ctx context.Context, scope chatgate.Scope) (chatgate.SegmentState, error) {
	state, err := r.readState(ctx, scope)
	if err != nil || len(state.Segments.Assignments) == 0 {
		return state.Segments, err
	}
	in := map[string]bool{}
	err = r.Store.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND state='active' AND left_at IS NULL`, scope.Tenant, scope.Conversation)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return e
			}
			in[id] = true
		}
		return rows.Err()
	})
	if err != nil {
		return chatgate.SegmentState{}, err
	}
	for person := range state.Segments.Assignments {
		if !in[person] {
			delete(state.Segments.Assignments, person)
		}
	}
	return state.Segments, nil
}

var _ chatgate.StateReader = (*GateRepository)(nil)

// ListSubmissionRows implements chatgate.SubmissionLister from the submission log:
// the latest revision of each submission, with the questions it carries left out
// by the database, so a gate with ten thousand submissions is listed and
// filtered without reading and parsing all of them whole.
func (r *GateRepository) ListSubmissionRows(ctx context.Context, scope chatgate.Scope, status string) ([]chatgate.Submission, error) {
	if r == nil || r.Store == nil || scope.Tenant == "" || scope.Conversation == "" {
		return nil, chatgate.ErrInvalid
	}
	out := []chatgate.Submission{}
	err := r.Store.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT l.record FROM (SELECT DISTINCT ON (submission_id) submission_id, record_json - 'Fields' AS record, record_json->>'SubmittedAt' AS at FROM chat_gate_submission_revision WHERE tenant_id=$1 AND conversation_id=$2 ORDER BY submission_id, revision DESC) l WHERE ($3='' OR l.record->>'Status'=$3) ORDER BY l.at, l.submission_id`, scope.Tenant, scope.Conversation, status)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var record []byte
			if e = rows.Scan(&record); e != nil {
				return e
			}
			var sub chatgate.Submission
			if e = json.Unmarshal(record, &sub); e != nil {
				return e
			}
			out = append(out, sub)
		}
		return rows.Err()
	})
	return out, err
}

var _ chatgate.SubmissionLister = (*GateRepository)(nil)
