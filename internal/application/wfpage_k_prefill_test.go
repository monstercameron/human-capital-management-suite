package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type workflowPagePrefillReader struct {
	value   string
	verdict productui.AuthorizedField
}

func (reader workflowPagePrefillReader) ReadWorkflowPagePrefill(context.Context, values.TenantId, string, productui.PrefillSource, string) (string, productui.AuthorizedField, error) {
	return reader.value, reader.verdict, nil
}

func TestTodo_WFPAGE_019_ApplicationSecurity(t *testing.T) {
	tenant := values.TenantId("11111111-1111-1111-1111-111111111111")
	service := WorkflowPagePrefillService{Reader: workflowPagePrefillReader{value: "90000", verdict: productui.AuthorizedField{Effect: productui.PresentationDenied, Reason: "restricted"}}}
	got, err := service.Resolve(context.Background(), tenant, "person:1", productui.ResolveProductLocale("en-US"), []productui.WorkflowPagePrefillBinding{{FieldID: "salary", Source: productui.PrefillPerson, SourceField: "base_pay"}})
	if err != nil || len(got) != 1 || got[0].Value != "" || got[0].Reason != "restricted" {
		t.Fatalf("server prefill projection = %+v, err=%v", got, err)
	}
}
