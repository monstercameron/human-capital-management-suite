package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var ErrWorkflowPageStartUnavailable = errors.New("app: workflow page start is unavailable")

// WorkflowPagePublicationReader resolves the tenant-authorized published
// page version. The request's page version is an assertion to check, never an
// authorization fact to trust.
type WorkflowPagePublicationReader interface {
	PublishedPage(context.Context, runtime.PageStartRequest) (runtime.PagePublication, error)
}

type WorkflowPageStartService struct {
	Publications WorkflowPagePublicationReader
	Registry     *runtime.PageStartRegistry
	Start        runtime.PageStartExecutor
}

// SubmitWorkflowPage resolves publication through the server-side reader,
// binds the typed inputs and page version, and sends the result through the
// same start executor used by the existing intent path. Registry replay is
// keyed by tenant, workflow, subject and idempotency key.
func (service WorkflowPageStartService) SubmitWorkflowPage(ctx context.Context, request runtime.PageStartRequest) (runtime.PageStartReceipt, error) {
	if service.Publications == nil || service.Registry == nil || service.Start == nil {
		return runtime.PageStartReceipt{}, ErrWorkflowPageStartUnavailable
	}
	publication, err := service.Publications.PublishedPage(ctx, request)
	if err != nil {
		return runtime.PageStartReceipt{}, fmt.Errorf("%w: resolve published page: %v", ErrWorkflowPageStartUnavailable, err)
	}
	binding, err := runtime.BindPageStart(request, publication)
	if err != nil {
		return runtime.PageStartReceipt{}, err
	}
	return service.Registry.Submit(ctx, binding, service.Start)
}
