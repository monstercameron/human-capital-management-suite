package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatstream"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type policyRevocationDB struct {
	failPolicyWrite bool
	writes          int
	policy          *chatpolicy.Channel
}

func (d *policyRevocationDB) RunTx(ctx context.Context, fn func(dbport.Tx) error) error {
	return fn(policyRevocationTx{db: d})
}

type policyRevocationTx struct{ db *policyRevocationDB }

func (tx policyRevocationTx) Exec(_ context.Context, query string, args ...any) (int64, error) {
	switch {
	case strings.Contains(query, "set_config('hcmnext.tenant_id'"):
		return 1, nil
	case strings.Contains(query, "INSERT INTO chat_channel_policy") || strings.Contains(query, "UPDATE chat_channel_policy"):
		if tx.db.failPolicyWrite {
			return 0, errors.New("policy write failed")
		}
		tx.db.writes++
		revision := uint64(1)
		if tx.db.policy != nil {
			revision = tx.db.policy.Revision + 1
		}
		tx.db.policy = &chatpolicy.Channel{ID: args[1].(string), HostTenant: args[0].(string), RequiredRoles: append([]string(nil), args[2].([]string)...), RoleMode: chatpolicy.RoleMode(args[3].(int)), RequiredQualifications: append([]string(nil), args[4].([]string)...), AllowedPrincipals: append([]string(nil), args[5].([]string)...), AllowedTenants: append([]string(nil), args[6].([]string)...), Classification: args[7].(string), Residency: args[8].(string), Revision: revision}
		return 1, nil
	case strings.Contains(query, "INSERT INTO chat_outbox"):
		return 1, nil
	default:
		return 0, fmt.Errorf("unexpected authority statement %q", query)
	}
}

func (policyRevocationTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected authority query")
}

func (tx policyRevocationTx) QueryRow(_ context.Context, query string, _ ...any) dbport.Row {
	if strings.Contains(query, "nextval(pg_get_serial_sequence") {
		return policySequenceRow{sequence: 17}
	}
	if strings.Contains(query, "FROM chat_channel_policy") {
		return policyChannelRow{channel: tx.db.policy}
	}
	return policyChannelRow{err: errors.New("unexpected policy row query")}
}

func (policyRevocationTx) Commit(context.Context) error   { return nil }
func (policyRevocationTx) Rollback(context.Context) error { return nil }

type policySequenceRow struct{ sequence int64 }

func (r policySequenceRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("unexpected sequence scan")
	}
	v, ok := dest[0].(*int64)
	if !ok {
		return errors.New("unexpected sequence destination")
	}
	*v = r.sequence
	return nil
}

type policyChannelRow struct {
	channel *chatpolicy.Channel
	err     error
}

func (r policyChannelRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.channel == nil {
		return dbport.ErrNoRows
	}
	if len(dest) != 8 {
		return errors.New("unexpected policy scan")
	}
	*dest[0].(*uint64) = r.channel.Revision
	*dest[1].(*[]string) = append([]string(nil), r.channel.RequiredRoles...)
	*dest[2].(*int) = int(r.channel.RoleMode)
	*dest[3].(*[]string) = append([]string(nil), r.channel.RequiredQualifications...)
	*dest[4].(*[]string) = append([]string(nil), r.channel.AllowedPrincipals...)
	*dest[5].(*[]string) = append([]string(nil), r.channel.AllowedTenants...)
	*dest[6].(*string) = r.channel.Classification
	*dest[7].(*string) = r.channel.Residency
	return nil
}

type policyWatchReader struct{}

func (policyWatchReader) Read(context.Context, chatstream.ReadRequest) (chatstream.Page, error) {
	return chatstream.Page{Complete: true}, nil
}

type policyWatchAuthorizer struct{}

func (policyWatchAuthorizer) Authorize(context.Context, chatstream.Access) error { return nil }

type policyReadFacts struct{}

func (policyReadFacts) ResolveChatFacts(_ context.Context, tenant, subject string, _ time.Time) (chatpolicy.Principal, error) {
	return chatpolicy.Principal{ID: subject, Tenant: tenant, Active: true, AuthorityRevision: 1}, nil
}

type policyReadStore struct {
	chatcore.Store
	searchCalls int
}

func (s *policyReadStore) GetConversation(_ context.Context, tenant, id string) (chatcore.Conversation, error) {
	return chatcore.Conversation{ID: id, TenantID: tenant, Kind: chatcore.PrivateChannel, Revision: 1}, nil
}

func (s *policyReadStore) GetMembership(_ context.Context, tenant, id, home, subject string) (chatcore.Membership, error) {
	joined := time.Now().UTC().Add(-time.Hour)
	return chatcore.Membership{ConversationID: id, TenantID: tenant, HomeTenantID: home, SubjectID: subject, Role: chatcore.Member, JoinedAt: &joined, Revision: 1}, nil
}

func (s *policyReadStore) Search(context.Context, chatcore.SearchRequest) (chatcore.SearchResponse, error) {
	s.searchCalls++
	return chatcore.SearchResponse{}, nil
}

func newPolicyRevocationStream(t *testing.T) *chatstream.Stream {
	t.Helper()
	stream, err := chatstream.New(chatstream.Config{Key: []byte("policy-revocation-test-key"), Reader: policyWatchReader{}, Authorizer: policyWatchAuthorizer{}, QueueSize: 4, ReplayLimit: 2, CursorTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func policyAdminContext(t *testing.T, at time.Time) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "admin", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-host", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func setRestrictiveChannelPolicy(ctx context.Context, grants *ChatCompanyGrants, at time.Time) error {
	return grants.SetChannelPolicy(ctx, "host", "conversation", []string{"manager"}, nil, nil, nil, chatpolicy.RolesAll, "confidential", "US", 0, at)
}

func TestTodo_CHAT_020_SetChannelPolicySynchronouslyInvalidatesStreamsAndCache(t *testing.T) {
	at := time.Now().UTC()
	db := &policyRevocationDB{}
	cache := newChatAuthorityCache(time.Minute, nil)
	cachedChatPut(cache, cache.policies, chatCacheKey("host", "conversation"), cachedChatPolicy{channel: chatpolicy.Channel{Revision: 1}})
	stream := newPolicyRevocationStream(t)
	grants := NewChatCompanyGrants(chatAdminFacts{role: "hcm_admin"}, chatauthority.New(db)).withRevocation(cache, stream)

	queued, err := stream.Watch(context.Background(), chatstream.WatchRequest{TenantID: "host", HomeTenantID: "guest", SubjectID: "queued", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer queued.Close()
	idle, err := stream.Watch(context.Background(), chatstream.WatchRequest{TenantID: "host", HomeTenantID: "host", SubjectID: "idle", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	if err := stream.Publish(context.Background(), chatstream.Event{TenantID: "host", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1, Payload: []byte("private")}); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	if err := setRestrictiveChannelPolicy(policyAdminContext(t, at), grants, at); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if elapsed > 250*time.Millisecond {
		t.Fatalf("policy revocation took %s, budget is 250ms", elapsed)
	}
	if db.writes != 1 {
		t.Fatalf("policy writes=%d, want one", db.writes)
	}
	if _, ok := cachedChatGet(cache, cache.policies, chatCacheKey("host", "conversation")); ok {
		t.Fatal("policy cache remained readable after update returned")
	}
	for name, sub := range map[string]*chatstream.Subscription{"queued": queued, "idle": idle} {
		select {
		case <-sub.Done():
		default:
			t.Fatalf("%s stream was still open when policy update returned", name)
		}
	}
}

func TestTodo_CHAT_020_Security_QueuedDeliveryFailsAfterPolicyUpdate(t *testing.T) {
	at := time.Now().UTC()
	stream := newPolicyRevocationStream(t)
	grants := NewChatCompanyGrants(chatAdminFacts{role: "hcm_admin"}, chatauthority.New(&policyRevocationDB{})).withRevocation(nil, stream)
	sub, err := stream.Watch(context.Background(), chatstream.WatchRequest{TenantID: "host", HomeTenantID: "guest", SubjectID: "member", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if err := stream.Publish(context.Background(), chatstream.Event{TenantID: "host", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1, Payload: []byte("must not escape")}); err != nil {
		t.Fatal(err)
	}
	if err := setRestrictiveChannelPolicy(policyAdminContext(t, at), grants, at); err != nil {
		t.Fatal(err)
	}
	if event, err := sub.Next(context.Background()); !errors.Is(err, chatstream.ErrRevoked) || len(event.Payload) != 0 {
		t.Fatalf("queued event after policy update=%+v err=%v", event, err)
	}
}

func TestTodo_CHAT_020_Fault_FailedPolicyWriteKeepsCurrentAuthority(t *testing.T) {
	at := time.Now().UTC()
	db := &policyRevocationDB{failPolicyWrite: true}
	cache := newChatAuthorityCache(time.Minute, nil)
	cachedChatPut(cache, cache.policies, chatCacheKey("host", "conversation"), cachedChatPolicy{channel: chatpolicy.Channel{Revision: 1}})
	stream := newPolicyRevocationStream(t)
	grants := NewChatCompanyGrants(chatAdminFacts{role: "hcm_admin"}, chatauthority.New(db)).withRevocation(cache, stream)
	sub, err := stream.Watch(context.Background(), chatstream.WatchRequest{TenantID: "host", HomeTenantID: "host", SubjectID: "member", ConversationID: "conversation", MembershipEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if err := setRestrictiveChannelPolicy(policyAdminContext(t, at), grants, at); err == nil {
		t.Fatal("failed policy write returned success")
	}
	if _, ok := cachedChatGet(cache, cache.policies, chatCacheKey("host", "conversation")); !ok {
		t.Fatal("failed write invalidated the still-current policy")
	}
	if err := stream.Publish(context.Background(), chatstream.Event{TenantID: "host", ConversationID: "conversation", Sequence: 1, MembershipEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	if event, err := sub.Next(context.Background()); err != nil || event.Sequence != 1 {
		t.Fatalf("failed policy write closed current stream: event=%+v err=%v", event, err)
	}
}

func TestTodo_CHAT_020_Security_SearchAndMediaDeniedAfterPolicyUpdate(t *testing.T) {
	at := time.Now().UTC()
	db := &policyRevocationDB{}
	cache := newChatAuthorityCache(time.Minute, nil)
	store := &policyReadStore{}
	service := chatcore.NewService(store, func() time.Time { return at })
	service.SetAuthority(newChatCurrentAuthority(policyReadFacts{}, store, chatauthority.New(db), cache))
	principal := chatcore.Principal{TenantID: "host", SubjectID: "member"}
	memberCtx := func() context.Context {
		p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "member", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-member", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "policy-member"})
		if err != nil {
			t.Fatal(err)
		}
		return trust.WithPrincipal(context.Background(), p)
	}
	if _, err := service.GetConversation(memberCtx(), chatcore.GetConversationRequest{Principal: principal, TenantID: "host", ConversationID: "conversation"}); err != nil {
		t.Fatalf("initial policy should allow the current member: %v", err)
	}
	grants := NewChatCompanyGrants(chatAdminFacts{role: "hcm_admin"}, chatauthority.New(db)).withRevocation(cache, nil)
	if err := setRestrictiveChannelPolicy(policyAdminContext(t, at), grants, at); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Search(memberCtx(), chatcore.SearchRequest{Principal: principal, TenantID: "host", Query: "secret", ConversationID: "conversation"}); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("search after policy update err=%v, want permission denied", err)
	}
	if store.searchCalls != 0 {
		t.Fatalf("search backend called %d times after revocation", store.searchCalls)
	}
	mediaCtx := memberCtx()
	p, _ := trust.FromContext(mediaCtx)
	mediaCtx = transport.WithInvocation(mediaCtx, &transport.Invocation{})
	mediaCtx = trust.WithPrincipal(mediaCtx, p)
	extensions := &ChatExtensions{Conversations: service}
	if err := extensions.AuthorizeMedia(mediaCtx, chatmedia.AccessRequest{TenantID: "host", ConversationID: "conversation", PrincipalID: "member"}); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("media authorization after policy update err=%v, want permission denied", err)
	}
}
