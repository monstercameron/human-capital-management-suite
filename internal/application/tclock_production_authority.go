package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	capauthority "github.com/monstercameron/human-capital-management-suite/internal/capability/authority"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	errClockAuthorityUnavailable = errors.New("clock authority: governed evaluator and worker directory are required")
	errClockAuthorityDenied      = errors.New("clock authority: governed evaluation denied")
	errClockAuthorityInvalid     = errors.New("clock authority: invalid trusted principal or scope")
)

// ClockCapabilityIDs names published capability definitions. There are no
// fallback IDs: an unpublished clock capability keeps the surface disabled.
type ClockCapabilityIDs struct {
	Punch, DeviceAdmin, SupervisorOverride, MissingPunchRequest, MissingPunchDecision string
}

// ClockAuthorityFacts resolves current roles, grants, delegation, step-up and
// dual-approval evidence from their authoritative stores.
type ClockAuthorityFacts interface {
	ResolveClockAuthority(context.Context, *trust.Principal, string, string, string, string, time.Time) (capauthority.Authority, error)
}

// RegistryClockAuthority is the concrete governed evaluator used by the
// production clock adapter. It resolves a published capability definition
// and evaluates it with capability/authority.Authorize; an unknown ID or
// missing server-resolved facts fails closed.
type RegistryClockAuthority struct {
	Registry *capability.Registry
	Facts    ClockAuthorityFacts
}

// ProductionClockAuthorityFacts binds the governed capability evaluator to
// the durable role-assignment snapshot and authoritative worker directory.
// It deliberately supplies no synthetic delegation, scope or approval data.
type ProductionClockAuthorityFacts struct {
	Roles                 roleaccess.Store
	Workers               clockservice.WorkerDirectory
	DeviceAdminCapability string
}

var _ ClockAuthorityFacts = ProductionClockAuthorityFacts{}

// ResolveClockAuthority resolves the actor's current role assignment and
// validates any worker subject through the authoritative workforce reader.
// Missing role assignments, inactive workers and malformed tenant scope fail
// closed; credential-carried roles are never copied into EffectiveRoles.
func (f ProductionClockAuthorityFacts) ResolveClockAuthority(ctx context.Context, p *trust.Principal, tenant, capabilityID, subject, _ string, _ time.Time) (capauthority.Authority, error) {
	if f.Roles == nil || p == nil || string(p.Tenant()) != strings.TrimSpace(tenant) || p.OrganizationScopeID() == "" {
		return capauthority.Authority{}, errClockAuthorityUnavailable
	}
	snapshot, err := f.Roles.Load(ctx, p.Tenant(), p.OrganizationScopeID())
	if err != nil {
		return capauthority.Authority{}, err
	}
	var roles []string
	for _, assignment := range snapshot.Assignments {
		if assignment.WorkerRef == p.Subject() {
			roles = append(roles, assignment.RoleIDs...)
			break
		}
	}
	if len(roles) == 0 {
		return capauthority.Authority{}, errClockAuthorityDenied
	}
	if f.Workers != nil && strings.TrimSpace(subject) != "" && capabilityID != f.DeviceAdminCapability {
		if _, active, err := f.Workers.ResolveWorker(ctx, tenant, subject); err != nil || !active {
			if err != nil {
				return capauthority.Authority{}, err
			}
			return capauthority.Authority{}, errClockAuthorityDenied
		}
	}
	return capauthority.Authority{EffectiveRoles: roles}, nil
}

func (e RegistryClockAuthority) evaluate(ctx context.Context, p *trust.Principal, tenant, id, subject, assignment string, now time.Time) (bool, error) {
	if e.Registry == nil || e.Facts == nil || strings.TrimSpace(id) == "" {
		return false, errClockAuthorityUnavailable
	}
	record, ok := e.Registry.LatestActive(id)
	if !ok || record.Status != capability.StatusActive {
		return false, errClockAuthorityDenied
	}
	facts, err := e.Facts.ResolveClockAuthority(ctx, p, tenant, id, subject, assignment, now)
	if err != nil {
		return false, err
	}
	decision := capauthority.Authorize(p, "timekeeping", record.Definition, facts, now)
	return decision.Decision == capability.Allow, nil
}

// ProductionClockAuthority binds clockservice to the authoritative worker
// directory and the governed capability evaluator. Every method resolves the
// canonical worker and assignment before evaluating authority, so a client
// cannot authorize an alias, display name or forged subject reference.
type ProductionClockAuthority struct {
	Workers      clockservice.WorkerDirectory
	Evaluator    RegistryClockAuthority
	Capabilities ClockCapabilityIDs
	Clock        func() time.Time
}

// MissingPunchSessionLookupAuthorizer is the pre-read gate for a session
// lookup. It prevents a caller from using session existence or timing as an
// oracle before the worker-specific authority can be evaluated.
type MissingPunchSessionLookupAuthorizer interface {
	AuthorizeSessionLookup(context.Context, *trust.Principal, string, string) error
}

var _ clockservice.Authorizer = ProductionClockAuthority{}
var _ clockservice.MissingPunchAuthorization = ProductionClockAuthority{}
var _ MissingPunchSessionLookupAuthorizer = ProductionClockAuthority{}

// AuthorizePunch evaluates current authority for a canonical worker and
// assignment. The worker and assignment are re-resolved at decision time.
func (a ProductionClockAuthority) AuthorizePunch(ctx context.Context, p *trust.Principal, tenant, worker, assignment string) (bool, error) {
	canonicalWorker, canonicalAssignment, err := a.resolveScope(ctx, p, tenant, worker, assignment)
	if err != nil {
		return false, err
	}
	allowed, err := a.evaluate(ctx, p, tenant, a.Capabilities.Punch, canonicalWorker, canonicalAssignment)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, errClockAuthorityDenied
	}
	return false, nil
}

// AuthorizeDeviceAdmin evaluates device administration against the site
// named by the authenticated device or administrator. It never trusts a
// role supplied in the request.
func (a ProductionClockAuthority) AuthorizeDeviceAdmin(ctx context.Context, p *trust.Principal, tenant, site string) error {
	if err := validatePrincipalTenant(p, tenant); err != nil {
		return err
	}
	if strings.TrimSpace(site) == "" {
		return errClockAuthorityInvalid
	}
	allowed, err := a.evaluate(ctx, p, tenant, a.Capabilities.DeviceAdmin, site, "")
	if err != nil {
		return err
	}
	if !allowed {
		return errClockAuthorityDenied
	}
	return nil
}

// AuthorizeSupervisorOverride independently evaluates supervisor authority
// for the site. This check is intentionally separate from worker authority.
func (a ProductionClockAuthority) AuthorizeSupervisorOverride(ctx context.Context, p *trust.Principal, tenant, site string) error {
	if err := validatePrincipalTenant(p, tenant); err != nil {
		return err
	}
	if strings.TrimSpace(site) == "" {
		return errClockAuthorityInvalid
	}
	allowed, err := a.evaluate(ctx, p, tenant, a.Capabilities.SupervisorOverride, site, "")
	if err != nil {
		return err
	}
	if !allowed {
		return errClockAuthorityDenied
	}
	return nil
}

// AuthorizeRequest admits only the worker's own missing-punch request and
// then requires an independent governed decision for that capability.
func (a ProductionClockAuthority) AuthorizeRequest(ctx context.Context, p *trust.Principal, tenant, worker string) error {
	canonical, _, err := a.resolveScope(ctx, p, tenant, worker, "")
	if err != nil {
		return err
	}
	if p.Subject() != canonical {
		return errClockAuthorityDenied
	}
	allowed, err := a.evaluate(ctx, p, tenant, a.Capabilities.MissingPunchRequest, canonical, "")
	if err != nil {
		return err
	}
	if !allowed {
		return errClockAuthorityDenied
	}
	return nil
}

// AuthorizeSessionLookup evaluates the published missing-punch request
// capability at the session boundary before a session row is read. The
// worker-specific request check remains mandatory after the row is loaded.
func (a ProductionClockAuthority) AuthorizeSessionLookup(ctx context.Context, p *trust.Principal, tenant, session string) error {
	if err := validatePrincipalTenant(p, tenant); err != nil || strings.TrimSpace(session) == "" {
		return errClockAuthorityInvalid
	}
	allowed, err := a.evaluate(ctx, p, tenant, a.Capabilities.MissingPunchRequest, session, "")
	if err != nil {
		return err
	}
	if !allowed {
		return errClockAuthorityDenied
	}
	return nil
}

// AuthorizeDecision admits a supervisor's independent missing-punch review.
// Self-approval is refused before evaluation and therefore cannot be hidden
// behind a broad manager role.
func (a ProductionClockAuthority) AuthorizeDecision(ctx context.Context, p *trust.Principal, tenant, worker string) error {
	canonical, _, err := a.resolveScope(ctx, p, tenant, worker, "")
	if err != nil {
		return err
	}
	if p.Subject() == canonical {
		return errClockAuthorityDenied
	}
	allowed, err := a.evaluate(ctx, p, tenant, a.Capabilities.MissingPunchDecision, canonical, "")
	if err != nil {
		return err
	}
	if !allowed {
		return errClockAuthorityDenied
	}
	return nil
}

func (a ProductionClockAuthority) resolveScope(ctx context.Context, p *trust.Principal, tenant, worker, assignment string) (string, string, error) {
	if err := validatePrincipalTenant(p, tenant); err != nil {
		return "", "", err
	}
	if a.Workers == nil {
		return "", "", errClockAuthorityUnavailable
	}
	canonical, active, err := a.Workers.ResolveWorker(ctx, tenant, strings.TrimSpace(worker))
	if err != nil {
		return "", "", err
	}
	if !active || canonical == "" {
		return "", "", errClockAuthorityDenied
	}
	if assignment == "" {
		return canonical, "", nil
	}
	_, _, ok, err := a.Workers.ResolveAssignment(ctx, tenant, canonical, strings.TrimSpace(assignment))
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", errClockAuthorityDenied
	}
	return canonical, strings.TrimSpace(assignment), nil
}

func (a ProductionClockAuthority) evaluate(ctx context.Context, p *trust.Principal, tenant, capability, subject, assignment string) (bool, error) {
	if a.Evaluator.Registry == nil || a.Evaluator.Facts == nil || a.Clock == nil {
		return false, errClockAuthorityUnavailable
	}
	now := a.Clock().UTC()
	if now.IsZero() || p.IssuedAt().After(now) || !p.ExpiresAt().After(now) {
		return false, errClockAuthorityInvalid
	}
	allowed, err := a.Evaluator.evaluate(ctx, p, tenant, capability, subject, assignment, now)
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func validatePrincipalTenant(p *trust.Principal, tenant string) error {
	if p == nil || p.Subject() == "" || strings.TrimSpace(tenant) == "" || string(p.Tenant()) != strings.TrimSpace(tenant) {
		return errClockAuthorityInvalid
	}
	return nil
}

// ProductionClockDependencies are the authority ports needed by the serve
// root to enable the clock surface. The root must provide real RLS-backed
// workforce and governed-evaluation implementations.
type ProductionClockDependencies struct {
	Workers      clockservice.WorkerDirectory
	Registry     *capability.Registry
	Facts        ClockAuthorityFacts
	Capabilities ClockCapabilityIDs
	Clock        func() time.Time
}

// NewProductionClockAuthority validates and constructs the production
// authority used by ComposeClock. Missing ports fail before any effect can be
// attempted.
func NewProductionClockAuthority(deps ProductionClockDependencies) (ProductionClockAuthority, error) {
	if deps.Workers == nil || deps.Registry == nil || deps.Facts == nil || deps.Clock == nil || !validClockCapabilities(deps.Capabilities) {
		return ProductionClockAuthority{}, errClockAuthorityUnavailable
	}
	return ProductionClockAuthority{Workers: deps.Workers, Evaluator: RegistryClockAuthority{Registry: deps.Registry, Facts: deps.Facts}, Capabilities: deps.Capabilities, Clock: deps.Clock}, nil
}

func validClockCapabilities(ids ClockCapabilityIDs) bool {
	return ids.Punch != "" && ids.DeviceAdmin != "" && ids.SupervisorOverride != "" && ids.MissingPunchRequest != "" && ids.MissingPunchDecision != ""
}

// ProductionMissingPunchResolver resolves only server-owned missing-punch
// facts. It authorizes each read before returning worker or original-event
// references, then leaves compare-and-swap and workflow effects to the
// clockservice and durable time store.
type ProductionMissingPunchResolver struct {
	Sessions      clockservice.MissingPunchSessionReader
	Reviews       clockservice.MissingPunchReviewReader
	Observations  clockservice.MissingPunchObservationReader
	Original      OriginalClockInResolver
	Authorization clockservice.MissingPunchAuthorization
}

var _ MissingPunchResolver = ProductionMissingPunchResolver{}

// ResolveMissingPunchSession checks request authority before exposing the
// canonical worker and original clock-in observation bound to the session.
func (r ProductionMissingPunchResolver) ResolveMissingPunchSession(ctx context.Context, p *trust.Principal, session string) (MissingPunchSessionFacts, error) {
	if r.Sessions == nil || r.Observations == nil || r.Original == nil || r.Authorization == nil || p == nil || strings.TrimSpace(session) == "" {
		return MissingPunchSessionFacts{}, errClockAuthorityUnavailable
	}
	tenant := string(p.Tenant())
	lookup, ok := r.Authorization.(MissingPunchSessionLookupAuthorizer)
	if !ok {
		return MissingPunchSessionFacts{}, errClockAuthorityUnavailable
	}
	if err := lookup.AuthorizeSessionLookup(ctx, p, tenant, strings.TrimSpace(session)); err != nil {
		return MissingPunchSessionFacts{}, err
	}
	facts, err := r.Sessions.GetMissingPunchSession(ctx, tenant, strings.TrimSpace(session))
	if err != nil {
		return MissingPunchSessionFacts{}, err
	}
	if facts.TenantID != tenant || facts.ID != strings.TrimSpace(session) || facts.WorkerRef == "" || !facts.MissingOut || facts.Status != "OPEN" {
		return MissingPunchSessionFacts{}, errClockAuthorityDenied
	}
	if err := r.Authorization.AuthorizeRequest(ctx, p, tenant, facts.WorkerRef); err != nil {
		return MissingPunchSessionFacts{}, err
	}
	originalID, err := r.Original.ResolveOriginalClockIn(ctx, tenant, facts.ID)
	if err != nil {
		return MissingPunchSessionFacts{}, err
	}
	original, err := r.Observations.GetMissingPunchObservation(ctx, tenant, originalID)
	if err != nil || original.TenantID != tenant || original.ID != originalID || original.Observation.WorkerRef != facts.WorkerRef || original.Observation.EventType != clockdomain.EventClockIn || !original.Observation.Accepted {
		return MissingPunchSessionFacts{}, errClockAuthorityDenied
	}
	return MissingPunchSessionFacts{WorkerRef: facts.WorkerRef, OriginalObservationID: originalID, ExpectedRevision: facts.Revision, PeriodClosed: facts.PeriodClosed}, nil
}

// ResolveMissingPunchRequest checks independent decision authority before
// exposing the affected worker. The service performs the final revision CAS
// after this authorization succeeds.
func (r ProductionMissingPunchResolver) ResolveMissingPunchRequest(ctx context.Context, p *trust.Principal, request string) (MissingPunchRequestFacts, error) {
	if r.Reviews == nil || r.Authorization == nil || p == nil || strings.TrimSpace(request) == "" {
		return MissingPunchRequestFacts{}, errClockAuthorityUnavailable
	}
	tenant := string(p.Tenant())
	record, err := r.Reviews.GetMissingPunchRequest(ctx, tenant, strings.TrimSpace(request))
	if err != nil {
		return MissingPunchRequestFacts{}, err
	}
	if record.TenantID != tenant || record.ID != strings.TrimSpace(request) || record.WorkerRef == "" {
		return MissingPunchRequestFacts{}, errClockAuthorityDenied
	}
	if err := r.Authorization.AuthorizeDecision(ctx, p, tenant, record.WorkerRef); err != nil {
		return MissingPunchRequestFacts{}, err
	}
	return MissingPunchRequestFacts{WorkerRef: record.WorkerRef}, nil
}

// OriginalClockInResolver resolves the immutable accepted clock-in bound to
// a session. The implementation must use the durable session/outbox link;
// it may not infer an observation from a client-provided id.
type OriginalClockInResolver interface {
	ResolveOriginalClockIn(context.Context, string, string) (string, error)
}

// TimestoreOriginalClockInResolver reads the observation id written in the
// same punch commit as the session opening. It is intentionally read-only;
// the workflow's durable CAS remains the only writer.
type TimestoreOriginalClockInResolver struct{ Store *timestore.Store }

var _ OriginalClockInResolver = TimestoreOriginalClockInResolver{}

// ResolveOriginalClockIn returns the accepted CLOCK_IN observation for a
// session, refusing sessions with no durable observation link.
func (r TimestoreOriginalClockInResolver) ResolveOriginalClockIn(ctx context.Context, tenant, session string) (string, error) {
	if r.Store == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(session) == "" {
		return "", errClockAuthorityUnavailable
	}
	var observation string
	err := r.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload->>'observation_id' FROM time_outbox WHERE tenant_id=$1 AND payload->>'session_id'=$2 AND payload ? 'observation_id' ORDER BY sequence LIMIT 1`, tenant, session).Scan(&observation)
	})
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return "", timestore.ErrNotFound
		}
		return "", err
	}
	if strings.TrimSpace(observation) == "" {
		return "", errClockAuthorityDenied
	}
	return observation, nil
}
