package simulate

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// PromotionProposalTransformRef is the transform reference the promotion
// reference workflow's TRANSFORM node binds.
const PromotionProposalTransformRef = "transforms.promotion.build_proposal"

// PromotionTransforms builds the typed promotion proposal by running the
// promotion domain's own preflight and simulation.
//
// The proposal digest a workflow carries forward is therefore the digest of a
// real, versioned, zero-effect simulation result - the same artifact an
// approval would later be bound to - and not a hash the workflow layer made up.
// That matters because an approval bound to a digest nobody can reproduce is
// an approval of nothing.
type PromotionTransforms struct {
	Env *Environment
}

// Transform implements TransformPort.
func (t PromotionTransforms) Transform(ctx context.Context, req TransformRequest) (ret0 TransformResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.simulate.transform", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if t.Env == nil {
		return TransformResult{}, refuse(CodeInvalidOptions, req.NodeID, "no environment is bound")
	}
	if req.Transform.TransformRef != PromotionProposalTransformRef {
		return TransformResult{}, refuse(CodeHandlerFailed, req.NodeID,
			"no implementation is bound to transform %q", req.Transform.TransformRef)
	}

	base, err := moneyInput(req.Inputs, "proposed_base_pay")
	if err != nil {
		return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "build proposal")
	}
	ratio, err := req.Inputs.Get(FieldRaiseRatio)
	if err != nil {
		return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "build proposal")
	}
	position, err := req.Inputs.Get(FieldBandPosition)
	if err != nil {
		return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "build proposal")
	}

	explanation, err := t.Env.explain(ctx, t.Env.EffectiveDate)
	if err != nil {
		return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "governed worker read")
	}

	preflight := promotion.PreflightRequest{
		Tenant:         t.Env.Tenant,
		Subject:        t.Env.Worker,
		WorkerState:    explanation,
		Target:         t.Env.Target,
		Current:        t.Env.Current,
		Proposed:       t.Env.proposedSnapshot(base, t.Env.EffectiveDate),
		EffectiveDate:  t.Env.EffectiveDate,
		EvaluationDate: t.Env.EvaluationDate,
		BusinessReason: t.Env.BusinessReason,
		Budget:         t.Env.Budget,
		Policy:         t.Env.Policy,
		Annualization:  t.Env.Annualization,
	}
	result, err := promotion.SimulatePromotion(ctx, t.Env.Catalog, preflight)
	if err != nil {
		return TransformResult{}, wrap(CodeHandlerFailed, req.NodeID, err, "promotion simulation")
	}

	// A blocked, denied or under-informed preflight is not a proposal. The
	// transform's FAILED route exists exactly so the workflow stops instead of
	// carrying a digest of something the domain refused.
	if result.Preflight.Status != promotion.StatusReady {
		return TransformResult{
			Outcome: workflow.OutcomeFailed,
			Outputs: Bag{},
			Detail: "preflight verdict " + result.Preflight.Status.String() +
				" with " + itoa(uint64(len(result.Preflight.Blocking()))) + " blocking finding(s)",
		}, nil
	}

	return TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: Bag{
			"proposal_digest": NewString(result.ResultDigest),
			FieldRaiseRatio:   ratio,
			FieldBandPosition: position,
		},
		Detail: "proposal " + result.ResultDigest[:16] + " simulated: preflight " +
			result.Preflight.Status.String() + ", compensation " + result.CompensationState.String() +
			", executable=" + boolText(result.Executable),
	}, nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
