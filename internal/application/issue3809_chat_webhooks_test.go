package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type issue3809Store struct {
	allowed  map[string]bool
	calls    int
	posts    chatstore.WebhookPostPage
	postsErr error
	after    int64
}

func (s *issue3809Store) AllowWebhookChannel(_ context.Context, tenant, conversation, actor string) (bool, error) {
	s.calls++
	s.allowed[tenant+"/"+conversation] = true
	return true, nil
}

func (s *issue3809Store) RemoveWebhookChannel(_ context.Context, tenant, conversation, actor string) (bool, error) {
	s.calls++
	delete(s.allowed, tenant+"/"+conversation)
	return true, nil
}

func (s *issue3809Store) ListWebhookChannels(_ context.Context, tenant string) ([]chatstore.WebhookChannel, error) {
	s.calls++
	return []chatstore.WebhookChannel{{TenantID: tenant, ConversationID: "pub-a"}}, nil
}

func (s *issue3809Store) ListWebhookPosts(_ context.Context, _ string, after int64, _ int) (chatstore.WebhookPostPage, error) {
	s.after = after
	return s.posts, s.postsErr
}

type issue3809Admins map[string]bool

func (a issue3809Admins) CanManageChatWebhooks(_ context.Context, tenant, subject string) bool {
	return a[tenant+"/"+subject]
}

type issue3809Subs struct {
	revisions []subscription.EventSubscription
	grants    map[string]subscription.ScopeGrant
}

func (s issue3809Subs) ActiveChatWebhookSubscriptions(context.Context, string) ([]subscription.EventSubscription, map[string]subscription.ScopeGrant, error) {
	return s.revisions, s.grants, nil
}

type issue3809Signers map[string]*subscription.CredentialRing

func (s issue3809Signers) SignerFor(destination string) *subscription.CredentialRing {
	return s[destination]
}

type issue3809Provider struct{ attempts []ChatWebhookAttempt }

func (p *issue3809Provider) DeliverChatEvent(_ context.Context, a ChatWebhookAttempt) (subscription.ProviderReceipt, error) {
	p.attempts = append(p.attempts, a)
	return subscription.ProviderReceipt{ReceiptID: "r", Provider: "test", IdempotencyKey: a.Delivery.Request.IdempotencyKey, Destination: a.Delivery.Request.Destination, Accepted: true, ReceivedAt: a.Delivery.AttemptedAt}, nil
}

func issue3809Ring(t *testing.T, destination string, now time.Time) *subscription.CredentialRing {
	t.Helper()
	provider, err := subscription.NewHMACProvider("key-1", []byte("chat-webhook-test-signing-key-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := subscription.NewCredentialRing(destination, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(subscription.SigningCredential{Destination: destination, Profile: "chat-v1", Version: "v1", KeyRef: "key-1", NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), Provider: provider}); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", now); err != nil {
		t.Fatal(err)
	}
	return ring
}

func issue3809Subscription(t *testing.T, id, tenant, endpoint string, fields []string, mutate func(*subscription.RevisionRequest)) (subscription.EventSubscription, subscription.ScopeGrant) {
	t.Helper()
	kind := subscription.EventChatMessagePosted
	req := subscription.RevisionRequest{
		SubscriptionID: id, Requester: "requester", Subscriber: subscription.Subscriber{PartnerRef: "partner-1"},
		EventKinds: []subscription.EventKind{kind}, DeclaredFields: map[subscription.EventKind][]string{kind: fields},
		DeliveryEndpointRef: endpoint, DeliveryGuarantee: subscription.GuaranteeAtLeastOnce, TenantScope: tenant,
	}
	if mutate != nil {
		mutate(&req)
	}
	draft, err := subscription.NewDraft(req)
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("requester", "approver")
	if err != nil {
		t.Fatal(err)
	}
	return active, subscription.ScopeGrant{PartnerRef: "partner-1", TenantScope: tenant, Purpose: "incident-bridge", EventKinds: []subscription.EventKind{kind},
		Resources: subscription.RequiredResources(active), Fields: map[subscription.EventKind][]string{kind: fields}}
}

func issue3809Post(id int64) chatstore.WebhookPost {
	return chatstore.WebhookPost{OutboxID: id, TenantID: "host", ConversationID: "pub-a", ConversationName: "incidents", PostID: "p" + string(rune('0'+id)), Author: "u-2", Body: "disk full on db-3", Sequence: id, PostedAt: time.Unix(1700000000, 0).UTC()}
}

func issue3809Service(t *testing.T, now time.Time, store *issue3809Store, subs issue3809Subs, endpoints ...string) (*ChatWebhooks, *issue3809Provider) {
	t.Helper()
	signers := issue3809Signers{}
	for _, endpoint := range endpoints {
		signers[endpoint] = issue3809Ring(t, endpoint, now)
	}
	provider := &issue3809Provider{}
	return &ChatWebhooks{Store: store, Admins: issue3809Admins{"host/admin": true}, Subscriptions: subs, Signers: signers,
		Journal: subscription.NewDeliveryJournal(func() time.Time { return now }), Provider: provider, Now: func() time.Time { return now }}, provider
}

func TestIssue3809_ChatWebhookSchemas(t *testing.T) {
	registry := ChatWebhookSchemas()
	if registry == nil {
		t.Fatal("schemas did not register")
	}
	for _, kind := range []subscription.EventKind{subscription.EventChatMessagePosted, subscription.EventChatApprovalDecided} {
		if _, ok := registry.Schema(kind, ChatWebhookSchemaVersion); !ok {
			t.Fatalf("no schema for %s", kind)
		}
	}
	schema, _ := registry.Schema(subscription.EventChatMessagePosted, ChatWebhookSchemaVersion)
	for _, f := range schema.Fields {
		if (f.Name == "body" || f.Name == "author_id") && f.Classification != subscription.ClassificationConfidential {
			t.Fatalf("%s is not confidential", f.Name)
		}
	}
}

func TestIssue3809_OnlyAdministratorsManageChannels_Security(t *testing.T) {
	now := time.Unix(1700000100, 0).UTC()
	store := &issue3809Store{allowed: map[string]bool{}}
	svc, _ := issue3809Service(t, now, store, issue3809Subs{})
	ctx := context.Background()
	member := chat.Principal{TenantID: "host", SubjectID: "member"}
	if _, err := svc.AllowChannel(ctx, member, "pub-a"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member allowed a channel: %v", err)
	}
	if _, err := svc.RemoveChannel(ctx, member, "pub-a"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member removed a channel: %v", err)
	}
	if _, err := svc.Channels(ctx, member); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member listed channels: %v", err)
	}
	// An administrator of another company is not one here.
	if _, err := svc.AllowChannel(ctx, chat.Principal{TenantID: "other", SubjectID: "admin"}, "pub-a"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("foreign administrator: %v", err)
	}
	if _, err := svc.AllowChannel(ctx, chat.Principal{}, "pub-a"); err == nil {
		t.Fatal("anonymous caller allowed a channel")
	}
	if store.calls != 0 {
		t.Fatalf("the store was reached %d times before authorization", store.calls)
	}
	// A service identity, even one named like the administrator, is not a person.
	at := time.Now().UTC()
	machine, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "admin", SubjectKind: trust.SubjectKindService, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "s", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "d"})
	if err == nil {
		if _, err := svc.AllowChannel(trust.WithPrincipal(ctx, machine), chat.Principal{TenantID: "host", SubjectID: "admin"}, "pub-a"); !errors.Is(err, chat.ErrPermissionDenied) || store.calls != 0 {
			t.Fatalf("service identity: %v calls=%d", err, store.calls)
		}
	}
	admin := chat.Principal{TenantID: "host", SubjectID: "admin"}
	if added, err := svc.AllowChannel(ctx, admin, "pub-a"); err != nil || !added || !store.allowed["host/pub-a"] {
		t.Fatalf("administrator: %v %v", added, err)
	}
	if list, err := svc.Channels(ctx, admin); err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if removed, err := svc.RemoveChannel(ctx, admin, "pub-a"); err != nil || !removed || store.allowed["host/pub-a"] {
		t.Fatalf("remove: %v %v", removed, err)
	}
	var noAdmins ChatWebhooks
	if _, err := noAdmins.AllowChannel(ctx, admin, "pub-a"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("unwired service: %v", err)
	}
}

func TestIssue3809_PublishMasksFieldsAndSigns_Integration(t *testing.T) {
	now := time.Unix(1700000200, 0).UTC()
	narrow, narrowGrant := issue3809Subscription(t, "sub-narrow", "host", "endpoint:narrow", []string{"conversation_id", "post_id", "sequence"}, nil)
	wide, wideGrant := issue3809Subscription(t, "sub-wide", "host", "endpoint:wide", []string{"conversation_id", "conversation_name", "post_id", "body", "posted_at", "sequence"}, nil)
	store := &issue3809Store{allowed: map[string]bool{}, posts: chatstore.WebhookPostPage{Posts: []chatstore.WebhookPost{issue3809Post(7)}, Through: 9}}
	svc, provider := issue3809Service(t, now, store, issue3809Subs{revisions: []subscription.EventSubscription{narrow, wide}, grants: map[string]subscription.ScopeGrant{"sub-narrow": narrowGrant, "sub-wide": wideGrant}}, "endpoint:narrow", "endpoint:wide")

	page, err := svc.PublishMessages(context.Background(), "host", 3, 50)
	if err != nil || len(page.Deliveries) != 2 || len(provider.attempts) != 2 || page.Through != 9 || store.after != 3 {
		t.Fatalf("publish: deliveries=%d calls=%d through=%d after=%d err=%v", len(page.Deliveries), len(provider.attempts), page.Through, store.after, err)
	}
	for _, attempt := range provider.attempts {
		sub := attempt.Delivery.Request.SubscriptionID
		payload := attempt.Event.Payload
		switch sub {
		case "sub-narrow":
			if len(payload) != 3 || payload["post_id"] == nil || payload["body"] != nil || payload["author_id"] != nil {
				t.Fatalf("narrow payload leaked or lost fields: %v", payload)
			}
		case "sub-wide":
			if payload["body"] != "disk full on db-3" || payload["author_id"] != nil || payload["conversation_name"] != "incidents" {
				t.Fatalf("wide payload: %v", payload)
			}
		default:
			t.Fatalf("delivery to %q", sub)
		}
		if attempt.Event.EventType != "chat.message.posted" || attempt.Event.Sequence != 7 {
			t.Fatalf("event: %+v", attempt.Event)
		}
		ring := issue3809Ring(t, attempt.Delivery.Request.Destination, now)
		if err := VerifyChatWebhookEvent(attempt.Event, ring, now); err != nil {
			t.Fatalf("%s: signature: %v", sub, err)
		}
		// A changed payload no longer matches what was signed.
		tampered := attempt.Event
		tampered.Payload = map[string]any{"body": "something else"}
		if err := VerifyChatWebhookEvent(tampered, ring, now); err == nil {
			t.Fatalf("%s: tampered payload verified", sub)
		}
	}
	// The same page again is the same journaled operation, not a second send.
	again, err := svc.PublishMessages(context.Background(), "host", 3, 50)
	if err != nil || len(again.Deliveries) != 2 || !again.Deliveries[0].Result.Duplicate || len(provider.attempts) != 2 {
		t.Fatalf("replay: %+v calls=%d err=%v", again.Deliveries, len(provider.attempts), err)
	}
}

func TestIssue3809_PublishRefusesUnprovenSubscriptions_Security(t *testing.T) {
	now := time.Unix(1700000300, 0).UTC()
	good, goodGrant := issue3809Subscription(t, "sub-good", "host", "endpoint:good", []string{"post_id"}, nil)
	otherTenant, otherGrant := issue3809Subscription(t, "sub-other", "other", "endpoint:good", []string{"post_id"}, nil)
	scoped, scopedGrant := issue3809Subscription(t, "sub-org", "host", "endpoint:good", []string{"post_id"}, func(r *subscription.RevisionRequest) { r.OrganizationScopeRef = "org-1" })
	noSigner, noSignerGrant := issue3809Subscription(t, "sub-nosigner", "host", "endpoint:unsigned", []string{"post_id"}, nil)
	// A grant narrower than the subscription: the body is declared but not granted.
	over, overGrant := issue3809Subscription(t, "sub-over", "host", "endpoint:good", []string{"post_id", "body"}, nil)
	overGrant.Fields = map[subscription.EventKind][]string{subscription.EventChatMessagePosted: {"post_id"}}
	// A subscription that asked only for another kind of event.
	clockOnly, clockGrant := issue3809Subscription(t, "sub-kind", "host", "endpoint:good", []string{"post_id"}, func(r *subscription.RevisionRequest) {
		r.EventKinds = []subscription.EventKind{subscription.EventChatApprovalDecided}
		r.DeclaredFields = map[subscription.EventKind][]string{subscription.EventChatApprovalDecided: {"decision"}}
	})
	clockGrant.EventKinds = []subscription.EventKind{subscription.EventChatApprovalDecided}
	clockGrant.Fields = map[subscription.EventKind][]string{subscription.EventChatApprovalDecided: {"decision"}}

	store := &issue3809Store{allowed: map[string]bool{}, posts: chatstore.WebhookPostPage{Posts: []chatstore.WebhookPost{issue3809Post(1)}, Through: 1}}
	svc, provider := issue3809Service(t, now, store, issue3809Subs{
		revisions: []subscription.EventSubscription{good, otherTenant, scoped, noSigner, over, clockOnly},
		grants:    map[string]subscription.ScopeGrant{"sub-good": goodGrant, "sub-other": otherGrant, "sub-org": scopedGrant, "sub-nosigner": noSignerGrant, "sub-over": overGrant, "sub-kind": clockGrant},
	}, "endpoint:good")
	page, err := svc.PublishMessages(context.Background(), "host", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Deliveries) != 1 || page.Deliveries[0].SubscriptionID != "sub-good" || len(provider.attempts) != 1 {
		t.Fatalf("delivered to: %+v", page.Deliveries)
	}
	if page.Skipped < 3 {
		t.Fatalf("skipped %d subscriptions", page.Skipped)
	}
	for _, a := range provider.attempts {
		if strings.Contains(a.Delivery.Request.SubscriptionID, "other") {
			t.Fatal("another company's subscription received a message")
		}
	}
}

func TestIssue3809_PublishWithoutSubscriptionsConsumesPage(t *testing.T) {
	now := time.Unix(1700000400, 0).UTC()
	store := &issue3809Store{allowed: map[string]bool{}, posts: chatstore.WebhookPostPage{Posts: []chatstore.WebhookPost{issue3809Post(1)}, Through: 4}}
	svc, provider := issue3809Service(t, now, store, issue3809Subs{})
	page, err := svc.PublishMessages(context.Background(), "host", 0, 10)
	if err != nil || len(page.Deliveries) != 0 || page.Through != 4 || len(provider.attempts) != 0 {
		t.Fatalf("page: %+v %v", page, err)
	}
	for _, bad := range []struct {
		tenant string
		after  int64
		limit  int
	}{{"", 0, 10}, {"host", -1, 10}, {"host", 0, 0}, {"host", 0, ChatWebhookMaxBatch + 1}} {
		if _, err := svc.PublishMessages(context.Background(), bad.tenant, bad.after, bad.limit); !errors.Is(err, ErrChatWebhooksUnavailable) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
}

func TestIssue3809_PublishDoesNotAdvanceOnStoreFailure(t *testing.T) {
	now := time.Unix(1700000500, 0).UTC()
	sub, grant := issue3809Subscription(t, "sub-good", "host", "endpoint:good", []string{"post_id"}, nil)
	store := &issue3809Store{allowed: map[string]bool{}, postsErr: errors.New("store down")}
	svc, provider := issue3809Service(t, now, store, issue3809Subs{revisions: []subscription.EventSubscription{sub}, grants: map[string]subscription.ScopeGrant{"sub-good": grant}}, "endpoint:good")
	page, err := svc.PublishMessages(context.Background(), "host", 5, 10)
	if err == nil || page.Through != 0 || len(provider.attempts) != 0 {
		t.Fatalf("store failure: %+v %v", page, err)
	}
}
