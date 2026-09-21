package execution

// release.go is WF-COMP-006's governed release path for the workflows this
// composition ships. Composition publishes each shipped version as a DRAFT and
// never approves or activates it: an operator runs the version's declared
// conformance fixtures, approves on the sealed report ([ApproveRelease]
// re-runs every fixture before recording it), and activates as a separate
// step (workflowversionstore.Store.ActivateApproved). A developer database
// takes the same path in one explicit command, [BootstrapDevVersions]
// (`hcmnext workflow-version bootstrap-dev`), under a distinct development
// release approver.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// The conformance fixtures each shipped workflow declares at publication.
const (
	FixtureApprovalCompile     = "fixture:workflow.promotion-approval/reproducible-compile@v1"
	FixtureApprovalRequirement = "fixture:workflow.promotion-approval/approval-step@v1"
	// FixtureExecuteCompile and FixtureExecuteGraph are the frozen 1.0.0
	// execute version's fixtures: they compare against
	// promotionexec.CompileV1_0.
	FixtureExecuteCompile = "fixture:workflow.promotion-execute/reproducible-compile@v1"
	FixtureExecuteGraph   = "fixture:workflow.promotion-execute/graph-and-simulation@v1"
	// FixtureExecuteCompileV1_1 and FixtureExecuteGraphV1_1 are the 1.1.0
	// execute version's fixtures: they compare against promotionexec.Compile.
	FixtureExecuteCompileV1_1 = "fixture:workflow.promotion-execute/reproducible-compile@v2"
	FixtureExecuteGraphV1_1   = "fixture:workflow.promotion-execute/graph-and-simulation@v2"
	// FixtureExecuteApprovals holds for both execute versions: their approval
	// requirements are the same.
	FixtureExecuteApprovals = "fixture:workflow.promotion-execute/approval-separation@v1"
)

const (
	// DevReleaseApprover is the development release approver
	// [BootstrapDevVersions] approves under: never the publisher, and named so
	// a production registry shows at a glance that a version was only ever
	// bootstrapped for local development.
	DevReleaseApprover  = "cmd/hcmnext:dev-release-approver"
	devReleaseAuthority = "authority:local-development-bootstrap"
	// unitCompositionApprover activates the private in-memory registry a
	// composition without a durable registry keeps (unit compositions only).
	unitCompositionApprover = "internal/platform/execution:unit-composition"
)

var (
	// ErrNoActiveWorkflowVersion reports a served start against a shipped
	// workflow no governed approval has activated yet.
	ErrNoActiveWorkflowVersion = errors.New("platform execution: the promotion workflow has no ACTIVE version; " +
		"approve and activate it (hcmnext workflow-version fixtures, approve, activate), " +
		"or run `hcmnext workflow-version bootstrap-dev` on a development database")
	// ErrReleaseApproval reports a malformed release approval request.
	ErrReleaseApproval = errors.New("platform execution: invalid release approval")
)

// releaseApprovalNamespace derives a stable approval id per (version,
// approver, report), so replaying one approval records it once.
var releaseApprovalNamespace = uuid.MustParse("3f6a0b52-8c1e-4d7a-b9e4-6c2d51f0a8e3")

// ShippedFixtures is the in-process conformance suite for every workflow this
// composition publishes. Each fixture first proves the version is the exact
// plan it is about, so a fixture declared by one workflow never passes on
// another's version.
func ShippedFixtures() releasefixture.Suite {
	return releasefixture.Suite{
		FixtureApprovalCompile:     func(v version.CompiledVersion) error { return reproducesPlan(v, prototype.CompileApproval) },
		FixtureApprovalRequirement: approvalStepFixture,
		FixtureExecuteCompile:      executeV1_0.compileFixture,
		FixtureExecuteGraph:        executeV1_0.graphFixture,
		FixtureExecuteCompileV1_1:  executeV1_1.compileFixture,
		FixtureExecuteGraphV1_1:    executeV1_1.graphFixture,
		FixtureExecuteApprovals:    executeApprovalsFixture,
	}
}

// shippedExecute is one shipped version of the executable promotion workflow:
// its definition, its served compilations and its documented node order.
type shippedExecute struct {
	semanticVersion string
	definition      func() workflow.Definition
	compile         func() (*workflow.CompiledWorkflow, error)
	simulate        func() (*workflow.CompiledWorkflow, error)
	nodeOrder       func() []string
	fixtures        []string
}

var (
	// executeV1_0 is the frozen 1.0.0 graph, still served to the instances
	// that pinned it.
	executeV1_0 = shippedExecute{
		semanticVersion: promotionexec.SemanticVersionV1_0,
		definition:      promotionexec.DefinitionV1_0,
		compile:         func() (*workflow.CompiledWorkflow, error) { return promotionexec.CompileV1_0() },
		simulate:        promotionexec.CompileSimulationV1_0,
		nodeOrder:       promotionexec.NodeOrderV1_0,
		fixtures:        []string{FixtureExecuteCompile, FixtureExecuteGraph, FixtureExecuteApprovals},
	}
	// executeV1_1 is the current graph with the provider-confirmation waits.
	executeV1_1 = shippedExecute{
		semanticVersion: promotionexec.SemanticVersion,
		definition:      promotionexec.Definition,
		compile:         func() (*workflow.CompiledWorkflow, error) { return promotionexec.Compile() },
		simulate:        func() (*workflow.CompiledWorkflow, error) { return promotionexec.CompileSimulation() },
		nodeOrder:       promotionexec.NodeOrder,
		fixtures:        []string{FixtureExecuteCompileV1_1, FixtureExecuteGraphV1_1, FixtureExecuteApprovals},
	}
	// shippedExecuteVersions is publication order: the frozen version first,
	// so a superseding activation of the current one is the last word.
	shippedExecuteVersions = []shippedExecute{executeV1_0, executeV1_1}
)

// PublishShippedVersions publishes the prototype approval and the executable
// promotion workflows into store as DRAFT versions, idempotently: a version
// already published under the same compiled-plan digest is returned as it is
// stored, whatever its status. It approves and activates nothing. The
// executable promotion ships two versions side by side, in order: the frozen
// 1.0.0 its live instances pinned, then 1.1.0 for new starts.
func PublishShippedVersions(store version.Store, at time.Time) ([]version.CompiledVersion, error) {
	prototypePlan, err := prototype.CompileApproval()
	if err != nil {
		return nil, fmt.Errorf("platform execution: compile the promotion approval workflow: %w", err)
	}
	tools := map[string]string{"go": goruntime.Version(), "publisher": "internal/platform/execution"}
	approval, err := version.Publish(store, prototype.ApprovalDefinition(), prototypePlan,
		workflow.Options{Phase: workflow.PhaseP1B}, version.PublishMeta{
			SemanticVersion: "1.0.0", PublishedAt: at, PublishedBy: versionPublisher, ToolVersions: tools,
			FixtureRefs: []string{FixtureApprovalCompile, FixtureApprovalRequirement},
		})
	if err != nil {
		return nil, fmt.Errorf("platform execution: publish the promotion approval workflow: %w", err)
	}
	out := []version.CompiledVersion{approval}
	for _, shipped := range shippedExecuteVersions {
		executePlan, err := shipped.compile()
		if err != nil {
			return nil, fmt.Errorf("platform execution: compile the promotion execute workflow %s: %w", shipped.semanticVersion, err)
		}
		executed, err := version.Publish(store, promotionPublishDefinition(shipped.definition()), executePlan,
			promotionPublishOptions(), version.PublishMeta{
				SemanticVersion: shipped.semanticVersion, PublishedAt: at, PublishedBy: versionPublisher, ToolVersions: tools,
				FixtureRefs: append([]string(nil), shipped.fixtures...),
			})
		if err != nil {
			return nil, fmt.Errorf("platform execution: publish the promotion execute workflow %s: %w", shipped.semanticVersion, err)
		}
		out = append(out, executed)
	}
	return out, nil
}

// ReleaseApproval is an operator's approval of one published version on the
// evidence of a fixture report.
type ReleaseApproval struct {
	CompiledPlanDigest string
	ApprovedBy         string
	Authority          string
	Reason             string
	Report             releasefixture.Report
	ApprovedAt         time.Time
}

// ApproveRelease is the governed approval action. It loads the version,
// refuses its own publisher, re-runs every fixture the version declares
// through suite and refuses unless the report is intact, bound to this exact
// record and reproduced (releasefixture sentinels), then records the approval
// with the report. It activates nothing: activation is the separate governed
// step workflowversionstore.Store.ActivateApproved.
func ApproveRelease(ctx context.Context, registry VersionRegistry, suite releasefixture.Suite, a ReleaseApproval) (ret0 workflowversionstore.Approval, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.approve_release", a.CompiledPlanDigest)
	defer func() { observe.DoneWith(obsOp, retErr) }()
	if registry == nil || strings.TrimSpace(a.CompiledPlanDigest) == "" || strings.TrimSpace(a.ApprovedBy) == "" ||
		strings.TrimSpace(a.Authority) == "" || a.ApprovedAt.IsZero() {
		return workflowversionstore.Approval{}, fmt.Errorf("%w: a registry, digest, approver, authority and instant are required", ErrReleaseApproval)
	}
	v, found, err := registry.GetByDigest(a.CompiledPlanDigest)
	if err != nil {
		return workflowversionstore.Approval{}, err
	}
	if !found {
		return workflowversionstore.Approval{}, fmt.Errorf("%w: no published version carries digest %s", ErrReleaseApproval, a.CompiledPlanDigest)
	}
	if a.ApprovedBy == v.PublishedBy {
		return workflowversionstore.Approval{}, fmt.Errorf("%w: %s published %s", workflowversionstore.ErrSelfApproval, a.ApprovedBy, a.CompiledPlanDigest)
	}
	if err := releasefixture.Reproduce(a.Report, v, suite); err != nil {
		return workflowversionstore.Approval{}, fmt.Errorf("platform execution: fixture evidence for %s: %w", a.CompiledPlanDigest, err)
	}
	report := a.Report
	approval := workflowversionstore.Approval{
		ApprovalID:         uuid.NewSHA1(releaseApprovalNamespace, []byte(v.CompiledPlanDigest+"\x00"+a.ApprovedBy+"\x00"+report.ReportDigest)),
		CompiledPlanDigest: v.CompiledPlanDigest, ReviewedPlanDigest: report.CompiledPlanDigest,
		ApprovedBy: a.ApprovedBy, Authority: a.Authority, Reason: a.Reason,
		TestsPassed: true, FixtureRefs: report.FixtureRefs(), FixtureReport: &report, ApprovedAt: a.ApprovedAt.UTC(),
	}
	if err := registry.RecordApproval(ctx, approval); err != nil {
		return workflowversionstore.Approval{}, err
	}
	return approval, nil
}

// BootstrapDevVersions is the explicit local-development release: it
// publishes the shipped versions (idempotently), runs each DRAFT's fixtures,
// approves it under [DevReleaseApprover] through [ApproveRelease] and
// activates it, superseding a version a previous build left active (the
// current promotion execute version supersedes the frozen 1.0.0, which stays
// QUARANTINED by supersession and keeps serving the instances pinned to it). An ACTIVE
// version is left alone, and so is a QUARANTINED or RETIRED one: bootstrap
// never undoes a governed quarantine. It returns every shipped version as it
// stands afterwards.
func BootstrapDevVersions(ctx context.Context, registry VersionRegistry, at time.Time) (ret0 []version.CompiledVersion, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.bootstrap_dev_versions")
	defer func() { observe.DoneWith(obsOp, retErr) }()
	if registry == nil {
		return nil, fmt.Errorf("%w: a registry is required", ErrReleaseApproval)
	}
	published, err := PublishShippedVersions(registry, at)
	if err != nil {
		return nil, err
	}
	suite := ShippedFixtures()
	out := make([]version.CompiledVersion, 0, len(published))
	for _, v := range published {
		if v.Status != version.StatusDraft {
			out = append(out, v)
			continue
		}
		report := releasefixture.Run(suite, v, DevReleaseApprover, at)
		if _, err := ApproveRelease(ctx, registry, suite, ReleaseApproval{
			CompiledPlanDigest: v.CompiledPlanDigest, ApprovedBy: DevReleaseApprover, Authority: devReleaseAuthority,
			Reason: fmt.Sprintf("local development bootstrap of %s %s", v.WorkflowID, v.SemanticVersion),
			Report: report, ApprovedAt: at,
		}); err != nil {
			return nil, err
		}
		active, err := registry.ActivateApproved(ctx, v.CompiledPlanDigest, true)
		if err != nil {
			return nil, err
		}
		out = append(out, active)
	}
	// A later shipped version may have superseded an earlier one this run
	// activated (promotion execute 1.1.0 supersedes 1.0.0): report each as it
	// stands now.
	for i, v := range out {
		current, found, err := registry.GetByDigest(v.CompiledPlanDigest)
		if err != nil {
			return nil, err
		}
		if found {
			out[i] = current
		}
	}
	return out, nil
}

// activateInMemory is the unit-composition activation of the private
// in-memory registry: the version's fixtures are run in-process and the
// activation carries their actual outcome rather than an asserted pass.
func activateInMemory(registry version.Store, published version.CompiledVersion, at time.Time) error {
	report := releasefixture.Run(ShippedFixtures(), published, unitCompositionApprover, at)
	if err := releasefixture.Verify(report, published); err != nil {
		return err
	}
	_, err := version.Activate(registry, published.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: unitCompositionApprover, Authority: "authority:unit-composition",
		Reason: "in-process fixture report " + report.ReportDigest, ApprovedAt: at,
		ReviewedPlanDigest: report.CompiledPlanDigest, TestsPassed: report.Passed(),
		// The shipped execute versions activate in publication order, so the
		// current one supersedes the frozen one exactly as a durable
		// bootstrap does.
		SupersedeActive: true,
	})
	return err
}

// noActiveVersion names the governed commands when a start is refused because
// the shipped workflow was never activated.
func noActiveVersion(err error) error {
	if runtime.CodeOf(err) == runtime.CodeVersionNotActive {
		return fmt.Errorf("%w: %w", ErrNoActiveWorkflowVersion, err)
	}
	return err
}

// reproducesPlan proves v is intact and is exactly the plan compile produces
// now: the same compiled-plan digest, workflow, compiler and canonical bytes.
func reproducesPlan(v version.CompiledVersion, compile func() (*workflow.CompiledWorkflow, error)) error {
	if err := v.Verify(); err != nil {
		return err
	}
	plan, err := compile()
	if err != nil {
		return fmt.Errorf("recompile: %w", err)
	}
	if plan.Digest() != v.CompiledPlanDigest || plan.WorkflowID != v.WorkflowID || plan.CompilerVersion != v.CompilerVersion {
		return fmt.Errorf("recompiled %s@%s (%s) is not version %s@%s (%s)",
			plan.WorkflowID, plan.Digest(), plan.CompilerVersion, v.WorkflowID, v.CompiledPlanDigest, v.CompilerVersion)
	}
	canonical, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("render the recompiled plan: %w", err)
	}
	if !bytes.Equal(append(canonical, '\n'), v.CanonicalPlanBytes) {
		return fmt.Errorf("the recompiled plan's canonical bytes differ from the published bytes")
	}
	return nil
}

// approvalStepFixture proves the served step runner parks the prototype's
// APPROVAL node on its compiled requirement and completes its END node, and
// that the requirement compiles deterministically.
func approvalStepFixture(v version.CompiledVersion) error {
	if err := reproducesPlan(v, prototype.CompileApproval); err != nil {
		return err
	}
	plan, _ := prototype.CompileApproval()
	var approvals, ends int
	for _, node := range plan.Nodes {
		outcome, _, err := runPrototypeStep(execute.StepRequest{Node: node, Plan: plan})
		switch node.Type {
		case workflow.StepApproval:
			if err != nil || outcome.Await != frontier.AwaitWorkItem || outcome.AwaitRef != prototype.ApprovalRequirementID {
				return fmt.Errorf("APPROVAL node %s did not park on %s (%+v, %v)", node.ID, prototype.ApprovalRequirementID, outcome, err)
			}
			approvals++
		case workflow.StepEnd:
			if err != nil || outcome.Await != "" {
				return fmt.Errorf("END node %s did not complete (%+v, %v)", node.ID, outcome, err)
			}
			ends++
		}
	}
	if approvals != 1 || ends == 0 {
		return fmt.Errorf("plan holds %d APPROVAL and %d END nodes, want one APPROVAL and at least one END", approvals, ends)
	}
	deadline := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first, err := prototype.CompileApprovalRequirement("principal:fixture-approver", deadline)
	if err != nil {
		return fmt.Errorf("compile the approval requirement: %w", err)
	}
	again, err := prototype.CompileApprovalRequirement("principal:fixture-approver", deadline)
	if err != nil || first.RequirementID != prototype.ApprovalRequirementID || first.Digest() != again.Digest() {
		return fmt.Errorf("the approval requirement is not deterministic (%s vs %s, %v)", first.Digest(), again.Digest(), err)
	}
	return nil
}

// compileFixture proves the executable version is both the publication
// projection of this shipped definition and the plan the served resolver
// runs for it.
func (e shippedExecute) compileFixture(v version.CompiledVersion) error {
	publication := func() (*workflow.CompiledWorkflow, error) {
		return workflow.Compile(promotionPublishDefinition(e.definition()), promotionPublishOptions())
	}
	if err := reproducesPlan(v, publication); err != nil {
		return err
	}
	served, err := e.compile()
	if err != nil {
		return fmt.Errorf("compile the served plan: %w", err)
	}
	if served.Digest() != v.CompiledPlanDigest {
		return fmt.Errorf("the served plan digests to %s, the version is %s", served.Digest(), v.CompiledPlanDigest)
	}
	return nil
}

// graphFixture proves the executable plan holds exactly the documented node
// set and that its zero-effect SIMULATE projection still compiles.
func (e shippedExecute) graphFixture(v version.CompiledVersion) error {
	if err := e.compileFixture(v); err != nil {
		return err
	}
	plan, _ := e.compile()
	want := map[string]bool{}
	for _, id := range e.nodeOrder() {
		want[id] = true
	}
	if len(plan.Nodes) != len(want) {
		return fmt.Errorf("plan holds %d nodes, the documented order names %d", len(plan.Nodes), len(want))
	}
	for _, node := range plan.Nodes {
		if !want[node.ID] {
			return fmt.Errorf("plan node %s is not in the documented order", node.ID)
		}
	}
	simulation, err := e.simulate()
	if err != nil {
		return fmt.Errorf("compile the SIMULATE projection: %w", err)
	}
	if simulation.Digest() == plan.Digest() {
		return fmt.Errorf("the SIMULATE projection digests to the EXECUTE plan")
	}
	return nil
}

// executeCompileFixture proves v is one of the shipped executable versions.
// The approval fixture holds for both, so it first proves which plan it is
// about.
func executeCompileFixture(v version.CompiledVersion) error {
	var errs []error
	for _, shipped := range shippedExecuteVersions {
		err := shipped.compileFixture(v)
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", shipped.semanticVersion, err))
	}
	return fmt.Errorf("version %s is no shipped promotion execute version: %w", v.CompiledPlanDigest, errors.Join(errs...))
}

// executeApprovalsFixture proves the executable plan's finance and manager
// approvals compile to distinct requirements for distinct derived principals,
// and that a set naming one principal for both authorities is refused.
func executeApprovalsFixture(v version.CompiledVersion) error {
	if err := executeCompileFixture(v); err != nil {
		return err
	}
	finance, err := promotionexec.FinanceApproverFor("principal:fixture-base")
	if err != nil {
		return err
	}
	manager, err := promotionexec.ManagerApproverFor("principal:fixture-base")
	if err != nil {
		return err
	}
	deadline := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	financeReq, err := promotionexec.CompileFinanceApprovalRequirement(finance, deadline)
	if err != nil {
		return fmt.Errorf("compile the finance requirement: %w", err)
	}
	managerReq, err := promotionexec.CompileManagerApprovalRequirement(manager, deadline)
	if err != nil {
		return fmt.Errorf("compile the manager requirement: %w", err)
	}
	if finance == manager || financeReq.Digest() == managerReq.Digest() || financeReq.RequirementID == managerReq.RequirementID {
		return fmt.Errorf("finance and manager approvals are not distinct")
	}
	authority := func(principal string, class promotionexec.AuthorityClass) promotionexec.ApprovalAuthority {
		return promotionexec.ApprovalAuthority{PrincipalID: principal, Class: class, AuthorityRef: "authority:fixture", Scope: "scope:fixture", Active: true}
	}
	if err := promotionexec.ValidatePromotionApprovers([]promotionexec.ApprovalAuthority{
		authority(finance, promotionexec.AuthorityClassFinancePartner), authority(manager, promotionexec.AuthorityClassCurrentManager),
	}); err != nil {
		return fmt.Errorf("distinct approvers were refused: %w", err)
	}
	if err := promotionexec.ValidatePromotionApprovers([]promotionexec.ApprovalAuthority{
		authority(finance, promotionexec.AuthorityClassFinancePartner), authority(finance, promotionexec.AuthorityClassCurrentManager),
	}); !errors.Is(err, promotionexec.ErrApprovalAuthorityNotDistinct) {
		return fmt.Errorf("one principal holding both approvals was not refused (%v)", err)
	}
	return nil
}
