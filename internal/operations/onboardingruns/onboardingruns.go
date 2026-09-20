package onboardingruns

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
)

const (
	OnboardingRunPublished       = "PUBLISHED"
	OnboardingRunPreflightPassed = "PREFLIGHT_PASSED"
	OnboardingRunPreflightFailed = "PREFLIGHT_FAILED"
	OnboardingRunExtracting      = "EXTRACTING"
	OnboardingRunExtracted       = "EXTRACTED"
	OnboardingRunAdjudicated     = "ADJUDICATED"
	OnboardingRunCutoverExecuted = "CUTOVER_EXECUTED"
	OnboardingRunAborted         = "ABORTED"
)

var (
	ErrOnboardingRunNotFound    = errors.New("onboarding: run not found")
	ErrOnboardingStateConflict  = errors.New("onboarding: run state forbids the transition")
	ErrOnboardingTenantMismatch = errors.New("onboarding: manifest tenant does not match caller tenant")
	ErrOnboardingUnconfigured   = errors.New("onboarding: no connector resolver configured")
	ErrOnboardingConflict       = errors.New("onboarding: manifest conflicts with the published run")
)

type OnboardingBundle struct {
	Connector       connectivity.Connector
	Connection      *connectivity.ConnectorConnection
	Observations    observe.ObservationStore
	Checkpoints     observe.CheckpointStore
	PublicKey       ed25519.PublicKey
	FreshnessBudget time.Duration
	Now             func() time.Time
}

type OnboardingResolver func(ctx context.Context, tenantID string) (OnboardingBundle, error)

type OnboardingAdjudicationScope struct {
	Object      connectivity.ObjectKind
	Pin         onboarding.ReferencePin
	Snapshot    onboarding.CrosswalkSnapshot
	ExternalIDs []string
}

type OnboardingRunView struct {
	RunID          string
	TenantID       string
	State          string
	ManifestDigest string
	AbortedReason  string
	Preflight      *onboarding.PreflightResult
	Extraction     []onboarding.ObjectResult
	Adjudications  []onboarding.Adjudication
	Cutover        *onboarding.CutoverDecision
	Reconciliation *onboarding.ReconciliationReport
}

type onboardingRun struct {
	tenant         string
	signed         onboarding.SignedManifest
	digest         string
	state          string
	abortWhy       string
	preflight      *onboarding.PreflightResult
	extraction     []onboarding.ObjectResult
	adjudications  []onboarding.Adjudication
	cutover        *onboarding.CutoverDecision
	reconciliation *onboarding.ReconciliationReport
}

type OnboardingRuns struct {
	mu       sync.Mutex
	resolver OnboardingResolver
	runs     map[string]map[string]*onboardingRun
}

func NewOnboardingRuns(resolver OnboardingResolver) *OnboardingRuns {
	return &OnboardingRuns{resolver: resolver, runs: make(map[string]map[string]*onboardingRun)}
}

func (r *OnboardingRuns) lookup(tenantID, runID string) (*onboardingRun, error) {
	run := r.runs[tenantID][runID]
	if run == nil {
		return nil, fmt.Errorf("onboarding: run %q: %w", runID, ErrOnboardingRunNotFound)
	}
	return run, nil
}

func (r *OnboardingRuns) bundle(ctx context.Context, tenantID string) (OnboardingBundle, error) {
	if r.resolver == nil {
		return OnboardingBundle{}, ErrOnboardingUnconfigured
	}
	b, err := r.resolver(ctx, tenantID)
	if err != nil {
		return OnboardingBundle{}, err
	}
	if b.Connector == nil || b.Connection == nil || b.Observations == nil || b.Checkpoints == nil || len(b.PublicKey) == 0 {
		return OnboardingBundle{}, fmt.Errorf("onboarding: resolver returned an incomplete bundle: %w", ErrOnboardingUnconfigured)
	}
	return b, nil
}
func (r *OnboardingRuns) Publish(ctx context.Context, tenantID string, sm onboarding.SignedManifest) (string, error) {
	b, err := r.bundle(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(tenantID) == "" {
		return "", fmt.Errorf("onboardingruns.OnboardingRuns.Publish: empty tenant: %w", ErrOnboardingTenantMismatch)
	}
	if sm.Manifest.TenantID != tenantID {
		return "", fmt.Errorf("onboardingruns.OnboardingRuns.Publish: manifest names tenant %q: %w", sm.Manifest.TenantID, ErrOnboardingTenantMismatch)
	}
	if err := sm.Manifest.Validate(); err != nil {
		return "", err
	}
	if _, err := onboarding.VerifyManifest(sm, b.PublicKey); err != nil {
		return "", err
	}
	digest, err := sm.Manifest.Digest()
	if err != nil {
		return "", err
	}
	runID := "ob-" + digestSuffix(digest)

	r.mu.Lock()
	defer r.mu.Unlock()
	byTenant := r.runs[tenantID]
	if byTenant == nil {
		byTenant = make(map[string]*onboardingRun)
		r.runs[tenantID] = byTenant
	}
	if existing := byTenant[runID]; existing != nil {
		if existing.digest != digest {
			return "", fmt.Errorf("onboardingruns.OnboardingRuns.Publish: run %q already published different content: %w", runID, ErrOnboardingConflict)
		}
		return runID, nil
	}
	byTenant[runID] = &onboardingRun{tenant: tenantID, signed: sm, digest: digest, state: OnboardingRunPublished}
	return runID, nil
}

func digestSuffix(digest string) string {
	hexPart := digest
	if i := strings.Index(digest, ":"); i >= 0 {
		hexPart = digest[i+1:]
	}
	if len(hexPart) > 16 {
		hexPart = hexPart[:16]
	}
	return hexPart
}

func (r *OnboardingRuns) RunPreflight(ctx context.Context, tenantID, runID string) (onboarding.PreflightResult, error) {
	b, err := r.bundle(ctx, tenantID)
	if err != nil {
		return onboarding.PreflightResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.lookup(tenantID, runID)
	if err != nil {
		return onboarding.PreflightResult{}, err
	}
	if run.state != OnboardingRunPublished && run.state != OnboardingRunPreflightFailed {
		return onboarding.PreflightResult{}, fmt.Errorf("onboardingruns.OnboardingRuns.RunPreflight: preflight from state %q: %w", run.state, ErrOnboardingStateConflict)
	}
	res, err := onboarding.Preflight{
		Connector:  b.Connector,
		Connection: b.Connection,
		PublicKey:  b.PublicKey,
		Now:        b.Now,
	}.Run(ctx, run.signed)
	if err != nil {
		run.state = OnboardingRunPreflightFailed
		if res.Reason != nil {
			run.preflight = &res
		}
		return res, err
	}
	run.preflight = &res
	run.state = OnboardingRunPreflightPassed
	return res, nil
}
func (r *OnboardingRuns) StartExtraction(ctx context.Context, tenantID, runID string, req onboarding.ExtractRequest) ([]onboarding.ObjectResult, error) {
	b, err := r.bundle(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.lookup(tenantID, runID)
	if err != nil {
		return nil, err
	}
	switch run.state {
	case OnboardingRunPreflightPassed, OnboardingRunExtracting, OnboardingRunExtracted:
	default:
		return nil, fmt.Errorf("application.OnboardingRuns.StartExtraction: extraction from state %q: %w", run.state, ErrOnboardingStateConflict)
	}
	budget := b.FreshnessBudget
	if budget <= 0 {
		budget = 24 * time.Hour
	}
	req.RunID = runID
	results, err := onboarding.Extractor{
		Connector:       b.Connector,
		Connection:      b.Connection,
		Observations:    b.Observations,
		Checkpoints:     b.Checkpoints,
		FreshnessBudget: budget,
		Now:             b.Now,
	}.Extract(ctx, run.signed.Manifest, req)
	if err != nil {
		return results, err
	}
	run.extraction = append([]onboarding.ObjectResult(nil), results...)
	run.state = OnboardingRunExtracting
	if extractionsComplete(results) {
		run.state = OnboardingRunExtracted
	}
	return append([]onboarding.ObjectResult(nil), results...), nil
}

func extractionsComplete(results []onboarding.ObjectResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, res := range results {
		if res.SnapshotChanged {
			return false
		}
		if res.Run.Status != observe.RunCompleted && res.Run.Status != observe.RunAlreadyComplete {
			return false
		}
	}
	return true
}

func (r *OnboardingRuns) ReviewAdjudication(ctx context.Context, tenantID, runID string, scopes []OnboardingAdjudicationScope) ([]onboarding.Adjudication, error) {
	if _, err := r.bundle(ctx, tenantID); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.lookup(tenantID, runID)
	if err != nil {
		return nil, err
	}
	if run.state != OnboardingRunExtracted && run.state != OnboardingRunAdjudicated {
		return nil, fmt.Errorf("application.OnboardingRuns.ReviewAdjudication: adjudication from state %q: %w", run.state, ErrOnboardingStateConflict)
	}
	snapshots := make(map[connectivity.ObjectKind]onboarding.CrosswalkSnapshot, len(scopes))
	var out []onboarding.Adjudication
	for _, scope := range scopes {
		if !pinDeclared(run.signed.Manifest.ReferencePins, scope.Pin) {
			return nil, fmt.Errorf("application.OnboardingRuns.ReviewAdjudication: pin %q at version %q is not declared by the manifest: %w", scope.Pin.Name, scope.Pin.Version, ErrOnboardingStateConflict)
		}
		snapshots[scope.Object] = scope.Snapshot
		adjudicator := onboarding.Adjudicator{Snapshots: snapshots}
		for _, externalID := range scope.ExternalIDs {
			adj, err := adjudicator.Adjudicate(scope.Object, externalID, scope.Pin)
			if err != nil {
				return nil, err
			}
			out = append(out, adj)
		}
	}
	run.adjudications = append([]onboarding.Adjudication(nil), out...)
	run.state = OnboardingRunAdjudicated
	return append([]onboarding.Adjudication(nil), out...), nil
}

func pinDeclared(pins []onboarding.ReferencePin, want onboarding.ReferencePin) bool {
	for _, p := range pins {
		if p.Name == want.Name && p.Version == want.Version && strings.EqualFold(p.Digest, want.Digest) {
			return true
		}
	}
	return false
}
func (r *OnboardingRuns) ExecuteCutover(ctx context.Context, tenantID, runID string, ev onboarding.CutoverEvidence, signer onboarding.CutoverSigner) (onboarding.CutoverDecision, error) {
	if _, err := r.bundle(ctx, tenantID); err != nil {
		return onboarding.CutoverDecision{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.lookup(tenantID, runID)
	if err != nil {
		return onboarding.CutoverDecision{}, err
	}
	if run.state != OnboardingRunAdjudicated {
		return onboarding.CutoverDecision{}, fmt.Errorf("application.OnboardingRuns.ExecuteCutover: cutover from state %q: %w", run.state, ErrOnboardingStateConflict)
	}
	if ev.Tenant != tenantID {
		return onboarding.CutoverDecision{}, fmt.Errorf("application.OnboardingRuns.ExecuteCutover: evidence names tenant %q: %w", ev.Tenant, ErrOnboardingTenantMismatch)
	}
	dec, err := onboarding.DecideCutover(ev, signer)
	if err != nil {
		return onboarding.CutoverDecision{}, err
	}
	run.cutover = &dec
	if dec.Outcome == onboarding.CutoverEpochSigned {
		run.state = OnboardingRunCutoverExecuted
	}
	return dec, nil
}

func (r *OnboardingRuns) Abort(_ context.Context, tenantID, runID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.lookup(tenantID, runID)
	if err != nil {
		return err
	}
	switch run.state {
	case OnboardingRunCutoverExecuted, OnboardingRunAborted:
		return fmt.Errorf("application.OnboardingRuns.Abort: abort from state %q: %w", run.state, ErrOnboardingStateConflict)
	}
	run.state = OnboardingRunAborted
	run.abortWhy = reason
	return nil
}

func (r *OnboardingRuns) ReportReconciliation(_ context.Context, tenantID, runID, importID string, source []onboarding.SourceRow, target []onboarding.TargetRecord) (onboarding.ReconciliationReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.lookup(tenantID, runID)
	if err != nil {
		return onboarding.ReconciliationReport{}, err
	}
	if run.state != OnboardingRunAdjudicated && run.state != OnboardingRunCutoverExecuted {
		return onboarding.ReconciliationReport{}, fmt.Errorf("application.OnboardingRuns.ReportReconciliation: reconciliation from state %q: %w", run.state, ErrOnboardingStateConflict)
	}
	rep, err := onboarding.Reconcile(importID, source, target)
	if err != nil {
		return onboarding.ReconciliationReport{}, err
	}
	run.reconciliation = &rep
	return rep, nil
}

func (r *OnboardingRuns) Get(_ context.Context, tenantID, runID string) (OnboardingRunView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.lookup(tenantID, runID)
	if err != nil {
		return OnboardingRunView{}, err
	}
	view := OnboardingRunView{
		RunID:          runID,
		TenantID:       run.tenant,
		State:          run.state,
		ManifestDigest: run.digest,
		AbortedReason:  run.abortWhy,
		Preflight:      run.preflight,
		Cutover:        run.cutover,
		Reconciliation: run.reconciliation,
	}
	view.Extraction = append([]onboarding.ObjectResult(nil), run.extraction...)
	view.Adjudications = append([]onboarding.Adjudication(nil), run.adjudications...)
	return view, nil
}

type OnboardingOperator interface {
	Publish(ctx context.Context, tenantID string, sm onboarding.SignedManifest) (string, error)
	RunPreflight(ctx context.Context, tenantID, runID string) (onboarding.PreflightResult, error)
	StartExtraction(ctx context.Context, tenantID, runID string, req onboarding.ExtractRequest) ([]onboarding.ObjectResult, error)
	ReviewAdjudication(ctx context.Context, tenantID, runID string, scopes []OnboardingAdjudicationScope) ([]onboarding.Adjudication, error)
	ExecuteCutover(ctx context.Context, tenantID, runID string, ev onboarding.CutoverEvidence, signer onboarding.CutoverSigner) (onboarding.CutoverDecision, error)
	Abort(ctx context.Context, tenantID, runID, reason string) error
	ReportReconciliation(ctx context.Context, tenantID, runID, importID string, source []onboarding.SourceRow, target []onboarding.TargetRecord) (onboarding.ReconciliationReport, error)
	Get(ctx context.Context, tenantID, runID string) (OnboardingRunView, error)
}
