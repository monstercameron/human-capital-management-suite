package chatstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// GateMembershipPort performs the existing membership mutation and current
// channel eligibility check on this transaction, not on a second connection.
type GateMembershipPort func(context.Context, dbport.Tx, chatgate.Scope, chatgate.Actor, string, bool) error
type GateRepository struct {
	Store      *Store
	Membership GateMembershipPort
}

func (r *GateRepository) ListGateScopes(ctx context.Context, tenant string) ([]chatgate.Scope, error) {
	out := []chatgate.Scope{}
	if r == nil || r.Store == nil {
		return nil, chatgate.ErrUnavailable
	}
	e := r.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT conversation_id FROM chat_gate_state WHERE tenant_id=$1 ORDER BY conversation_id`, tenant)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return e
			}
			out = append(out, chatgate.Scope{Tenant: tenant, Conversation: id})
		}
		return rows.Err()
	})
	return out, e
}

type chatgateTransaction struct {
	tx         dbport.Tx
	scope      chatgate.Scope
	state      chatgate.State
	answers    []chatgate.Answer
	membership GateMembershipPort
}

func (t *chatgateTransaction) State() *chatgate.State      { return &t.state }
func (t *chatgateTransaction) Answers() *[]chatgate.Answer { return &t.answers }
func (t *chatgateTransaction) Membership(ctx context.Context, a chatgate.Actor, v string, admit bool) error {
	if t.membership == nil {
		return chatgate.ErrUnavailable
	}
	// The database fence sees the decision only inside this transaction.
	b, err := json.Marshal(t.state)
	if err != nil {
		return err
	}
	if _, err = t.tx.Exec(ctx, `INSERT INTO chat_gate_state(tenant_id,conversation_id,revision,state_json) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET revision=excluded.revision,state_json=excluded.state_json`, t.scope.Tenant, t.scope.Conversation, t.state.Gate.Revision, b); err != nil {
		return err
	}
	if e := t.membership(ctx, t.tx, t.scope, a, v, admit); e != nil {
		return e
	}
	_, e := t.tx.Exec(ctx, `INSERT INTO chat_gate_membership_basis(tenant_id,conversation_id,person_id,semantic_version,revision,active) SELECT $1,$2,$3,$4,coalesce(max(revision),0)+1,$5 FROM chat_gate_membership_basis WHERE tenant_id=$1 AND conversation_id=$2 AND person_id=$3`, t.scope.Tenant, t.scope.Conversation, a.Person, v, admit)
	return e
}
func (r *GateRepository) Transact(ctx context.Context, scope chatgate.Scope, fn func(chatgate.Transaction) error) error {
	if r == nil || r.Store == nil || scope.Tenant == "" || scope.Conversation == "" || fn == nil {
		return chatgate.ErrInvalid
	}
	return r.Store.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		var id string
		if e := tx.QueryRow(ctx, `SELECT id FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, scope.Tenant, scope.Conversation).Scan(&id); e != nil {
			if errors.Is(e, dbport.ErrNoRows) {
				return chatgate.ErrNotFound
			}
			return e
		}
		if err := fenceContextWrite(ctx, tx, scope.Tenant, scope.Conversation); err != nil {
			return err
		}
		t := &chatgateTransaction{tx: tx, scope: scope, membership: r.Membership}
		var payload []byte
		e := tx.QueryRow(ctx, `SELECT state_json FROM chat_gate_state WHERE tenant_id=$1 AND conversation_id=$2`, scope.Tenant, scope.Conversation).Scan(&payload)
		if e != nil && !errors.Is(e, dbport.ErrNoRows) {
			return e
		}
		if len(payload) > 0 {
			if e = json.Unmarshal(payload, &t.state); e != nil {
				return e
			}
		}
		rows, e := tx.Query(ctx, `SELECT submission_id,person_id,field_id,value_json,expires_at,legal_hold FROM chat_gate_answer WHERE tenant_id=$1 AND conversation_id=$2`, scope.Tenant, scope.Conversation)
		if e != nil {
			return e
		}
		for rows.Next() {
			var a chatgate.Answer
			if e = rows.Scan(&a.SubmissionID, &a.Person, &a.FieldID, &a.Value, &a.ExpiresAt, &a.Held); e != nil {
				rows.Close()
				return e
			}
			t.answers = append(t.answers, a)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		oldAnswers := append([]chatgate.Answer(nil), t.answers...)
		beforeState, err := json.Marshal(t.state)
		if err != nil {
			return err
		}
		beforeAnswers, err := json.Marshal(t.answers)
		if err != nil {
			return err
		}
		oldVersions := len(t.state.Gate.Versions)
		oldAudits := len(t.state.Audits)
		oldEvents := len(t.state.Events)
		if e = fn(t); e != nil {
			return e
		}
		afterState, err := json.Marshal(t.state)
		if err != nil {
			return err
		}
		afterAnswers, err := json.Marshal(t.answers)
		if err != nil {
			return err
		}
		if bytes.Equal(beforeState, afterState) && bytes.Equal(beforeAnswers, afterAnswers) {
			return nil
		}
		if len(t.state.Gate.Versions) < oldVersions || len(t.state.Audits) < oldAudits || len(t.state.Events) < oldEvents {
			return chatgate.ErrConflict
		}
		for _, sub := range t.state.Submissions {
			b, e := json.Marshal(sub)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_gate_submission_revision(tenant_id,conversation_id,submission_id,revision,record_json) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id,submission_id,revision) DO NOTHING`, scope.Tenant, scope.Conversation, sub.ID, sub.Revision, b); e != nil {
				return e
			}
		}
		for _, d := range t.state.Gate.Versions[oldVersions:] {
			b, e := json.Marshal(d)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_gate_version(tenant_id,conversation_id,semantic_version,digest,definition_json) VALUES($1,$2,$3,$4,$5)`, scope.Tenant, scope.Conversation, d.Version.String(), d.Digest, b); e != nil {
				return e
			}
		}
		keep := map[string]bool{}
		for _, a := range t.answers {
			keep[a.SubmissionID+"\x00"+a.FieldID] = true
			b := []byte(a.Value)
			if _, e = tx.Exec(ctx, `INSERT INTO chat_gate_answer(tenant_id,conversation_id,submission_id,person_id,field_id,value_json,expires_at,legal_hold) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(tenant_id,conversation_id,submission_id,field_id) DO UPDATE SET value_json=excluded.value_json,expires_at=excluded.expires_at WHERE chat_gate_answer.submission_id LIKE 'draft:%' AND NOT chat_gate_answer.legal_hold`, scope.Tenant, scope.Conversation, a.SubmissionID, a.Person, a.FieldID, b, a.ExpiresAt, a.Held); e != nil {
				return e
			}
		}
		for _, a := range oldAnswers {
			if !keep[a.SubmissionID+"\x00"+a.FieldID] {
				if a.Held {
					return chatgate.ErrHeld
				}
				if _, e = tx.Exec(ctx, `DELETE FROM chat_gate_answer WHERE tenant_id=$1 AND conversation_id=$2 AND submission_id=$3 AND field_id=$4 AND NOT legal_hold`, scope.Tenant, scope.Conversation, a.SubmissionID, a.FieldID); e != nil {
					return e
				}
			}
		}
		for i, a := range t.state.Audits[oldAudits:] {
			b, e := json.Marshal(a)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_gate_read_audit(tenant_id,conversation_id,sequence,audit_json) VALUES($1,$2,$3,$4)`, scope.Tenant, scope.Conversation, oldAudits+i+1, b); e != nil {
				return e
			}
		}
		for _, event := range t.state.Events[oldEvents:] {
			b, e := json.Marshal(event)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4)`, scope.Tenant, scope.Conversation, "gate."+event.Kind, b); e != nil {
				return e
			}
		}
		b, e := json.Marshal(t.state)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO chat_gate_state(tenant_id,conversation_id,revision,state_json) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET revision=excluded.revision,state_json=excluded.state_json`, scope.Tenant, scope.Conversation, t.state.Gate.Revision, b)
		return e
	})
}

// GateInForce reports whether a conversation has a gate that joining must
// satisfy: one with a published version that is neither paused nor retired.
// It is one read with no lock, for the membership path: every add to every
// conversation asks it, and almost none of them has a gate.
func (r *GateRepository) GateInForce(ctx context.Context, scope chatgate.Scope) (bool, error) {
	if r == nil || r.Store == nil || scope.Tenant == "" || scope.Conversation == "" {
		return false, chatgate.ErrInvalid
	}
	inForce := false
	err := r.Store.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		var payload []byte
		if e := tx.QueryRow(ctx, `SELECT state_json FROM chat_gate_state WHERE tenant_id=$1 AND conversation_id=$2`, scope.Tenant, scope.Conversation).Scan(&payload); e != nil {
			if errors.Is(e, dbport.ErrNoRows) {
				return nil
			}
			return e
		}
		var state chatgate.State
		if e := json.Unmarshal(payload, &state); e != nil {
			return e
		}
		inForce = state.Gate.Current != "" && state.Gate.State != "paused" && state.Gate.State != "retired"
		return nil
	})
	return inForce, err
}

// GateSummary is what a person is told about a gate before they open it: how
// many questions joining takes and what the gate and the channel are for, and,
// for a member, whether the questions changed since they answered.
type GateSummary struct {
	Conversation string `json:"conversation"`
	Questions    int    `json:"questions"`
	// Purpose is the gate's own stated purpose; ChannelPurpose the channel's.
	Purpose        string `json:"purpose,omitempty"`
	ChannelPurpose string `json:"channel_purpose,omitempty"`
	// Mode is how answers admit: "automatic", "rule" or "review".
	Mode string `json:"mode"`
	// Member is true for a current member. AnswerAgain is true for a member
	// whose accepted answers are for an earlier major version; AnswerBy is the
	// date the current version gives them.
	Member      bool      `json:"member,omitempty"`
	AnswerAgain bool      `json:"answer_again,omitempty"`
	AnswerBy    time.Time `json:"answer_by,omitempty"`
}

// GateSummaries lists the gates in force that one person may know of: those of
// public channels, which anyone in the workspace finds in Browse, and those of
// the channels the person is in. It reads the questions' count and the stated
// purposes and nothing of anybody's answers.
func (r *GateRepository) GateSummaries(ctx context.Context, tenant, person string) ([]GateSummary, error) {
	if r == nil || r.Store == nil || tenant == "" || person == "" {
		return nil, chatgate.ErrInvalid
	}
	out := []GateSummary{}
	err := r.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT g.conversation_id,g.state_json,COALESCE(w.payload_json,'{}'::jsonb),m.member_id IS NOT NULL FROM chat_gate_state g JOIN chat_conversation c ON c.tenant_id=g.tenant_id AND c.id=g.conversation_id LEFT JOIN chat_channel_widget w ON w.tenant_id=g.tenant_id AND w.conversation_id=g.conversation_id AND w.kind='TEAM' LEFT JOIN chat_membership m ON m.tenant_id=g.tenant_id AND m.conversation_id=g.conversation_id AND m.home_tenant_id=g.tenant_id AND m.member_id=$2 AND m.state='active' WHERE g.tenant_id=$1 AND c.lifecycle<>'ARCHIVED' AND c.kind IN ('PUBLIC_CHANNEL','PRIVATE_CHANNEL') AND (c.kind='PUBLIC_CHANNEL' OR m.member_id IS NOT NULL) ORDER BY g.conversation_id LIMIT 500`, tenant, person)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var summary GateSummary
			var state, widget []byte
			if e = rows.Scan(&summary.Conversation, &state, &widget, &summary.Member); e != nil {
				return e
			}
			var gate chatgate.State
			if json.Unmarshal(state, &gate) != nil {
				continue
			}
			if gate.Gate.Current == "" || gate.Gate.State == "paused" || gate.Gate.State == "retired" {
				continue
			}
			live, e := chatgate.ParseVersion(gate.Gate.Current)
			if e != nil {
				continue
			}
			for _, d := range gate.Gate.Versions {
				if d.Version.String() == gate.Gate.Current {
					summary.Questions, summary.Purpose, summary.Mode, summary.AnswerBy = len(d.Fields), d.Purpose, d.Mode, d.AnswerBy
				}
			}
			var team teamPayload
			if json.Unmarshal(widget, &team) == nil {
				summary.ChannelPurpose = team.Purpose
			}
			if summary.Member {
				// A member answers again when what admitted them, their answers or
				// an administrator's override, is for an earlier major version.
				current := false
				for _, sub := range gate.Submissions {
					accepted, e := chatgate.ParseVersion(sub.Version)
					current = current || (sub.Person == person && sub.Status == "admitted" && e == nil && accepted.Major == live.Major)
				}
				for _, override := range gate.Overrides {
					current = current || (override.Person == person && override.Version == gate.Gate.Current)
				}
				summary.AnswerAgain = !current && live.Major > 1
			}
			if !summary.AnswerAgain {
				summary.AnswerBy = time.Time{}
			}
			out = append(out, summary)
		}
		return rows.Err()
	})
	return out, err
}

// GatePurpose is the purpose a channel's managers wrote for it (the Team
// widget's purpose), "" when they wrote none. The caller decides who may read
// it; nothing else of the channel is read.
func (r *GateRepository) GatePurpose(ctx context.Context, scope chatgate.Scope) (string, error) {
	if r == nil || r.Store == nil || scope.Tenant == "" || scope.Conversation == "" {
		return "", chatgate.ErrInvalid
	}
	purpose := ""
	err := r.Store.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		var payload []byte
		if e := tx.QueryRow(ctx, `SELECT payload_json FROM chat_channel_widget WHERE tenant_id=$1 AND conversation_id=$2 AND kind='TEAM'`, scope.Tenant, scope.Conversation).Scan(&payload); e != nil {
			if errors.Is(e, dbport.ErrNoRows) {
				return nil
			}
			return e
		}
		var team teamPayload
		if e := json.Unmarshal(payload, &team); e != nil {
			return e
		}
		purpose = team.Purpose
		return nil
	})
	return purpose, err
}

// GateMembership shares the existing membership event and audience transaction.
func (s *Adapter) GateMembership(ctx context.Context, tx dbport.Tx, scope chatgate.Scope, actor chatgate.Actor, _ string, admit bool) error {
	if scope.Tenant != actor.Tenant || actor.Person == "" {
		return chatgate.ErrDenied
	}
	if err := fenceContextWrite(ctx, tx, scope.Tenant, scope.Conversation); err != nil {
		return err
	}
	principal := chat.Principal{TenantID: actor.Tenant, SubjectID: actor.Person}
	if admit {
		_, err := putMembershipTx(ctx, tx, principal, chat.Membership{TenantID: scope.Tenant, ConversationID: scope.Conversation, HomeTenantID: actor.Tenant, SubjectID: actor.Person, Role: chat.Member, HistoryVisibility: chat.FullHistory})
		return err
	}
	var revision uint64
	if err := tx.QueryRow(ctx, `SELECT revision FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' AND left_at IS NULL FOR UPDATE`, scope.Tenant, scope.Conversation, actor.Tenant, actor.Person).Scan(&revision); errors.Is(err, dbport.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	_, err := removeMembershipTx(ctx, tx, principal, scope.Tenant, scope.Conversation, actor.Tenant, actor.Person, revision)
	return err
}
