// WF-UI-013: publish from the designer. A draft author submits their draft
// for review; submission compiles the draft through the production compiler
// and publishes it as a DRAFT compiled version carrying the designer fixture
// set. Approval and activation follow the existing governed release steps
// (platformexecution.ApproveRelease, workflowversionstore activation): a
// different principal approves after the saved fixtures pass, and the
// publisher can never approve their own submission.
package designeredit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// Designer fixture references: the conformance set every designer-published
// version declares. Each fixture proves its claim from the published record
// alone, so approval re-runs them without the draft.
const (
	// FixtureDesignerPlanSound proves the published plan parses and its
	// graph is sound: nodes exist with unique identities, the start node
	// and every edge endpoint resolve, and a terminal exists.
	FixtureDesignerPlanSound = "fixture:workflow.designer/plan-sound@v1"
	// FixtureDesignerRecordIntact proves the published record still
	// matches its own digest and names its definition and plan.
	FixtureDesignerRecordIntact = "fixture:workflow.designer/record-intact@v1"
	// FixtureDesignerCompilerPinned proves the record pins the compiler
	// identity and a valid semantic version, so the publication is
	// reproducible.
	FixtureDesignerCompilerPinned = "fixture:workflow.designer/compiler-pinned@v1"
)

// DesignerFixtureRefs returns the designer fixture set in declared order.
func DesignerFixtureRefs() []string {
	return []string{FixtureDesignerPlanSound, FixtureDesignerRecordIntact, FixtureDesignerCompilerPinned}
}

// DesignerFixtures maps the designer fixture set to its checks.
func DesignerFixtures() releasefixture.Suite {
	return releasefixture.Suite{
		FixtureDesignerPlanSound:      designerPlanSound,
		FixtureDesignerRecordIntact:   designerRecordIntact,
		FixtureDesignerCompilerPinned: designerCompilerPinned,
	}
}

func designerPlanSound(v workflowversion.CompiledVersion) error {
	if len(v.CanonicalPlanBytes) == 0 {
		return fmt.Errorf("designer plan-sound: %s carries no canonical plan", v.CompiledPlanDigest)
	}
	decoder := json.NewDecoder(bytes.NewReader(v.CanonicalPlanBytes))
	decoder.DisallowUnknownFields()
	var plan workflow.CompiledWorkflow
	if err := decoder.Decode(&plan); err != nil {
		return fmt.Errorf("designer plan-sound: canonical plan does not parse: %v", err)
	}
	if plan.WorkflowID != v.WorkflowID {
		return fmt.Errorf("designer plan-sound: plan names %q, version names %q", plan.WorkflowID, v.WorkflowID)
	}
	if len(plan.Nodes) == 0 {
		return fmt.Errorf("designer plan-sound: plan declares no nodes")
	}
	seen := make(map[string]bool, len(plan.Nodes))
	for _, node := range plan.Nodes {
		if strings.TrimSpace(node.ID) == "" || seen[node.ID] {
			return fmt.Errorf("designer plan-sound: node identity %q is empty or duplicated", node.ID)
		}
		seen[node.ID] = true
	}
	if !seen[plan.StartNodeID] {
		return fmt.Errorf("designer plan-sound: start node %q is not declared", plan.StartNodeID)
	}
	for _, edge := range plan.Edges {
		if !seen[edge.From] || !seen[edge.To] {
			return fmt.Errorf("designer plan-sound: edge %q --%q--> %q escapes the declared nodes",
				edge.From, edge.RouteKey, edge.To)
		}
	}
	if len(plan.Terminals) == 0 {
		return fmt.Errorf("designer plan-sound: plan declares no terminal")
	}
	return nil
}

func designerRecordIntact(v workflowversion.CompiledVersion) error {
	if err := v.Verify(); err != nil {
		return fmt.Errorf("designer record-intact: %v", err)
	}
	if strings.TrimSpace(v.DefinitionDigest) == "" || strings.TrimSpace(v.CompiledPlanDigest) == "" {
		return fmt.Errorf("designer record-intact: definition or plan digest is missing")
	}
	return nil
}

func designerCompilerPinned(v workflowversion.CompiledVersion) error {
	if strings.TrimSpace(v.CompilerVersion) == "" {
		return fmt.Errorf("designer compiler-pinned: compiler identity is missing")
	}
	if err := workflowversion.ValidateSemanticVersion(v.SemanticVersion); err != nil {
		return fmt.Errorf("designer compiler-pinned: %v", err)
	}
	return nil
}

// designerPublisher is the tool identity recorded on designer submissions.
const designerPublisher = "internal/workflow/designeredit"

// SubmitRequest asks to submit one owned draft revision for review.
// ToolVersions records the compiler/tooling identities material to
// reproducing the publication; the caller supplies them so a submission is
// hermetic and golden-testable.
type SubmitRequest struct {
	DraftID          string
	ExpectedRevision uint64
	ToolVersions     map[string]string
}

// Submission is the published DRAFT version a review approves: the draft
// identity that produced it and the version identity the release steps act
// on. The full record stays in the version store under CompiledPlanDigest.
type Submission struct {
	DraftID            string   `json:"draft_id"`
	DraftRevision      uint64   `json:"draft_revision"`
	AuthorRef          string   `json:"author_ref"`
	WorkflowID         string   `json:"workflow_id"`
	SemanticVersion    string   `json:"semantic_version"`
	DefinitionDigest   string   `json:"definition_digest"`
	CompiledPlanDigest string   `json:"compiled_plan_digest"`
	FixtureRefs        []string `json:"fixture_refs"`
	PublishedAt        string   `json:"published_at"`
	Status             string   `json:"status"`
}

// SubmitForReview compiles the author's owned draft and publishes it as a
// DRAFT compiled version carrying the designer fixture set. Only the draft
// owner may submit, and only at the current revision; anything else fails
// closed before anything is published. Publication is idempotent: resubmitting
// unchanged content returns the existing version.
func (s Service) SubmitForReview(ctx context.Context, tenant values.TenantId, author string, versions workflowversion.Store, opts workflow.Options, req SubmitRequest) (_ Submission, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.submit", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	if versions == nil {
		return Submission{}, fmt.Errorf("%w: version store is required", ErrInvalid)
	}
	if strings.TrimSpace(req.DraftID) == "" || req.ExpectedRevision == 0 {
		return Submission{}, fmt.Errorf("%w: draft and expected revision are required", ErrInvalid)
	}
	draft, err := s.owned(ctx, tenant, author, req.DraftID)
	if err != nil {
		return Submission{}, err
	}
	if draft.Revision != req.ExpectedRevision {
		return Submission{}, ErrConflict
	}
	definition, err := workflow.Load(draft.Document)
	if err != nil {
		return Submission{}, fmt.Errorf("%w: stored definition: %v", ErrInvalid, err)
	}
	plan, err := workflow.Compile(definition, opts)
	if err != nil {
		return Submission{}, fmt.Errorf("designeredit: submit draft %s: %w", draft.DraftID, err)
	}
	tools := map[string]string{"publisher": designerPublisher}
	for key, value := range req.ToolVersions {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return Submission{}, fmt.Errorf("%w: tool versions name %q", ErrInvalid, key)
		}
		tools[key] = value
	}
	published, err := workflowversion.Publish(versions, definition, plan, opts, workflowversion.PublishMeta{
		SemanticVersion: draft.SemanticVersion, PublishedAt: s.now(), PublishedBy: strings.TrimSpace(author),
		ToolVersions: tools, FixtureRefs: DesignerFixtureRefs(),
	})
	if err != nil {
		return Submission{}, err
	}
	return Submission{
		DraftID: draft.DraftID, DraftRevision: draft.Revision, AuthorRef: draft.AuthorRef,
		WorkflowID: published.WorkflowID, SemanticVersion: published.SemanticVersion,
		DefinitionDigest: published.DefinitionDigest, CompiledPlanDigest: published.CompiledPlanDigest,
		FixtureRefs: append([]string(nil), published.FixtureRefs...),
		PublishedAt: published.PublishedAt.UTC().Format(time.RFC3339Nano), Status: string(published.Status),
	}, nil
}
