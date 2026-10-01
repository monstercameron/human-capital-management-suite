package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

const (
	// agentModelDestination is the egress destination every model step
	// declares; agentModelRegion is the residency the provider is held to.
	agentModelDestination = "agent.model.provider"
	agentModelRegion      = "global"

	// agentAuthorityWindow is the rolling validity of a resolved authority.
	// Each Resolve is taken at the instant of the call, so the window always
	// covers "now" while the durable role facts still say the user has access.
	agentAuthorityWindow = time.Hour
	agentResolveTimeout  = 10 * time.Second
)

// agentOrgScope is the organization scope every agent grant is issued under.
// The durable role facts are tenant-wide (roleaccess ignores the scope beyond
// requiring one) and the agent's only data is the user's own worker record, so
// the grant scope is the tenant rather than the org unit the session happened
// to carry; that keeps a wake after a restart resolvable from durable facts
// alone.
func agentOrgScope(tenant values.TenantId) string { return "agent-scope:" + tenant.String() }

// agentRoleFacts is the durable role read the resolver needs.
type agentRoleFacts interface {
	Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error)
}

// agentAuthority resolves a user's CURRENT authority from the durable role
// policy and worker identity. It never reads a token or the task prompt: the
// user is the grant's user id (the verified worker key), the tenant is the
// grant's tenant, and the answer is whatever the role assignments and the
// worker's lifecycle say at the instant of the call. A user with no active
// worker record or no active role resolves inactive, which makes the
// delegation service refuse grants, exchanges and wakes.
type agentAuthority struct {
	roles      agentRoleFacts
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
}

var _ agentdelegation.AuthorityResolver = agentAuthority{}

func (a agentAuthority) Resolve(userID string, tenant values.TenantId, purpose string, at time.Time) (agentdelegation.UserAuthority, error) {
	inactive := agentdelegation.UserAuthority{UserID: userID}
	if a.roles == nil || a.db == nil || a.tenantUUID == nil {
		return inactive, fmt.Errorf("application: agent authority is not composed")
	}
	if strings.TrimSpace(userID) == "" || tenant.Validate() != nil || purpose != agentPurpose || at.IsZero() {
		return inactive, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentResolveTimeout)
	defer cancel()
	active, err := a.workerActive(ctx, tenant, userID)
	if err != nil {
		return inactive, err
	}
	if !active {
		return inactive, nil
	}
	snapshot, err := a.roles.Load(ctx, tenant, agentOrgScope(tenant))
	if err != nil {
		return inactive, fmt.Errorf("application: load role facts for agent authority: %w", err)
	}
	if !hasActiveRole(snapshot, userID) {
		return inactive, nil
	}
	fields := agentReadFields()
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = string(f)
	}
	sort.Strings(names)
	return agentdelegation.UserAuthority{UserID: userID, Active: true, Authority: trust.AuthorityScope{
		Tenant: tenant, OrganizationScopeID: agentOrgScope(tenant),
		Capabilities: []string{agentReadCapabilityID},
		// The only resource is the user's own worker record.
		Resources: []string{"worker:" + userID}, Fields: names, Purposes: []string{agentPurpose},
		// The durable facts carry no authentication assurance; the starter
		// requires the session to have at least this much before it asks.
		Assurance: trust.AssuranceLow,
		NotBefore: at.Add(-time.Minute), ExpiresAt: at.Add(agentAuthorityWindow),
	}}, nil
}

func (a agentAuthority) workerActive(ctx context.Context, tenant values.TenantId, userID string) (bool, error) {
	tenantID := a.tenantUUID(tenant)
	if tenantID == uuid.Nil {
		return false, nil
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return false, err
	}
	row, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, userID)
	if err != nil {
		return false, err
	}
	return found && strings.EqualFold(row.LifecycleStatus, "active"), nil
}

// hasActiveRole reports whether the worker holds at least one active role.
func hasActiveRole(snapshot roleaccess.Snapshot, workerRef string) bool {
	active := make(map[string]struct{}, len(snapshot.Roles))
	for _, role := range snapshot.Roles {
		if role.Active {
			active[role.ID] = struct{}{}
		}
	}
	for _, assignment := range snapshot.Assignments {
		if assignment.WorkerRef != workerRef {
			continue
		}
		for _, id := range assignment.RoleIDs {
			if _, ok := active[id]; ok {
				return true
			}
		}
	}
	return false
}

// newAgentDLPInspector is the detector set both the egress evaluator and the
// redactor use, so what egress refuses and what redaction removes agree.
func newAgentDLPInspector() (*trustdlp.Inspector, error) {
	var detectors []trustdlp.Detector
	for _, d := range []struct {
		id       string
		class    trustdlp.DataClass
		severity trustdlp.Severity
		pattern  string
	}{
		{"national-id-us", trustdlp.ClassPII, trustdlp.SeverityHigh, `\b\d{3}-\d{2}-\d{4}\b`},
		{"email-address", trustdlp.ClassPII, trustdlp.SeverityMedium, `[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`},
		{"phone-number", trustdlp.ClassPII, trustdlp.SeverityMedium, `\b(?:\+?\d{1,3}[ .\-])?(?:\(\d{3}\)|\d{3})[ .\-]\d{3}[ .\-]\d{4}\b`},
		{"payment-card", trustdlp.ClassBank, trustdlp.SeverityHigh, `\b\d(?:[ \-]?\d){12,18}\b`},
	} {
		detector, err := trustdlp.NewDetector(d.id, d.class, d.severity, d.pattern)
		if err != nil {
			return nil, err
		}
		detectors = append(detectors, detector)
	}
	return trustdlp.NewInspector(detectors...)
}

// newAgentEgress builds the egress evaluator: an outbound policy that names
// the model destination for the agent purpose and clears PUBLIC data only, and
// the DLP inspector that classifies whatever a step actually tries to send.
func newAgentEgress(inspector *trustdlp.Inspector) (*agentegress.Evaluator, error) {
	trustPolicy, err := outbound.NewPolicy(outbound.Destination{
		Name: agentModelDestination, TrustBundleRef: "bundle:agent-model:v1",
		Purposes: []string{agentPurpose}, DataClasses: []string{string(trustdlp.ClassPublic)},
	})
	if err != nil {
		return nil, err
	}
	policy, err := trustdlp.NewPolicy(trustPolicy, trustdlp.Clearance{
		Destination: agentModelDestination, Classes: []trustdlp.DataClass{trustdlp.ClassPublic}, Decision: trustdlp.Allow,
	})
	if err != nil {
		return nil, err
	}
	return agentegress.NewEvaluator(policy, inspector, trustdlp.NewReceiptLog())
}

// agentRedactor is the PII-aware agentmodel.Redactor. It runs the DLP
// inspector over the prompt and over the typed output and replaces every
// finding with a class marker, so a value the model was never meant to see or
// to emit does not reach the task ledger even when egress missed it. It keeps
// no state.
type agentRedactor struct{ inspector *trustdlp.Inspector }

var _ agentmodel.Redactor = agentRedactor{}

func (r agentRedactor) Redact(_ context.Context, text string, _ []string) (agentmodel.Redaction, error) {
	inspection, err := r.inspector.Inspect([]byte(text))
	if err != nil {
		return agentmodel.Redaction{}, err
	}
	type span struct {
		start, end int
		class      trustdlp.DataClass
	}
	spans := make([]span, 0, len(inspection.Findings))
	for _, f := range inspection.Findings {
		spans = append(spans, span{f.Location.Start, f.Location.End, f.Class})
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})
	var out strings.Builder
	cursor := 0
	for _, s := range spans {
		if s.start < cursor {
			// An overlapping finding is already covered by the earlier span.
			if s.end > cursor {
				cursor = s.end
			}
			continue
		}
		out.WriteString(text[cursor:s.start])
		out.WriteString("[REDACTED:" + string(s.class) + "]")
		cursor = s.end
	}
	out.WriteString(text[cursor:])
	return agentmodel.Redaction{Text: out.String(), Digest: trustdlp.DigestPayload([]byte(out.String()))}, nil
}
