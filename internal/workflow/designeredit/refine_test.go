package designeredit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

func refinementService(t *testing.T) (*designeredit.Service, *memoryDraftStore, designeredit.View) {
	t.Helper()
	template := promotionexec.Definition()
	store := &memoryDraftStore{}
	service := &designeredit.Service{
		Store: store,
		Catalog: fixedCatalog{
			{ID: "template.promotion", Version: 1, Name: "Promotion", Kind: designerpalette.KindTemplate, Expansion: designerpalette.Expansion{Template: &template}},
			{ID: "kernel.task", Version: 1, Name: "Task", Kind: designerpalette.KindBlock, StepType: workflow.StepTask},
			{ID: "kernel.wait", Version: 1, Name: "Wait", Kind: designerpalette.KindBlock, StepType: workflow.StepWait},
			{ID: "kernel.signal", Version: 1, Name: "Signal", Kind: designerpalette.KindBlock, StepType: workflow.StepSignal},
		},
		NewID: func() (string, error) { return "01999f37-9f42-7000-8000-000000000106", nil },
	}
	created, err := service.Create(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.CreateRequest{TemplateID: "template.promotion", TemplateVersion: 1, SemanticVersion: "1.1.1"})
	if err != nil {
		t.Fatalf("create promotion draft: %v", err)
	}
	return service, store, created.Draft
}

func TestTodo_WF_UI_006(t *testing.T) {
	service, store, draft := refinementService(t)
	change, err := service.UpdateNodeParameters(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: promotionexec.NodeAwaitPayrollConfirmation,
		Values: map[string]string{"display_name": "Payroll acknowledgement", "signal_timeout_seconds": "7200"},
	})
	if err != nil {
		t.Fatalf("UpdateNodeParameters: %v", err)
	}
	if change.Draft.Revision != 2 {
		t.Fatalf("revision = %d, want 2", change.Draft.Revision)
	}
	node := draftNode(t, change.Draft, promotionexec.NodeAwaitPayrollConfirmation)
	if node.Label != "Payroll acknowledgement" || parameterValue(node, "signal_timeout_seconds") != "7200" {
		t.Fatalf("updated node = %+v", node)
	}
	before := append([]byte(nil), store.draft.Document...)
	_, err = service.UpdateNodeParameters(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: draft.DraftID, ExpectedRevision: 2, NodeID: promotionexec.NodeAwaitPayrollConfirmation,
		Values: map[string]string{"signal_timeout_seconds": "not-an-integer"},
	})
	if !errors.Is(err, designeredit.ErrInvalid) || string(before) != string(store.draft.Document) || store.draft.Revision != 2 {
		t.Fatalf("invalid typed update = %v, revision %d", err, store.draft.Revision)
	}
}

func TestTodo_WF_UI_009_RepeatedKeyboardSubmissionDoesNotInventARevision(t *testing.T) {
	service, store, draft := refinementService(t)
	// The durable store uses jsonb, which returns semantically identical JSON
	// without preserving workflow.Marshal's indentation. Reproduce that round
	// trip so this test protects the production path rather than only the
	// in-memory byte representation.
	var compact bytes.Buffer
	if err := json.Compact(&compact, store.draft.Document); err != nil {
		t.Fatalf("compact stored document: %v", err)
	}
	store.draft.Document = append(store.draft.Document[:0], compact.Bytes()...)
	node := draftNode(t, draft, promotionexec.NodeRaiseThreshold)
	submitted := make(map[string]string, len(node.Parameters))
	for _, parameter := range node.Parameters {
		submitted[parameter.ID] = parameter.Value
	}
	before := append([]byte(nil), store.draft.Document...)
	change, err := service.UpdateNodeParameters(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, NodeID: promotionexec.NodeRaiseThreshold, Values: submitted,
	})
	if err != nil {
		t.Fatalf("repeat current parameters: %v", err)
	}
	if change.Draft.Revision != draft.Revision || store.draft.Revision != draft.Revision || string(store.draft.Document) != string(before) {
		t.Fatalf("unchanged keyboard submit created history: view=%d store=%d", change.Draft.Revision, store.draft.Revision)
	}
}

// TestIssue3621_FreshWaitAndSignalExposeTypedSettings inserts Wait and Signal
// blocks the way the palette does. Their typed parameters must be projected
// before any configuration exists, and saved values must round-trip through
// the stored document.
func TestIssue3621_FreshWaitAndSignalExposeTypedSettings(t *testing.T) {
	service, _, draft := refinementService(t)
	ctx := context.Background()
	tenant := values.TenantId("tenant-a")

	waited, err := service.ApplyTemplateOverlay(ctx, tenant, "author-a", designeredit.ApplyTemplateOverlayRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, Operation: designeredit.OverlayAdd, EntryID: "kernel.wait", EntryVersion: 1,
	})
	if err != nil || len(waited.InsertedNodeIDs) != 1 {
		t.Fatalf("add wait: %v, inserted %v", err, waited.InsertedNodeIDs)
	}
	waitID := waited.InsertedNodeIDs[0]
	wait := draftNode(t, waited.Draft, waitID)
	if parameterKind(wait, "wait_wake_kind") != designeredit.ParameterEnum || len(parameterOptions(wait, "wait_wake_kind")) != 3 {
		t.Fatalf("fresh wait schema = %+v", wait.Parameters)
	}
	if parameterKind(wait, "wait_zone") != designeredit.ParameterText || parameterKind(wait, "wait_calendar") != designeredit.ParameterText {
		t.Fatalf("fresh wait zone or calendar missing = %+v", wait.Parameters)
	}
	if _, err := service.UpdateNodeParameters(ctx, tenant, "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: waited.Draft.DraftID, ExpectedRevision: waited.Draft.Revision, NodeID: waitID,
		Values: map[string]string{"wait_wake_kind": "NEVER", "wait_zone": "Europe/Berlin", "wait_calendar": "business-de"},
	}); !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("unknown wake kind = %v, want ErrInvalid", err)
	}
	saved, err := service.UpdateNodeParameters(ctx, tenant, "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: waited.Draft.DraftID, ExpectedRevision: waited.Draft.Revision, NodeID: waitID,
		Values: map[string]string{"wait_wake_kind": "AT_LOCAL_DATE", "wait_zone": "Europe/Berlin", "wait_calendar": "business-de"},
	})
	if err != nil {
		t.Fatalf("save wait settings: %v", err)
	}
	wait = draftNode(t, saved.Draft, waitID)
	if parameterValue(wait, "wait_wake_kind") != "AT_LOCAL_DATE" || parameterValue(wait, "wait_zone") != "Europe/Berlin" || parameterValue(wait, "wait_calendar") != "business-de" {
		t.Fatalf("saved wait settings did not round-trip: %+v", wait.Parameters)
	}

	signalled, err := service.ApplyTemplateOverlay(ctx, tenant, "author-a", designeredit.ApplyTemplateOverlayRequest{
		DraftID: saved.Draft.DraftID, ExpectedRevision: saved.Draft.Revision, Operation: designeredit.OverlayAdd, EntryID: "kernel.signal", EntryVersion: 1,
	})
	if err != nil || len(signalled.InsertedNodeIDs) != 1 {
		t.Fatalf("add signal: %v, inserted %v", err, signalled.InsertedNodeIDs)
	}
	signalID := signalled.InsertedNodeIDs[0]
	signal := draftNode(t, signalled.Draft, signalID)
	if parameterKind(signal, "signal_event_type") != designeredit.ParameterText || parameterKind(signal, "signal_timeout_seconds") != designeredit.ParameterInteger {
		t.Fatalf("fresh signal schema = %+v", signal.Parameters)
	}
	if _, err := service.UpdateNodeParameters(ctx, tenant, "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: signalled.Draft.DraftID, ExpectedRevision: signalled.Draft.Revision, NodeID: signalID,
		Values: map[string]string{"signal_event_type": "   "},
	}); !errors.Is(err, designeredit.ErrInvalid) {
		t.Fatalf("blank required event type = %v, want ErrInvalid", err)
	}
	savedSignal, err := service.UpdateNodeParameters(ctx, tenant, "author-a", designeredit.UpdateNodeParametersRequest{
		DraftID: signalled.Draft.DraftID, ExpectedRevision: signalled.Draft.Revision, NodeID: signalID,
		Values: map[string]string{"signal_event_type": "payroll.acknowledged", "signal_timeout_seconds": "3600"},
	})
	if err != nil {
		t.Fatalf("save signal settings: %v", err)
	}
	signal = draftNode(t, savedSignal.Draft, signalID)
	if parameterValue(signal, "signal_event_type") != "payroll.acknowledged" || parameterValue(signal, "signal_timeout_seconds") != "3600" {
		t.Fatalf("saved signal settings did not round-trip: %+v", signal.Parameters)
	}
	if parameterValue(draftNode(t, savedSignal.Draft, waitID), "wait_zone") != "Europe/Berlin" {
		t.Fatalf("wait settings lost after signal edit")
	}
}

func TestTodo_WF_UI_006_Browser(t *testing.T) {
	_, _, draft := refinementService(t)
	signal := draftNode(t, draft, promotionexec.NodeAwaitPayrollConfirmation)
	if len(signal.Parameters) < 3 || parameterKind(signal, "signal_timeout_seconds") != designeredit.ParameterInteger {
		t.Fatalf("signal inspector schema = %+v", signal.Parameters)
	}
	wait := draftNode(t, draft, promotionexec.NodeWaitEffectiveDate)
	if parameterKind(wait, "wait_wake_kind") != designeredit.ParameterEnum || len(parameterOptions(wait, "wait_wake_kind")) != 3 {
		t.Fatalf("wait inspector schema = %+v", wait.Parameters)
	}
	locked := map[string]bool{}
	for _, node := range draft.Nodes {
		if node.Locked {
			locked[node.LockKind] = true
		}
	}
	for _, kind := range []string{"GOVERNANCE", "REVALIDATION", "RECONCILIATION", "CLOSURE"} {
		if !locked[kind] {
			t.Fatalf("promotion inspector has no locked %s phase", kind)
		}
	}
}

func TestTodo_WF_UI_006_Property(t *testing.T) {
	service, store, draft := refinementService(t)
	for _, node := range draft.Nodes {
		if !node.Locked {
			continue
		}
		before := append([]byte(nil), store.draft.Document...)
		_, err := service.ApplyTemplateOverlay(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ApplyTemplateOverlayRequest{
			DraftID: draft.DraftID, ExpectedRevision: store.draft.Revision, Operation: designeredit.OverlayOmit, TargetNodeID: node.ID, Reason: "property probe",
		})
		if !errors.Is(err, designeredit.ErrInvalid) {
			t.Fatalf("locked %s node %q omission = %v, want ErrInvalid", node.LockKind, node.ID, err)
		}
		if string(before) != string(store.draft.Document) || store.draft.Revision != draft.Revision {
			t.Fatalf("locked node %q changed the draft", node.ID)
		}
	}

	added, err := service.ApplyTemplateOverlay(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ApplyTemplateOverlayRequest{
		DraftID: draft.DraftID, ExpectedRevision: draft.Revision, Operation: designeredit.OverlayAdd, EntryID: "kernel.task", EntryVersion: 1,
	})
	if err != nil || len(added.Draft.Overlays) != 1 || added.Draft.Overlays[0].Operation != designeredit.OverlayAdd {
		t.Fatalf("recorded add overlay = %+v, %v", added.Draft.Overlays, err)
	}
	replaced, err := service.ApplyTemplateOverlay(context.Background(), values.TenantId("tenant-a"), "author-a", designeredit.ApplyTemplateOverlayRequest{
		DraftID: draft.DraftID, ExpectedRevision: added.Draft.Revision, Operation: designeredit.OverlayReplace,
		TargetNodeID: promotionexec.NodeSimulateCompensation, EntryID: "kernel.task", EntryVersion: 1, Reason: "customer review task",
	})
	if err != nil || len(replaced.Draft.Overlays) != 2 || replaced.Draft.Overlays[1].Reason != "customer review task" {
		t.Fatalf("recorded replace overlay = %+v, %v", replaced.Draft.Overlays, err)
	}
}

func draftNode(t *testing.T, draft designeredit.View, id string) designeredit.NodeView {
	t.Helper()
	for _, node := range draft.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("draft has no node %q", id)
	return designeredit.NodeView{}
}

func parameterValue(node designeredit.NodeView, id string) string {
	for _, parameter := range node.Parameters {
		if parameter.ID == id {
			return parameter.Value
		}
	}
	return ""
}

func parameterKind(node designeredit.NodeView, id string) designeredit.ParameterKind {
	for _, parameter := range node.Parameters {
		if parameter.ID == id {
			return parameter.Kind
		}
	}
	return ""
}

func parameterOptions(node designeredit.NodeView, id string) []string {
	for _, parameter := range node.Parameters {
		if parameter.ID == id {
			return parameter.Options
		}
	}
	return nil
}
