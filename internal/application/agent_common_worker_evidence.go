package application

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CommonAgentModelEvidence retains route evidence with the actual sponsored
// principal chain and proves every egress field against its current native owner.
// It never inserts a human actor to fit an on-behalf-of audit contract.
type CommonAgentModelEvidence struct {
	Runtime        *CommonAgentRuntime
	Work           *DatabaseCommonAgentModelWorkSource
	Core           dbport.Beginner
	CoreSchema     string
	RetentionClass string
	Now            func() time.Time
}

func NewCommonAgentModelEvidence(e CommonAgentModelEvidence) (*CommonAgentModelEvidence, error) {
	if e.Runtime == nil || e.Work == nil || e.Work.cfg.Runtime != e.Runtime || e.Core == nil || !required(e.CoreSchema) || !required(e.RetentionClass) || e.Now == nil {
		return nil, ErrCommonAgentWorker
	}
	return &e, nil
}

func (e *CommonAgentModelEvidence) current(ctx context.Context) (commonAgentModelEvidence, error) {
	if e == nil || ctx == nil || e.Runtime == nil || e.Work == nil {
		return commonAgentModelEvidence{}, ErrCommonAgentWorker
	}
	bound, ok := ctx.Value(commonAgentModelEvidenceKey{}).(commonAgentModelEvidence)
	if !ok {
		return commonAgentModelEvidence{}, ErrCommonAgentWorker
	}
	dispatch, ok := ctx.Value(openAIModelDispatchContextKey{}).(openAIModelDispatchBinding)
	if !ok || dispatch.tenant != bound.Run.TenantID || dispatch.runID != bound.Run.ID || dispatch.stepID != bound.Request.StepID {
		return commonAgentModelEvidence{}, ErrCommonAgentWorker
	}
	record, err := e.Runtime.GetAdmission(ctx, bound.Run.TenantID, bound.Run.ID)
	if err != nil || !reflect.DeepEqual(record, bound.Record) {
		return commonAgentModelEvidence{}, ErrCommonAgentWorker
	}
	run, err := e.Runtime.GetRun(ctx, bound.Run.TenantID, bound.Run.ID)
	if err != nil || run.Lease == nil || bound.Run.Lease == nil || run.Fence != bound.Run.Fence || run.Version != bound.Run.Version || !reflect.DeepEqual(run.Lease, bound.Run.Lease) || !run.Lease.Until.After(e.Now().UTC()) || commonAgentWorkerRequest(record, run, bound.Request) != nil {
		return commonAgentModelEvidence{}, ErrCommonAgentWorker
	}
	if err := e.Runtime.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return commonAgentModelEvidence{}, err
	}
	bound.Record, bound.Run = record, run
	return bound, nil
}

func (e *CommonAgentModelEvidence) RecordRoute(ctx context.Context, route agentmodel.RouteRecord) error {
	bound, err := e.current(ctx)
	if err != nil {
		return err
	}
	_, _, policy, err := e.Work.current(ctx, bound.Record, bound.Run)
	selected := route.Selected == policy.Route.Pin.Primary && route.Selected == bound.Request.Route.Pin.Primary && route.Denial == ""
	denied := route.Selected == (agentmodel.ModelSelection{}) && route.Denial != ""
	if err != nil || route.TraceID != bound.Request.StepID || route.AgentVersionDigest != bound.Run.AgentDigest || route.TaskProfileID != bound.Request.Route.Task.ID || route.Region != bound.Request.Route.Task.Region || route.BudgetMicros != bound.Request.Route.BudgetRemainingMicros || (!selected && !denied) || !personaRunAuthorityDigest("sha256:"+route.Digest) {
		return ErrCommonAgentWorker
	}
	withoutDigest := route
	withoutDigest.Digest = ""
	routeBytes, err := json.Marshal(withoutDigest)
	if err != nil || personaRunBytesDigest(routeBytes) != "sha256:"+route.Digest {
		return ErrCommonAgentWorker
	}
	tenant := e.Work.cfg.TenantUUID(values.TenantId(bound.Run.TenantID))
	encoded, err := json.Marshal(struct {
		AdmissionID   string                  `json:"admission_id"`
		RequestDigest string                  `json:"request_digest"`
		Principal     agentrun.PrincipalChain `json:"principal_chain"`
		Audience      agentrun.AudienceScope  `json:"audience"`
		Context       agentrun.ContextScope   `json:"context_scope"`
		Fence         uint64                  `json:"worker_fence"`
		Route         agentmodel.RouteRecord  `json:"route"`
	}{bound.Run.ID, bound.Record.RequestDigest, bound.Record.Authority.Principal, bound.Record.Authority.Audience, bound.Record.Authority.Context, bound.Run.Fence, route})
	if err != nil {
		return err
	}
	tx, err := e.Core.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	schema := artifacts.Schema(e.CoreSchema)
	record, _, err := artifacts.Put(ctx, tx, schema, artifacts.PutRequest{Tenant: tenant, Content: encoded, MediaType: "application/json", Classification: model.ClassInternal, RetentionClass: e.RetentionClass, CreatorPrincipalRef: commonAgentExecutionPrincipal(bound.Record.Request), EvidenceID: bound.Run.ID})
	if err != nil {
		return err
	}
	if err := artifacts.AddReference(ctx, tx, schema, tenant, record.ContentID, artifacts.OwnerRef{Kind: artifacts.OwnerObservation, ID: bound.Run.ID + ":model-route:" + route.TraceID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CommonAgentToolModelEvidence must reload a durable owner receipt and verify
// its exact output before a capability result may return to the model.
type CommonAgentToolModelEvidence interface {
	VerifyCommonAgentToolModelSource(context.Context, agentrun.Record, agentegress.SourceClassificationRequest) error
}

func (e *CommonAgentModelEvidence) VerifySourceClassification(ctx context.Context, request agentegress.SourceClassificationRequest) error {
	bound, err := e.current(ctx)
	if err != nil {
		return err
	}
	if request.Tenant != bound.Run.TenantID || request.Purpose != bound.Request.Outbound.Purpose || !slices.Contains(request.Provenance, "common-agent-run:"+bound.Run.ID) {
		return ErrCommonAgentWorker
	}
	manifest, instructions, route, err := e.Work.current(ctx, bound.Record, bound.Run)
	if err != nil {
		return err
	}
	owner := e.Work.cfg.Contexts[bound.Record.Request.Source.Kind]
	if owner == nil {
		return ErrCommonAgentWorker
	}
	material, err := owner.ReadCommonAgentSourceContext(ctx, bound.Record, bound.Run)
	if err != nil || !commonAgentSourceContextValid(material) {
		return ErrCommonAgentWorker
	}
	var value string
	switch request.SourceClass {
	case "common-agent-profile":
		if request.DataClass != route.ProfileClass {
			return ErrCommonAgentWorker
		}
		switch request.FieldName {
		case "model.message.0":
			value = manifest.Purpose
		case "model.message.1":
			value = instructions
		default:
			return ErrCommonAgentWorker
		}
	case "common-agent-source-context":
		if request.DataClass != route.ThreadClass {
			return ErrCommonAgentWorker
		}
		for i, message := range material.Messages {
			if request.FieldName == fmt.Sprintf("model.message.%d", i+2) {
				value = message.Content
				break
			}
		}
	case "common-agent-context-ref":
		if request.DataClass != route.ThreadClass {
			return ErrCommonAgentWorker
		}
		for i, ref := range material.References {
			if request.FieldName == fmt.Sprintf("model.context.%d", i) {
				value = fmt.Sprintf("%s\x00%s\x00%s", ref.ID, ref.Version, ref.Digest)
				break
			}
		}
	case "common-agent-tool-proposal", "common-agent-tool-result":
		toolOwner, ok := owner.(CommonAgentToolModelEvidence)
		if !ok || request.DataClass != route.ThreadClass || !strings.HasPrefix(request.FieldName, "model.message.") {
			return ErrCommonAgentWorker
		}
		return toolOwner.VerifyCommonAgentToolModelSource(ctx, bound.Record, request)
	default:
		return ErrCommonAgentWorker
	}
	if value == "" || personaRunBytesDigest([]byte(value)) != request.ValueDigest {
		return ErrCommonAgentWorker
	}
	return nil
}

var _ agentmodel.RouteRecorder = (*CommonAgentModelEvidence)(nil)
var _ agentegress.SourceClassificationVerifier = (*CommonAgentModelEvidence)(nil)
