package promotion

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func wfData040Diff(t *testing.T) (config.DiffResult, config.ParameterDefinition) {
	t.Helper()
	beforeEntry, err := config.NewEntry("people.promotion.threshold", config.KindString, "10%", true, config.SemanticPolicy, config.Refs{Workflows: []string{"workflow:promotion"}})
	if err != nil {
		t.Fatal(err)
	}
	afterEntry, err := config.NewEntry("people.promotion.threshold", config.KindString, "15%", true, config.SemanticPolicy, config.Refs{Workflows: []string{"workflow:promotion"}})
	if err != nil {
		t.Fatal(err)
	}
	definition := config.ParameterDefinition{
		Key: "people.promotion.threshold", Type: workflow.ValueType{Kind: workflow.KindDecimal},
		Classification: "CONFIDENTIAL", Owner: "compensation", HighImpact: true,
		AllowedConsumers: []config.Consumer{{Kind: config.ConsumerWorkflow, ID: "workflow:promotion"}},
	}
	before, err := config.NewSnapshotWithDefinitions("tenant-a", "v1", []config.Entry{beforeEntry}, []config.ParameterDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	after, err := config.NewSnapshotWithDefinitions("tenant-a", "v2", []config.Entry{afterEntry}, []config.ParameterDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	return config.Diff(before, after), definition
}

func wfData040Request(t *testing.T) (ChangeRequest, ImpactPreview) {
	t.Helper()
	diff, definition := wfData040Diff(t)
	request, err := AuthorizeParameterChanges("package-2", "sha256:package-2", "author-a", diff,
		[]config.ParameterDefinition{definition}, []ParameterPermission{{Key: "people.promotion.threshold", Editors: []string{"author-a"}}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewImpact(request, ImpactCatalog{Complete: true, Readers: []ParameterReader{
		{Key: "people.promotion.threshold", WorkflowID: "workflow:promotion", PackageID: "workflow-package:promotion-v3", RunningInstanceID: "run-17", BindingMode: "PINNED_AT_START"},
		{Key: "people.promotion.threshold", WorkflowID: "workflow:promotion", PackageID: "workflow-package:promotion-v4", BindingMode: "LIVE"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return request, preview
}

func TestTodo_WF_DATA_040(t *testing.T) {
	request, preview := wfData040Request(t)
	if !request.ApprovalRequired || len(request.HighImpactKeys) != 1 || len(preview.Impacts) != 2 {
		t.Fatalf("governance request/preview = %+v %+v", request, preview)
	}
	approval, err := ApproveParameterChange(request, preview, "reviewer-b", time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if approval.Author != request.Author || approval.Approver != "reviewer-b" || approval.RequestDigest != request.Digest || approval.PreviewDigest != preview.Digest || approval.Digest == "" {
		t.Fatalf("approval is not bound to the request and preview: %+v", approval)
	}
	if _, err := PreviewImpact(request, ImpactCatalog{Readers: nil}); !errors.Is(err, ErrImpactCatalogIncomplete) {
		t.Fatalf("incomplete impact catalog error = %v", err)
	}
}

func TestTodo_WF_DATA_040_Golden(t *testing.T) {
	request, preview := wfData040Request(t)
	if request.Digest != "495091fb471613fa7d5e0bec4ca1b23ef578bbb2de46ed5f4bdae3dbb16481a3" || preview.Digest != "a70495ac21245705d4aa0b962a1ec197775723483b1ff3214cfc2a74fa5fefac" {
		t.Fatalf("governance evidence changed: request=%s preview=%s", request.Digest, preview.Digest)
	}
}

func TestTodo_WF_DATA_040_Security(t *testing.T) {
	diff, definition := wfData040Diff(t)
	if _, err := AuthorizeParameterChanges("package-2", "sha256:package-2", "unapproved-author", diff,
		[]config.ParameterDefinition{definition}, []ParameterPermission{{Key: "people.promotion.threshold", Editors: []string{"authorized-author"}}}); !errors.Is(err, ErrChangeNotPermitted) {
		t.Fatalf("unauthorized change error = %v", err)
	}
	request, preview := wfData040Request(t)
	if _, err := ApproveParameterChange(request, preview, request.Author, time.Unix(100, 0)); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("self-approval error = %v", err)
	}
	mutated := preview
	mutated.Impacts = append([]ParameterImpact(nil), preview.Impacts...)
	mutated.Impacts[0].WorkflowID = "workflow:forged"
	if _, err := ApproveParameterChange(request, mutated, "reviewer-b", time.Unix(100, 0)); !errors.Is(err, ErrStale) {
		t.Fatalf("mutated impact preview approval error = %v", err)
	}
}
