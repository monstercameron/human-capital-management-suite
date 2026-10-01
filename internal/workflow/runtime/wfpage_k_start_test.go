package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func pageStartFixture() (PageStartRequest, PagePublication) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	request := PageStartRequest{TenantID: tenant, WorkflowID: "hire", WorkflowVersion: 7, PageID: "hire-page", PageVersion: 3, SubjectRef: "person:1", IdempotencyKey: "submit-1", Inputs: map[string]json.RawMessage{"name": json.RawMessage(`"Ada"`), "salary": json.RawMessage(`90000`)}}
	publication := PagePublication{TenantID: tenant, WorkflowID: "hire", PageID: "hire-page", PageVersion: 3, Digest: "sha256:page", Published: true}
	return request, publication
}

func TestTodo_WFPAGE_021(t *testing.T) {
	request, publication := pageStartFixture()
	binding, err := BindPageStart(request, publication)
	if err != nil || binding.InputDigest == "" || binding.PageVersion != 3 {
		t.Fatalf("binding = %+v, err=%v", binding, err)
	}
	registry := NewPageStartRegistry()
	calls := 0
	receipt, err := registry.Submit(context.Background(), binding, func(context.Context, PageStartBinding) (PageStartReceipt, error) {
		calls++
		return PageStartReceipt{InstanceID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")}, nil
	})
	if err != nil || calls != 1 || receipt.PageVersion != 3 || receipt.InputDigest != binding.InputDigest {
		t.Fatalf("first submit = %+v calls=%d err=%v", receipt, calls, err)
	}
	replay, err := registry.Submit(context.Background(), binding, func(context.Context, PageStartBinding) (PageStartReceipt, error) {
		calls++
		return PageStartReceipt{}, nil
	})
	if err != nil || !replay.Replay || replay.InstanceID != receipt.InstanceID || calls != 1 {
		t.Fatalf("replay = %+v calls=%d err=%v", replay, calls, err)
	}
}

func TestTodo_WFPAGE_021_Security(t *testing.T) {
	request, publication := pageStartFixture()
	request.PageVersion = 99
	if _, err := BindPageStart(request, publication); !errors.Is(err, ErrPageNotPublished) {
		t.Fatalf("forged page version err=%v, want publication refusal", err)
	}
	publication.TenantID = uuid.New()
	request.PageVersion = 3
	if _, err := BindPageStart(request, publication); !errors.Is(err, ErrPageNotPublished) {
		t.Fatalf("foreign publication err=%v, want publication refusal", err)
	}
}

func TestTodo_WFPAGE_021_Integration(t *testing.T) {
	request, publication := pageStartFixture()
	first, err := BindPageStart(request, publication)
	if err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.Inputs = map[string]json.RawMessage{"name": json.RawMessage(`"Grace"`)}
	second, err := BindPageStart(changed, publication)
	if err != nil {
		t.Fatal(err)
	}
	if first.InputDigest == second.InputDigest {
		t.Fatal("different typed page inputs share an input digest")
	}
}

func TestTodo_WFPAGE_021_Fault(t *testing.T) {
	request, publication := pageStartFixture()
	binding, err := BindPageStart(request, publication)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewPageStartRegistry()
	calls := 0
	start := func(context.Context, PageStartBinding) (PageStartReceipt, error) {
		calls++
		if calls == 1 {
			return PageStartReceipt{}, errors.New("connection lost after accept")
		}
		return PageStartReceipt{InstanceID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")}, nil
	}
	if _, err := registry.Submit(context.Background(), binding, start); err == nil {
		t.Fatal("faulty start succeeded")
	}
	retry, err := registry.Submit(context.Background(), binding, start)
	if err != nil || retry.InstanceID == uuid.Nil || calls != 2 {
		t.Fatalf("retry = %+v calls=%d err=%v", retry, calls, err)
	}
}
