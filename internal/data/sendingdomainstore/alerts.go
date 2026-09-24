package sendingdomainstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
)

const domainAlertOwner = "operations-on-call"

func (s *Store) Raise(ctx context.Context, domain delivery.ActiveSendingDomain, reason string, now time.Time) error {
	if err := s.validate(domain); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" || now.IsZero() {
		return delivery.ErrInvalidEmail
	}
	if strings.TrimSpace(domain.AlertCycle) == "" {
		return delivery.ErrInvalidEmail
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	owner := domain.Owner
	secondary := domainAlertOwner
	if owner == secondary {
		secondary = "security-on-call"
	}
	evidence := sha256.Sum256([]byte("mail-domain-auth\x00" + domain.Profile.Domain + "\x00" + domain.AlertCycle))
	evidenceDigest := hex.EncodeToString(evidence[:])
	incidentKey := domainAlertPrefix(domain.Profile.Domain) + domain.AlertCycle
	declaredAt, err := time.Parse(time.RFC3339Nano, domain.AlertCycle)
	if err != nil || declaredAt.IsZero() {
		return delivery.ErrInvalidEmail
	}
	scope, _ := json.Marshal(map[string]string{"domain": domain.Profile.Domain, "check": "SPF_DKIM_DMARC"})
	incident := opsmeta.AlertIncident{
		TenantID: s.tenant, IncidentID: uuid.New(), IncidentKey: incidentKey, Severity: "SEV3",
		Scope: scope, CorrelationKey: "dns-auth:" + evidenceDigest, EvidenceDigest: evidenceDigest, DeclaredAt: declaredAt,
		PrimaryOwner: owner, SecondaryRoute: secondary, StormLimit: 100, StormWindow: 24 * time.Hour,
	}
	if _, err := opsmeta.RouteAlert(ctx, tx, incident); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Resolve(ctx context.Context, domain delivery.ActiveSendingDomain, at time.Time) error {
	if err := s.validate(domain); err != nil {
		return err
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Resolve by the owned domain prefix. alert_cycle can be missing after an
	// older partial write, while the incident itself remains durable.
	if err := opsmeta.ResolveIncidentsByKeyPrefix(ctx, tx, s.tenant, domainAlertPrefix(domain.Profile.Domain), at); err != nil {
		return fmt.Errorf("sendingdomainstore: resolve owned domain alert: %w", err)
	}
	return tx.Commit(ctx)
}

func domainAlertPrefix(domain string) string { return "mail-domain-auth:" + domain + ":" }

var _ delivery.DomainAlertOwner = (*Store)(nil)
