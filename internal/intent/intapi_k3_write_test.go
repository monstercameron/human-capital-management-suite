package intent

import (
	"context"
	"errors"
	"testing"
	"time"
)

type intapiK3Authorizer struct{ calls int }

func (a *intapiK3Authorizer) AuthorizeWrite(_ context.Context, request WriteRequest) error {
	a.calls++
	if request.RequestedBy == "denied" {
		return errors.New("denied")
	}
	return nil
}

func intapiK3Request(i int) WriteRequest {
	return WriteRequest{TenantID: "tenant-a", IntentType: WriteHire, ResourceID: "worker-" + string(rune('a'+i)), SubjectID: "subject-" + string(rune('a'+i)), PayloadDigest: "sha256:payload", RequestedBy: "actor-a", IdempotencyKey: "key-" + string(rune('a'+i))}
}

func TestTodo_INTAPI_011(t *testing.T) {
	authorizer := &intapiK3Authorizer{}
	service := NewWriteService(authorizer, func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	created, err := service.Create(context.Background(), intapiK3Request(0), false)
	if err != nil || created.Status != WriteCreated || created.IntentID == "" {
		t.Fatalf("create = %+v, err=%v", created, err)
	}
	submitted, err := service.Submit(context.Background(), created.IntentID, "actor-a")
	if err != nil || submitted.Status != WriteSubmitted || submitted.Revision != 2 {
		t.Fatalf("submit = %+v, err=%v", submitted, err)
	}
	if _, err := service.Create(context.Background(), WriteRequest{TenantID: "tenant-a", IntentType: GovernedWriteType("generic_update"), ResourceID: "r", SubjectID: "s", PayloadDigest: "d", RequestedBy: "actor-a", IdempotencyKey: "x"}, true); !errors.Is(err, ErrUnsupportedWrite) {
		t.Fatalf("generic update error = %v", err)
	}
}

func TestTodo_INTAPI_011_Integration(t *testing.T) {
	service := NewWriteService(nil, nil)
	results := service.SubmitBatch(context.Background(), BatchWriteRequest{Items: []WriteRequest{intapiK3Request(0), intapiK3Request(1)}, MaxItems: 2, Submit: true})
	if len(results) != 2 || results[0].Error != "" || results[1].Intent.Status != WriteSubmitted {
		t.Fatalf("batch results = %+v", results)
	}
}

func TestTodo_INTAPI_011_Security(t *testing.T) {
	authorizer := &intapiK3Authorizer{}
	service := NewWriteService(authorizer, nil)
	request := intapiK3Request(0)
	request.RequestedBy = "denied"
	if _, err := service.Create(context.Background(), request, true); err == nil {
		t.Fatal("unauthorized write was accepted")
	}
	if authorizer.calls != 1 {
		t.Fatalf("authorizer calls = %d, want 1", authorizer.calls)
	}
}

func TestTodo_INTAPI_011_Property(t *testing.T) {
	service := NewWriteService(nil, nil)
	results := service.SubmitBatch(context.Background(), BatchWriteRequest{Items: []WriteRequest{intapiK3Request(0), intapiK3Request(1), intapiK3Request(2)}, MaxItems: 2, Submit: true})
	for i, result := range results {
		if result.Intent.IntentType != "" {
			t.Fatalf("item %d created despite batch limit: %+v", i, result)
		}
		if result.Error == "" {
			t.Fatalf("item %d missing bounded batch error", i)
		}
	}
	if _, err := service.Get("worker-1"); !errors.Is(err, ErrWriteNotFound) {
		t.Fatalf("governed fact lookup unexpectedly succeeded: %v", err)
	}
}
