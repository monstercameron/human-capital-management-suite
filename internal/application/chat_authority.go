package application

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatappstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/session"
)

// ChatAuthorityFacts loads current governance facts for the authenticated
// subject. Facts without a trusted current source remain absent and cannot
// satisfy a policy requirement.
type ChatAuthorityFacts interface {
	ResolveChatFacts(context.Context, string, string, time.Time) (chatpolicy.Principal, error)
}

type chatAuthoritySource struct{ facts ChatAuthorityFacts }

var errChatIdentity = errors.New("chat: authenticated identity unavailable")

// Resolve binds the request identity to the trusted authentication context,
// then loads the current authority. Request profile fields are never facts.
func (s chatAuthoritySource) Resolve(ctx context.Context, tenant, subject string, at time.Time) (chatpolicy.Principal, error) {
	authenticated, ok := trust.FromContext(ctx)
	if !ok || authenticated.Subject() != subject || string(authenticated.Tenant()) != tenant || at.IsZero() || !at.Before(authenticated.ExpiresAt()) || s.facts == nil {
		return chatpolicy.Principal{}, errChatIdentity
	}
	p, err := s.facts.ResolveChatFacts(ctx, tenant, subject, at)
	if err != nil || p.ID != subject || p.Tenant != tenant || !p.Current(at) || p.AuthorityRevision == 0 {
		return chatpolicy.Principal{}, errChatIdentity
	}
	return p, nil
}

func newChatAuthoritySource(facts ChatAuthorityFacts) chatAuthoritySource {
	return chatAuthoritySource{facts: facts}
}

// currentRoleChatFacts reads the persisted role assignment on each request.
// A missing assignment is not evidence of employment. A configured session
// checker invalidates logged-out sessions before a chat policy is evaluated.
type currentRoleChatFacts struct {
	roles interface {
		Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error)
	}
	sessions   session.RevocationChecker
	workers    dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
}

func newCurrentRoleChatFacts(roles interface {
	Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error)
}, sessions session.RevocationChecker) ChatAuthorityFacts {
	return currentRoleChatFacts{roles: roles, sessions: sessions}
}

func newCurrentWorkerChatFacts(roles interface {
	Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error)
}, workers dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, sessions session.RevocationChecker) ChatAuthorityFacts {
	return currentRoleChatFacts{roles: roles, workers: workers, tenantUUID: tenantUUID, sessions: sessions}
}

func (f currentRoleChatFacts) ResolveChatFacts(ctx context.Context, tenant, subject string, at time.Time) (chatpolicy.Principal, error) {
	trusted, ok := trust.FromContext(ctx)
	if !ok || f.roles == nil || trusted.Subject() != subject || string(trusted.Tenant()) != tenant {
		return chatpolicy.Principal{}, errChatIdentity
	}
	if err := f.CheckChatSession(ctx, tenant, subject, at); err != nil {
		return chatpolicy.Principal{}, err
	}
	var workerRevision uint64
	if f.workers != nil {
		if f.tenantUUID == nil {
			return chatpolicy.Principal{}, errChatIdentity
		}
		tenantID := f.tenantUUID(values.TenantId(tenant))
		tx, err := f.workers.Begin(ctx)
		if err != nil {
			return chatpolicy.Principal{}, err
		}
		defer tx.Rollback(ctx)
		if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
			return chatpolicy.Principal{}, err
		}
		worker, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, subject)
		if err != nil {
			return chatpolicy.Principal{}, err
		}
		if !found || !strings.EqualFold(worker.LifecycleStatus, "active") {
			return chatpolicy.Principal{}, errChatIdentity
		}
		workerRevision = uint64(worker.RevisionSequence)
	}
	snapshot, err := f.roles.Load(ctx, values.TenantId(tenant), trusted.OrganizationScopeID())
	if err != nil {
		return chatpolicy.Principal{}, err
	}
	var assignment *roleaccess.Assignment
	for i := range snapshot.Assignments {
		if snapshot.Assignments[i].WorkerRef == subject {
			assignment = &snapshot.Assignments[i]
			break
		}
	}
	if assignment == nil && f.workers == nil {
		return chatpolicy.Principal{}, errChatIdentity
	}
	if assignment != nil && assignment.Version <= 0 {
		return chatpolicy.Principal{}, errChatIdentity
	}
	var number [8]byte
	if assignment != nil {
		binary.LittleEndian.PutUint64(number[:], uint64(assignment.Version))
	}
	state := append([]byte(nil), number[:]...)
	binary.LittleEndian.PutUint64(number[:], workerRevision)
	state = append(state, number[:]...)
	active := make(map[string]roleaccess.Role, len(snapshot.Roles))
	for _, role := range snapshot.Roles {
		if role.Active {
			active[role.ID] = role
		}
	}
	var roleIDs []string
	if assignment != nil {
		roleIDs = slices.Clone(assignment.RoleIDs)
	}
	slices.Sort(roleIDs)
	roles := make([]string, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		role, ok := active[roleID]
		if !ok {
			continue
		}
		roles = append(roles, roleID)
		binary.LittleEndian.PutUint64(number[:], uint64(len(roleID)))
		state = append(state, number[:]...)
		state = append(state, roleID...)
		binary.LittleEndian.PutUint64(number[:], uint64(role.Version))
		state = append(state, number[:]...)
	}
	digest := sha256.Sum256(state)
	revision := binary.LittleEndian.Uint64(digest[:8])
	if revision == 0 {
		revision = 1
	}
	return chatpolicy.Principal{ID: subject, Tenant: tenant, Active: true, Roles: roles, AuthorityRevision: revision}, nil
}

// CheckChatSession is deliberately separate from the cached role and worker
// facts. Logout must be observed on every authorization even when those slower
// changing governance facts are still fresh in the cache.
func (f currentRoleChatFacts) CheckChatSession(ctx context.Context, tenant, subject string, at time.Time) error {
	trusted, ok := trust.FromContext(ctx)
	if !ok || trusted.Subject() != subject || string(trusted.Tenant()) != tenant || at.IsZero() {
		return errChatIdentity
	}
	if f.sessions != nil {
		return f.sessions.CheckRevocation(ctx, trusted.SessionRef(), at)
	}
	return nil
}

type chatSessionVerifier interface {
	CheckChatSession(context.Context, string, string, time.Time) error
}

// DefaultChatAuthorityTTL is the policy freshness contract for chat
// authorization: a cached principal fact or channel policy is at most this old.
// The spec's revocation row requires affected caches and streams to be
// invalidated within the defined revocation budget, so every revocation path
// (membership removal, grant revoke, channel policy change) invalidates the
// entries it affects explicitly; this TTL is only the bound for a change that
// arrives through no such path.
const DefaultChatAuthorityTTL = 10 * time.Second

// maxChatAuthorityEntries bounds the cache so a tenant with many principals or
// conversations cannot turn it into an unbounded process-local map.
const maxChatAuthorityEntries = 4096

type cachedChatValue[T any] struct {
	value   T
	expires time.Time
}

// chatAuthorityCache keeps the per-principal facts and per-conversation policy
// that chat authorization would otherwise read on every call: the facts come
// from the core database (worker row plus role assignment) and the policy from
// the chat database. It is a cache, not the source of truth: every entry
// expires, and each revocation path drops the entries it affects.
type chatAuthorityCache struct {
	ttl      time.Duration
	now      func() time.Time
	mu       sync.Mutex
	facts    map[string]cachedChatValue[chatpolicy.Principal]
	policies map[string]cachedChatValue[cachedChatPolicy]
}

// cachedChatPolicy remembers an absent policy too. A conversation governed only
// by membership has no policy row, and re-reading that absence on every call is
// exactly the per-call database hit this cache exists to remove.
type cachedChatPolicy struct {
	channel chatpolicy.Channel
	absent  bool
}

func newChatAuthorityCache(ttl time.Duration, now func() time.Time) *chatAuthorityCache {
	if ttl <= 0 {
		ttl = DefaultChatAuthorityTTL
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &chatAuthorityCache{ttl: ttl, now: now, facts: map[string]cachedChatValue[chatpolicy.Principal]{}, policies: map[string]cachedChatValue[cachedChatPolicy]{}}
}

const chatCacheSeparator = "\x00"

func chatCacheKey(parts ...string) string { return strings.Join(parts, chatCacheSeparator) }

func cachedChatGet[T any](c *chatAuthorityCache, m map[string]cachedChatValue[T], key string) (T, bool) {
	var zero T
	if c == nil {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := m[key]
	if !ok {
		return zero, false
	}
	if !c.now().Before(entry.expires) {
		delete(m, key)
		return zero, false
	}
	return entry.value, true
}

func cachedChatPut[T any](c *chatAuthorityCache, m map[string]cachedChatValue[T], key string, value T) {
	if c == nil {
		return
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(m) >= maxChatAuthorityEntries {
		for k, entry := range m {
			if !now.Before(entry.expires) {
				delete(m, k)
			}
		}
	}
	if len(m) >= maxChatAuthorityEntries {
		// Still full of live entries: drop one so the map stays bounded. The
		// evicted principal simply re-reads its own authority.
		for k := range m {
			delete(m, k)
			break
		}
	}
	m[key] = cachedChatValue[T]{value: value, expires: now.Add(c.ttl)}
}

// InvalidatePrincipal drops one principal's cached facts. Membership removal and
// any other authority change for that subject call it so the next chat call
// re-reads the core database instead of waiting out the TTL.
func (c *chatAuthorityCache) InvalidatePrincipal(tenant, subject string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := chatCacheKey(tenant, subject) + chatCacheSeparator
	for key := range c.facts {
		if strings.HasPrefix(key, prefix) {
			delete(c.facts, key)
		}
	}
}

// InvalidateConversation drops one conversation's cached channel policy. Policy
// writes and grant changes call it.
func (c *chatAuthorityCache) InvalidateConversation(host, conversation string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.policies, chatCacheKey(host, conversation))
}

// InvalidateTenant drops every cached fact for one tenant. A tenant-wide
// authority change, such as a revoked cross-company grant, cannot enumerate the
// affected subjects.
func (c *chatAuthorityCache) InvalidateTenant(tenant string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := tenant + chatCacheSeparator
	for key := range c.facts {
		if strings.HasPrefix(key, prefix) {
			delete(c.facts, key)
		}
	}
}

// cachedChatFacts bounds how often chat authorization reads the core database
// for one principal. The trusted authentication binding is still re-checked on
// every call by chatAuthoritySource.Resolve; only the governance reads behind it
// are cached, and the session reference is part of the key so a new session
// always reloads.
type cachedChatFacts struct {
	inner ChatAuthorityFacts
	cache *chatAuthorityCache
}

func newCachedChatFacts(inner ChatAuthorityFacts, cache *chatAuthorityCache) ChatAuthorityFacts {
	if inner == nil || cache == nil {
		return inner
	}
	return cachedChatFacts{inner: inner, cache: cache}
}

func (f cachedChatFacts) ResolveChatFacts(ctx context.Context, tenant, subject string, at time.Time) (chatpolicy.Principal, error) {
	reference := ""
	if trusted, ok := trust.FromContext(ctx); ok {
		reference = trusted.SessionRef()
	}
	key := chatCacheKey(tenant, subject, reference)
	if p, ok := cachedChatGet(f.cache, f.cache.facts, key); ok && p.Current(at) {
		if checker, ok := f.inner.(chatSessionVerifier); ok {
			if err := checker.CheckChatSession(ctx, tenant, subject, at); err != nil {
				f.cache.InvalidatePrincipal(tenant, subject)
				return chatpolicy.Principal{}, err
			}
		}
		return p, nil
	}
	p, err := f.inner.ResolveChatFacts(ctx, tenant, subject, at)
	if err != nil {
		f.cache.InvalidatePrincipal(tenant, subject)
		return chatpolicy.Principal{}, err
	}
	if !p.Current(at) {
		f.cache.InvalidatePrincipal(tenant, subject)
		return chatpolicy.Principal{}, errChatIdentity
	}
	cachedChatPut(f.cache, f.cache.facts, key, p)
	return p, nil
}

type chatCurrentAuthority struct {
	source  chatAuthoritySource
	members chat.Store
	policy  *chatauthority.Store
	cache   *chatAuthorityCache
	apps    interface {
		Get(context.Context, string) (chatapps.Installation, error)
	}
}

func newChatCurrentAuthority(facts ChatAuthorityFacts, members chat.Store, policy *chatauthority.Store, cache *chatAuthorityCache, apps ...interface {
	Get(context.Context, string) (chatapps.Installation, error)
}) chat.Authority {
	a := chatCurrentAuthority{source: newChatAuthoritySource(newCachedChatFacts(facts, cache)), members: members, policy: policy, cache: cache}
	if len(apps) > 0 {
		a.apps = apps[0]
	}
	return a
}

// machineAdmission uses the verified actor kind and a current, named
// installation. An installation is distinct from human membership, but its
// active revision supplies the policy evaluator's admission evidence.
func (a chatCurrentAuthority) machineAdmission(ctx context.Context, p chat.Principal, c chat.Conversation, action chatpolicy.Action, at time.Time) (chatpolicy.Principal, chatpolicy.Membership, error) {
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.Subject() != p.SubjectID || verified.Tenant().String() != p.TenantID || !at.Before(verified.ExpiresAt()) || p.TenantID != c.TenantID || a.apps == nil {
		return chatpolicy.Principal{}, chatpolicy.Membership{}, chat.ErrPermissionDenied
	}
	inv, ok := transport.InvocationFromContext(ctx)
	if !ok || inv == nil || inv.Principal() != verified {
		return chatpolicy.Principal{}, chatpolicy.Membership{}, chat.ErrPermissionDenied
	}
	scope := ""
	switch {
	case action == chatpolicy.ActionPost && inv.Method() == "/hcmnext.chat.v1.ConversationService/SendPost":
		scope = "chat.posts.write"
	case action == chatpolicy.ActionRead && (inv.Method() == "/hcmnext.chat.v1.ConversationService/ListPosts" || inv.Method() == "/hcmnext.chat.v1.ConversationService/GetConversation" || inv.Method() == "/hcmnext.chat.v1.ConversationService/WatchConversation"):
		scope = "chat.posts.read"
	default:
		return chatpolicy.Principal{}, chatpolicy.Membership{}, chat.ErrPermissionDenied
	}
	v, err := a.apps.Get(chatappstore.WithTenant(ctx, c.TenantID), c.TenantID+":"+c.ID+":"+p.SubjectID)
	if err != nil {
		if errors.Is(err, chatapps.ErrNotFound) || errors.Is(err, chatapps.ErrDenied) || errors.Is(err, chatapps.ErrSuspended) || errors.Is(err, chatapps.ErrRevoked) {
			return chatpolicy.Principal{}, chatpolicy.Membership{}, chat.ErrPermissionDenied
		}
		return chatpolicy.Principal{}, chatpolicy.Membership{}, chat.ErrUnavailable
	}
	if !v.Current(at) || v.Tenant != c.TenantID || v.Conversation != c.ID || v.AppID != p.SubjectID || v.Revision == 0 {
		return chatpolicy.Principal{}, chatpolicy.Membership{}, chat.ErrPermissionDenied
	}
	if (verified.SubjectKind() == trust.SubjectKindAgent) != (v.Manifest.Agent != nil) {
		return chatpolicy.Principal{}, chatpolicy.Membership{}, chat.ErrPermissionDenied
	}
	granted := false
	for _, s := range v.GrantedScopes {
		if s == scope {
			granted = true
			break
		}
	}
	if !granted {
		return chatpolicy.Principal{}, chatpolicy.Membership{}, chat.ErrPermissionDenied
	}
	// Machine token roles are never a substitute for a channel's human role
	// requirement. The installation scopes authorize the narrow API operation;
	// role-qualified channels still need a separate declared machine policy.
	return chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: v.Revision}, chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: v.Revision, JoinedAt: v.CreatedAt}, nil
}

// channelPolicy reads the conversation's policy through the bounded cache. The
// chat database stays the source of truth; SetChannelPolicy and grant changes
// invalidate the entry, so a steady-state send or stream recheck does not
// re-read it.
func (a chatCurrentAuthority) channelPolicy(ctx context.Context, host, conversation string) (chatpolicy.Channel, error) {
	key := chatCacheKey(host, conversation)
	if entry, ok := cachedChatGet(a.cache, a.cache.policies, key); ok {
		if entry.absent {
			return chatpolicy.Channel{}, dbport.ErrNoRows
		}
		return entry.channel, nil
	}
	p, err := a.policy.Policy(ctx, host, conversation)
	switch {
	case errors.Is(err, dbport.ErrNoRows):
		cachedChatPut(a.cache, a.cache.policies, key, cachedChatPolicy{absent: true})
		return chatpolicy.Channel{}, err
	case err != nil:
		return chatpolicy.Channel{}, err
	}
	cachedChatPut(a.cache, a.cache.policies, key, cachedChatPolicy{channel: p})
	return p, nil
}

func (a chatCurrentAuthority) Authorize(ctx context.Context, principal chat.Principal, conversation chat.Conversation, action chatpolicy.Action, at time.Time) (chatpolicy.Input, error) {
	if a.members == nil || a.policy == nil {
		return chatpolicy.Input{}, errChatIdentity
	}
	verified, ok := trust.FromContext(ctx)
	var current chatpolicy.Principal
	var machineMembership chatpolicy.Membership
	var machine bool
	var err error
	if ok && verified != nil && (verified.SubjectKind() == trust.SubjectKindAgent || verified.SubjectKind() == trust.SubjectKindIntegration) {
		machine = true
		current, machineMembership, err = a.machineAdmission(ctx, principal, conversation, action, at)
	} else {
		current, err = a.source.Resolve(ctx, principal.TenantID, principal.SubjectID, at)
	}
	if err != nil {
		return chatpolicy.Input{}, err
	}
	channel := chatpolicy.Channel{ID: conversation.ID, HostTenant: conversation.TenantID, Private: conversation.Kind != chat.PublicChannel, Enabled: !conversation.Archived, Revision: conversation.Revision}
	policy, err := a.channelPolicy(ctx, conversation.TenantID, conversation.ID)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return chatpolicy.Input{}, err
	}
	if err == nil {
		channel.RequiredRoles = policy.RequiredRoles
		channel.RoleMode = policy.RoleMode
		channel.RequiredQualifications = policy.RequiredQualifications
		channel.AllowedPrincipals = policy.AllowedPrincipals
		channel.AllowedTenants = policy.AllowedTenants
		channel.Classification = policy.Classification
		channel.Residency = policy.Residency
		var versions [16]byte
		binary.LittleEndian.PutUint64(versions[:8], conversation.Revision)
		binary.LittleEndian.PutUint64(versions[8:], policy.Revision)
		digest := sha256.Sum256(versions[:])
		channel.Revision = binary.LittleEndian.Uint64(digest[:8])
		if channel.Revision == 0 {
			channel.Revision = 1
		}
	}
	in := chatpolicy.Input{Principal: current, Channel: channel, Now: at}
	if machine {
		in.Membership = machineMembership
		in.HasMembership = true
	} else {
		m, err := a.members.GetMembership(ctx, conversation.TenantID, conversation.ID, principal.TenantID, principal.SubjectID)
		if err == nil && m.SubjectID == principal.SubjectID && m.HomeTenantID == principal.TenantID {
			state := chatpolicy.MembershipCurrent
			if m.LeftAt != nil {
				state = chatpolicy.MembershipLeft
			}
			in.Membership = chatpolicy.Membership{ConversationID: conversation.ID, PrincipalID: principal.SubjectID, Tenant: principal.TenantID, State: state, Revision: m.Revision, JoinedAt: valueChatTime(m.JoinedAt), LeftAt: valueChatTime(m.LeftAt)}
			in.HasMembership = true
		}
	}
	if principal.TenantID != conversation.TenantID {
		g, e := a.policy.CurrentGrant(ctx, conversation.TenantID, principal.TenantID, conversation.ID, at)
		if e != nil {
			return chatpolicy.Input{}, e
		}
		in.Grant = g
		in.HasGrant = true
	}
	return in, nil
}

func valueChatTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

// ChatCompanyGrants makes each company consent through its own currently
// authenticated administrator. The host cannot accept on the consumer's
// behalf, including by sending a forged tenant or role in a request body.
type ChatCompanyGrants struct {
	source  chatAuthoritySource
	store   *chatauthority.Store
	cache   *chatAuthorityCache
	streams chatStreamRevoker
}

// chatStreamRevoker is the live-stream half of revocation. Composition supplies
// the chat stream runtime; a composition without streaming supplies nothing and
// only the cache is invalidated.
type chatStreamRevoker interface {
	RevokeTenant(hostTenantID, homeTenantID, conversationID string)
}

func NewChatCompanyGrants(facts ChatAuthorityFacts, store *chatauthority.Store) *ChatCompanyGrants {
	return &ChatCompanyGrants{source: newChatAuthoritySource(facts), store: store}
}

// withRevocation binds this grant authority to the caches and live streams a
// revocation has to invalidate. Composition calls it once.
func (s *ChatCompanyGrants) withRevocation(cache *chatAuthorityCache, streams chatStreamRevoker) *ChatCompanyGrants {
	if s == nil {
		return s
	}
	s.cache = cache
	s.streams = streams
	return s
}

func (s *ChatCompanyGrants) admin(ctx context.Context, tenant string, at time.Time) (string, error) {
	if s == nil || s.store == nil {
		return "", chat.ErrUnavailable
	}
	trusted, ok := trust.FromContext(ctx)
	if !ok || string(trusted.Tenant()) != tenant {
		return "", chat.ErrPermissionDenied
	}
	p, err := s.source.Resolve(ctx, tenant, trusted.Subject(), at)
	if err != nil || !slices.Contains(p.Roles, "hcm_admin") {
		return "", chat.ErrPermissionDenied
	}
	return trusted.Subject(), nil
}

func chatGrantError(err error) error {
	if errors.Is(err, chatauthority.ErrDenied) {
		return chat.ErrPermissionDenied
	}
	if errors.Is(err, chatauthority.ErrConflict) {
		return chat.ErrConflict
	}
	return err
}

// SetChannelPolicy applies a revision-checked host policy through current
// company administration authority.
func (s *ChatCompanyGrants) SetChannelPolicy(ctx context.Context, host, conversation string, requiredRoles, qualifications, allowedPrincipals, allowedTenants []string, mode chatpolicy.RoleMode, classification, residency string, expected uint64, at time.Time) error {
	actor, err := s.admin(ctx, host, at)
	if err != nil {
		return err
	}
	if err := chatGrantError(s.store.PutPolicy(ctx, host, conversation, requiredRoles, qualifications, allowedPrincipals, allowedTenants, mode, classification, residency, expected, actor)); err != nil {
		return err
	}
	// The next authorization must see the new policy, not the cached one. A
	// policy revision can narrow access, so close every existing conversation
	// stream before returning; queued events then fail closed as well.
	s.cache.InvalidateConversation(host, conversation)
	if s.streams != nil {
		s.streams.RevokeTenant(host, "", conversation)
	}
	return nil
}

func (s *ChatCompanyGrants) Propose(ctx context.Context, host, consumer, conversation, classification, residency string, expiresAt, at time.Time) (string, error) {
	actor, err := s.admin(ctx, host, at)
	if err != nil {
		return "", err
	}
	if !at.Before(expiresAt) {
		return "", chat.ErrInvalidArgument
	}
	id := uuid.NewString()
	g, err := chatpolicy.ProposeGrant(id, conversation, host, consumer, "conversation", classification, residency, 1, expiresAt)
	if err != nil {
		return "", err
	}
	if err := s.store.Propose(ctx, host, actor, g); err != nil {
		return "", chatGrantError(err)
	}
	return id, nil
}

func (s *ChatCompanyGrants) Accept(ctx context.Context, host, consumer, conversation, id string, at time.Time) error {
	actor, err := s.admin(ctx, consumer, at)
	if err != nil {
		return err
	}
	if err := chatGrantError(s.store.Accept(ctx, host, consumer, conversation, id, actor, at)); err != nil {
		return err
	}
	s.cache.InvalidateConversation(host, conversation)
	return nil
}

// Revoke withdraws a cross-company grant. Revocation is event driven: the
// cached authority for the consumer tenant and the conversation is dropped and
// every live subscription that tenant holds on the conversation is closed before
// this call returns, rather than waiting out the stream's safety-net recheck.
func (s *ChatCompanyGrants) Revoke(ctx context.Context, host, consumer, conversation, id string, at time.Time) error {
	trusted, ok := trust.FromContext(ctx)
	if !ok {
		return chat.ErrPermissionDenied
	}
	actorTenant := string(trusted.Tenant())
	if actorTenant != host && actorTenant != consumer {
		return chat.ErrPermissionDenied
	}
	actor, err := s.admin(ctx, actorTenant, at)
	if err != nil {
		return err
	}
	if err := chatGrantError(s.store.Revoke(ctx, host, consumer, conversation, id, actorTenant, actor, at)); err != nil {
		return err
	}
	s.invalidateGrant(host, consumer, conversation)
	return nil
}

// invalidateGrant is the revocation fan-out for one host/consumer pair.
func (s *ChatCompanyGrants) invalidateGrant(host, consumer, conversation string) {
	s.cache.InvalidateConversation(host, conversation)
	s.cache.InvalidateTenant(consumer)
	if s.streams != nil {
		s.streams.RevokeTenant(host, consumer, conversation)
	}
}
