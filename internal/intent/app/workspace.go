package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// WorkspacePath is where the HTTP edge serves the human-facing workspace
// subtree. It is the workspace package's own route prefix, so the transport
// composition adapter and the workspace package cannot disagree.
const WorkspacePath = workspace.RoutePrefix

// DiscoveryPath is where [Cell.EdgeHandler] serves the API-001 discovery
// document. The app owns the advertised application shape and its rendering
// alike; there is no separate transport-owned copy of it.
const DiscoveryPath = "/v1/discovery"

// WorkspaceRoutesKey is the discovery-document key under which the transport
// composition adapter advertises workspace routes when the surface is served.
const WorkspaceRoutesKey = "workspace_routes"

// workspaceReader adapts one composed cell to the workspace port
// (internal/humanwork/workspace.Cell).
//
// It is an adapter and nothing more: every answer is produced by the same
// governed capability gateway the RPC surfaces invoke through, under the same
// authorization evaluator, with the same evidence recorded. There is no
// second path to a domain answer for the sake of a web page.
type workspaceReader struct {
	svc *IntentService
	// locate resolves the request's worker reference. It is the cell's own
	// locator, so this surface and the domain-input resolver cannot disagree
	// about what a reference names -- including a worker somebody created,
	// whose key is not a well-formed entity identifier and would otherwise be
	// refused here.
	locate WorkerLocator
	// now supplies the knowledge cut-off the governed read is taken at and
	// the instant the authorization decision is evaluated at.
	now func() time.Time
	// positionReader is PROMOUX-004's real position.PositionFacts adapter,
	// the cell's own (nil on a cell with no execution database). This page
	// never writes -- ground five (reservation ownership) is deliberately
	// never reached here; see promotion.evaluateTargetPositionSelection's
	// package doc for why a bare identifier's check stops at ground four.
	positionReader position.PositionFacts
	// managerFacts is PROMOUX-005's real org.WorkerFacts adapter, the cell's
	// own (nil on a cell with no execution database). It answers both
	// existence/disclosure and the reporting-chain reachability walk for a
	// selected target manager and every affected direct report; see
	// promotion.evaluateTargetManagerSelection.
	managerFacts org.WorkerFacts
}

var _ workspace.Cell = workspaceReader{}

// WorkspacePort returns this cell as the workspace's live-data port.
func (c *Cell) WorkspacePort() workspace.Cell {
	now := c.Config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return workspaceReader{svc: c.Service, now: now, locate: c.locateWorker, positionReader: c.positionReader, managerFacts: c.managerFacts}
}

// WorkspaceHandler builds the workspace HTTP surface over this cell, admitted
// by the same transport.Config the RPC surfaces use.
func (c *Cell) WorkspaceHandler() (http.Handler, error) {
	return workspace.NewHandler(workspace.Options{
		Cell:         c.WorkspacePort(),
		Config:       c.Config,
		Now:          c.Config.Now,
		RoleAccess:   c.RoleAccess,
		PublicOrigin: c.PublicOrigin(),
		// Nil on every cell composed without both an execution driver and its
		// database: the journey page then reports ErrJourneyUnavailable rather
		// than rendering a surface nothing can act on.
		Journey: c.Journey,
	})
}

// ReadPromotion implements [workspace.Cell].
//
// The ordering is the P1A ordering and is not negotiable: the governed worker
// read runs first and is the only source of baseline facts, the compensation
// gate is evaluated once, and the promotion preflight and simulation are
// invoked with the explanation the read produced rather than with anything
// the caller asserted.
func (a workspaceReader) ReadPromotion(ctx context.Context, req workspace.Request) (workspace.Reading, error) {
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return workspace.Reading{}, fmt.Errorf("%w: no verified principal", workspace.ErrDenied)
	}
	purpose := principal.DefaultPurpose()
	evaluatedAt := values.NewInstant(a.now())

	subject, resolved, locateErr := a.worker(ctx, principal.Tenant(), req.WorkerRef)
	if locateErr != nil {
		return workspace.Reading{}, locateErr
	}
	if !resolved {
		return workspace.Reading{}, fmt.Errorf("%w: %q", workspace.ErrWorkerUnknown, req.WorkerRef)
	}
	asOf, err := workspaceAsOf(req.EffectiveDate, evaluatedAt)
	if err != nil {
		return workspace.Reading{}, err
	}

	reading := workspace.Reading{
		Tenant:            principal.Tenant().String(),
		Worker:            subject,
		PolicyVersion:     AuthorizationPolicyVersion,
		CapabilityID:      promotion.IntentType,
		CapabilityVersion: fmt.Sprintf("%d", bootstrapCapabilityVersion),
		AsOf:              evaluatedAt.Time(),
	}

	fieldSet := workspaceFields(req.Fields)
	stateDecision, err := authorizeRead(principal, purpose, authorizationRequest{
		Subject:     subject,
		EvaluatedAt: evaluatedAt,
		Read:        peopleFields(fieldSet),
	})
	if err != nil {
		return workspace.Reading{}, workspaceDenial(err)
	}

	explanation, evidenceID, ownedErr := invokeExplain(ctx, a.svc, principal, purpose, people.ExplainWorkerStateRequest{
		Tenant:        subject.Tenant,
		Worker:        subject,
		AsOf:          asOf,
		Fields:        fieldSet,
		Authorization: peopleDecision(stateDecision, fieldSet),
	})
	if ownedErr != nil {
		return workspace.Reading{}, ownedErr
	}
	reading.Explanation = explanation
	reading.EvidenceIDs = append(reading.EvidenceIDs, evidenceID)

	switch {
	case explanation.Disclosure == people.DisclosureWithheld:
		return workspace.Reading{}, fmt.Errorf("%w: %s", workspace.ErrDenied, explanation.WithheldReason)
	case explanation.Presence == people.SubjectAbsent:
		return workspace.Reading{}, fmt.Errorf("%w: %q", workspace.ErrWorkerUnknown, req.WorkerRef)
	}

	// The compensation half is gated as a whole. A caller who may see the
	// assignment but not the pay gets the page without it rather than a
	// refusal, and without a simulation computed from numbers they may not
	// have: the two capabilities that would compute them are simply not
	// invoked.
	if _, gateErr := authorizeRead(principal, purpose, authorizationRequest{
		Subject:     subject,
		EvaluatedAt: evaluatedAt,
		Gate:        []authz.FieldID{authz.FieldBaseSalary, authz.FieldBonusTarget},
	}); gateErr != nil {
		if errors.Is(gateErr, ErrAuthorizationDenied) {
			reading.CompensationDenial = gateErr.Error()
			return reading, nil
		}
		return workspace.Reading{}, gateErr
	}
	reading.CompensationDisclosed = true

	proposed, ok := req.Proposed.Base.Get()
	if !ok {
		return workspace.Reading{}, fmt.Errorf("%w: the proposed base pay is %s",
			workspace.ErrQueryInvalid, req.Proposed.Base.State())
	}
	band := rewards.BandQuery{
		Tenant:   subject.Tenant,
		JobCode:  req.Target.JobCode,
		Grade:    req.Target.Grade,
		PayZone:  req.Target.PayZone,
		Currency: req.Currency,
		AsOf:     req.EffectiveDate,
	}
	position, evidenceID, ownedErr := invokePayBand(ctx, a.svc, principal, purpose, PayBandInputs{Query: band, Amount: proposed})
	if ownedErr != nil {
		return workspace.Reading{}, ownedErr
	}
	reading.PayBand = position
	reading.EvidenceIDs = append(reading.EvidenceIDs, evidenceID)

	compensation, evidenceID, ownedErr := invokeCompensation(ctx, a.svc, principal, purpose, rewards.SimulateCompensationInput{
		Tenant:        subject.Tenant,
		Subject:       subject,
		Current:       req.Current,
		Proposed:      req.Proposed,
		Annualization: req.Annualization,
		EffectiveDate: req.EffectiveDate,
		Band:          &band,
	})
	if ownedErr != nil {
		return workspace.Reading{}, ownedErr
	}
	reading.Compensation = compensation
	reading.EvidenceIDs = append(reading.EvidenceIDs, evidenceID)

	preflightRequest := promotion.PreflightRequest{
		Tenant:         subject.Tenant,
		Subject:        subject,
		Target:         req.Target,
		Current:        req.Current,
		Proposed:       req.Proposed,
		EffectiveDate:  req.EffectiveDate,
		EvaluationDate: req.EvaluationDate,
		BusinessReason: req.BusinessReason,
		Budget:         req.Budget,
		Policy:         req.Policy,
		Annualization:  req.Annualization,
		WorkerState:    explanation,
		// PROMOUX-004: whenever req.Target.PositionID is non-empty,
		// PreflightPromotion checks it against the real Position domain
		// through this reader rather than trusting it. A nil reader (no
		// execution database composed) still refuses a non-empty value --
		// see evaluateTargetPositionSelection -- rather than accepting it
		// unproven.
		PositionReader: a.positionReader,
		// PROMOUX-005: mirrors PositionReader immediately above. A nil
		// managerFacts (no execution database composed) still refuses a
		// non-nil TargetManagerSelection rather than accepting it unproven --
		// see evaluateTargetManagerSelection's own "no manager facts reader
		// configured" contract failure.
		ManagerFacts:           a.managerFacts,
		TargetManagerSelection: req.TargetManagerSelection,
	}
	preflight, evidenceID, ownedErr := invokePromotion(ctx, a.svc, principal, purpose,
		promotionCall{Mode: promotionModePreflight, Request: preflightRequest})
	if ownedErr != nil {
		return workspace.Reading{}, ownedErr
	}
	reading.Preflight = preflight.Preflight
	reading.EvidenceIDs = append(reading.EvidenceIDs, evidenceID)

	simulated, evidenceID, ownedErr := invokePromotion(ctx, a.svc, principal, purpose,
		promotionCall{Mode: promotionModeSimulate, Request: preflightRequest})
	if ownedErr != nil {
		return workspace.Reading{}, ownedErr
	}
	reading.Simulation = simulated.Simulation
	reading.Preflight = simulated.Preflight
	reading.EvidenceIDs = append(reading.EvidenceIDs, evidenceID)

	return reading, nil
}

// workspaceFields is the governed projection the workspace reads: everything
// the promotion preflight requires, plus the two name fields the page needs
// to address the worker as a person rather than as an identifier.
//
// The names are added to the projection rather than fetched separately so
// that they travel under the same authorization decision as everything else:
// a caller who may not see the name gets a page with no name, not a page
// assembled from two decisions made at different moments.
func workspaceFields(required []people.FieldID) []people.FieldID {
	fields := append([]people.FieldID(nil), required...)
	return append(fields, people.FieldPreferredName, people.FieldLegalName)
}

// worker resolves this surface's worker reference through the cell's own
// locator, in the caller's own tenant.
func (a workspaceReader) worker(
	ctx context.Context, tenant values.TenantId, ref string,
) (values.EntityRef, bool, error) {
	locate := a.locate
	if locate == nil {
		locate = corpusWorkerLocator
	}
	location, ok, err := locate(ctx, tenant, ref)
	if err != nil {
		return values.EntityRef{}, false, err
	}
	return location.Ref, ok, nil
}

// workspaceWorker resolves a worker reference against the corpus alone, with
// no tenant of its own.
//
// It survives for one caller: [journeySummaryFromProto], which renders a
// stored intent's summary without a context or a database to reach the created
// population with. That is safe there and nowhere else, because the summary's
// worker id is immediately overwritten from the intent's own EMPLOYMENT
// subject, which is the authoritative identity either way. The summary also
// replaces the corpus tenant with the stored intent's tenant before exposing
// the ref. Every surface that resolves a reference somebody typed uses the
// cell's locator instead.
func workspaceWorker(ref string) (values.EntityRef, bool) {
	location, ok, err := locateCorpusWorker(fixtures.Tenant, ref)
	if err != nil || !ok {
		return values.EntityRef{}, false
	}
	return location.Ref, true
}

// workspaceAsOf pins the bitemporal coordinate the governed read is taken at:
// the promotion's own effective date, known as of the trusted clock.
func workspaceAsOf(effective values.LocalDate, at values.Instant) (people.AsOf, error) {
	known, err := values.NewKnownAt(at)
	if err != nil {
		return people.AsOf{}, fmt.Errorf("app: workspace known-at: %w", err)
	}
	return people.AsOf{EffectiveOn: effective, KnownAt: known}, nil
}

// workspaceDenial projects an authorization refusal onto the workspace's own
// sentinel, so the page can answer 403 without importing the envelope model.
func workspaceDenial(err error) error {
	if errors.Is(err, ErrAuthorizationDenied) {
		return fmt.Errorf("%w: %s", workspace.ErrDenied, err.Error())
	}
	return err
}

// The four typed gateway invocations. Each one names the capability it means
// and asserts the answer's type, so a capability rebound to a handler that
// answers something else fails here rather than rendering as an empty page.

func invokeExplain(
	ctx context.Context, svc *IntentService, principal *trust.Principal, purpose string,
	req people.ExplainWorkerStateRequest,
) (people.Explanation, string, error) {
	answer, evidenceID, ownedErr := svc.invoke(ctx, principal, purpose,
		capabilityKeyFor(intent.Ref{TypeID: people.ExplainWorkerStateIntentType, Version: 1}), req)
	if ownedErr != nil {
		return people.Explanation{}, "", workspaceGatewayError(ownedErr)
	}
	got, ok := answer.(people.Explanation)
	if !ok {
		return people.Explanation{}, "", fmt.Errorf("app: explain_worker_state returned %T", answer)
	}
	return got, evidenceID, nil
}

func invokePayBand(
	ctx context.Context, svc *IntentService, principal *trust.Principal, purpose string,
	in PayBandInputs,
) (rewards.PayBandEvaluation, string, error) {
	answer, evidenceID, ownedErr := svc.invoke(ctx, principal, purpose,
		capabilityKeyFor(intent.Ref{TypeID: rewards.EvaluatePayBandIntentType, Version: 1}), in)
	if ownedErr != nil {
		return rewards.PayBandEvaluation{}, "", workspaceGatewayError(ownedErr)
	}
	got, ok := answer.(rewards.PayBandEvaluation)
	if !ok {
		return rewards.PayBandEvaluation{}, "", fmt.Errorf("app: evaluate_pay_band_position returned %T", answer)
	}
	return got, evidenceID, nil
}

func invokeCompensation(
	ctx context.Context, svc *IntentService, principal *trust.Principal, purpose string,
	in rewards.SimulateCompensationInput,
) (rewards.SimulateCompensationResult, string, error) {
	answer, evidenceID, ownedErr := svc.invoke(ctx, principal, purpose,
		capabilityKeyFor(intent.Ref{TypeID: rewards.SimulateCompensationIntentType, Version: 1}), in)
	if ownedErr != nil {
		return rewards.SimulateCompensationResult{}, "", workspaceGatewayError(ownedErr)
	}
	got, ok := answer.(rewards.SimulateCompensationResult)
	if !ok {
		return rewards.SimulateCompensationResult{}, "", fmt.Errorf("app: simulate_compensation returned %T", answer)
	}
	return got, evidenceID, nil
}

func invokePromotion(
	ctx context.Context, svc *IntentService, principal *trust.Principal, purpose string,
	call promotionCall,
) (promotionAnswer, string, error) {
	answer, evidenceID, ownedErr := svc.invoke(ctx, principal, purpose,
		capabilityKeyFor(intent.Ref{TypeID: promotion.IntentType, Version: 1}), call)
	if ownedErr != nil {
		return promotionAnswer{}, "", workspaceGatewayError(ownedErr)
	}
	got, ok := answer.(promotionAnswer)
	if !ok {
		return promotionAnswer{}, "", fmt.Errorf("app: promote_worker returned %T", answer)
	}
	return got, evidenceID, nil
}

// isPermissionDenied reports whether an owned error is the policy saying no,
// as distinct from the cell being unable to answer.
func isPermissionDenied(err error) bool {
	var owned *envelope.Error
	return errors.As(err, &owned) && owned.Code() == envelope.CodePermissionDenied
}

// workspaceGatewayError re-labels a gateway refusal for the workspace.
//
// A permission denial becomes the workspace's own ErrDenied so the page
// answers 403; everything else travels as the owned error's message, because
// a rendering surface has no use for the typed detail an RPC client parses.
func workspaceGatewayError(err error) error {
	if err == nil {
		return nil
	}
	if isPermissionDenied(err) {
		return fmt.Errorf("%w: %s", workspace.ErrDenied, err.Error())
	}
	return fmt.Errorf("app: workspace capability invocation: %w", err)
}
