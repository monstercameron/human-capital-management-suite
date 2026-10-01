package chatstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// SealedBackgroundPersonaAuthority is resolved by the current admission owner,
// never from a chat request principal or caller-selected recipient/destination.
type SealedBackgroundPersonaAuthority struct {
	Worker workload.Identity
	Source chat.BackgroundThreadSnapshot
}
type SealedBackgroundPersonaAuthorizer interface {
	AuthorizeSealedBackgroundPersonaReply(context.Context, agentsecurity.FinalOutputPersistence) (SealedBackgroundPersonaAuthority, error)
}
type SealedBackgroundPersonaRenderer interface {
	RenderSealedBackgroundPersonaReply(agentsecurity.FinalOutputPersistence) (string, error)
}
type SealedBackgroundPersonaDelivery struct {
	store    *Store
	owner    SealedBackgroundPersonaAuthorizer
	renderer SealedBackgroundPersonaRenderer
	now      func() time.Time
}

type sealedBackgroundTransactionKey struct{}
type sealedBackgroundTransaction struct {
	store    *Store
	tenantID string
	tx       dbport.Tx
}

// Only the native commit can bind this private key. Chat owner reads reuse its
// transaction so reauthorization cannot deadlock against its own source fence.
func sealedBackgroundTxFromContext(ctx context.Context, store *Store, tenantID string) (dbport.Tx, bool) {
	bound, ok := ctx.Value(sealedBackgroundTransactionKey{}).(sealedBackgroundTransaction)
	return bound.tx, ok && bound.tx != nil && bound.store == store && bound.tenantID == tenantID
}

func NewSealedBackgroundPersonaDelivery(store *Store, owner SealedBackgroundPersonaAuthorizer, renderer SealedBackgroundPersonaRenderer, now func() time.Time) (*SealedBackgroundPersonaDelivery, error) {
	if store == nil || owner == nil || renderer == nil || now == nil {
		return nil, chat.ErrUnavailable
	}
	return &SealedBackgroundPersonaDelivery{store: store, owner: owner, renderer: renderer, now: now}, nil
}

// CommitSealedBackgroundPersonaReply atomically creates the exact persona DM
// copy and recipient-only envelope. Background output never enters the source
// conversation's history, search, unread counts, or public outbox.
func (s *SealedBackgroundPersonaDelivery) CommitSealedBackgroundPersonaReply(ctx context.Context, output agentsecurity.FinalOutputPersistence) (chat.EphemeralPost, error) {
	if s == nil || ctx == nil || s.store == nil || s.owner == nil || s.renderer == nil || s.now == nil {
		return chat.EphemeralPost{}, chat.ErrUnavailable
	}
	i := output.Identity()
	if _, _, err := output.Payload(); err != nil || output.Digest() == "" {
		return chat.EphemeralPost{}, chat.ErrPermissionDenied
	}
	authority, err := s.owner.AuthorizeSealedBackgroundPersonaReply(ctx, output)
	if err != nil || !sealedBackgroundAuthorityMatches(authority, i, s.now().UTC()) {
		return chat.EphemeralPost{}, chat.ErrPermissionDenied
	}
	body, err := s.renderer.RenderSealedBackgroundPersonaReply(output)
	if err != nil || strings.TrimSpace(body) == "" || len(body) > 4000 {
		return chat.EphemeralPost{}, chat.ErrInvalidArgument
	}
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return chat.EphemeralPost{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, i.TenantID); err != nil {
		return chat.EphemeralPost{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "persona-background:"+i.TenantID+":"+i.ConversationID); err != nil {
		return chat.EphemeralPost{}, err
	}
	// Hold source authority while the current admission owner rechecks core
	// delegation and invoker state. Shared reads permit the owner's source read.
	current, err := readBackgroundThreadSnapshot(ctx, tx, chat.BackgroundThreadSnapshotRequest{TenantID: i.TenantID, ReaderID: i.InvokerID, ConversationID: i.ConversationID, ThreadID: i.ThreadID, InvokingPostID: i.PostID, Limit: chat.MaxThreadSnapshotPosts})
	if err != nil || !reflect.DeepEqual(current, authority.Source) {
		return chat.EphemeralPost{}, chat.ErrAudienceChanged
	}
	if err = lockEphemeralRecipient(ctx, tx, i.TenantID, i.ConversationID, i.TenantID, i.InvokerID); err != nil {
		return chat.EphemeralPost{}, err
	}
	ownerContext := context.WithValue(ctx, sealedBackgroundTransactionKey{}, sealedBackgroundTransaction{store: s.store, tenantID: i.TenantID, tx: tx})
	checked, err := s.owner.AuthorizeSealedBackgroundPersonaReply(ownerContext, output)
	if err != nil || !sealedBackgroundAuthorityMatches(checked, i, s.now().UTC()) || !reflect.DeepEqual(checked.Source, current) {
		return chat.EphemeralPost{}, chat.ErrPermissionDenied
	}
	if err = fenceContextWrite(ctx, tx, i.TenantID, i.ConversationID); err != nil {
		return chat.EphemeralPost{}, err
	}
	var lifecycle, kind string
	if err = tx.QueryRow(ctx, `SELECT lifecycle,kind FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, i.TenantID, i.ConversationID).Scan(&lifecycle, &kind); err != nil {
		return chat.EphemeralPost{}, err
	}
	if lifecycle != "ACTIVE" || (kind != "DIRECT" && kind != "GROUP" && kind != "PRIVATE_CHANNEL" && kind != "PUBLIC_CHANNEL") {
		return chat.EphemeralPost{}, chat.ErrPermissionDenied
	}
	eid := sealedBackgroundID("ephemeral", i.TenantID, i.OutputID)
	dm, err := resolveSealedBackgroundDM(ctx, tx, i)
	if err != nil {
		return chat.EphemeralPost{}, err
	}
	if existing, e := readEphemeralByID(ctx, tx, i.TenantID, i.ConversationID, eid); e == nil {
		var digest string
		if e = tx.QueryRow(ctx, `SELECT fingerprint FROM chat_idempotency WHERE tenant_id=$1 AND conversation_id=$2 AND client_key=$3 AND post_id=$4`, i.TenantID, dm, "persona-output:"+i.OutputID, existing.post.DurableCopyPostID).Scan(&digest); e != nil {
			return chat.EphemeralPost{}, e
		}
		if existing.post.Body != body || existing.post.RecipientSubjectID != i.InvokerID || existing.post.ThreadID != i.ThreadID || existing.post.DurableCopyConversationID != dm || digest != output.Digest() {
			return chat.EphemeralPost{}, chat.ErrConflict
		}
		return existing.post, tx.Commit(ctx)
	} else if !errors.Is(e, dbport.ErrNoRows) {
		return chat.EphemeralPost{}, e
	}
	// A source route lease cannot authorize a different destination. Existing
	// placed DMs require their own routed composition and fail closed here.
	if err = routeFence(ctx, tx, i.TenantID, dm, 0, ""); err != nil {
		return chat.EphemeralPost{}, err
	}
	post, err := insertSealedBackgroundDMPost(ctx, tx, i, dm, body, output.Digest())
	if err != nil {
		return chat.EphemeralPost{}, err
	}
	now := s.now().UTC()
	p := chat.EphemeralPost{ID: eid, TenantID: i.TenantID, ConversationID: i.ConversationID, ThreadID: i.ThreadID, RecipientHomeTenantID: i.TenantID, RecipientSubjectID: i.InvokerID, Body: body, OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(chat.EphemeralLifetime), DurableCopyConversationID: dm, DurableCopyPostID: post.ID, ThreadLink: "/chat/" + url.PathEscape(i.ConversationID) + "?thread=" + url.QueryEscape(i.ThreadID)}
	fp, err := ephemeralFingerprint(p)
	if err != nil {
		return chat.EphemeralPost{}, err
	}
	var offset int64
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('chat_outbox','id'))`).Scan(&offset); err != nil {
		return chat.EphemeralPost{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_ephemeral_post(id,tenant_id,conversation_id,stream_offset,thread_id,recipient_home_tenant_id,recipient_subject_id,body,only_visible_to_you,created_at,expires_at,durable_copy_conversation_id,durable_copy_post_id,thread_link,fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7,$8,true,$9,$10,$11,$12,$13,$14)`, p.ID, p.TenantID, p.ConversationID, offset, p.ThreadID, p.RecipientHomeTenantID, p.RecipientSubjectID, p.Body, p.CreatedAt, p.ExpiresAt, p.DurableCopyConversationID, p.DurableCopyPostID, p.ThreadLink, fp)
	if err != nil {
		return chat.EphemeralPost{}, err
	}
	p.Sequence = uint64(offset)
	return p, tx.Commit(ctx)
}

func sealedBackgroundAuthorityMatches(a SealedBackgroundPersonaAuthority, i agentsecurity.FinalOutputIdentity, now time.Time) bool {
	s := a.Source
	digest, err := chat.BackgroundThreadSnapshotDigest(s)
	return a.Worker.Role() == workload.RoleWorker && a.Worker.Fingerprint() != "" && a.Worker.ValidAt(now) && err == nil && digest == s.Digest && s.SnapshotID == "chat-background-thread-"+digest && s.TenantID == i.TenantID && s.ReaderTenantID == i.TenantID && s.ReaderID == i.InvokerID && s.ConversationID == i.ConversationID && s.ThreadID == i.ThreadID && s.InvokingPostID == i.PostID
}

func resolveSealedBackgroundDM(ctx context.Context, tx dbport.Tx, i agentsecurity.FinalOutputIdentity) (string, error) {
	dm, err := chat.DirectPairConversationID(i.TenantID, []chat.MemberRef{{TenantID: i.TenantID, SubjectID: i.InvokerID}, {TenantID: i.TenantID, SubjectID: i.PersonaID}})
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,owner_id,lifecycle) VALUES($1,$2,'DIRECT',$3,'ACTIVE') ON CONFLICT(id) DO NOTHING`, dm, i.TenantID, i.InvokerID)
	if err != nil {
		return "", err
	}
	var tenantID, kind, lifecycle string
	if err = tx.QueryRow(ctx, `SELECT tenant_id,kind,lifecycle FROM chat_conversation WHERE id=$1 FOR UPDATE`, dm).Scan(&tenantID, &kind, &lifecycle); err != nil {
		return "", err
	}
	if tenantID != i.TenantID || kind != "DIRECT" || lifecycle != "ACTIVE" {
		return "", chat.ErrPermissionDenied
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state) VALUES($1,$2,$1,$3,'manager','active'),($1,$2,$1,$4,'member','active') ON CONFLICT DO NOTHING`, i.TenantID, dm, i.InvokerID, i.PersonaID)
	if err != nil {
		return "", err
	}
	rows, err := tx.Query(ctx, `SELECT home_tenant_id,member_id,state FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 FOR UPDATE`, i.TenantID, dm)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var home, id, state string
		if err = rows.Scan(&home, &id, &state); err != nil {
			return "", err
		}
		if home != i.TenantID || (id != i.InvokerID && id != i.PersonaID) || state != "active" {
			return "", chat.ErrPermissionDenied
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if count != 2 {
		return "", chat.ErrPermissionDenied
	}
	return dm, nil
}

func insertSealedBackgroundDMPost(ctx context.Context, tx dbport.Tx, i agentsecurity.FinalOutputIdentity, dm, body, digest string) (chat.Post, error) {
	key := "persona-output:" + i.OutputID
	var seq, event, policy int64
	if err := tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1,post_sequence=post_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING event_sequence,settings_revision,post_sequence`, i.TenantID, dm).Scan(&event, &policy, &seq); err != nil {
		return chat.Post{}, err
	}
	p := chat.Post{ID: uuid.NewString(), TenantID: i.TenantID, ConversationID: dm, AuthorID: i.PersonaID, AuthorHomeTenantID: i.TenantID, Body: body, Sequence: uint64(seq), Revision: 1}
	if err := tx.QueryRow(ctx, `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,client_key) VALUES($1,$2,$3,$4,$2,$5,$6,$7) RETURNING created_at`, p.ID, i.TenantID, dm, i.PersonaID, seq, body, key).Scan(&p.CreatedAt); err != nil {
		return chat.Post{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body) VALUES($1,$2,1,$3,$4)`, i.TenantID, p.ID, i.PersonaID, body); err != nil {
		return chat.Post{}, err
	}
	if err := writeOutbox(ctx, tx, outboxWrite{TenantID: i.TenantID, ConversationID: dm, AggregateID: p.ID, EventType: "post.created", ActorHomeTenantID: i.TenantID, ActorID: i.PersonaID, TargetID: p.ID, RecordID: "post:" + p.ID, RecordKind: "POST", SourceID: p.ID, Revision: 1, PolicyRevision: policy, EventSequence: event, CorrelationID: key, Value: p}); err != nil {
		return chat.Post{}, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO chat_idempotency(tenant_id,conversation_id,client_key,fingerprint,post_id,sequence) VALUES($1,$2,$3,$4,$5,$6)`, i.TenantID, dm, key, digest, p.ID, seq)
	return p, err
}

func sealedBackgroundID(kind, tenant, output string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + tenant + "\x00" + output))
	return kind + "-" + hex.EncodeToString(sum[:])
}
