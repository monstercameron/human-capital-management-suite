package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// RuntimeVersion names this workflow runtime in every execution context it
// pins, so replay and repair can tell which runtime produced a history.
const RuntimeVersion = "hcmnext.workflow.runtime/v1"

// Execution-context refusal codes.
const (
	// CodeModeNotAllowed: a node would run in an execution mode its compiled
	// effect class does not admit.
	CodeModeNotAllowed = "MODE_NOT_ALLOWED"
	// CodeContextDrift: the stored execution context no longer matches the
	// digest the instance pinned at start.
	CodeContextDrift = "EXECUTION_CONTEXT_DRIFT"
)

// DefaultLocale is the BCP 47 "undetermined" tag a start that names no locale
// pins, so the context is explicit rather than silently absent.
const DefaultLocale = "und"

// ExecutionContext is the immutable WorkflowExecutionContext an instance runs
// under (specs/workflow-runtime.md): who, for which tenant and organization,
// in which locale and legal, entitlement, risk and billing context, in which
// mode, on which workflow and runtime version. It is pinned at start and
// never changes; every step receives the same value.
type ExecutionContext struct {
	Principal     string `json:"principal"`
	PrincipalKind string `json:"principal_kind,omitempty"`
	Tenant        string `json:"tenant"`
	Organization  string `json:"organization,omitempty"`

	Locale             string `json:"locale"`
	LegalEntity        string `json:"legal_entity,omitempty"`
	LegalContextDigest string `json:"legal_context_digest,omitempty"`
	Purpose            string `json:"purpose,omitempty"`
	Residency          string `json:"residency,omitempty"`
	EntitlementDigest  string `json:"entitlement_digest,omitempty"`
	RiskClass          string `json:"risk_class,omitempty"`
	BillingRef         string `json:"billing_ref"`

	ExecutionMode      workflow.ExecutionMode `json:"execution_mode"`
	WorkflowID         string                 `json:"workflow_id"`
	WorkflowVersion    uint32                 `json:"workflow_version"`
	CompiledPlanDigest string                 `json:"compiled_plan_digest"`
	RuntimeVersion     string                 `json:"runtime_version"`
}

// Validate refuses a context missing any field the runtime relies on.
func (c ExecutionContext) Validate() error {
	switch {
	case strings.TrimSpace(c.Principal) == "":
		return refuse(CodeInvalidRecord, "", "", "execution context names no principal")
	case strings.TrimSpace(c.Tenant) == "":
		return refuse(CodeInvalidRecord, "", "", "execution context names no tenant")
	case strings.TrimSpace(c.Locale) == "":
		return refuse(CodeInvalidRecord, "", "", "execution context names no locale")
	case strings.TrimSpace(c.BillingRef) == "":
		return refuse(CodeInvalidRecord, "", "", "execution context names no billing reference")
	case !modeValid(c.ExecutionMode):
		return refuse(CodeInvalidRecord, "", "", "execution context mode %q is not declared", string(c.ExecutionMode))
	case c.WorkflowID == "" || c.CompiledPlanDigest == "" || c.RuntimeVersion == "":
		return refuse(CodeInvalidRecord, "", "", "execution context names no workflow, plan digest or runtime version")
	}
	return nil
}

// Digest is the canonical content digest the instance pins.
func (c ExecutionContext) Digest() string {
	body, _ := json.Marshal(c)
	sum := sha256.Sum256(append([]byte("hcmnext.workflow.ExecutionContext/v1\n"), body...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// NodeAllowsMode reports whether node's compiled effect class admits mode
// through the one shared rule, [workflow.CompiledNode.AdmitsMode].
func NodeAllowsMode(node workflow.CompiledNode, mode workflow.ExecutionMode) bool {
	return node.AdmitsMode(mode)
}

// DeriveExecutionContext builds the context a start pins from the approved
// proposal revision, the selected plan and the start request. It is pure, so
// a driver resuming an instance re-derives the same value from the start
// request it reconstructs and proves it against the pinned digest on every
// advancement.
func DeriveExecutionContext(req StartRequest, sel WorkflowSelection) ExecutionContext {
	rev := req.Proposal.Revision
	principal, principalKind := rev.CreatedBy.PrincipalID, string(rev.CreatedBy.Kind)
	organization, legalEntity, legalDigest := rev.OrganizationScopeID, rev.LegalEntityID, rev.ControlSnapshots.LegalContextDigest
	purpose, residency := rev.Purpose.Purpose, rev.Purpose.ResidencyRef
	entitlement := rev.ControlSnapshots.EntitlementDigest
	if req.StartSourceValue().Kind != StartSourceProposal {
		// Non-proposal starts still pin an explicit, deterministic principal.
		// The trigger or parent identity is the authority for the source.
		source := req.StartSourceValue()
		switch source.Kind {
		case StartSourceTrigger:
			principal, principalKind = "trigger:"+source.Trigger.TriggerID, "SYSTEM"
		case StartSourceParent:
			principal, principalKind = "workflow:"+source.Parent.InstanceID.String(), "WORKFLOW"
		}
		organization, legalEntity, legalDigest = "", "", ""
		purpose, residency, entitlement = "", "", ""
	}
	locale := strings.TrimSpace(req.Locale)
	if locale == "" {
		locale = DefaultLocale
	}
	tenant := req.TenantID.String()
	billing := strings.TrimSpace(req.BillingRef)
	if billing == "" {
		billing = "billing:tenant:" + tenant
	}
	out := ExecutionContext{
		Principal: principal, PrincipalKind: principalKind,
		Tenant: tenant, Organization: organization,
		Locale: locale, LegalEntity: legalEntity, LegalContextDigest: legalDigest,
		Purpose: purpose, Residency: residency,
		EntitlementDigest: entitlement, BillingRef: billing,
		ExecutionMode: req.ExecutionMode, WorkflowID: sel.WorkflowID, RuntimeVersion: RuntimeVersion,
	}
	if sel.Plan != nil {
		out.RiskClass, out.WorkflowVersion, out.CompiledPlanDigest = sel.Plan.RiskClass, sel.Plan.Version, sel.Plan.Digest()
	}
	return out
}

// recordExecutionContext inserts the instance's context once. A replayed
// start inserts nothing and keeps the originally pinned row.
func recordExecutionContext(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, c ExecutionContext, recordedAt time.Time) error {
	body, err := json.Marshal(c)
	if err != nil {
		return wrap(CodeInvalidRecord, instanceID.String(), "", err, "encode execution context")
	}
	if _, err := ex.Exec(ctx, `INSERT INTO workflow_execution_context (tenant_id, instance_id, context_digest, context, recorded_at)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT (tenant_id, instance_id) DO NOTHING`,
		tenantID, instanceID, c.Digest(), body, recordedAt.UTC()); err != nil {
		return wrap(CodeStorageFailed, instanceID.String(), "", err, "record execution context")
	}
	return nil
}

// LoadExecutionContext returns the context an instance pinned at start and
// refuses it unless the stored content still digests to the instance's
// effective_context_ref. found is false for an instance started before
// execution contexts were recorded.
func LoadExecutionContext(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 ExecutionContext, found bool, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.load_execution_context", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr, ret0.Digest()) }()
	inst, err := (Store{}).LoadInstance(ctx, ex, tenantID, instanceID)
	if err != nil {
		return ExecutionContext{}, false, err
	}
	if inst.EffectiveContextRef == "" {
		return ExecutionContext{}, false, nil
	}
	var body []byte
	var stored string
	err = ex.QueryRow(ctx, `SELECT context, context_digest FROM workflow_execution_context WHERE tenant_id = $1 AND instance_id = $2`,
		tenantID, instanceID).Scan(&body, &stored)
	if errors.Is(err, dbport.ErrNoRows) {
		return ExecutionContext{}, false, refuse(CodeContextDrift, instanceID.String(), "",
			"instance pins execution context %s but no context row exists", inst.EffectiveContextRef)
	}
	if err != nil {
		return ExecutionContext{}, false, wrap(CodeStorageFailed, instanceID.String(), "", err, "load execution context")
	}
	var c ExecutionContext
	if err := json.Unmarshal(body, &c); err != nil {
		return ExecutionContext{}, false, wrap(CodeInvalidRecord, instanceID.String(), "", err, "decode execution context")
	}
	if stored != inst.EffectiveContextRef || c.Digest() != inst.EffectiveContextRef {
		return ExecutionContext{}, false, refuse(CodeContextDrift, instanceID.String(), "",
			"stored execution context no longer matches the digest %s the instance pinned", inst.EffectiveContextRef)
	}
	return c, true, nil
}
