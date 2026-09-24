package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/siemstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/subscription"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/siem"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var ErrSIEMFeedGovernance = errors.New("application: SIEM feed subscription is not authorized")

type siemGovernanceReader interface {
	LoadActive(context.Context, uuid.UUID) ([]subscription.EventSubscription, error)
	LoadAuthorizationEvents(context.Context, uuid.UUID, string) ([]subscription.AuthorizationEvent, error)
}

// ReadAuthenticated binds the feed subscription to the authenticated
// principal's tenant and subscriber identity before reading the durable page.
func (s SIEMFeedService) ReadAuthenticated(ctx context.Context, principal *trust.Principal,
	subscriptionID string, cursor siem.Cursor, limit int, at time.Time,
	ring *subscription.CredentialRing) (siem.Feed, error) {
	if s.Governance == nil || principal == nil || principal.Subject() == "" {
		return siem.Feed{}, ErrSIEMFeedGovernance
	}
	tenant := pgstore.TenantID(string(principal.Tenant()))
	active, err := s.Governance.LoadActive(ctx, tenant)
	if err != nil {
		return siem.Feed{}, fmt.Errorf("application: load SIEM subscriptions: %w", err)
	}
	for _, revision := range active {
		if revision.SubscriptionID != subscriptionID {
			continue
		}
		bound := revision.Subscriber.PrincipalRef == principal.Subject() || revision.Subscriber.PartnerRef == principal.Subject() ||
			(principal.ClientID() != "" && revision.Subscriber.PartnerRef == principal.ClientID())
		if !bound {
			return siem.Feed{}, ErrSIEMFeedGovernance
		}
		return s.Read(ctx, tenant, subscriptionID, cursor, limit, at, ring)
	}
	return siem.Feed{}, ErrSIEMFeedGovernance
}

type siemPageReader interface {
	ReadPage(context.Context, uuid.UUID, uint64, string, int) (siemstore.Page, error)
}

// SIEMFeedService serves signed cursor pages from the durable feed only when
// the current active subscription and latest authorization decision govern
// this tenant and destination.
type SIEMFeedService struct {
	Governance siemGovernanceReader
	Feed       siemPageReader
}

func (s SIEMFeedService) Read(ctx context.Context, tenant uuid.UUID, subscriptionID string,
	cursor siem.Cursor, limit int, at time.Time, ring *subscription.CredentialRing) (siem.Feed, error) {
	if s.Governance == nil || s.Feed == nil || tenant == uuid.Nil || subscriptionID == "" ||
		cursor.Tenant != tenant.String() || ring == nil || at.IsZero() {
		return siem.Feed{}, ErrSIEMFeedGovernance
	}
	active, err := s.Governance.LoadActive(ctx, tenant)
	if err != nil {
		return siem.Feed{}, fmt.Errorf("application: load SIEM subscriptions: %w", err)
	}
	var governed *subscription.EventSubscription
	for i := range active {
		if active[i].SubscriptionID == subscriptionID {
			governed = &active[i]
			break
		}
	}
	if governed == nil || governed.TenantScope != tenant.String() || !governsSIEMKinds(*governed) ||
		governed.DeliveryEndpointRef != ring.Destination() {
		return siem.Feed{}, ErrSIEMFeedGovernance
	}
	authorization, err := s.Governance.LoadAuthorizationEvents(ctx, tenant, subscriptionID)
	if err != nil || len(authorization) == 0 {
		return siem.Feed{}, ErrSIEMFeedGovernance
	}
	last := authorization[len(authorization)-1]
	if last.SubscriptionID != subscriptionID || last.Revision != governed.Revision || !last.Allowed || last.Validate() != nil {
		return siem.Feed{}, ErrSIEMFeedGovernance
	}
	page, err := s.Feed.ReadPage(ctx, tenant, cursor.Sequence, cursor.Digest, limit)
	if err != nil {
		return siem.Feed{}, fmt.Errorf("application: read durable SIEM feed: %w", err)
	}
	if page.Tenant != tenant || page.From != cursor.Sequence {
		return siem.Feed{}, siem.ErrInvalidFeed
	}
	events := make([]siem.Event, 0, len(page.Events))
	for _, record := range page.Events {
		event := siem.Event{Sequence: record.Sequence, Tenant: record.Tenant.String(), Type: siem.EventType(record.Type),
			OccurredAt: record.OccurredAt, SourceRef: record.SourceRef, EvidenceDigest: record.EvidenceDigest,
			RuleID: record.RuleID, RuleVersion: record.RuleVersion, PreviousDigest: record.PreviousDigest, Digest: record.Digest}
		events = append(events, event)
	}
	return siem.SignPage(tenant.String(), cursor, events, at, ring)
}

func governsSIEMKinds(revision subscription.EventSubscription) bool {
	if revision.Verify() != nil || revision.State != subscription.StateActive {
		return false
	}
	alert, audit := false, false
	for _, kind := range revision.EventKinds {
		alert = alert || kind == subscription.EventSecurityAlert
		audit = audit || kind == subscription.EventSecurityAudit
	}
	return alert && audit
}
