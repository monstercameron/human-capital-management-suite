package application

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transportcell "github.com/monstercameron/human-capital-management-suite/internal/transport/cell"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/list"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type notificationJourneyAuthorityStub struct {
	journey      workspace.JourneySummary
	currentOwner string
}

func (s *notificationJourneyAuthorityStub) ListJourneys(ctx context.Context) ([]workspace.JourneySummary, error) {
	if _, err := trust.MustFromContext(ctx); err != nil {
		return nil, err
	}
	return []workspace.JourneySummary{s.journey}, nil
}

func (s *notificationJourneyAuthorityStub) WorkflowNotifications(ctx context.Context, journeys []workspace.JourneySummary) ([]workspace.WorkflowNotification, error) {
	p, err := trust.MustFromContext(ctx)
	if err != nil || p.Subject() != s.currentOwner || len(journeys) != 1 {
		return nil, nil
	}
	return []workspace.WorkflowNotification{{JourneyID: s.journey.IntentID, WorkItemID: "current-owner-item", Purpose: "APPROVAL", Status: "ASSIGNED"}}, nil
}

type stubVisibility struct {
	hidden map[uuid.UUID]bool
	fail   bool
}

func (s *stubVisibility) VisibleWorkflows(_ context.Context, _ uuid.UUID, _ string, instanceIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if s.fail {
		return nil, fmt.Errorf("visibility backend down")
	}
	out := make(map[uuid.UUID]bool, len(instanceIDs))
	for _, id := range instanceIDs {
		out[id] = !s.hidden[id]
	}
	return out, nil
}

type notificationFixture struct {
	feed    *NotificationFeed
	db      *pgtest.DB
	tenant  uuid.UUID
	tenants map[values.TenantId]uuid.UUID
	vis     *stubVisibility
	secret  string
	now     time.Time
}

func newNotificationFixture(t *testing.T) *notificationFixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,'naas4','cell-local','tenant naas4','ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant)
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	fx := &notificationFixture{
		db:      db,
		tenant:  tenant,
		tenants: map[values.TenantId]uuid.UUID{"naas4": tenant},
		vis:     &stubVisibility{hidden: map[uuid.UUID]bool{}},
		secret:  "naas4-test-cursor-secret-min-32-bytes-long",
		now:     time.Now().UTC(),
	}
	feed, err := NewNotificationFeed(conn, func(key values.TenantId) (uuid.UUID, error) {
		id, ok := fx.tenants[key]
		if !ok {
			return uuid.Nil, fmt.Errorf("unknown tenant %q", key)
		}
		return id, nil
	}, fx.vis, fx.secret, func() time.Time { return fx.now })
	if err != nil {
		t.Fatalf("NewNotificationFeed: %v", err)
	}
	fx.feed = feed
	return fx
}

func (fx *notificationFixture) principal(t *testing.T, tenant values.TenantId, subject string) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-1", Purposes: []string{"notifications"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-1", IssuedAt: fx.now.Add(-time.Hour), ExpiresAt: fx.now.Add(time.Hour),
		CredentialDigest: "cred:sha256:1",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func (fx *notificationFixture) publish(t *testing.T, subject, purpose, correlation string, instanceID uuid.UUID, at time.Time) inbox.Record {
	t.Helper()
	conn, ok := fx.feed.db.(*pgxadapter.Conn)
	if !ok {
		t.Fatal("feed database is not a pgx connection")
	}
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rec, err := (inbox.Store{}).PublishWorkflow(context.Background(), tx, inbox.WorkflowNotice{
		TenantID: fx.tenant, WorkItemID: uuid.New(), InstanceID: instanceID,
		SubjectRef: subject, Purpose: purpose, CorrelationID: correlation,
		AudienceDigest: "resolution", CreatedAt: at,
	})
	if err != nil {
		t.Fatalf("PublishWorkflow: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return rec
}

func envelopeCode(t *testing.T, err error) envelope.Code {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	envErr, ok := envelope.As(err)
	if !ok {
		t.Fatalf("error %v is not an envelope error", err)
	}
	return envErr.Code()
}

// TestTodo_REV_011_01 proves an inbox row reaches the authenticated recipient
// through the served feed, whose visibility port is checked before disclosure.
func TestTodo_REV_011_01(t *testing.T) {
	fx := newNotificationFixture(t)
	instance := uuid.New()
	seed := fx.publish(t, "approval-owner", "APPROVAL", "corr-rev-011", instance, fx.now.Add(-time.Minute))

	owner, err := fx.feed.ListNotifications(fx.principal(t, "naas4", "approval-owner"), &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil {
		t.Fatalf("owner feed: %v", err)
	}
	if len(owner.Notifications) != 1 || owner.Notifications[0].Id != seed.InboxRecordID.String() {
		t.Fatalf("owner feed = %+v, want the routed approval notice", owner.Notifications)
	}

	other, err := fx.feed.ListNotifications(fx.principal(t, "naas4", "other-principal"), &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil {
		t.Fatalf("other recipient feed: %v", err)
	}
	if len(other.Notifications) != 0 {
		t.Fatalf("another recipient received the approval notice: %+v", other.Notifications)
	}
}

// TestTodo_REV_011_01_Security proves recipient scoping and the current
// workflow visibility check both withhold the notice from unauthorized views.
func TestTodo_REV_011_01_Security(t *testing.T) {
	fx := newNotificationFixture(t)
	instance := uuid.New()
	fx.publish(t, "approval-owner", "APPROVAL", "corr-rev-011-security", instance, fx.now.Add(-time.Minute))
	ctx := fx.principal(t, "naas4", "approval-owner")

	fx.vis.hidden[instance] = true
	withheld, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil {
		t.Fatalf("withheld feed: %v", err)
	}
	if len(withheld.Notifications) != 0 {
		t.Fatalf("currently hidden workflow reached the caller: %+v", withheld.Notifications)
	}

	fx.vis.hidden[instance] = false
	otherTenant := fx.principal(t, "naas4-ghost", "approval-owner")
	_, crossTenantErr := fx.feed.ListNotifications(otherTenant, &notificationv1.ListNotificationsRequest{PageSize: 10})
	if code := envelopeCode(t, crossTenantErr); code != envelope.CodePermissionDenied {
		t.Fatalf("cross-tenant feed code = %v, want PermissionDenied", code)
	}
}

func TestTodo_REV_011_01_ServedGRPC(t *testing.T) {
	fx := newNotificationFixture(t)
	instance := uuid.New()
	seed := fx.publish(t, "approval-owner", "APPROVAL", "corr-rev-011-grpc", instance, fx.now.Add(-time.Minute))
	ownerCtx := fx.principal(t, "naas4", "approval-owner")
	owner, _ := trust.MustFromContext(ownerCtx)
	otherCtx := fx.principal(t, "naas4", "another-approver")
	other, _ := trust.MustFromContext(otherCtx)
	authority := &notificationJourneyAuthorityStub{currentOwner: "approval-owner", journey: workspace.JourneySummary{IntentID: "intent-current", InstanceID: instance.String()}}
	feed, err := NewNotificationFeed(fx.feed.db, func(key values.TenantId) (uuid.UUID, error) {
		id, ok := fx.tenants[key]
		if !ok {
			return uuid.Nil, fmt.Errorf("unknown tenant %q", key)
		}
		return id, nil
	}, journeyNotificationVisibility{engine: authority, notifications: authority}, fx.secret, func() time.Time { return fx.now })
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		p := owner
		if len(md.Get("x-test-subject")) > 0 && md.Get("x-test-subject")[0] == other.Subject() {
			p = other
		}
		return handler(trust.WithPrincipal(ctx, p), req)
	}))
	transportcell.RegisterNotificationFeed(server, feed)
	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := notificationv1.NewNotificationServiceClient(conn)
	ownerResponse, err := client.ListNotifications(context.Background(), &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil {
		t.Fatalf("owner served notification feed: %v", err)
	}
	if len(ownerResponse.Notifications) != 1 || ownerResponse.Notifications[0].Id != seed.InboxRecordID.String() {
		t.Fatalf("owner served feed = %+v, want current assigned approver's notice", ownerResponse.Notifications)
	}
	otherResponse, err := client.ListNotifications(metadata.AppendToOutgoingContext(context.Background(), "x-test-subject", other.Subject()), &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil {
		t.Fatalf("other served notification feed: %v", err)
	}
	if len(otherResponse.Notifications) != 0 {
		t.Fatalf("unassigned principal received approval notice: %+v", otherResponse.Notifications)
	}
	authority.currentOwner = "another-approver"
	revoked, err := client.ListNotifications(context.Background(), &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil {
		t.Fatalf("revoked recipient feed: %v", err)
	}
	if len(revoked.Notifications) != 0 {
		t.Fatalf("stale inbox recipient retained visibility after reassignment: %+v", revoked.Notifications)
	}
}

// TestTodo_NAAS_004_Security proves the feed fails closed: no principal, no
// data; forged, expired, cross-principal, cross-tenant and cross-filter
// cursors are refused before any storage read; wrong-version and
// foreign-tenant mutations are refused; a visibility failure surfaces
// Unavailable instead of a partial list; and withheld instances never reach
// the caller.
func TestTodo_NAAS_004_Security(t *testing.T) {
	fx := newNotificationFixture(t)
	ctx := fx.principal(t, "naas4", "recipient-1")
	at := fx.now.Add(-time.Minute).Truncate(time.Second)
	instance := uuid.New()
	seed := fx.publish(t, "recipient-1", "APPROVAL", "corr-1", instance, at)

	if _, err := fx.feed.ListNotifications(context.Background(), &notificationv1.ListNotificationsRequest{}); envelopeCode(t, err) != envelope.CodeUnauthenticated {
		t.Fatalf("anonymous list code = %v, want Unauthenticated", err)
	}

	first, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first.Notifications) != 1 || first.Notifications[0].Id != seed.InboxRecordID.String() {
		t.Fatalf("list = %+v, want the seeded notice", first.Notifications)
	}
	if first.Notifications[0].WorkflowInstanceId != instance.String() || first.Notifications[0].Purpose != "APPROVAL" {
		t.Fatalf("notice carries no workflow reference: %+v", first.Notifications[0])
	}

	mint := func(p list.CursorPayload) string {
		token, err := list.EncodeCursor(p, fx.secret)
		if err != nil {
			t.Fatalf("EncodeCursor: %v", err)
		}
		return token
	}
	base := list.CursorPayload{Principal: "recipient-1", Tenant: fx.tenant.String(), Version: 1, ExpiresAt: fx.now.Add(time.Hour).Unix(), Nonce: "n1"}
	cases := map[string]string{
		"forged signature": mint(base)[:len(mint(base))-2] + "ff",
		"expired": func() string {
			p := base
			p.ExpiresAt = fx.now.Add(-time.Minute).Unix()
			return mint(p)
		}(),
		"cross-principal": func() string {
			p := base
			p.Principal = "recipient-2"
			return mint(p)
		}(),
		"cross-filter": func() string {
			p := base
			p.Filter = filterKey("TASK", "", false)
			return mint(p)
		}(),
		"garbage": "not-a-cursor",
	}
	for name, cursor := range cases {
		req := &notificationv1.ListNotificationsRequest{PageSize: 10, Cursor: cursor}
		if _, err := fx.feed.ListNotifications(ctx, req); envelopeCode(t, err) != envelope.CodeInvalidArgument {
			t.Fatalf("%s cursor: code = %v, want InvalidArgument", name, err)
		}
	}

	read, err := fx.feed.MarkNotificationRead(ctx, &notificationv1.MarkNotificationReadRequest{Id: seed.InboxRecordID.String(), ExpectedVersion: seed.Version})
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if read.Notification.ReadState != "READ" || read.Notification.Version != seed.Version+1 {
		t.Fatalf("read = %+v, want READ at version %d", read.Notification, seed.Version+1)
	}
	if _, err := fx.feed.MarkNotificationRead(ctx, &notificationv1.MarkNotificationReadRequest{Id: seed.InboxRecordID.String(), ExpectedVersion: seed.Version}); envelopeCode(t, err) != envelope.CodeFailedPrecondition {
		t.Fatalf("stale CAS: code = %v, want FailedPrecondition", err)
	}

	archived, err := fx.feed.ArchiveNotification(ctx, &notificationv1.ArchiveNotificationRequest{Id: seed.InboxRecordID.String(), Archived: true, ExpectedVersion: seed.Version + 1})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archived.Notification.Archived {
		t.Fatalf("archive = %+v, want archived", archived.Notification)
	}
	pinned, err := fx.feed.PinNotification(ctx, &notificationv1.PinNotificationRequest{Id: seed.InboxRecordID.String(), Pinned: true, ExpectedVersion: seed.Version + 2})
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	if !pinned.Notification.Pinned {
		t.Fatalf("pin = %+v, want pinned", pinned.Notification)
	}

	otherCtx := fx.principal(t, "naas4", "recipient-2")
	if _, err := fx.feed.MarkNotificationRead(otherCtx, &notificationv1.MarkNotificationReadRequest{Id: seed.InboxRecordID.String(), ExpectedVersion: seed.Version + 3}); err == nil {
		t.Fatal("cross-recipient mutation = success, want refusal")
	}
	_, ghostErr := fx.feed.MarkNotificationRead(ctx, &notificationv1.MarkNotificationReadRequest{Id: uuid.New().String(), ExpectedVersion: 1})
	if code := envelopeCode(t, ghostErr); code != envelope.CodeFailedPrecondition && code != envelope.CodeNotFound {
		t.Fatalf("ghost mutation: code = %v, want FailedPrecondition or NotFound", code)
	}
	if _, err := fx.feed.MarkNotificationRead(ctx, &notificationv1.MarkNotificationReadRequest{Id: "not-a-uuid", ExpectedVersion: 1}); envelopeCode(t, err) != envelope.CodeInvalidArgument {
		t.Fatalf("malformed id: code = %v, want InvalidArgument", err)
	}
	if _, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{PageSize: 500}); envelopeCode(t, err) != envelope.CodeInvalidArgument {
		t.Fatalf("oversize page: code = %v, want InvalidArgument", err)
	}

	fx.vis.fail = true
	if _, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{}); envelopeCode(t, err) != envelope.CodeUnavailable {
		t.Fatalf("visibility outage: code = %v, want Unavailable, never partial", err)
	}
	fx.vis.fail = false

	fx.vis.hidden[instance] = true
	hidden, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{})
	if err != nil {
		t.Fatalf("withheld list: %v", err)
	}
	for _, n := range hidden.Notifications {
		if n.Id == seed.InboxRecordID.String() {
			t.Fatal("withheld instance reached the caller")
		}
	}

	unknownCtx := fx.principal(t, "naas4-ghost", "recipient-1")
	if _, err := fx.feed.ListNotifications(unknownCtx, &notificationv1.ListNotificationsRequest{}); envelopeCode(t, err) != envelope.CodePermissionDenied {
		t.Fatalf("unknown tenant: code = %v, want PermissionDenied", err)
	}
}

// TestTodo_NAAS_004_Performance proves bounded-batch traversal: 250 seeded
// notices page through at size 25 with every record returned exactly once
// and the cursor exhausting cleanly, without ListJourneys or full-history
// materialization.
func TestTodo_NAAS_004_Performance(t *testing.T) {
	fx := newNotificationFixture(t)
	ctx := fx.principal(t, "naas4", "recipient-9")
	const total = 250
	for i := 0; i < total; i++ {
		fx.publish(t, "recipient-9", "TASK", fmt.Sprintf("corr-%03d", i), uuid.New(), fx.now.Add(-time.Duration(total-i)*time.Second).Truncate(time.Second))
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		resp, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{PageSize: 25, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		for _, n := range resp.Notifications {
			if seen[n.Id] {
				t.Fatalf("duplicate record %s across pages", n.Id)
			}
			seen[n.Id] = true
		}
		cursor = resp.NextCursor
		if cursor == "" {
			break
		}
		if pages > 20 {
			t.Fatal("traversal did not exhaust within 20 pages")
		}
	}
	if len(seen) != total {
		t.Fatalf("traversed %d records, want %d", len(seen), total)
	}
	if pages != total/25 {
		t.Fatalf("pages = %d, want %d bounded pages", pages, total/25)
	}
}
