package main

import (
	"context"
	"errors"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
)

type rev09301Service struct {
	journeyclient.Service
	requests chan *journeyv1.PreviewRoleAccessRequest
}

func (s *rev09301Service) PreviewRoleAccess(_ context.Context, request *journeyv1.PreviewRoleAccessRequest) (*journeyv1.PreviewRoleAccessResponse, error) {
	s.requests <- request
	units := request.GetProposed().GetOrganizationUnits()
	return &journeyv1.PreviewRoleAccessResponse{
		RoleId: request.GetProposed().GetRoleId(), RoleName: "People manager", ExplicitRoles: []string{"manager"},
		Current:    &journeyv1.RoleAccessScope{Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering"}},
		Proposed:   &journeyv1.RoleAccessScope{Mode: request.GetProposed().GetMode(), OrganizationUnits: units},
		AddedUnits: []string{"Sales"}, HolderCount: 2,
	}, nil
}

type rev09301Refusing struct{}

func (rev09301Refusing) Submit(context.Context, taskmux.Spec, func(context.Context) error) (*taskmux.Handle, error) {
	return nil, errors.New("lane full")
}

// TestTodo_REV_093_01_WasmWiring proves the editor's preview button reaches
// PreviewRoleAccess through the shared task lane and hands back the
// qualified answer, and that a client without the call keeps the editor's
// unavailable notice instead of inventing a preview.
func TestTodo_REV_093_01_WasmWiring(t *testing.T) {
	if roleAccessPreviewRequest(context.Background(), taskmux.New(taskmux.Options{}), struct{ journeyclient.Service }{}) != nil {
		t.Fatal("a client without PreviewRoleAccess produced a preview request")
	}
	if roleAccessPreviewRequest(context.Background(), nil, &rev09301Service{}) != nil {
		t.Fatal("a missing task lane produced a preview request")
	}

	service := &rev09301Service{requests: make(chan *journeyv1.PreviewRoleAccessRequest, 1)}
	request := roleAccessPreviewRequest(context.Background(), taskmux.New(taskmux.Options{MaxRunning: 1, MaxQueued: 4}), service)
	if request == nil {
		t.Fatal("connected client produced no preview request")
	}
	type answer struct {
		preview productui.RoleAccessPreview
		err     error
	}
	answers := make(chan answer, 1)
	request(productui.OrganizationVisibilityPolicy{RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering", "Sales"}}, func(preview productui.RoleAccessPreview, err error) {
		answers <- answer{preview, err}
	})
	select {
	case got := <-answers:
		if got.err != nil {
			t.Fatalf("preview failed: %v", got.err)
		}
		if got.preview.RoleID != "manager" || got.preview.HolderCount != 2 || len(got.preview.ProposedUnits) != 2 || got.preview.AddedUnits[0] != "Sales" {
			t.Fatalf("preview = %+v", got.preview)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preview never answered")
	}
	if sent := <-service.requests; sent.GetProposed().GetRoleId() != "manager" || sent.GetProposed().GetMode() != "ALLOWLIST" {
		t.Fatalf("sent draft = %+v", sent.GetProposed())
	}

	// A lane that refuses the work reports an unavailable preview.
	refused := roleAccessPreviewRequest(context.Background(), rev09301Refusing{}, service)
	var refusal error
	refused(productui.OrganizationVisibilityPolicy{RoleID: "manager", Mode: "ALL"}, func(_ productui.RoleAccessPreview, err error) { refusal = err })
	if !errors.Is(refusal, productclient.ErrAccessPreviewUnavailable) {
		t.Fatalf("refused lane err = %v", refusal)
	}
	refused(productui.OrganizationVisibilityPolicy{RoleID: "manager"}, nil)
}
