// Package chatauthority persists conversation policy and bilateral company consent
// in the independent chat database.
package chatauthority

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrDenied = errors.New("chat authority: denied")
var ErrConflict = errors.New("chat authority: revision conflict")

type Transactor interface {
	RunTx(context.Context, func(dbport.Tx) error) error
}
type Store struct{ db Transactor }

func New(db Transactor) *Store { return &Store{db: db} }

func scope(ctx context.Context, tx dbport.Tx, tenant string) error {
	if tenant == "" {
		return ErrDenied
	}
	_, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant)
	return err
}

// outboxPayload is the chat_outbox JSON body for authority events (grant and
// policy changes). The conversation_id/event_sequence/schema_version/
// event_type keys exist so internal/data/chatstore's outbox watch path
// (contracts_adapter.go Watch/watchPage, which filters on the conversation
// id inside the payload) can see these events at all; before this fix the
// payload carried no conversation identifier so grant/policy events were
// invisible to watchers.
//
// TODO(chatauthority): internal/data/chatstore currently writes its own
// outbox payloads with PascalCase keys (see contracts_adapter.go's
// payload["ConversationID"] and the watch query on payload->>'ConversationID'),
// not these snake_case ones. Until the chatstore lane exports typed outbox
// key constants (or a single shared outbox writer) that both sides adopt,
// this package's events will NOT be matched by that watch query. Switch to
// the shared constants/writer as soon as they land; do not hand-roll a
// second key convention here permanently.
type outboxPayload struct {
	ConversationID string `json:"conversation_id"`
	EventType      string `json:"event_type"`
	SchemaVersion  int    `json:"schema_version"`
	EventSequence  int64  `json:"event_sequence"`
	GrantID        string `json:"grant_id"`
	ConsumerTenant string `json:"consumer_tenant"`
	ActorTenant    string `json:"actor_tenant"`
	Actor          string `json:"actor"`
}

func appendEvent(ctx context.Context, tx dbport.Tx, host, conversation, eventType, grantID, consumer, actorTenant, actor string) error {
	// chat_outbox forbids UPDATE/DELETE (chat_forbid_mutation), so the row's
	// own id cannot be learned via INSERT...RETURNING and then folded back
	// into the payload with a second statement. Reserve the id up front from
	// its own sequence and insert it explicitly instead.
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('chat_outbox','id'))`).Scan(&sequence); err != nil {
		return err
	}
	payload, err := json.Marshal(outboxPayload{
		ConversationID: conversation,
		EventType:      eventType,
		SchemaVersion:  1,
		EventSequence:  sequence,
		GrantID:        grantID,
		ConsumerTenant: consumer,
		ActorTenant:    actorTenant,
		Actor:          actor,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_outbox (id,tenant_id,aggregate_id,event_type,payload) VALUES ($1,$2,$3,$4,$5::jsonb)`, sequence, host, conversation, eventType, string(payload))
	return err
}

// PutPolicy changes the policy only when its expected revision matches.
func (s *Store) PutPolicy(ctx context.Context, host, conversation string, requiredRoles, qualifications, allowedPrincipals, allowedTenants []string, mode chatpolicy.RoleMode, classification, residency string, expected uint64, actor string) error {
	if s == nil || s.db == nil || host == "" || conversation == "" || actor == "" || (mode != chatpolicy.RolesAny && mode != chatpolicy.RolesAll) {
		return ErrDenied
	}
	if requiredRoles == nil {
		requiredRoles = []string{}
	}
	if qualifications == nil {
		qualifications = []string{}
	}
	if allowedPrincipals == nil {
		allowedPrincipals = []string{}
	}
	if allowedTenants == nil {
		allowedTenants = []string{}
	}
	return s.db.RunTx(ctx, func(tx dbport.Tx) error {
		if err := scope(ctx, tx, host); err != nil {
			return err
		}
		if expected == 0 {
			n, err := tx.Exec(ctx, `INSERT INTO chat_channel_policy (tenant_id,conversation_id,revision,required_roles,role_mode,required_qualifications,allowed_principals,allowed_tenants,classification,residency) VALUES ($1,$2,1,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, host, conversation, requiredRoles, int(mode), qualifications, allowedPrincipals, allowedTenants, classification, residency)
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrConflict
			}
			return appendEvent(ctx, tx, host, conversation, "chat.policy.created", "", "", host, actor)
		}
		n, err := tx.Exec(ctx, `UPDATE chat_channel_policy SET revision=revision+1,required_roles=$3,role_mode=$4,required_qualifications=$5,allowed_principals=$6,allowed_tenants=$7,classification=$8,residency=$9 WHERE tenant_id=$1 AND conversation_id=$2 AND revision=$10`, host, conversation, requiredRoles, int(mode), qualifications, allowedPrincipals, allowedTenants, classification, residency, expected)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		return appendEvent(ctx, tx, host, conversation, "chat.policy.updated", "", "", host, actor)
	})
}

func (s *Store) Policy(ctx context.Context, host, conversation string) (chatpolicy.Channel, error) {
	var c chatpolicy.Channel
	var mode int
	if s == nil || s.db == nil {
		return c, ErrDenied
	}
	err := s.db.RunTx(ctx, func(tx dbport.Tx) error {
		if err := scope(ctx, tx, host); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT revision,required_roles,role_mode,required_qualifications,allowed_principals,allowed_tenants,classification,residency FROM chat_channel_policy WHERE tenant_id=$1 AND conversation_id=$2`, host, conversation).Scan(&c.Revision, &c.RequiredRoles, &mode, &c.RequiredQualifications, &c.AllowedPrincipals, &c.AllowedTenants, &c.Classification, &c.Residency)
	})
	if err != nil {
		return chatpolicy.Channel{}, err
	}
	c.ID = conversation
	c.HostTenant = host
	c.RoleMode = chatpolicy.RoleMode(mode)
	return c, nil
}

type Grant struct {
	chatpolicy.Grant
	ProposedBy, AcceptedBy string
}

func (s *Store) Propose(ctx context.Context, actorTenant, actor string, grant chatpolicy.Grant) error {
	if s == nil || s.db == nil || actor == "" || actorTenant != grant.HostTenant || !grant.Proposed || !grant.AcceptedByHost || grant.AcceptedByConsumer || grant.HostTenant == grant.ConsumerTenant {
		return ErrDenied
	}
	return s.db.RunTx(ctx, func(tx dbport.Tx) error {
		if err := scope(ctx, tx, grant.HostTenant); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `INSERT INTO chat_share_grant (id,tenant_id,conversation_id,consumer_tenant,version,scope,classification,residency,proposed_by,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, grant.ID, grant.HostTenant, grant.ConversationID, grant.ConsumerTenant, grant.Version, grant.Scope, grant.Classification, grant.Residency, actor, grant.ExpiresAt)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		return appendEvent(ctx, tx, grant.HostTenant, grant.ConversationID, "chat.grant.proposed", grant.ID, grant.ConsumerTenant, grant.HostTenant, actor)
	})
}

func (s *Store) Accept(ctx context.Context, host, consumer, conversation, id, actor string, at time.Time) error {
	if s == nil || s.db == nil || consumer == "" || consumer == host || actor == "" || at.IsZero() {
		return ErrDenied
	}
	return s.db.RunTx(ctx, func(tx dbport.Tx) error {
		if err := scope(ctx, tx, host); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `UPDATE chat_share_grant SET accepted_by=$5,accepted_at=$6,version=version+1 WHERE tenant_id=$1 AND consumer_tenant=$2 AND conversation_id=$3 AND id=$4 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>$6`, host, consumer, conversation, id, actor, at)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrDenied
		}
		return appendEvent(ctx, tx, host, conversation, "chat.grant.accepted", id, consumer, consumer, actor)
	})
}

func (s *Store) Revoke(ctx context.Context, host, consumer, conversation, id, actorTenant, actor string, at time.Time) error {
	if s == nil || s.db == nil || actor == "" || at.IsZero() || (actorTenant != host && actorTenant != consumer) {
		return ErrDenied
	}
	return s.db.RunTx(ctx, func(tx dbport.Tx) error {
		if err := scope(ctx, tx, host); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, `UPDATE chat_share_grant SET revoked_at=$5,revoked_by=$6,version=version+1 WHERE tenant_id=$1 AND consumer_tenant=$2 AND conversation_id=$3 AND id=$4 AND revoked_at IS NULL`, host, consumer, conversation, id, at, actor)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrDenied
		}
		return appendEvent(ctx, tx, host, conversation, "chat.grant.revoked", id, consumer, actorTenant, actor)
	})
}

func (s *Store) CurrentGrant(ctx context.Context, host, consumer, conversation string, at time.Time) (chatpolicy.Grant, error) {
	var g chatpolicy.Grant
	if s == nil || s.db == nil || host == consumer || at.IsZero() {
		return g, ErrDenied
	}
	err := s.db.RunTx(ctx, func(tx dbport.Tx) error {
		if err := scope(ctx, tx, host); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT id,version,scope,classification,residency,expires_at FROM chat_share_grant WHERE tenant_id=$1 AND consumer_tenant=$2 AND conversation_id=$3 AND accepted_at IS NOT NULL AND revoked_at IS NULL AND expires_at>$4 ORDER BY version DESC LIMIT 1`, host, consumer, conversation, at).Scan(&g.ID, &g.Version, &g.Scope, &g.Classification, &g.Residency, &g.ExpiresAt)
	})
	if err != nil {
		return chatpolicy.Grant{}, err
	}
	g.ConversationID = conversation
	g.HostTenant = host
	g.ConsumerTenant = consumer
	g.Proposed = true
	g.AcceptedByHost = true
	g.AcceptedByConsumer = true
	return g, nil
}
