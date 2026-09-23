package chatstore

// This file is the narrow translation between the durable rows and the
// transport-neutral collaboration contract. Authorization remains in the
// collaboration service; every query still carries tenant context and RLS.
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Adapter exposes the collaboration contract without overloading the raw
// repository methods used by the chat outbox and migration code.
type Adapter struct{ *Store }

func NewAdapter(s *Store) *Adapter { return &Adapter{Store: s} }

var _ chat.Store = (*Adapter)(nil)

func (s *Adapter) CreateConversation(ctx context.Context, c chat.Conversation, members []chat.Membership, key string) (chat.Conversation, error) {
	if c.TenantID == "" || c.ID == "" || len(members) == 0 {
		return chat.Conversation{}, chat.ErrInvalidArgument
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return chat.Conversation{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, c.TenantID); err != nil {
		return chat.Conversation{}, err
	}
	identity := make([]string, 0, len(members))
	for _, m := range members {
		identity = append(identity, m.HomeTenantID+"\x00"+m.SubjectID+"\x00"+string(m.Role))
	}
	sort.Strings(identity)
	fpBytes, _ := json.Marshal(struct {
		Kind        chat.ConversationKind
		Name, Owner string
		Members     []string
	}{c.Kind, c.Name, c.OwnerID, identity})
	fp := fingerprint(string(fpBytes))
	if key != "" {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fingerprint(c.TenantID+"\x00"+c.OwnerID+"\x00"+key)); err != nil {
			return chat.Conversation{}, err
		}
		var id, storedFP string
		err = tx.QueryRow(ctx, `SELECT conversation_id,fingerprint FROM chat_conversation_idempotency WHERE tenant_id=$1 AND owner_id=$2 AND client_key=$3`, c.TenantID, c.OwnerID, key).Scan(&id, &storedFP)
		if err == nil {
			if storedFP != fp {
				return chat.Conversation{}, chat.ErrConflict
			}
			if err = tx.Commit(ctx); err != nil {
				return chat.Conversation{}, err
			}
			return s.GetConversation(ctx, c.TenantID, id)
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return chat.Conversation{}, err
		}
	}
	// Create is the one write that cannot take the route fence, because the row
	// it would lock does not exist yet. The lease instead records the placement
	// that every later write to this conversation must match. A conversation
	// created without a lease is recorded with no shard, has no entry in the core
	// route directory and is therefore unreachable through the routed service; it
	// is still fenced on route state.
	shard := ""
	epoch := uint64(1)
	if lease, ok := chatrouting.WriteLeaseFromContext(ctx); ok {
		if lease.Route.ConversationID != c.ID || lease.Route.HostTenantID != c.TenantID || lease.Route.ShardID == "" {
			return chat.Conversation{}, chatrouting.ErrStaleEpoch
		}
		shard = lease.Route.ShardID
		epoch = lease.Route.Epoch
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_conversation(id,tenant_id,kind,name,owner_id,settings_revision,lifecycle,route_shard,route_epoch) VALUES($1,$2,$3,$4,$5,$6,'ACTIVE',$7,$8)`, c.ID, c.TenantID, string(c.Kind), c.Name, c.OwnerID, c.Revision, shard, epoch)
	if err != nil {
		return chat.Conversation{}, err
	}
	for _, m := range members {
		if m.ConversationID != c.ID || m.TenantID != c.TenantID || m.HomeTenantID == "" || m.SubjectID == "" {
			return chat.Conversation{}, chat.ErrInvalidArgument
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state,history_visibility,revision) VALUES($1,$2,$3,$4,$5,'active',$6,1)`, c.TenantID, c.ID, m.HomeTenantID, m.SubjectID, string(m.Role), string(m.HistoryVisibility))
		if err != nil {
			return chat.Conversation{}, err
		}
	}
	if key != "" {
		_, err = tx.Exec(ctx, `INSERT INTO chat_conversation_idempotency(tenant_id,owner_id,client_key,conversation_id,fingerprint) VALUES($1,$2,$3,$4,$5)`, c.TenantID, c.OwnerID, key, c.ID, fp)
		if err != nil {
			return chat.Conversation{}, err
		}
	}
	eventCtx := context.WithValue(ctx, chatEventCorrelationKey{}, uuid.NewString())
	if err = emitAdapterEvent(eventCtx, tx, c.TenantID, c.ID, "conversation.created", c.TenantID, c.OwnerID, c.ID, c.Revision, c); err != nil {
		return chat.Conversation{}, err
	}
	for _, m := range members {
		if err = emitAdapterEvent(eventCtx, tx, c.TenantID, c.ID, "membership.added", c.TenantID, c.OwnerID, m.HomeTenantID+":"+m.SubjectID, m.Revision, m); err != nil {
			return chat.Conversation{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return chat.Conversation{}, err
	}
	return c, nil
}
func (s *Adapter) GetConversation(ctx context.Context, tenantID, id string) (chat.Conversation, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return chat.Conversation{}, e
	}
	defer tx.Rollback(ctx)
	if e = tenant(ctx, tx, tenantID); e != nil {
		return chat.Conversation{}, e
	}
	var c chat.Conversation
	var kind, life string
	var rev, members int64
	e = tx.QueryRow(ctx, getConversationRow, tenantID, id).Scan(&c.ID, &c.TenantID, &kind, &c.Name, &c.OwnerID, &rev, &life, &members, &c.LastActivityAt)
	if e != nil {
		if errors.Is(e, dbport.ErrNoRows) {
			return chat.Conversation{}, chat.ErrNotFound
		}
		return c, e
	}
	c.Kind = chat.ConversationKind(kind)
	c.Revision = uint64(rev)
	c.Archived = life != "ACTIVE"
	c.MemberCount = uint32(members)
	return c, tx.Commit(ctx)
}

// memberCountSubquery and lastActivitySubquery are the two derived columns every
// conversation read carries: how many people are in the room and when it last
// said anything. They are correlated subqueries against the conversation alias
// `c` rather than a second round trip per row, so a fifty-row channel directory
// is still one query. A tombstoned post is not activity, and a room with no
// surviving post yields NULL rather than a zero time.
const memberCountSubquery = `(SELECT count(*) FROM chat_membership mc WHERE mc.tenant_id=c.tenant_id AND mc.conversation_id=c.id AND mc.state='active')`

const lastActivitySubquery = `(SELECT max(lp.created_at) FROM chat_post lp WHERE lp.tenant_id=c.tenant_id AND lp.conversation_id=c.id AND lp.tombstoned=false)`

const getConversationRow = `SELECT c.id,c.tenant_id,c.kind,c.name,c.owner_id,c.settings_revision,c.lifecycle,` +
	memberCountSubquery + ` AS member_count,` + lastActivitySubquery + ` AS last_activity_at ` +
	`FROM chat_conversation c WHERE c.tenant_id=$1 AND c.id=$2`

// listConversationsJoined is the caller's own rooms: an inner join on an active
// membership. listConversationsDiscoverable widens it with the tenant's active
// public channels the caller has not joined, and reports which is which so the
// service can apply the discovery policy to the added rows only. Both stay inside
// the tenant transaction, so RLS still bounds the result.
const listConversationsJoined = `SELECT c.id,c.tenant_id,c.kind,c.name,c.owner_id,c.settings_revision,c.lifecycle,true,` +
	memberCountSubquery + ` AS member_count,` + lastActivitySubquery + ` AS last_activity_at ` +
	`FROM chat_conversation c JOIN chat_membership m ON m.tenant_id=c.tenant_id AND m.conversation_id=c.id WHERE c.tenant_id=$1 AND m.member_id=$2 AND m.home_tenant_id=$3 AND m.state='active' AND c.id>$4 ORDER BY c.id LIMIT $5`

const listConversationsDiscoverable = `SELECT c.id,c.tenant_id,c.kind,c.name,c.owner_id,c.settings_revision,c.lifecycle,(m.member_id IS NOT NULL) AS joined,` +
	memberCountSubquery + ` AS member_count,` + lastActivitySubquery + ` AS last_activity_at ` +
	`FROM chat_conversation c LEFT JOIN chat_membership m ON m.tenant_id=c.tenant_id AND m.conversation_id=c.id AND m.member_id=$2 AND m.home_tenant_id=$3 AND m.state='active' WHERE c.tenant_id=$1 AND c.id>$4 AND (m.member_id IS NOT NULL OR (c.kind='PUBLIC_CHANNEL' AND c.lifecycle='ACTIVE')) ORDER BY c.id LIMIT $5`

func (s *Adapter) ListConversations(ctx context.Context, principal chat.Principal, tenantID string, p chat.Page, scope chat.ConversationScope) (chat.ListConversationsResponse, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return chat.ListConversationsResponse{}, e
	}
	defer tx.Rollback(ctx)
	if e = tenant(ctx, tx, tenantID); e != nil {
		return chat.ListConversationsResponse{}, e
	}
	n := p.PageSize
	if n == 0 || n > 200 {
		n = 200
	}
	cursor, err := decodeCursor(p.Cursor)
	if err != nil {
		return chat.ListConversationsResponse{}, chat.ErrInvalidArgument
	}
	query := listConversationsJoined
	if scope.IncludeDiscoverable {
		query = listConversationsDiscoverable
	}
	rows, e := tx.Query(ctx, query, tenantID, principal.SubjectID, principal.TenantID, cursor, n+1)
	if e != nil {
		return chat.ListConversationsResponse{}, e
	}
	defer rows.Close()
	var out chat.ListConversationsResponse
	for rows.Next() {
		var c chat.Conversation
		var k, l string
		var r, members int64
		if e = rows.Scan(&c.ID, &c.TenantID, &k, &c.Name, &c.OwnerID, &r, &l, &c.Joined, &members, &c.LastActivityAt); e != nil {
			return out, e
		}
		c.Kind = chat.ConversationKind(k)
		c.Revision = uint64(r)
		c.Archived = l != "ACTIVE"
		c.MemberCount = uint32(members)
		out.Conversations = append(out.Conversations, c)
	}
	if len(out.Conversations) > int(n) {
		out.Conversations = out.Conversations[:n]
		out.NextCursor = encodeCursor(out.Conversations[len(out.Conversations)-1].ID)
	}
	return out, rows.Err()
}

func encodeCursor(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func decodeCursor(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	if len(b) > 1024 {
		return "", chat.ErrInvalidArgument
	}
	return string(b), nil
}
func (s *Adapter) UpdateConversation(ctx context.Context, c chat.Conversation, expected uint64) (chat.Conversation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, c.TenantID); err != nil {
		return c, err
	}
	if err = fenceContextWrite(ctx, tx, c.TenantID, c.ID); err != nil {
		return c, err
	}
	var rev int64
	var owner string
	err = tx.QueryRow(ctx, `UPDATE chat_conversation SET name=$1,settings_revision=settings_revision+1,lifecycle=$2 WHERE tenant_id=$3 AND id=$4 AND settings_revision=$5 RETURNING settings_revision,owner_id`, c.Name, map[bool]string{true: "ARCHIVED", false: "ACTIVE"}[c.Archived], c.TenantID, c.ID, expected).Scan(&rev, &owner)
	if errors.Is(err, dbport.ErrNoRows) {
		return c, chat.ErrConflict
	}
	if err != nil {
		return c, err
	}
	c.Revision = uint64(rev)
	c.OwnerID = owner
	if err = emitAdapterEvent(ctx, tx, c.TenantID, c.ID, "conversation.updated", c.TenantID, owner, c.ID, c.Revision, c); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (s *Adapter) GetMembership(ctx context.Context, t, cid, home, mid string) (chat.Membership, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return chat.Membership{}, e
	}
	defer tx.Rollback(ctx)
	if e = tenant(ctx, tx, t); e != nil {
		return chat.Membership{}, e
	}
	var m chat.Membership
	var role, hist string
	var rev int64
	var joined time.Time
	e = tx.QueryRow(ctx, `SELECT conversation_id,tenant_id,home_tenant_id,member_id,role,joined_at,left_at,history_visibility,revision FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4`, t, cid, home, mid).Scan(&m.ConversationID, &m.TenantID, &m.HomeTenantID, &m.SubjectID, &role, &joined, &m.LeftAt, &hist, &rev)
	if e != nil {
		return m, e
	}
	m.Role = chat.MembershipRole(role)
	m.JoinedAt = &joined
	m.HistoryVisibility = chat.ReadHistoryFrom(hist)
	m.Revision = uint64(rev)
	return m, tx.Commit(ctx)
}
func (s *Adapter) ListMemberships(ctx context.Context, t, cid string, p chat.Page) (chat.ListMembershipsResponse, error) {
	var out chat.ListMembershipsResponse
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return out, err
	}
	limit := int(p.PageSize)
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	home, subject, err := decodePair(p.Cursor)
	if err != nil {
		return out, chat.ErrInvalidArgument
	}
	rows, err := tx.Query(ctx, `SELECT tenant_id,conversation_id,home_tenant_id,member_id,role,joined_at,left_at,history_visibility,revision FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND (home_tenant_id,member_id)>($3,$4) ORDER BY home_tenant_id,member_id LIMIT $5`, t, cid, home, subject, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var m chat.Membership
		var role, hist string
		var joined time.Time
		var rev int64
		if err = rows.Scan(&m.TenantID, &m.ConversationID, &m.HomeTenantID, &m.SubjectID, &role, &joined, &m.LeftAt, &hist, &rev); err != nil {
			return out, err
		}
		m.JoinedAt = &joined
		m.Role = chat.MembershipRole(role)
		m.HistoryVisibility = chat.ReadHistoryFrom(hist)
		m.Revision = uint64(rev)
		out.Memberships = append(out.Memberships, m)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Memberships) > limit {
		out.Memberships = out.Memberships[:limit]
		last := out.Memberships[len(out.Memberships)-1]
		out.NextCursor = encodePair(last.HomeTenantID, last.SubjectID)
	}
	return out, tx.Commit(ctx)
}

func encodePair(a, b string) string {
	raw, _ := json.Marshal([2]string{a, b})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodePair(s string) (string, string, error) {
	if s == "" {
		return "", "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) > 2048 {
		return "", "", chat.ErrInvalidArgument
	}
	var pair [2]string
	if err = json.Unmarshal(raw, &pair); err != nil {
		return "", "", chat.ErrInvalidArgument
	}
	return pair[0], pair[1], nil
}

type chatEventCorrelationKey struct{}

func emitAdapterEvent(ctx context.Context, tx dbport.Tx, tenantID, conversationID, eventType, actorHome, actorID, targetID string, revision uint64, value any) error {
	var policyRevision, eventSequence int64
	if err := tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING settings_revision,event_sequence`, tenantID, conversationID).Scan(&policyRevision, &eventSequence); err != nil {
		return err
	}
	correlationID, _ := ctx.Value(chatEventCorrelationKey{}).(string)
	recordID, kind, sourceID := "", "", targetID
	switch eventType {
	case "conversation.created", "conversation.updated":
		recordID, kind = "conversation:"+conversationID, "CONVERSATION"
	case "membership.added", "membership.removed":
		recordID, kind = "membership:"+conversationID+":"+targetID, "MEMBERSHIP"
	case "reaction.added", "reaction.removed":
		recordID, kind = "reaction:"+conversationID+":"+targetID+":"+actorHome+":"+actorID, "REACTION"
	}
	auditRevision := revision
	if eventType == "pin.removed" {
		auditRevision++ // removal carries the prior pin revision
	}
	return writeOutbox(ctx, tx, outboxWrite{
		TenantID: tenantID, ConversationID: conversationID, AggregateID: conversationID, EventType: eventType,
		ActorHomeTenantID: actorHome, ActorID: actorID, TargetID: targetID,
		RecordID: recordID, RecordKind: kind, SourceID: sourceID,
		Revision: revision, AuditRevision: auditRevision, PolicyRevision: policyRevision,
		EventSequence: eventSequence, CorrelationID: correlationID, Value: value,
	})
}
func (s *Adapter) PutMembership(ctx context.Context, actor chat.Principal, m chat.Membership) (chat.Membership, error) {
	if m.TenantID == "" || m.HomeTenantID == "" || m.SubjectID == "" {
		return m, chat.ErrInvalidArgument
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return m, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, m.TenantID); err != nil {
		return m, err
	}
	if err = fenceContextWrite(ctx, tx, m.TenantID, m.ConversationID); err != nil {
		return m, err
	}
	var joined time.Time
	var rev int64
	err = tx.QueryRow(ctx, `INSERT INTO chat_membership(tenant_id,conversation_id,home_tenant_id,member_id,role,state,history_visibility,revision) VALUES($1,$2,$3,$4,$5,'active',$6,1) ON CONFLICT (tenant_id,conversation_id,home_tenant_id,member_id) DO UPDATE SET role=EXCLUDED.role,state='active',joined_at=CASE WHEN chat_membership.state='active' THEN chat_membership.joined_at ELSE now() END,left_at=NULL,history_visibility=EXCLUDED.history_visibility,revision=chat_membership.revision+1 RETURNING joined_at,revision`, m.TenantID, m.ConversationID, m.HomeTenantID, m.SubjectID, string(m.Role), string(m.HistoryVisibility)).Scan(&joined, &rev)
	if err != nil {
		return m, err
	}
	m.JoinedAt = &joined
	m.LeftAt = nil
	m.Revision = uint64(rev)
	if err = emitAdapterEvent(ctx, tx, m.TenantID, m.ConversationID, "membership.added", actor.TenantID, actor.SubjectID, m.HomeTenantID+":"+m.SubjectID, m.Revision, m); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}
func (s *Adapter) RemoveMembership(ctx context.Context, actor chat.Principal, t, cid, home, mid string, expected uint64) (chat.Membership, error) {
	var m chat.Membership
	m.TenantID = t
	m.HomeTenantID = home
	m.ConversationID = cid
	m.SubjectID = mid
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return m, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return m, err
	}
	if err = fenceContextWrite(ctx, tx, t, cid); err != nil {
		return m, err
	}
	var left time.Time
	var rev int64
	err = tx.QueryRow(ctx, `UPDATE chat_membership SET state='removed',left_at=now(),revision=revision+1 WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND revision=$5 AND state='active' RETURNING left_at,revision`, t, cid, home, mid, expected).Scan(&left, &rev)
	if errors.Is(err, dbport.ErrNoRows) {
		return m, chat.ErrConflict
	}
	if err != nil {
		return m, err
	}
	m.LeftAt = &left
	m.Revision = uint64(rev)
	if err = emitAdapterEvent(ctx, tx, t, cid, "membership.removed", actor.TenantID, actor.SubjectID, home+":"+mid, m.Revision, m); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}
func (s *Adapter) SendPost(ctx context.Context, r chat.SendPostRequest, p chat.Post) (chat.Post, error) {
	if r.Principal.SubjectID != "" && r.Principal.SubjectID != p.AuthorID {
		return chat.Post{}, chat.ErrPermissionDenied
	}
	if p.AuthorHomeTenantID != "" && r.Principal.TenantID != "" && p.AuthorHomeTenantID != r.Principal.TenantID {
		return chat.Post{}, chat.ErrPermissionDenied
	}
	refs := p.References
	if refs == nil {
		refs = r.References
	}
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return chat.Post{}, err
	}
	source := p.SourceAttribution
	if source == nil {
		source = r.SourceAttribution
	}
	var sourceJSON []byte
	if source != nil {
		sourceJSON, err = json.Marshal(source)
		if err != nil {
			return chat.Post{}, err
		}
	}
	parent := p.ParentID
	if parent == "" {
		parent = r.ParentID
	}
	// A caller that hands over a post with a timestamp is stating when it
	// happened; the live service leaves it zero and the column decides.
	raw := SendRequest{TenantID: r.TenantID, ConversationID: r.ConversationID, HomeTenantID: r.Principal.TenantID, AuthorID: p.AuthorID, ClientKey: r.IdempotencyKey, Body: p.Body, ParentID: parent, References: refsJSON, SourceAttribution: sourceJSON, CreatedAt: p.CreatedAt}
	raw.RouteEpoch, raw.ShardID = leaseFence(ctx)
	q, e := s.sendPostRaw(ctx, raw)
	if e != nil {
		return p, e
	}
	return chatPost(q)
}

func chatPost(q Post) (chat.Post, error) {
	p := chat.Post{ID: q.ID, TenantID: q.TenantID, ConversationID: q.ConversationID, AuthorID: q.AuthorID, AuthorHomeTenantID: q.AuthorHomeTenantID, Body: q.Body, Sequence: uint64(q.Sequence), Revision: uint64(q.Revision), ParentID: q.ParentID, Deleted: q.Tombstoned, CreatedAt: q.CreatedAt}
	if len(q.References) > 0 {
		if err := json.Unmarshal(q.References, &p.References); err != nil {
			return chat.Post{}, err
		}
	}
	if len(q.SourceAttribution) > 0 {
		var source chat.SourceAttribution
		if err := json.Unmarshal(q.SourceAttribution, &source); err != nil {
			return chat.Post{}, err
		}
		p.SourceAttribution = &source
	}
	return p, nil
}

// listPostsForward and listPostsBackward differ only in the sequence bound and
// the order. The membership join and the history-visibility predicate are
// identical, because paging direction must not be a way to read posts from before
// a member joined.
const listPostsSelect = `SELECT p.id,p.tenant_id,p.conversation_id,p.author_id,p.author_home_tenant_id,p.sequence,p.body,p.revision,p.tombstoned,p.created_at,p.parent_id,p.references_json,p.source_attribution FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))`

const listPostsForward = listPostsSelect + ` AND p.sequence>$5 ORDER BY p.sequence LIMIT $6`

const listPostsBackward = listPostsSelect + ` AND p.sequence<$5 ORDER BY p.sequence DESC LIMIT $6`

// Machine readers use the named installation as their row-level admission
// evidence. They see history only from installation time, and revocation or
// scope removal takes effect before each page is returned.
const listMachinePostsSelect = `SELECT p.id,p.tenant_id,p.conversation_id,p.author_id,p.author_home_tenant_id,p.sequence,p.body,p.revision,p.tombstoned,p.created_at,p.parent_id,p.references_json,p.source_attribution FROM chat_post p JOIN chat_app_installation i ON i.tenant_id=p.tenant_id AND i.conversation_id=p.conversation_id AND i.tenant_id=$3 AND i.app_id=$4 AND i.id=i.tenant_id||':'||i.conversation_id||':'||i.app_id AND i.status='ACTIVE' AND i.version>0 AND i.created_at<=now() AND 'chat.posts.read'=ANY(i.granted_scopes) WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.created_at>=i.created_at`

const listMachinePostsForward = listMachinePostsSelect + ` AND p.sequence>$5 ORDER BY p.sequence LIMIT $6`
const listMachinePostsBackward = listMachinePostsSelect + ` AND p.sequence<$5 ORDER BY p.sequence DESC LIMIT $6`

func (s *Adapter) ListPosts(ctx context.Context, principal chat.Principal, t, cid string, after uint64, p chat.Page, w chat.PostWindow) (chat.ListPostsResponse, error) {
	var out chat.ListPostsResponse
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return out, err
	}
	limit := int(p.PageSize)
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	// A backward page walks down from an exclusive upper edge. An unset edge means
	// "from the newest post", which is how a client opens a conversation at its
	// end without first paging through everything before it.
	// The bound is a bigint column, so the open edge is MaxInt64 rather than
	// MaxUint64: the larger value is not encodable as a parameter at all.
	before := w.BeforeSequence
	if w.Descending && (before == 0 || before > math.MaxInt64) {
		before = math.MaxInt64
	}
	if p.Cursor != "" {
		raw, decodeErr := decodeCursor(p.Cursor)
		if decodeErr != nil {
			return out, chat.ErrInvalidArgument
		}
		cursor, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil {
			return out, chat.ErrInvalidArgument
		}
		if w.Descending {
			// On a backward page the cursor is the next upper edge, and it only
			// ever moves further back.
			if cursor < before {
				before = cursor
			}
		} else if cursor > after {
			after = cursor
		}
	}
	query, bound := listPostsForward, after
	machine, identityErr := machineActor(ctx, principal.TenantID, principal.SubjectID)
	if identityErr != nil {
		return out, chat.ErrPermissionDenied
	}
	if machine {
		if principal.TenantID != t {
			return out, chat.ErrPermissionDenied
		}
		query = listMachinePostsForward
	}
	if w.Descending {
		query, bound = listPostsBackward, before
		if machine {
			query = listMachinePostsBackward
		}
	}
	rows, err := tx.Query(ctx, query, t, cid, principal.TenantID, principal.SubjectID, bound, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var x Post
		if err = rows.Scan(&x.ID, &x.TenantID, &x.ConversationID, &x.AuthorID, &x.AuthorHomeTenantID, &x.Sequence, &x.Body, &x.Revision, &x.Tombstoned, &x.CreatedAt, &x.ParentID, &x.References, &x.SourceAttribution); err != nil {
			return out, err
		}
		p, err := chatPost(x)
		if err != nil {
			return out, err
		}
		out.Posts = append(out.Posts, p)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Posts) > limit {
		out.Posts = out.Posts[:limit]
		out.NextCursor = encodeCursor(strconv.FormatUint(out.Posts[len(out.Posts)-1].Sequence, 10))
	}
	if w.Descending {
		// The page was read newest-first so the bound and the cursor are the
		// oldest row; it is handed back oldest-first so a caller renders it in
		// conversation order without reversing it itself.
		sort.Slice(out.Posts, func(i, j int) bool { return out.Posts[i].Sequence < out.Posts[j].Sequence })
	}
	return out, tx.Commit(ctx)
}

func (s *Adapter) GetPost(ctx context.Context, tenantID, conversationID, postID string) (chat.Post, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return chat.Post{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, tenantID); err != nil {
		return chat.Post{}, err
	}
	var x Post
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,revision,tombstoned,created_at,parent_id,references_json,source_attribution FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, tenantID, conversationID, postID).Scan(&x.ID, &x.TenantID, &x.ConversationID, &x.AuthorID, &x.AuthorHomeTenantID, &x.Sequence, &x.Body, &x.Revision, &x.Tombstoned, &x.CreatedAt, &x.ParentID, &x.References, &x.SourceAttribution)
	if err != nil {
		return chat.Post{}, err
	}
	p, err := chatPost(x)
	if err != nil {
		return chat.Post{}, err
	}
	return p, tx.Commit(ctx)
}
func (s *Adapter) EditPost(ctx context.Context, r chat.EditPostRequest) (chat.Post, error) {
	return s.revise(ctx, r.TenantID, r.ConversationID, r.PostID, r.Principal.TenantID, r.Principal.SubjectID, r.Body, false, r.ExpectedRevision)
}
func (s *Adapter) DeletePost(ctx context.Context, r chat.DeletePostRequest) (chat.Post, error) {
	return s.revise(ctx, r.TenantID, r.ConversationID, r.PostID, r.Principal.TenantID, r.Principal.SubjectID, "", true, r.ExpectedRevision)
}

// currentTombstone returns the caller's own post when it is already tombstoned.
// It is the read that tells a repeated delete apart from a stale revision, and
// it runs inside the caller's transaction so it sees the same snapshot the
// update did.
func currentTombstone(ctx context.Context, tx dbport.Tx, tenantID, conversationID, postID, home, author string) (chat.Post, error) {
	var x Post
	err := tx.QueryRow(ctx, `SELECT id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,revision,tombstoned,created_at,parent_id,references_json,source_attribution FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND author_home_tenant_id=$4 AND author_id=$5 AND tombstoned=true`, tenantID, conversationID, postID, home, author).Scan(&x.ID, &x.TenantID, &x.ConversationID, &x.AuthorID, &x.AuthorHomeTenantID, &x.Sequence, &x.Body, &x.Revision, &x.Tombstoned, &x.CreatedAt, &x.ParentID, &x.References, &x.SourceAttribution)
	if err != nil {
		return chat.Post{}, err
	}
	return chatPost(x)
}

func (s *Adapter) revise(ctx context.Context, t, cid, id, home, author, body string, deleted bool, expected uint64) (chat.Post, error) {
	var p chat.Post
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return p, err
	}
	// The revise path takes the same conversation row lock and the same route
	// fence as the send path, so an edit or tombstone cannot commit against a
	// conversation whose route is moving or whose shard the caller no longer holds.
	if err = fenceContextWrite(ctx, tx, t, cid); err != nil {
		return p, err
	}
	var x Post
	err = tx.QueryRow(ctx, `UPDATE chat_post SET body=$1,tombstoned=$2,revision=revision+1,updated_at=now() WHERE tenant_id=$3 AND conversation_id=$4 AND id=$5 AND author_home_tenant_id=$6 AND author_id=$7 AND revision=$8 AND tombstoned=false RETURNING id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,revision,tombstoned,created_at,parent_id,references_json,source_attribution`, body, deleted, t, cid, id, home, author, expected).Scan(&x.ID, &x.TenantID, &x.ConversationID, &x.AuthorID, &x.AuthorHomeTenantID, &x.Sequence, &x.Body, &x.Revision, &x.Tombstoned, &x.CreatedAt, &x.ParentID, &x.References, &x.SourceAttribution)
	if errors.Is(err, dbport.ErrNoRows) {
		// The update matched nothing for one of two very different reasons, and
		// collapsing both into a conflict is what made the live log unreadable:
		// a stale revision and a post that is already a tombstone answered
		// identically. A delete of an already-deleted post has already achieved
		// what it asked for, so it is idempotent — the existing tombstone comes
		// back with no new revision and no second event. An edit of a tombstoned
		// post, and any genuine revision mismatch, still conflict.
		if deleted {
			if current, lookupErr := currentTombstone(ctx, tx, t, cid, id, home, author); lookupErr == nil {
				return current, tx.Commit(ctx)
			}
		}
		return p, chat.ErrConflict
	}
	if err != nil {
		return p, err
	}
	p, err = chatPost(x)
	if err != nil {
		return p, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body,parent_id,references_json,source_attribution,tombstoned) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, t, id, x.Revision, author, body, x.ParentID, x.References, x.SourceAttribution, deleted)
	if err != nil {
		return p, err
	}
	var policyRevision, eventSequence int64
	if err = tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING settings_revision,event_sequence`, t, cid).Scan(&policyRevision, &eventSequence); err != nil {
		return p, err
	}
	event, kind := "post.edited", "EDIT"
	if deleted {
		event, kind = "post.deleted", "TOMBSTONE"
	}
	if err = writeOutbox(ctx, tx, outboxWrite{
		TenantID: t, ConversationID: cid, AggregateID: id, EventType: event,
		ActorHomeTenantID: home, ActorID: author, TargetID: id,
		RecordID: "post:" + id, RecordKind: kind, SourceID: id,
		Revision: p.Revision, PolicyRevision: policyRevision, EventSequence: eventSequence,
		Value: p,
	}); err != nil {
		return p, err
	}
	return p, tx.Commit(ctx)
}
func (s *Adapter) Search(ctx context.Context, r chat.SearchRequest) (chat.SearchResponse, error) {
	if strings.TrimSpace(r.Query) == "" || len(r.Query) > 512 || r.Principal.SubjectID == "" || r.Page.PageSize > 50 || len(r.Page.Cursor) > 4096 || len(r.ChannelCursor) > 4096 {
		return chat.SearchResponse{}, chat.ErrInvalidArgument
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return chat.SearchResponse{}, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, r.TenantID); err != nil {
		return chat.SearchResponse{}, err
	}
	limit := int(r.Page.PageSize)
	if limit <= 0 {
		limit = 20
	}
	messageBefore := time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	messageID := ""
	if !r.SkipMessages && r.Page.Cursor != "" {
		messageBefore, messageID, err = decodeSearchCursor(r.Page.Cursor)
		if err != nil {
			return chat.SearchResponse{}, chat.ErrInvalidArgument
		}
	}
	channelCursor := ""
	if !r.SkipChannels {
		channelCursor, err = decodeCursor(r.ChannelCursor)
		if err != nil {
			return chat.SearchResponse{}, chat.ErrInvalidArgument
		}
	}
	var out chat.SearchResponse
	if !r.SkipMessages {
		// The predicate repeats the GIN index expression exactly. Active
		// membership and history visibility filter before results or snippets.
		rows, err := tx.Query(ctx, `SELECT p.id,p.tenant_id,p.conversation_id,c.name,p.author_id,p.author_home_tenant_id,p.sequence,p.body,p.revision,p.tombstoned,p.created_at,p.parent_id,p.references_json,p.source_attribution FROM chat_post p JOIN chat_conversation c ON c.tenant_id=p.tenant_id AND c.id=p.conversation_id JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=$2 AND m.home_tenant_id=$3 AND m.state='active' WHERE p.tenant_id=$1 AND ($4='' OR p.conversation_id=$4) AND ($5='' OR p.author_id=$5) AND to_tsvector('simple', p.body) @@ plainto_tsquery('simple', $6) AND p.tombstoned=false AND c.lifecycle='ACTIVE' AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) AND (p.created_at,p.id)<($7,$8) ORDER BY p.created_at DESC,p.id DESC LIMIT $9`, r.TenantID, r.Principal.SubjectID, r.Principal.TenantID, r.ConversationID, r.AuthorID, r.Query, messageBefore, messageID, limit+1)
		if err != nil {
			return chat.SearchResponse{}, err
		}
		for rows.Next() {
			var x Post
			var conversationName string
			if err = rows.Scan(&x.ID, &x.TenantID, &x.ConversationID, &conversationName, &x.AuthorID, &x.AuthorHomeTenantID, &x.Sequence, &x.Body, &x.Revision, &x.Tombstoned, &x.CreatedAt, &x.ParentID, &x.References, &x.SourceAttribution); err != nil {
				rows.Close()
				return out, err
			}
			p, err := chatPost(x)
			if err != nil {
				rows.Close()
				return out, err
			}
			out.Results = append(out.Results, chat.SearchResult{Post: p, ConversationName: conversationName})
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
		if len(out.Results) > limit {
			out.Results = out.Results[:limit]
			out.NextCursor = encodeSearchCursor(out.Results[len(out.Results)-1].Post)
		}
	}
	if !r.SkipChannels {
		channelRows, err := tx.Query(ctx, `SELECT c.id,c.name,c.kind,(mine.member_id IS NOT NULL) FROM chat_conversation c LEFT JOIN chat_membership mine ON mine.tenant_id=c.tenant_id AND mine.conversation_id=c.id AND mine.member_id=$2 AND mine.home_tenant_id=$3 AND mine.state='active' WHERE c.tenant_id=$1 AND ($4='' OR c.id=$4) AND c.id>$5 AND c.lifecycle='ACTIVE' AND c.kind IN ('PUBLIC_CHANNEL','PRIVATE_CHANNEL') AND (mine.member_id IS NOT NULL OR c.kind='PUBLIC_CHANNEL') AND (to_tsvector('simple',c.name) @@ plainto_tsquery('simple',$6) OR lower(c.name) LIKE $7 ESCAPE E'\\') ORDER BY c.id LIMIT $8`, r.TenantID, r.Principal.SubjectID, r.Principal.TenantID, r.ConversationID, channelCursor, r.Query, escapeLikePrefix(r.Query), limit+1)
		if err != nil {
			return out, err
		}
		for channelRows.Next() {
			var hit chat.ChannelSearchResult
			var kind string
			if err = channelRows.Scan(&hit.ConversationID, &hit.Name, &kind, &hit.Joined); err != nil {
				channelRows.Close()
				return out, err
			}
			hit.Kind = chat.ConversationKind(kind)
			out.Channels = append(out.Channels, hit)
		}
		if err = channelRows.Err(); err != nil {
			channelRows.Close()
			return out, err
		}
		if len(out.Channels) > limit {
			out.Channels = out.Channels[:limit]
			out.ChannelNextCursor = encodeCursor(out.Channels[len(out.Channels)-1].ConversationID)
		}
		channelRows.Close()
	}

	return out, tx.Commit(ctx)
}

func escapeLikePrefix(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return strings.ToLower(value) + "%"
}

func encodeSearchCursor(p chat.Post) string {
	value := p.CreatedAt.UTC().Format(time.RFC3339Nano) + "\x00" + p.ID
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeSearchCursor(cursor string) (time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(b) > 2048 {
		return time.Time{}, "", chat.ErrInvalidArgument
	}
	parts := strings.SplitN(string(b), "\x00", 2)
	if len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", chat.ErrInvalidArgument
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", chat.ErrInvalidArgument
	}
	return at.UTC(), parts[1], nil
}

func (s *Adapter) GetReadState(ctx context.Context, t, cid, home, mid string) (chat.ReadState, error) {
	x := chat.ReadState{TenantID: t, ConversationID: cid, HomeTenantID: home, SubjectID: mid, Revision: 1}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return x, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return x, err
	}
	var active int
	if err = tx.QueryRow(ctx, `SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active'`, t, cid, home, mid).Scan(&active); errors.Is(err, dbport.ErrNoRows) {
		return x, chat.ErrPermissionDenied
	} else if err != nil {
		return x, err
	}
	var seq, rev int64
	err = tx.QueryRow(ctx, `SELECT last_sequence,revision FROM chat_cursor WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4`, t, cid, home, mid).Scan(&seq, &rev)
	if errors.Is(err, dbport.ErrNoRows) {
		return x, tx.Commit(ctx)
	}
	if err != nil {
		return x, err
	}
	x.LastReadSequence = uint64(seq)
	x.Revision = uint64(rev)
	return x, tx.Commit(ctx)
}
func (s *Adapter) PutReadState(ctx context.Context, x chat.ReadState, expected uint64) (chat.ReadState, error) {
	if expected == 0 || x.LastReadSequence > math.MaxInt64 {
		return x, chat.ErrInvalidArgument
	}
	if x.HomeTenantID == "" {
		x.HomeTenantID = x.TenantID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return x, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, x.TenantID); err != nil {
		return x, err
	}
	var maxVisible int64
	err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT max(p.sequence) FROM chat_post p WHERE p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))),0) FROM chat_membership m WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' FOR UPDATE OF m`, x.TenantID, x.ConversationID, x.HomeTenantID, x.SubjectID).Scan(&maxVisible)
	if errors.Is(err, dbport.ErrNoRows) {
		return x, chat.ErrPermissionDenied
	}
	if err != nil {
		return x, err
	}
	if x.LastReadSequence > uint64(maxVisible) {
		return x, chat.ErrInvalidArgument
	}
	var seq, rev int64
	err = tx.QueryRow(ctx, `UPDATE chat_cursor SET last_sequence=GREATEST(last_sequence,$1),revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND conversation_id=$3 AND home_tenant_id=$4 AND member_id=$5 AND revision=$6 RETURNING last_sequence,revision`, x.LastReadSequence, x.TenantID, x.ConversationID, x.HomeTenantID, x.SubjectID, expected).Scan(&seq, &rev)
	if errors.Is(err, dbport.ErrNoRows) && expected == 1 {
		err = tx.QueryRow(ctx, `INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence,revision) VALUES($1,$2,$3,$4,$5,2) ON CONFLICT DO NOTHING RETURNING last_sequence,revision`, x.TenantID, x.HomeTenantID, x.SubjectID, x.ConversationID, x.LastReadSequence).Scan(&seq, &rev)
	}
	if errors.Is(err, dbport.ErrNoRows) {
		return x, chat.ErrConflict
	}
	if err != nil {
		return x, err
	}
	x.LastReadSequence = uint64(seq)
	x.Revision = uint64(rev)
	return x, tx.Commit(ctx)
}
func (s *Adapter) GetPreferences(ctx context.Context, t, cid, home, mid string) (chat.NotificationPreferences, error) {
	x := chat.NotificationPreferences{TenantID: t, ConversationID: cid, HomeTenantID: home, SubjectID: mid, Revision: 1}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return x, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return x, err
	}
	var active int
	if err = tx.QueryRow(ctx, `SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active'`, t, cid, home, mid).Scan(&active); errors.Is(err, dbport.ErrNoRows) {
		return x, chat.ErrPermissionDenied
	} else if err != nil {
		return x, err
	}
	var b []byte
	var rev int64
	err = tx.QueryRow(ctx, `SELECT value,revision FROM chat_preference WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND marker='notification'`, t, cid, home, mid).Scan(&b, &rev)
	if errors.Is(err, dbport.ErrNoRows) {
		return x, tx.Commit(ctx)
	}
	if err != nil {
		return x, err
	}
	if err = json.Unmarshal(b, &x); err != nil {
		return x, err
	}
	x.Revision = uint64(rev)
	return x, tx.Commit(ctx)
}
func (s *Adapter) PutPreferences(ctx context.Context, x chat.NotificationPreferences, expected uint64) (chat.NotificationPreferences, error) {
	if expected == 0 {
		return x, chat.ErrInvalidArgument
	}
	if x.HomeTenantID == "" {
		x.HomeTenantID = x.TenantID
	}
	b, err := json.Marshal(struct {
		Muted        bool
		MentionsOnly bool
	}{x.Muted, x.MentionsOnly})
	if err != nil {
		return x, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return x, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, x.TenantID); err != nil {
		return x, err
	}
	var active int
	if err = tx.QueryRow(ctx, `SELECT 1 FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' FOR UPDATE`, x.TenantID, x.ConversationID, x.HomeTenantID, x.SubjectID).Scan(&active); errors.Is(err, dbport.ErrNoRows) {
		return x, chat.ErrPermissionDenied
	} else if err != nil {
		return x, err
	}
	var rev int64
	err = tx.QueryRow(ctx, `UPDATE chat_preference SET value=$1,revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND conversation_id=$3 AND home_tenant_id=$4 AND member_id=$5 AND marker='notification' AND revision=$6 RETURNING revision`, b, x.TenantID, x.ConversationID, x.HomeTenantID, x.SubjectID, expected).Scan(&rev)
	if errors.Is(err, dbport.ErrNoRows) && expected == 1 {
		err = tx.QueryRow(ctx, `INSERT INTO chat_preference(tenant_id,home_tenant_id,member_id,conversation_id,marker,value,revision) VALUES($1,$2,$3,$4,'notification',$5,2) ON CONFLICT DO NOTHING RETURNING revision`, x.TenantID, x.HomeTenantID, x.SubjectID, x.ConversationID, b).Scan(&rev)
	}
	if errors.Is(err, dbport.ErrNoRows) {
		return x, chat.ErrConflict
	}
	if err != nil {
		return x, err
	}
	x.Revision = uint64(rev)
	return x, tx.Commit(ctx)
}
func (s *Adapter) PutReaction(ctx context.Context, x chat.Reaction) (chat.Reaction, error) {
	if x.HomeTenantID == "" {
		x.HomeTenantID = x.TenantID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return x, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, x.TenantID); err != nil {
		return x, err
	}
	if err = fenceContextWrite(ctx, tx, x.TenantID, x.ConversationID); err != nil {
		return x, err
	}
	if err = lockVisibleReactionPost(ctx, tx, x.TenantID, x.ConversationID, x.PostID, x.HomeTenantID, x.SubjectID); err != nil {
		return x, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO chat_reaction(tenant_id,home_tenant_id,post_id,member_id,emoji) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING RETURNING created_at`, x.TenantID, x.HomeTenantID, x.PostID, x.SubjectID, x.Emoji).Scan(&x.CreatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT created_at FROM chat_reaction WHERE tenant_id=$1 AND home_tenant_id=$2 AND post_id=$3 AND member_id=$4 AND emoji=$5`, x.TenantID, x.HomeTenantID, x.PostID, x.SubjectID, x.Emoji).Scan(&x.CreatedAt)
		if err != nil {
			return x, err
		}
		return x, tx.Commit(ctx)
	}
	if err != nil {
		return x, err
	}
	if err = emitAdapterEvent(ctx, tx, x.TenantID, x.ConversationID, "reaction.added", x.HomeTenantID, x.SubjectID, x.PostID, 0, x); err != nil {
		return x, err
	}
	return x, tx.Commit(ctx)
}
func (s *Adapter) RemoveReaction(ctx context.Context, t, cid, p, home, member, e string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return err
	}
	if err = fenceContextWrite(ctx, tx, t, cid); err != nil {
		return err
	}
	if err = lockVisibleReactionPost(ctx, tx, t, cid, p, home, member); err != nil {
		return err
	}
	n, err := tx.Exec(ctx, `DELETE FROM chat_reaction x USING chat_post p WHERE x.tenant_id=$1 AND x.post_id=$2 AND x.home_tenant_id=$3 AND x.member_id=$4 AND x.emoji=$5 AND p.tenant_id=x.tenant_id AND p.id=x.post_id AND p.conversation_id=$6`, t, p, home, member, e, cid)
	if err != nil {
		return err
	}
	if n > 0 {
		value := chat.Reaction{TenantID: t, ConversationID: cid, PostID: p, HomeTenantID: home, SubjectID: member, Emoji: e}
		if err = emitAdapterEvent(ctx, tx, t, cid, "reaction.removed", home, member, p, 0, value); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Holding the active membership row until commit serializes reaction changes
// against revocation and enforces the same history window as ListPosts.
func lockVisibleReactionPost(ctx context.Context, tx dbport.Tx, t, cid, postID, home, subject string) error {
	var member string
	err := tx.QueryRow(ctx, `SELECT member_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' FOR UPDATE`, t, cid, home, subject).Scan(&member)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrPermissionDenied
	}
	if err != nil {
		return err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT p.id FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$4 AND m.member_id=$5 AND m.state='active' WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND p.tombstoned=false AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at))`, t, cid, postID, home, subject).Scan(&id)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrPermissionDenied
	}
	return err
}
func (s *Adapter) ListReactions(ctx context.Context, principal chat.Principal, t, cid, postID string, page chat.Page) (chat.ListReactionsResponse, error) {
	var out chat.ListReactionsResponse
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return out, err
	}
	limit := int(page.PageSize)
	if limit == 0 {
		limit = 200
	}
	if limit > 200 {
		return out, chat.ErrInvalidArgument
	}
	var cursor struct {
		Tenant, Conversation, Post, Home, Subject string
		Emoji, ReactionHome, ReactionSubject      string
	}
	if page.Cursor != "" {
		var raw string
		raw, err = decodeCursor(page.Cursor)
		if err != nil {
			return out, chat.ErrInvalidArgument
		}
		if json.Unmarshal([]byte(raw), &cursor) != nil || cursor.Tenant != t || cursor.Conversation != cid || cursor.Post != postID || cursor.Home != principal.TenantID || cursor.Subject != principal.SubjectID || cursor.Emoji == "" || cursor.ReactionHome == "" || cursor.ReactionSubject == "" {
			return out, chat.ErrInvalidArgument
		}
	}
	// The membership join enforces the same history window as ListPosts. A
	// tombstone or invisible post yields no reaction identities.
	rows, err := tx.Query(ctx, `SELECT r.home_tenant_id,r.member_id,r.emoji,r.created_at FROM chat_reaction r JOIN chat_post p ON p.tenant_id=r.tenant_id AND p.id=r.post_id AND p.conversation_id=$2 AND p.tombstoned=false JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.home_tenant_id=$4 AND m.member_id=$5 AND m.state='active' WHERE r.tenant_id=$1 AND r.post_id=$3 AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND p.created_at>=m.joined_at)) AND ($6='' OR (r.emoji,r.home_tenant_id,r.member_id)>($6,$7,$8)) ORDER BY r.emoji,r.home_tenant_id,r.member_id LIMIT $9`, t, cid, postID, principal.TenantID, principal.SubjectID, cursor.Emoji, cursor.ReactionHome, cursor.ReactionSubject, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		x := chat.Reaction{TenantID: t, ConversationID: cid, PostID: postID}
		if err = rows.Scan(&x.HomeTenantID, &x.SubjectID, &x.Emoji, &x.CreatedAt); err != nil {
			return out, err
		}
		out.Reactions = append(out.Reactions, x)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Reactions) > limit {
		out.Reactions = out.Reactions[:limit]
		cursor.Tenant, cursor.Conversation, cursor.Post, cursor.Home, cursor.Subject = t, cid, postID, principal.TenantID, principal.SubjectID
		last := out.Reactions[len(out.Reactions)-1]
		cursor.Emoji, cursor.ReactionHome, cursor.ReactionSubject = last.Emoji, last.HomeTenantID, last.SubjectID
		b, marshalErr := json.Marshal(cursor)
		if marshalErr != nil {
			return out, marshalErr
		}
		out.NextCursor = encodeCursor(string(b))
	}
	return out, tx.Commit(ctx)
}
func (s *Adapter) PutPin(ctx context.Context, x chat.Pin) (chat.Pin, error) {
	if x.PinnedByHomeTenantID == "" {
		x.PinnedByHomeTenantID = x.TenantID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return x, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, x.TenantID); err != nil {
		return x, err
	}
	if err = fenceContextWrite(ctx, tx, x.TenantID, x.ConversationID); err != nil {
		return x, err
	}
	var rev int64
	err = tx.QueryRow(ctx, `INSERT INTO chat_pin(tenant_id,home_tenant_id,conversation_id,post_id,member_id) SELECT $1,$2,$3,p.id,$5 FROM chat_post p WHERE p.tenant_id=$1 AND p.conversation_id=$3 AND p.id=$4 AND p.tombstoned=false ON CONFLICT (tenant_id,home_tenant_id,conversation_id,post_id,member_id) DO UPDATE SET revision=chat_pin.revision+1 RETURNING revision,created_at`, x.TenantID, x.PinnedByHomeTenantID, x.ConversationID, x.PostID, x.PinnedBy).Scan(&rev, &x.CreatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return x, chat.ErrNotFound
	}
	if err != nil {
		return x, err
	}
	x.Revision = uint64(rev)
	if err = emitAdapterEvent(ctx, tx, x.TenantID, x.ConversationID, "pin.added", x.PinnedByHomeTenantID, x.PinnedBy, x.PostID, x.Revision, x); err != nil {
		return x, err
	}
	return x, tx.Commit(ctx)
}
func (s *Adapter) RemovePin(ctx context.Context, actor chat.Principal, t, cid, p, home string, expected uint64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return err
	}
	if err = fenceContextWrite(ctx, tx, t, cid); err != nil {
		return err
	}
	var id string
	err = tx.QueryRow(ctx, `DELETE FROM chat_pin WHERE tenant_id=$1 AND home_tenant_id=$2 AND conversation_id=$3 AND post_id=$4 AND revision=$5 AND member_id=$6 RETURNING post_id`, t, home, cid, p, expected, actor.SubjectID).Scan(&id)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.ErrConflict
	}
	if err != nil {
		return err
	}
	if err = emitAdapterEvent(ctx, tx, t, cid, "pin.removed", actor.TenantID, actor.SubjectID, p, expected, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Adapter) ListPins(ctx context.Context, t, cid string) ([]chat.Pin, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, t); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT pin.conversation_id,pin.post_id,pin.tenant_id,pin.home_tenant_id,pin.member_id,pin.revision,pin.created_at FROM chat_pin pin JOIN chat_post post ON post.tenant_id=pin.tenant_id AND post.conversation_id=pin.conversation_id AND post.id=pin.post_id WHERE pin.tenant_id=$1 AND pin.conversation_id=$2 AND post.tombstoned=false ORDER BY pin.created_at DESC,pin.post_id DESC LIMIT 200`, t, cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chat.Pin
	for rows.Next() {
		var p chat.Pin
		var rev int64
		if err = rows.Scan(&p.ConversationID, &p.PostID, &p.TenantID, &p.PinnedByHomeTenantID, &p.PinnedBy, &rev, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Revision = uint64(rev)
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
func (s *Adapter) Watch(ctx context.Context, r chat.WatchConversationRequest) (<-chan chat.WatchEvent, error) {
	events, _, err := s.WatchWithErrors(ctx, r)
	return events, err
}

// WatchWithErrors is Watch plus the reason the stream ended. The chat.Store
// contract can only return the event channel, so a closed channel there is
// ambiguous: it means end of stream, caller cancellation or a failed poll.
// Callers that must distinguish a transient database blip from a clean end read
// the error channel, which carries at most one value and is closed with the
// event channel. A nil error never arrives; an empty closed channel means the
// stream ended because the caller's context did.
func (s *Adapter) WatchWithErrors(ctx context.Context, r chat.WatchConversationRequest) (<-chan chat.WatchEvent, <-chan error, error) {
	if r.TenantID == "" || r.ConversationID == "" || r.Principal.SubjectID == "" || r.Principal.TenantID == "" {
		return nil, nil, chat.ErrInvalidArgument
	}
	machine, identityErr := machineActor(ctx, r.Principal.TenantID, r.Principal.SubjectID)
	if identityErr != nil {
		return nil, nil, chat.ErrPermissionDenied
	}
	if machine {
		if _, err := s.MachineWatchEpoch(ctx, r.TenantID, r.ConversationID, r.Principal); err != nil {
			return nil, nil, err
		}
	} else {
		m, err := s.GetMembership(ctx, r.TenantID, r.ConversationID, r.Principal.TenantID, r.Principal.SubjectID)
		if err != nil || m.LeftAt != nil {
			return nil, nil, chat.ErrPermissionDenied
		}
	}
	after := int64(r.AfterSequence)
	if r.AfterSequence > uint64(^uint64(0)>>1) {
		return nil, nil, chat.ErrInvalidArgument
	}
	if r.ResumeCursor != "" {
		parsed, err := strconv.ParseInt(r.ResumeCursor, 10, 64)
		if err != nil || parsed < 0 {
			return nil, nil, chat.ErrInvalidArgument
		}
		after = parsed
	}
	events, errs := s.watchFrom(ctx, r, after)
	return events, errs, nil
}

// MachineWatchEpoch resolves the current installation revision without creating
// a human membership. The caller must carry the verified machine identity.
func (s *Adapter) MachineWatchEpoch(ctx context.Context, tenantID, conversationID string, principal chat.Principal) (uint64, error) {
	machine, err := machineActor(ctx, principal.TenantID, principal.SubjectID)
	if err != nil || !machine || tenantID != principal.TenantID || conversationID == "" {
		return 0, chat.ErrPermissionDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err := tenant(ctx, tx, tenantID); err != nil {
		return 0, err
	}
	var revision int64
	err = tx.QueryRow(ctx, `SELECT revision FROM chat_app_installation WHERE tenant_id=$1 AND conversation_id=$2 AND app_id=$3 AND id=tenant_id||':'||conversation_id||':'||app_id AND status='ACTIVE' AND version>0 AND created_at<=now() AND 'chat.posts.read'=ANY(granted_scopes)`, tenantID, conversationID, principal.SubjectID).Scan(&revision)
	if errors.Is(err, dbport.ErrNoRows) || revision <= 0 && err == nil {
		return 0, chat.ErrPermissionDenied
	}
	if err != nil {
		return 0, fmt.Errorf("machine watch installation: %w: %w", chat.ErrUnavailable, err)
	}
	return uint64(revision), tx.Commit(ctx)
}

func (s *Adapter) watchFrom(ctx context.Context, r chat.WatchConversationRequest, after int64) (<-chan chat.WatchEvent, <-chan error) {
	out := make(chan chat.WatchEvent, 32)
	// The error channel is buffered so the poll loop never blocks on a caller
	// that only took the event channel through Watch.
	fail := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(fail)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			next, events, _, err := s.watchPage(ctx, r, after, 100)
			if err != nil {
				fail <- err
				return
			}
			for _, event := range events {
				if machine, identityErr := machineActor(ctx, r.Principal.TenantID, r.Principal.SubjectID); identityErr != nil {
					fail <- chat.ErrPermissionDenied
					return
				} else if machine {
					if _, authErr := s.MachineWatchEpoch(ctx, r.TenantID, r.ConversationID, r.Principal); authErr != nil {
						fail <- authErr
						return
					}
				}
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
			after = next
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return out, fail
}

// EventPage advances NextOffset across filtered records as well as visible
// events, so a subscriber with restricted history can catch up without replaying
// the same old records on every poll.
type EventPage struct {
	Events     []chat.WatchEvent
	NextOffset uint64
	Complete   bool
}

func (s *Adapter) ReadConversationEvents(ctx context.Context, r chat.WatchConversationRequest, after uint64, limit int) (EventPage, error) {
	if after > uint64(^uint64(0)>>1) {
		return EventPage{}, chat.ErrInvalidArgument
	}
	next, events, complete, err := s.watchPage(ctx, r, int64(after), limit)
	if err != nil {
		return EventPage{}, err
	}
	return EventPage{Events: events, NextOffset: uint64(next), Complete: complete}, nil
}

func (s *Adapter) watchPage(ctx context.Context, r chat.WatchConversationRequest, after int64, limit int) (int64, []chat.WatchEvent, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return after, nil, false, err
	}
	defer tx.Rollback(ctx)
	if err = tenant(ctx, tx, r.TenantID); err != nil {
		return after, nil, false, err
	}
	var hist string
	var joined time.Time
	machine, identityErr := machineActor(ctx, r.Principal.TenantID, r.Principal.SubjectID)
	if identityErr != nil {
		return after, nil, false, chat.ErrPermissionDenied
	}
	if machine {
		if r.Principal.TenantID != r.TenantID {
			return after, nil, false, chat.ErrPermissionDenied
		}
		hist = string(chat.FromJoin)
		err = tx.QueryRow(ctx, `SELECT created_at FROM chat_app_installation WHERE tenant_id=$1 AND conversation_id=$2 AND app_id=$3 AND id=tenant_id||':'||conversation_id||':'||app_id AND status='ACTIVE' AND version>0 AND created_at<=now() AND 'chat.posts.read'=ANY(granted_scopes)`, r.TenantID, r.ConversationID, r.Principal.SubjectID).Scan(&joined)
	} else {
		err = tx.QueryRow(ctx, `SELECT history_visibility,joined_at FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active'`, r.TenantID, r.ConversationID, r.Principal.TenantID, r.Principal.SubjectID).Scan(&hist, &joined)
	}
	if err != nil {
		if machine && !errors.Is(err, dbport.ErrNoRows) {
			return after, nil, false, fmt.Errorf("machine watch installation: %w: %w", chat.ErrUnavailable, err)
		}
		return after, nil, false, chat.ErrPermissionDenied
	}
	// The filter uses the one payload key every chatstore producer writes; see
	// the OutboxKey constants in store.go.
	query := `SELECT id,event_type,payload,created_at FROM chat_outbox WHERE tenant_id=$1 AND id>$2 AND payload->>'` + OutboxKeyConversationID + `'=$3 ORDER BY id LIMIT $4`
	args := []any{r.TenantID, after, r.ConversationID, limit}
	if machine {
		// Filter before LIMIT. A newly installed agent must reach its first
		// visible post even when thousands of older outbox rows exist.
		query = `SELECT o.id,o.event_type,o.payload,o.created_at FROM chat_outbox o JOIN chat_post p ON p.id=(o.payload->>'` + OutboxKeyTargetID + `') AND p.tenant_id=o.tenant_id AND p.conversation_id=$3 AND p.created_at>=$5 WHERE o.tenant_id=$1 AND o.id>$2 AND o.payload->>'` + OutboxKeyConversationID + `'=$3 AND o.created_at>=$5 AND o.event_type IN ('post.created','post.edited','post.deleted') ORDER BY o.id LIMIT $4`
		args = append(args, joined)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return after, nil, false, err
	}
	defer rows.Close()
	var out []chat.WatchEvent
	scanned := 0
	for rows.Next() {
		scanned++
		var id int64
		var kind string
		var b []byte
		var at time.Time
		if err = rows.Scan(&id, &kind, &b, &at); err != nil {
			return after, nil, false, err
		}
		after = id
		if hist != string(chat.FullHistory) && at.Before(joined) {
			continue
		}
		// A machine installation with chat.posts.read grants access to post
		// payloads only. Other event kinds contain membership and conversation
		// metadata and require separate subscription authority.
		if machine && kind != "post.created" && kind != "post.edited" && kind != "post.deleted" {
			continue
		}
		event := chat.ConversationEvent{Sequence: uint64(id)}
		switch kind {
		case "post.created":
			event.Kind = chat.PostCreated
		case "post.edited":
			event.Kind = chat.PostEdited
		case "post.deleted":
			event.Kind = chat.PostDeleted
		case "conversation.created", "conversation.updated":
			event.Kind = chat.ConversationUpdated
		case "membership.added", "membership.removed":
			event.Kind = chat.MembershipChanged
			event.Removed = kind == "membership.removed"
		case "reaction.added", "reaction.removed":
			event.Kind = chat.ReactionChanged
			event.Removed = kind == "reaction.removed"
		case "pin.added", "pin.removed":
			event.Kind = chat.PinChanged
			event.Removed = kind == "pin.removed"
		default:
			continue
		}
		var envelope OutboxEnvelope
		if err = json.Unmarshal(b, &envelope); err != nil {
			return after, nil, false, err
		}
		// The event a client sees is numbered per conversation, not by the
		// outbox row identifier. The outbox counter is shared by every
		// conversation and every event kind, so a room's second event could
		// arrive as 4192 after its first arrived as 4103 — which any client
		// checking for continuity correctly reads as a gap, and which made the
		// browser resubscribe every two seconds. chat_conversation.event_sequence
		// is the per-conversation number, and the producer already writes it into
		// the envelope.
		if envelope.EventSequence > 0 {
			event.Sequence = uint64(envelope.EventSequence)
		}
		if strings.HasPrefix(kind, "post.") {
			var p chat.Post
			if err = json.Unmarshal(envelope.Value, &p); err != nil {
				return after, nil, false, err
			}
			if hist != string(chat.FullHistory) && p.CreatedAt.Before(joined) {
				continue
			}
			event.Post = &p
			event.Revision = p.Revision
		} else {
			event.Revision = envelope.Revision
			switch event.Kind {
			case chat.ConversationUpdated:
				var v chat.Conversation
				if err = json.Unmarshal(envelope.Value, &v); err != nil {
					return after, nil, false, err
				}
				event.Conversation = &v
			case chat.MembershipChanged:
				var v chat.Membership
				if err = json.Unmarshal(envelope.Value, &v); err != nil {
					return after, nil, false, err
				}
				event.Membership = &v
			case chat.ReactionChanged:
				var v chat.Reaction
				if err = json.Unmarshal(envelope.Value, &v); err != nil {
					return after, nil, false, err
				}
				event.Reaction = &v
			case chat.PinChanged:
				var v chat.Pin
				if len(envelope.Value) > 0 && string(envelope.Value) != "null" {
					if err = json.Unmarshal(envelope.Value, &v); err != nil {
						return after, nil, false, err
					}
				} else {
					v.PostID = envelope.TargetID
					v.ConversationID = r.ConversationID
					v.TenantID = r.TenantID
				}
				event.Pin = &v
			}
		}
		out = append(out, chat.WatchEvent{Event: event, ResumeCursor: strconv.FormatInt(id, 10)})
	}
	if err = rows.Err(); err != nil {
		return after, nil, false, err
	}
	return after, out, scanned < limit, tx.Commit(ctx)
}
