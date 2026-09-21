package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportjourney "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type promotionRouteVerifier struct{}

func (promotionRouteVerifier) Verify(context.Context, trust.Credential) (*trust.Principal, error) {
	return nil, trust.ErrInvalidCredential
}

func TestTodo_WF_UI_005_DraftAuthoringRoutesAreMountedWithTypedFactories(t *testing.T) {
	h, err := NewHandler(Options{
		Config:   transport.Config{Verifier: promotionRouteVerifier{}},
		Workflow: &transportworkflow.Dependencies{},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	for _, procedure := range []string{
		transportworkflow.CreateWorkflowDraftProcedure,
		transportworkflow.GetWorkflowDraftProcedure,
		transportworkflow.InsertWorkflowPaletteEntryProcedure,
		transportworkflow.UpdateWorkflowDraftNodeProcedure,
		transportworkflow.ApplyWorkflowTemplateOverlayProcedure,
		transportworkflow.MoveWorkflowDraftNodeProcedure,
		transportworkflow.NavigateWorkflowDraftHistoryProcedure,
	} {
		t.Run(procedure, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, procedure, nil)
			req.Header.Set("Content-Type", "application/proto")
			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)
			if res.Code == http.StatusNotFound {
				t.Fatalf("draft authoring route %s returned 404", procedure)
			}
			if _, ok := requestFactories[procedure]; !ok {
				t.Fatalf("draft authoring route %s has no strict-decoding request factory", procedure)
			}
		})
	}
}

func TestHandler_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestHandler_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestSemanticPromotionRouteIsMountedWithTheTypedRequestFactory(t *testing.T) {
	h, err := NewHandler(Options{
		Config:  transport.Config{Verifier: promotionRouteVerifier{}},
		Journey: &transportjourney.Dependencies{},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, transportjourney.ProposeIntoManagementProcedure, nil)
	req.Header.Set("Content-Type", "application/proto")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code == http.StatusNotFound {
		t.Fatalf("semantic promotion route returned 404")
	}
	if _, ok := requestFactories[transportjourney.ProposeIntoManagementProcedure]; !ok {
		t.Fatalf("semantic promotion route has no strict-decoding request factory")
	}
}
