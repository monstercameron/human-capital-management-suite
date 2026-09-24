package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/siemstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/siem"
)

type siemAppendFixture struct {
	tenant uuid.UUID
	input  siemstore.AppendRequest
	err    error
}

func (f *siemAppendFixture) Append(_ context.Context, tenant uuid.UUID, input siemstore.AppendRequest) (siemstore.Record, error) {
	f.tenant, f.input = tenant, input
	return siemstore.Record{Tenant: tenant, Sequence: 1}, f.err
}

type siemGovernanceFixture struct {
	revision subscription.EventSubscription
	event    subscription.AuthorizationEvent
}

func (g siemGovernanceFixture) LoadActive(context.Context, uuid.UUID) ([]subscription.EventSubscription, error) {
	return []subscription.EventSubscription{g.revision}, nil
}
func (g siemGovernanceFixture) LoadAuthorizationEvents(context.Context, uuid.UUID, string) ([]subscription.AuthorizationEvent, error) {
	return []subscription.AuthorizationEvent{g.event}, nil
}

type siemPageFixture struct {
	page siemstore.Page
	err  error
}

func (f siemPageFixture) ReadPage(context.Context, uuid.UUID, uint64, string, int) (siemstore.Page, error) {
	return f.page, f.err
}

func governedSIEMFixture(t *testing.T) (uuid.UUID, time.Time, subscription.EventSubscription, subscription.AuthorizationEvent) {
	t.Helper()
	tenant := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	request := subscription.RevisionRequest{
		SubscriptionID: "siem-customer", Requester: "admin:requester", Subscriber: subscription.Subscriber{PartnerRef: "partner:customer"},
		EventKinds:          []subscription.EventKind{subscription.EventSecurityAlert, subscription.EventSecurityAudit},
		DeclaredFields:      map[subscription.EventKind][]string{subscription.EventSecurityAlert: {}, subscription.EventSecurityAudit: {}},
		DeliveryEndpointRef: "endpoint:customer-siem", DeliveryGuarantee: subscription.GuaranteeAtLeastOnce, TenantScope: tenant.String(),
	}
	draft, err := subscription.NewDraft(request)
	if err != nil {
		t.Fatal(err)
	}
	active, err := draft.Activate("admin:approver", "admin:second-approver")
	if err != nil {
		t.Fatal(err)
	}
	grant := subscription.ScopeGrant{PartnerRef: "partner:customer", TenantScope: tenant.String(), Purpose: "security-monitoring",
		EventKinds: []subscription.EventKind{subscription.EventSecurityAlert, subscription.EventSecurityAudit},
		Resources:  []string{tenant.String()}, Fields: map[subscription.EventKind][]string{}}
	decision, _ := subscription.Authorize(active, grant)
	event := decision.Event(grant)
	if err := event.Validate(); err != nil {
		t.Fatalf("authorization fixture invalid: %v", err)
	}
	return tenant, time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), active, event
}

func TestTodo_REV_099_05_ServedDurableRead(t *testing.T) {
	tenant, at, active, authorization := governedSIEMFixture(t)
	_, _, ring := siemFixture(t)
	page := siemstore.Page{Tenant: tenant, From: 0}
	service := SIEMFeedService{Governance: siemGovernanceFixture{active, authorization}, Feed: siemPageFixture{page: page}}
	feed, err := service.Read(context.Background(), tenant, active.SubscriptionID, siem.Cursor{Tenant: tenant.String()}, 50, at, ring)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Tenant != tenant.String() || feed.From.Sequence != 0 || feed.Next.Sequence != 0 || len(feed.Events) != 0 {
		t.Fatalf("durable empty feed page has invalid cursor binding: %+v", feed)
	}
	if err := siem.Verify(feed, tenant.String(), at, ring); err != nil {
		t.Fatalf("signed feed did not verify: %v", err)
	}
}

func TestTodo_REV_099_05_ServedGovernance(t *testing.T) {
	tenant, at, active, authorization := governedSIEMFixture(t)
	_, _, ring := siemFixture(t)
	page := siemstore.Page{Tenant: tenant}
	base := SIEMFeedService{Governance: siemGovernanceFixture{active, authorization}, Feed: siemPageFixture{page: page}}
	wrongDestination, err := subscription.NewCredentialRing("endpoint:other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Read(context.Background(), tenant, active.SubscriptionID, siem.Cursor{Tenant: tenant.String()}, 10, at, wrongDestination); !errors.Is(err, ErrSIEMFeedGovernance) {
		t.Fatalf("mismatched endpoint accepted: %v", err)
	}
	denied := authorization
	denied.Allowed = false
	base.Governance = siemGovernanceFixture{active, denied}
	if _, err := base.Read(context.Background(), tenant, active.SubscriptionID, siem.Cursor{Tenant: tenant.String()}, 10, at, ring); !errors.Is(err, ErrSIEMFeedGovernance) {
		t.Fatalf("denied authorization accepted: %v", err)
	}
	if _, err := base.Read(context.Background(), uuid.New(), active.SubscriptionID, siem.Cursor{Tenant: tenant.String()}, 10, at, ring); !errors.Is(err, ErrSIEMFeedGovernance) {
		t.Fatalf("cross-tenant cursor accepted: %v", err)
	}
	withoutAudit := active
	withoutAudit.EventKinds = []subscription.EventKind{subscription.EventSecurityAlert}
	withoutAudit.Digest = "tampered"
	base.Governance = siemGovernanceFixture{withoutAudit, authorization}
	if _, err := base.Read(context.Background(), tenant, active.SubscriptionID, siem.Cursor{Tenant: tenant.String()}, 10, at, ring); !errors.Is(err, ErrSIEMFeedGovernance) {
		t.Fatalf("revision without audit event kind accepted: %v", err)
	}
}

func TestTodo_REV_099_05_IngestSecuritySignals(t *testing.T) {
	tenant := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	ctx := telemetry.WithCorrelationID(context.Background(), "siem-ingest-correlation")
	base, err := telemetry.BuildEnvelope(ctx, telemetry.Resource{SchemaVersion: 1, ServiceName: "hcmnext", ServiceVersion: "test",
		ServiceInstanceID: "instance-a", Environment: "test", CellID: "cell-a", Region: "us-east",
		ProcessRole: telemetry.ProcessRoleAPI, BuildDigest: "build-a", TenantClass: telemetry.TenantClassStandard}, telemetry.OutcomeDenied, 1, nil, allow)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := securityevidence.NewEnvelope(base, securityevidence.TagSecurity,
		securityevidence.RetentionSecurityDenial, securityevidence.HoldNone, "")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	signal, err := securityevidence.NewSignal(securityevidence.SequenceBreakGlassUse, at, envelope)
	if err != nil {
		t.Fatal(err)
	}
	store := &siemAppendFixture{}
	ingestor := SIEMEventIngestor{Store: store}
	if _, err := ingestor.RecordSignal(ctx, tenant, signal); err != nil {
		t.Fatal(err)
	}
	if store.tenant != tenant || store.input.Type != string(siem.EventAdmin) || store.input.EvidenceDigest == "" || store.input.SourceRef == "" {
		t.Fatalf("signal was not durably projected: tenant=%s input=%+v", store.tenant, store.input)
	}
	alert := testTrustedAlert(tenant)
	if _, err := ingestor.RecordAlert(ctx, tenant, alert); err != nil {
		t.Fatal(err)
	}
	if store.input.Type != string(siem.EventAlertRule) || store.input.RuleID != alert.alert.RuleID || store.input.RuleVersion != alert.alert.RuleVersion {
		t.Fatalf("alert was not durably projected: %+v", store.input)
	}
	if _, err := ingestor.RecordAlert(ctx, uuid.New(), alert); !errors.Is(err, ErrSIEMExportInvalid) {
		t.Fatalf("cross-tenant alert was accepted: %v", err)
	}
}
