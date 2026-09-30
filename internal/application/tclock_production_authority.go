package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	errClockAuthorityUnavailable = errors.New("clock authority: governed evaluator and worker directory are required")
	errClockAuthorityDenied      = errors.New("clock authority: governed evaluation denied")
	errClockAuthorityInvalid     = errors.New("clock authority: invalid trusted principal or scope")
)

const (
	clockCapabilityPunch         = "hcmnext.time.clock_punch"
	clockCapabilityDeviceAdmin   = "hcmnext.time.clock_device_admin"
	clockCapabilitySupervisor    = "hcmnext.time.clock_supervisor_override"
	clockCapabilityMissingPunch  = "hcmnext.time.missing_punch.request"
	clockCapabilityMissingReview = "hcmnext.time.missing_punch.decide"
)

// ClockAuthorization is the result of a current, governed authority check.
// The decision is deliberately smaller than a capability decision: callers
// need only the allow bit and the delegated marker needed by clockservice.
type ClockAuthorization struct {
	Allowed   bool
	Delegated bool
}

// ClockAuthorityEvaluator evaluates a clock capability against current
// server-resolved facts. Implementations must load roles, grants and
// obligations from durable stores and use the existing governed evaluator;
// this interface has no development or allow-all implementation.
type ClockAuthorityEvaluator interface {
	EvaluateClock(context.Context, *trust.Principal, string, string, string, string, time.Time) (ClockAuthorization, error)
}

// ProductionClockAuthority binds clockservice to the authoritative worker
// directory and the governed capability evaluator. Every method resolves the
// canonical worker and assignment before evaluating authority, so a client
// cannot authorize an alias, display name or forged subject reference.
type ProductionClockAuthority struct {
	Workers   clockservice.WorkerDirectory
	Evaluator ClockAuthorityEvaluator
	Clock     func() time.Time
}

var _ clockservice.Authorizer = ProductionClockAuthority{}
var _ clockservice.MissingPunchAuthorization = ProductionClockAuthority{}

// AuthorizePunch evaluates current authority for a canonical worker and
// assignment. The worker and assignment are re-resolved at decision time.
func (a ProductionClockAuthority) AuthorizePunch(ctx context.Context, p *trust.Principal, tenant, worker, assignment string) (bool, error) {
	canonicalWorker, canonicalAssignment, err := a.resolveScope(ctx, p, tenant, worker, assignment)
	if err != nil {
		return false, err
	}
	decision, err := a.evaluate(ctx, p, tenant, clockCapabilityPunch, canonicalWorker, canonicalAssignment)
	if err != nil {
		return false, err
	}
	if !decision.Allowed {
		return false, errClockAuthorityDenied
	}
	return decision.Delegated, nil
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
	decision, err := a.evaluate(ctx, p, tenant, clockCapabilityDeviceAdmin, site, "")
	if err != nil {
		return err
	}
	if !decision.Allowed {
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
	decision, err := a.evaluate(ctx, p, tenant, clockCapabilitySupervisor, site, "")
	if err != nil {
		return err
	}
	if !decision.Allowed {
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
	decision, err := a.evaluate(ctx, p, tenant, clockCapabilityMissingPunch, canonical, "")
	if err != nil {
		return err
	}
	if !decision.Allowed {
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
	decision, err := a.evaluate(ctx, p, tenant, clockCapabilityMissingReview, canonical, "")
	if err != nil {
		return err
	}
	if !decision.Allowed {
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

func (a ProductionClockAuthority) evaluate(ctx context.Context, p *trust.Principal, tenant, capability, subject, assignment string) (ClockAuthorization, error) {
	if a.Evaluator == nil || a.Clock == nil {
		return ClockAuthorization{}, errClockAuthorityUnavailable
	}
	now := a.Clock().UTC()
	if now.IsZero() || p.IssuedAt().After(now) || !p.ExpiresAt().After(now) {
		return ClockAuthorization{}, errClockAuthorityInvalid
	}
	decision, err := a.Evaluator.EvaluateClock(ctx, p, tenant, capability, subject, assignment, now)
	if err != nil {
		return ClockAuthorization{}, err
	}
	return decision, nil
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
	Workers   clockservice.WorkerDirectory
	Evaluator ClockAuthorityEvaluator
	Clock     func() time.Time
}

// NewProductionClockAuthority validates and constructs the production
// authority used by ComposeClock. Missing ports fail before any effect can be
// attempted.
func NewProductionClockAuthority(deps ProductionClockDependencies) (ProductionClockAuthority, error) {
	if deps.Workers == nil || deps.Evaluator == nil || deps.Clock == nil {
		return ProductionClockAuthority{}, errClockAuthorityUnavailable
	}
	return ProductionClockAuthority{Workers: deps.Workers, Evaluator: deps.Evaluator, Clock: deps.Clock}, nil
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
