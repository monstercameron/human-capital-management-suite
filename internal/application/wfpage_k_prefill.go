package application

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// WorkflowPagePrefillReader is the server-side field-authorized source
// boundary. It returns the raw value only inside the application process; the
// service immediately projects it through the productui authorization
// contract before returning anything to a caller.
type WorkflowPagePrefillReader interface {
	ReadWorkflowPagePrefill(context.Context, values.TenantId, string, productui.PrefillSource, string) (string, productui.AuthorizedField, error)
}

type WorkflowPagePrefillService struct {
	Reader WorkflowPagePrefillReader
}

func (service WorkflowPagePrefillService) Resolve(ctx context.Context, tenant values.TenantId, subject string, locale productui.LocaleContext, bindings []productui.WorkflowPagePrefillBinding) ([]productui.WorkflowPagePrefill, error) {
	if service.Reader == nil {
		return nil, fmt.Errorf("workflow page prefill: reader is unavailable")
	}
	sources := make(map[productui.PrefillSource]map[string]string)
	authorized := make([]productui.WorkflowPagePrefillBinding, 0, len(bindings))
	for _, binding := range bindings {
		value, verdict, err := service.Reader.ReadWorkflowPagePrefill(ctx, tenant, subject, binding.Source, binding.SourceField)
		if err != nil {
			return nil, fmt.Errorf("workflow page prefill %s/%s: %w", binding.Source, binding.SourceField, err)
		}
		if sources[binding.Source] == nil {
			sources[binding.Source] = make(map[string]string)
		}
		sources[binding.Source][binding.SourceField] = value
		binding.Authorized = verdict
		authorized = append(authorized, binding)
	}
	return productui.ResolveWorkflowPagePrefill(locale, authorized, sources), nil
}
