package application

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	positionv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/position/v1"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/positionfacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportposition "github.com/monstercameron/human-capital-management-suite/internal/transport/position"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_REV_076_03_Integration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	tenantKey := values.TenantId("rev-076-03-position-read")
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'REV-076-03 position read', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID, string(tenantKey))
	positionID := seedREV076Position(t, db, tenantID)
	seedREV076Occupant(t, db, tenantID, positionID)
	reader := positionfacts.Reader{DB: db.Conn, TenantUUID: func(tenant values.TenantId) uuid.UUID {
		if tenant == tenantKey {
			return tenantID
		}
		return uuid.Nil
	}}
	asOf := position.AsOf{
		EffectiveOn: mustREV076Date(t, "2026-06-01"),
		KnownAt:     mustREV076KnownAt(t, "2026-05-15T00:00:00Z"),
	}
	positionRef := values.EntityRef{Tenant: tenantKey, Kind: position.KindPosition, Id: positionID.String()}
	revision, exists, err := reader.PositionRevisionAt(ctx, position.PositionQuery{Tenant: tenantKey, Position: positionRef, AsOf: asOf})
	if err != nil || !exists {
		t.Fatalf("real position lookup: exists=%v err=%v", exists, err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenantKey, Subject: "viewer-rev-076", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "ENGINEERING", AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-rev-076", IssuedAt: time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), CredentialDigest: "credential-rev-076",
	})
	if err != nil {
		t.Fatal(err)
	}
	authorized := true
	service := PositionReadService{
		Facts: reader, Directory: reader,
		CanView: func(_ context.Context, viewer *trust.Principal, page string) bool {
			return viewer == principal && (page == "position-object" || page == "position-occupancy")
		},
		Authorize: func(_ context.Context, viewer *trust.Principal, page string, resolved position.PositionRevision) bool {
			return authorized && viewer == principal && viewer.Tenant() == tenantKey && viewer.Subject() == "viewer-rev-076" &&
				resolved.Position == positionRef && resolved.OrgUnit == viewer.OrganizationScopeID() &&
				(page == "position-object" || page == "position-occupancy")
		},
		Now: func() time.Time { return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC) },
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = transport.WithInvocation(trust.WithPrincipal(ctx, principal), &transport.Invocation{})
		return handler(ctx, req)
	}))
	transportposition.Register(server, positionTransportDependencies(service))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///position-test", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := positionv1.NewPositionServiceClient(conn)
	objectOptions, err := client.ListPositionObjectOptions(ctx, &positionv1.ListPositionObjectOptionsRequest{})
	if err != nil || len(objectOptions.GetOptions()) != 1 {
		t.Fatalf("served object selector options = %+v, %v", objectOptions, err)
	}
	ref := objectOptions.GetOptions()[0].GetPositionRevisionRef()
	selectedPosition, selectedRevision, err := position.RevisionRef(ref).Decode()
	if err != nil || selectedPosition != positionRef || selectedRevision != revision.Revision {
		t.Fatalf("selected directory reference = position=%+v revision=%+v, %v", selectedPosition, selectedRevision, err)
	}
	object, err := client.GetPositionObject(ctx, &positionv1.GetPositionObjectRequest{PositionRevisionRef: ref})
	if err != nil || object.GetPositionId() != positionID.String() || object.GetJobCode() != "ENG-REV076" || object.GetOrgUnit() != "ENGINEERING" || !object.GetCompatible() {
		t.Fatalf("authorized real PositionObject = %+v, %v", object, err)
	}
	occupancyOptions, err := client.ListPositionOccupancyOptions(ctx, &positionv1.ListPositionOccupancyOptionsRequest{})
	if err != nil || len(occupancyOptions.GetOptions()) != 1 || occupancyOptions.GetOptions()[0].GetPositionRevisionRef() != ref {
		t.Fatalf("served occupancy selector options = %+v, %v", occupancyOptions, err)
	}
	occupancy, err := client.GetPositionOccupancy(ctx, &positionv1.GetPositionOccupancyRequest{PositionRevisionRef: ref})
	if err != nil {
		t.Fatalf("authorized real PositionOccupancy: %v", err)
	}
	if occupancy.PositionId != positionID.String() || occupancy.CapacityFte != "2.0000" || occupancy.ConsumedFte != "0.5000" || occupancy.AvailableFte != "1.5000" || occupancy.ConsumedHeads != 1 || len(occupancy.Occupants) != 1 {
		t.Fatalf("real occupancy projection = %+v", occupancy)
	}
	authorized = false
	deniedOptions, err := client.ListPositionObjectOptions(ctx, &positionv1.ListPositionObjectOptionsRequest{})
	if err != nil || len(deniedOptions.GetOptions()) != 0 {
		t.Fatalf("row-denied selector options = %+v, %v; want empty", deniedOptions, err)
	}
	if _, err := client.GetPositionObject(ctx, &positionv1.GetPositionObjectRequest{PositionRevisionRef: ref}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("row-denied selected object = %v, want PERMISSION_DENIED", err)
	}
	if _, err := client.GetPositionObject(ctx, &positionv1.GetPositionObjectRequest{PositionRevisionRef: string(mustREV076CrossTenantRef(t, positionID))}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-tenant position reference error = %v, want PERMISSION_DENIED", err)
	}
}

func seedREV076Position(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	from, recorded := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 1, 2, 9, 0, 0, 0, time.UTC)
	store := aggregates.OrganizationStore{}
	legalID, orgID, jobID, positionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	legal, err := aggregates.NewLegalEntity(tenantID, legalID, from, nil, recorded, "REV076 Inc.", "ACTIVE")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PutLegalEntity(ctx, tx, legal); err != nil {
		t.Fatal(err)
	}
	org, err := aggregates.NewOrganizationUnit(tenantID, orgID, from, nil, recorded, "DEPARTMENT", "ENGINEERING", "Engineering", &legalID, nil, "ACTIVE")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PutOrganizationUnit(ctx, tx, org); err != nil {
		t.Fatal(err)
	}
	job, err := aggregates.NewJob(tenantID, jobID, from, nil, recorded, "ENG-REV076", "Software Engineer", "ENGINEERING", "M1", "EXEMPT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PutJob(ctx, tx, job); err != nil {
		t.Fatal(err)
	}
	jobPosition, err := aggregates.NewJobPosition(tenantID, positionID, jobID, orgID, nil, from, nil, recorded, "P-REV076", "", "2.0000", "OPEN")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PutJobPosition(ctx, tx, jobPosition); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return positionID
}

func seedREV076Occupant(t *testing.T, db *pgtest.DB, tenantID, positionID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	workerID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO aggregate_entity (tenant_id, entity_id, kind, canonical_id) VALUES ($1, $2, 'worker', $3)`, tenantID, workerID, "worker-"+workerID.String()); err != nil {
		t.Fatal(err)
	}
	from, recorded := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 1, 2, 9, 0, 0, 0, time.UTC)
	occupancy, err := aggregates.NewPositionOccupancy(tenantID, uuid.New(), positionID, nil, &workerID, from, nil, recorded, "0.5000", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (aggregates.OrganizationStore{}).PutPositionOccupancy(ctx, tx, occupancy); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func mustREV076Date(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func mustREV076KnownAt(t *testing.T, text string) values.KnownAt {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func mustREV076CrossTenantRef(t *testing.T, id uuid.UUID) position.RevisionRef {
	t.Helper()
	entity := values.EntityRef{Tenant: values.TenantId("another-tenant"), Kind: position.KindPosition, Id: id.String()}
	revision, err := values.NewSequenceRevision("position.revision.cross-tenant", 1)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := position.EncodeRevisionRef(entity, revision)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
