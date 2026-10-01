package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type workflowPagePublicationReader struct{ publication runtime.PagePublication }

func (reader workflowPagePublicationReader) PublishedPage(context.Context, runtime.PageStartRequest) (runtime.PagePublication, error) {
	return reader.publication, nil
}

func TestTodo_WFPAGE_021_IntentService(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	request := runtime.PageStartRequest{TenantID: tenant, WorkflowID: "hire", WorkflowVersion: 2, PageID: "page", PageVersion: 4, SubjectRef: "person:1", IdempotencyKey: "idem", Inputs: map[string]json.RawMessage{"name": json.RawMessage(`"Ada"`)}}
	service := WorkflowPageStartService{
		Publications: workflowPagePublicationReader{publication: runtime.PagePublication{TenantID: tenant, WorkflowID: "hire", PageID: "page", PageVersion: 4, Published: true}},
		Registry:     runtime.NewPageStartRegistry(),
		Start: func(_ context.Context, binding runtime.PageStartBinding) (runtime.PageStartReceipt, error) {
			return runtime.PageStartReceipt{InstanceID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), InputDigest: binding.InputDigest}, nil
		},
	}
	first, err := service.SubmitWorkflowPage(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SubmitWorkflowPage(context.Background(), request)
	if err != nil || !second.Replay || first.InstanceID != second.InstanceID {
		t.Fatalf("duplicate submit = %+v then %+v, err=%v", first, second, err)
	}
}
