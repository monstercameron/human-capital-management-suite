package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workforce"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var errSelfClockDenied = errors.New("self clock authority: a worker may act only for their own clock")

// SelfClockWorkforce answers the worker self-clock's identity questions from
// the canonical workforce record. The worker reference it publishes is the
// worker key, which is exactly the trusted principal's subject, so a
// self-service punch is never mistaken for a delegated one. The current
// assignment is the workforce assignment id: the clock needs an assignment to
// pin a time profile to, and it never invents a project or site.
type SelfClockWorkforce struct {
	Pool *pgxadapter.Pool
}

var _ clockservice.WorkerDirectory = SelfClockWorkforce{}

// ResolveSelfWorker maps the principal's subject to its own active worker.
func (w SelfClockWorkforce) ResolveSelfWorker(ctx context.Context, tenant, subject string) (clockservice.SelfWorker, error) {
	row, err := w.read(ctx, tenant, subject)
	if err != nil {
		return clockservice.SelfWorker{}, err
	}
	name := strings.TrimSpace(row.PreferredName)
	if name == "" {
		name = strings.TrimSpace(row.LegalName)
	}
	return clockservice.SelfWorker{WorkerRef: row.WorkerKey, DisplayName: name, AssignmentRef: row.AssignmentID, ScheduleLabel: scheduleLabel(row), Active: true}, nil
}

// ResolveWorker returns the canonical worker key for an active worker.
func (w SelfClockWorkforce) ResolveWorker(ctx context.Context, tenant, ref string) (string, bool, error) {
	row, err := w.read(ctx, tenant, ref)
	if err != nil {
		if errors.Is(err, clockservice.ErrWorkerNotEligible) {
			return "", false, nil
		}
		return "", false, err
	}
	return row.WorkerKey, true, nil
}

// ResolveAssignment accepts only the worker's own current assignment.
func (w SelfClockWorkforce) ResolveAssignment(ctx context.Context, tenant, worker, assignment string) (string, string, bool, error) {
	row, err := w.read(ctx, tenant, worker)
	if err != nil {
		if errors.Is(err, clockservice.ErrWorkerNotEligible) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return "", "", row.AssignmentID != "" && row.AssignmentID == strings.TrimSpace(assignment), nil
}

func (w SelfClockWorkforce) read(ctx context.Context, tenant, ref string) (workforce.WorkerRow, error) {
	if w.Pool == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(ref) == "" {
		return workforce.WorkerRow{}, clockservice.ErrUnavailable
	}
	tenantID := pgstore.TenantID(tenant)
	tx, err := w.Pool.BeginReadOnly(ctx)
	if err != nil {
		return workforce.WorkerRow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return workforce.WorkerRow{}, err
	}
	row, found, err := (workforce.Store{}).Get(ctx, tx, tenantID, strings.TrimSpace(ref))
	if err != nil {
		return workforce.WorkerRow{}, err
	}
	if !found || !strings.EqualFold(row.LifecycleStatus, "active") {
		return workforce.WorkerRow{}, clockservice.NotEligible(clockservice.ReasonNoWorkerRecord, "principal does not resolve to an active worker")
	}
	return row, nil
}

// scheduleLabel is the schedule fact shown beside the clock: the recorded job
// and where the worker is based. No published shift is claimed.
func scheduleLabel(row workforce.WorkerRow) string {
	title, place := strings.TrimSpace(row.JobTitle), strings.TrimSpace(row.Location)
	switch {
	case title != "" && place != "":
		return title + " · " + place
	case title != "":
		return title
	case place != "":
		return place
	}
	return "No shift published"
}

// SelfClockAuthority lets a signed-in human act only for their own clock. It
// grants no delegation, no device administration and no supervisor override:
// those belong to other capabilities this composition does not serve.
type SelfClockAuthority struct {
	Workers clockservice.WorkerDirectory
}

var _ clockservice.Authorizer = SelfClockAuthority{}

// AuthorizePunch allows a punch when the principal's own worker is the worker
// being punched, re-resolved at decision time.
func (a SelfClockAuthority) AuthorizePunch(ctx context.Context, p *trust.Principal, tenant, worker, _ string) (bool, error) {
	if a.Workers == nil || p == nil || strings.TrimSpace(tenant) == "" || string(p.Tenant()) != strings.TrimSpace(tenant) {
		return false, errSelfClockDenied
	}
	own, active, err := a.Workers.ResolveWorker(ctx, tenant, p.Subject())
	if err != nil {
		return false, err
	}
	target, targetActive, err := a.Workers.ResolveWorker(ctx, tenant, worker)
	if err != nil {
		return false, err
	}
	if !active || !targetActive || own == "" || own != target {
		return false, errSelfClockDenied
	}
	return false, nil
}

// AuthorizeDeviceAdmin is never granted through the worker self clock.
func (SelfClockAuthority) AuthorizeDeviceAdmin(context.Context, *trust.Principal, string, string) error {
	return errSelfClockDenied
}

// AuthorizeSupervisorOverride is never granted through the worker self clock.
func (SelfClockAuthority) AuthorizeSupervisorOverride(context.Context, *trust.Principal, string, string) error {
	return errSelfClockDenied
}

// clockIDs derives stable identifiers: a deterministic id is a name-based
// UUID of its parts, so a replayed punch resolves to the same observation.
type clockIDs struct{}

var _ clockservice.IDs = clockIDs{}

func (clockIDs) Deterministic(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return uuid.NewSHA1(uuid.NameSpaceOID, sum[:]).String()
}

func (clockIDs) Random() string { return uuid.NewString() }

const clockSignalSource = "hcmnext.time.clock"

// clockAttestedSignalVerifier admits only signals the clock service built
// itself after authorizing the punch: the clock's own source and no external
// signature. Webhook bytes reaching this path by mistake carry a signature or
// another source and are refused.
type clockAttestedSignalVerifier struct{}

var _ stepSignal.Verifier = clockAttestedSignalVerifier{}

func (clockAttestedSignalVerifier) Verify(sig stepSignal.Signal) error {
	if sig.Source != clockSignalSource {
		return fmt.Errorf("clock signal: source %q is not the attested %q", sig.Source, clockSignalSource)
	}
	if len(sig.Signature) != 0 {
		return errors.New("clock signal: an attested clock signal carries no external signature")
	}
	return nil
}

// clockPlanResolver resolves the published clock-in/out plan for every start
// and continuation; the plan is pinned by digest and looked up in the durable
// version registry, never selected by name alone.
type clockPlanResolver struct{ plan *workflow.CompiledWorkflow }

var _ runtime.WorkflowResolver = clockPlanResolver{}

func (r clockPlanResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	if r.plan == nil {
		return runtime.WorkflowSelection{}, errors.New("clock workflow: plan is not composed")
	}
	return runtime.WorkflowSelection{WorkflowID: r.plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: r.plan.Digest()}, Plan: r.plan}, nil
}

// clockPlanSteps runs the clock plan's non-commit nodes: the wait for a
// clock-out parks on its signal, and a terminal simply completes. The commit
// nodes never reach it; the request-scoped commit runner owns them.
type clockPlanSteps struct{}

var _ execute.StepRunner = clockPlanSteps{}

func (clockPlanSteps) Run(_ context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	switch req.Node.Type {
	case workflow.StepSignal:
		return frontier.NodeOutcome{NodeID: req.Node.ID, Await: frontier.AwaitSignal, AwaitRef: "clock_out"}, runtime.GovernanceRefs{}, nil
	case workflow.StepEnd:
		// The terminal fact records what the END node produced, and refuses a
		// terminal with no output digest. The node's only output is its fixed
		// terminal code, so its digest is stable per instance and node.
		return frontier.NodeOutcome{NodeID: req.Node.ID, OutputDigest: clockTerminalDigest(req)}, runtime.GovernanceRefs{}, nil
	}
	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("clock workflow: node %s of type %s has no step handler", req.Node.ID, req.Node.Type)
}

// clockTerminalDigest is the output digest of a clock terminal node: a
// sha256 over the node and the instance it closed.
func clockTerminalDigest(req execute.StepRequest) string {
	sum := sha256.Sum256([]byte("clock-terminal\x00" + req.Node.ID + "\x00" + req.InstanceID.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
