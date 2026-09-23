package workflow

import (
	"context"
	"sync/atomic"
	"testing"

	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
)

func TestTodo_WF_UI_005_TransportReturnsTenantFilteredPalette(t *testing.T) {
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatal(err)
	}
	allowed := capability.Key{ID: "hcmnext.people.promote_worker", Version: 1}
	palette := designerpalette.Catalog{
		Capabilities: registry,
		Policy: draftcompile.CapabilityPolicyFunc(func(_ context.Context, tenant values.TenantId, key capability.Key) bool {
			return tenant == "acme-corp" && key == allowed
		}),
	}
	srv := &server{deps: Dependencies{Palette: palette, Authorize: allowWorkflowCalls}}
	response, err := srv.ListWorkflowBlocks(workflowTestContext(t, ListWorkflowBlocksProcedure), &workflowv1.ListWorkflowBlocksRequest{})
	if err != nil {
		t.Fatalf("ListWorkflowBlocks: %v", err)
	}
	foundAllowed := false
	for _, entry := range response.GetEntries() {
		if entry.GetId() == "hcmnext.rewards.simulate_compensation" {
			t.Fatalf("unallowlisted capability leaked: %+v", entry)
		}
		if entry.GetId() == allowed.ID {
			foundAllowed = entry.GetKind() == "BLOCK" && entry.GetDomain() == "People" && entry.GetEffectClass() != ""
		}
	}
	if !foundAllowed {
		t.Fatalf("allowed capability missing from palette: %+v", response.GetEntries())
	}
}

func TestTodo_WF_UI_005_TransportSecurityAuthorizesBeforeListing(t *testing.T) {
	palette := &wfui005PaletteSpy{}
	srv := &server{deps: Dependencies{
		Palette:   palette,
		Authorize: func(context.Context, *trust.Principal, string) bool { return false },
	}}
	_, err := srv.ListWorkflowBlocks(workflowTestContext(t, ListWorkflowBlocksProcedure), &workflowv1.ListWorkflowBlocksRequest{})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodePermissionDenied {
		t.Fatalf("denial = %v", err)
	}
	if palette.calls.Load() != 0 {
		t.Fatalf("denied request listed palette %d times", palette.calls.Load())
	}
}

type wfui005PaletteSpy struct{ calls atomic.Int64 }

func (s *wfui005PaletteSpy) List(context.Context, values.TenantId) []designerpalette.Entry {
	s.calls.Add(1)
	return nil
}
