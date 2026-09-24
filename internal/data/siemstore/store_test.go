package siemstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/siemstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/subscriptionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/siem"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/siemhttp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func siemTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := pgstore.TenantID(key)
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func siemAppConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set app role: %v", err)
	}
	return conn
}

func siemRequest(kind, source string) siemstore.AppendRequest {
	sum := sha256.Sum256([]byte(source))
	return siemstore.AppendRequest{Type: kind, OccurredAt: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
		SourceRef: "security-signal:" + hex.EncodeToString(sum[:]), EvidenceDigest: hex.EncodeToString(sum[:])}
}

func sameRecord(a, b siemstore.Record) bool {
	return a.Tenant == b.Tenant && a.Sequence == b.Sequence && a.Type == b.Type && a.OccurredAt.Equal(b.OccurredAt) &&
		a.SourceRef == b.SourceRef && a.EvidenceDigest == b.EvidenceDigest && a.RuleID == b.RuleID &&
		a.RuleVersion == b.RuleVersion && a.PreviousDigest == b.PreviousDigest && a.Digest == b.Digest
}

type staticSIEMRing struct{ ring *subscription.CredentialRing }

func (r staticSIEMRing) ResolveSIEMRing(context.Context, *trust.Principal, string) (*subscription.CredentialRing, error) {
	return r.ring, nil
}

func TestTodo_REV_099_05(t *testing.T) {
	db := pgtest.New(t)
	tenant := siemTenant(t, db, "siem-primary")
	store := siemstore.New(siemAppConn(t, db))
	first, err := store.Append(context.Background(), tenant, siemRequest("DLP_SIGNAL", "dlp-primary"))
	if err != nil || first.Sequence != 1 || first.PreviousDigest != "" || first.Digest == "" {
		t.Fatalf("first append = %+v, %v", first, err)
	}
	replay, err := store.Append(context.Background(), tenant, siemRequest("DLP_SIGNAL", "dlp-primary"))
	if err != nil || !sameRecord(replay, first) {
		t.Fatalf("idempotent append = %+v, %v; want %+v", replay, err, first)
	}
}

func TestTodo_REV_099_05_Security(t *testing.T) {
	if _, err := siemstore.New(nil).Append(context.Background(), uuid.Nil, siemRequest("ADMIN_ACTION", "admin")); !errors.Is(err, siemstore.ErrInvalid) {
		t.Fatalf("invalid tenant/database error = %v", err)
	}
	if _, err := siemstore.New(nil).ReadPage(context.Background(), uuid.New(), 0, "", 1001); !errors.Is(err, siemstore.ErrInvalidCursor) {
		t.Fatalf("oversized page error = %v", err)
	}
	if _, err := siemstore.New(nil).Append(context.Background(), uuid.New(), siemstore.AppendRequest{Type: "RAW", OccurredAt: time.Now(), SourceRef: "payload", EvidenceDigest: "secret"}); !errors.Is(err, siemstore.ErrInvalid) {
		t.Fatalf("raw payload event error = %v", err)
	}
}

func TestTodo_REV_099_05_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantKeyA, tenantKeyB := "siem-integration-a", "siem-integration-b"
	tenantA := siemTenant(t, db, tenantKeyA)
	tenantB := siemTenant(t, db, tenantKeyB)
	requestA := siemRequest("DLP_SIGNAL", "dlp-integration")
	requestB := siemRequest("ACCESS_SIGNAL", "access-integration")
	firstStore := siemstore.New(siemAppConn(t, db))
	recordA, err := firstStore.Append(context.Background(), tenantA, requestA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstStore.Append(context.Background(), tenantB, requestB); err != nil {
		t.Fatal(err)
	}

	// Re-open the store on a fresh database connection to prove the cursor and
	// feed survive process-local state loss.
	freshConn := siemAppConn(t, db)
	fresh := siemstore.New(freshConn)
	page, err := fresh.ReadPage(context.Background(), tenantA, 0, "", 1)
	if err != nil || len(page.Events) != 1 || !sameRecord(page.Events[0], recordA) || page.Next != 1 || page.Digest != recordA.Digest {
		t.Fatalf("fresh first page = %+v, %v", page, err)
	}

	// Serve the persisted record through the application service using a
	// separately loaded active subscription and authorization evidence.
	request := subscription.RevisionRequest{SubscriptionID: "siem-served", Requester: "operator:one",
		Subscriber:          subscription.Subscriber{PartnerRef: "partner:customer"},
		EventKinds:          []subscription.EventKind{subscription.EventSecurityAlert, subscription.EventSecurityAudit},
		DeclaredFields:      map[subscription.EventKind][]string{subscription.EventSecurityAlert: {}, subscription.EventSecurityAudit: {}},
		DeliveryEndpointRef: "endpoint:siem-served", DeliveryGuarantee: subscription.GuaranteeAtLeastOnce, TenantScope: tenantA.String()}
	draft, err := subscription.NewDraft(request)
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("operator:approver", "operator:second-approver")
	if err != nil {
		t.Fatal(err)
	}
	governance := subscriptionstore.New(siemAppConn(t, db), tenantA)
	if err := governance.AppendRevision(context.Background(), tenantA, draft); err != nil {
		t.Fatal(err)
	}
	if err := governance.AppendRevision(context.Background(), tenantA, active); err != nil {
		t.Fatal(err)
	}
	grant := subscription.ScopeGrant{PartnerRef: "partner:customer", TenantScope: tenantA.String(), Purpose: "security-monitoring",
		EventKinds: request.EventKinds, Resources: []string{tenantA.String()}, Fields: map[subscription.EventKind][]string{}}
	decision, err := subscription.Authorize(active, grant)
	if err != nil {
		t.Fatal(err)
	}
	if err := governance.AppendAuthorizationEvent(context.Background(), tenantA, decision.Event(grant), 1); err != nil {
		t.Fatal(err)
	}
	provider, err := subscription.NewHMACProvider("siem-integration-key", []byte("integration-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := subscription.NewCredentialRing(active.DeliveryEndpointRef, func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Add(subscription.SigningCredential{Destination: active.DeliveryEndpointRef, Profile: "hmac-test", Version: "v1", KeyRef: "siem-integration-key",
		NotBefore: time.Date(2026, 9, 24, 11, 0, 0, 0, time.UTC), NotAfter: time.Date(2026, 9, 24, 13, 0, 0, 0, time.UTC), Provider: provider}); err != nil {
		t.Fatal(err)
	}
	if err := ring.Activate("v1", time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	principalA, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenantKeyA), Subject: "partner:customer",
		SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "siem-route-session-a", IssuedAt: time.Date(2026, 9, 24, 11, 59, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 9, 24, 13, 0, 0, 0, time.UTC), CredentialDigest: "verified-siem-route-token-a"})
	if err != nil {
		t.Fatal(err)
	}
	principalB, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenantKeyB), Subject: "partner:customer",
		SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "siem-route-session-b", IssuedAt: time.Date(2026, 9, 24, 11, 59, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 9, 24, 13, 0, 0, 0, time.UTC), CredentialDigest: "verified-siem-route-token-b"})
	if err != nil {
		t.Fatal(err)
	}
	verifier := trust.VerifierFunc(func(_ context.Context, credential trust.Credential) (*trust.Principal, error) {
		switch credential.Token {
		case "siem-route-a":
			return principalA, nil
		case "siem-route-b":
			return principalB, nil
		default:
			return nil, trust.ErrInvalidCredential
		}
	})
	service := application.SIEMFeedService{Governance: subscriptionstore.New(siemAppConn(t, db), tenantA), Feed: fresh}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	handler := siemhttp.Handler{Config: transport.Config{Verifier: verifier, Now: func() time.Time { return at }}, Reader: service, Rings: staticSIEMRing{ring},
		ClassifyReadError: func(err error) int {
			switch {
			case errors.Is(err, application.ErrSIEMFeedGovernance):
				return http.StatusForbidden
			case errors.Is(err, siemstore.ErrInvalidCursor), errors.Is(err, siem.ErrInvalidCursor):
				return http.StatusConflict
			default:
				return http.StatusServiceUnavailable
			}
		}}
	server := httptest.NewServer(siemhttp.Overlay(http.NotFoundHandler(), handler))
	defer server.Close()
	get := func(path, token string) (*http.Response, error) {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set(transport.AuthorizationMetadataKey, "Bearer "+token)
		return server.Client().Do(req)
	}
	response, err := get(siemhttp.Path+"?subscription_id=siem-served&limit=10", "siem-route-a")
	if err != nil {
		t.Fatal(err)
	}
	var served siem.Feed
	if response.StatusCode != http.StatusOK {
		t.Fatalf("served pull route status=%d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&served); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if len(served.Events) != 1 || served.Events[0].Digest != recordA.Digest || served.Events[0].Tenant != tenantA.String() {
		t.Fatalf("served persisted feed = %+v", served)
	}
	if err := siem.Verify(served, tenantA.String(), at, ring); err != nil {
		t.Fatalf("served persisted page signature = %v", err)
	}
	response, err = get(siemhttp.Path+"?subscription_id=siem-served&after=1&digest="+recordA.Digest, "siem-route-a")
	if err != nil {
		t.Fatal(err)
	}
	var resumed siem.Feed
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&resumed) != nil || len(resumed.Events) != 0 || resumed.Next.Sequence != 1 {
		response.Body.Close()
		t.Fatalf("resumed HTTP cursor status=%d feed=%+v", response.StatusCode, resumed)
	}
	response.Body.Close()
	response, err = get(siemhttp.Path+"?subscription_id=siem-served&after=1&digest="+strings.Repeat("0", 64), "siem-route-a")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusConflict {
		response.Body.Close()
		t.Fatalf("forged cursor/gap HTTP status=%d", response.StatusCode)
	}
	response.Body.Close()
	response, err = get(siemhttp.Path+"?subscription_id=siem-served", "siem-route-b")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		response.Body.Close()
		t.Fatalf("cross-tenant HTTP route status=%d", response.StatusCode)
	}
	response.Body.Close()
	resumedPage, err := fresh.ReadPage(context.Background(), tenantA, page.Next, page.Digest, 10)
	if err != nil || len(resumedPage.Events) != 0 || resumedPage.Next != page.Next {
		t.Fatalf("resumed page = %+v, %v", resumedPage, err)
	}
	if _, err := fresh.ReadPage(context.Background(), tenantA, 1, "forged", 10); !errors.Is(err, siemstore.ErrInvalidCursor) {
		t.Fatalf("forged anchor error = %v", err)
	}

	// The immutable event and its protobuf outbox reference committed together.
	tx, err := freshConn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenantA); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE tenant_id=$1 AND ordering_key='security-siem-feed' AND schema_ref=$2`, tenantA, siemstore.OutboxSchemaRef).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("durable delivery intents = %d, %v; want one", queued, err)
	}
	var crossTenantRows int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM security_siem_feed_event WHERE tenant_id=$1`, tenantB).Scan(&crossTenantRows); err != nil || crossTenantRows != 0 {
		t.Fatalf("tenant-a connection read tenant-b rows = %d, %v", crossTenantRows, err)
	}
}

func TestTodo_REV_099_05_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := siemTenant(t, db, "siem-mutation")
	store := siemstore.New(siemAppConn(t, db))
	if _, err := store.Append(context.Background(), tenant, siemRequest("ADMIN_ACTION", "admin-mutation")); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE security_siem_feed_event SET event_type='DLP_SIGNAL' WHERE tenant_id=$1 AND sequence=1`, tenant); err == nil {
		t.Fatal("append-only SIEM feed accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM security_siem_feed_event WHERE tenant_id=$1 AND sequence=1`, tenant); err == nil {
		t.Fatal("append-only SIEM feed accepted DELETE")
	}
}
