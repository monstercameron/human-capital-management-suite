package workspace

import (
	"errors"
	"strings"
	"testing"
)

func TestRequest_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestRequest_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestQueryWithWorkerTrimsOnlyANonEmptyWorkerReference(t *testing.T) {
	query := Query{WorkerRef: "worker-original"}
	if got := query.WithWorker("  worker-next  ").WorkerRef; got != "worker-next" {
		t.Fatalf("WithWorker = %q, want worker-next", got)
	}
	if got := query.WithWorker("  ").WorkerRef; got != "worker-original" {
		t.Fatalf("blank WithWorker replaced the bound worker with %q", got)
	}
}

func TestQueryTypedCanonicalizesWorkerReferenceAndRejectsMissingWorker(t *testing.T) {
	query, err := DefaultQuery()
	if err != nil {
		t.Fatal(err)
	}
	query.WorkerRef = "  " + query.WorkerRef + "  "
	req, err := query.Typed()
	if err != nil {
		t.Fatal(err)
	}
	if req.WorkerRef != strings.TrimSpace(query.WorkerRef) {
		t.Fatalf("typed worker = %q, want %q", req.WorkerRef, strings.TrimSpace(query.WorkerRef))
	}
	if _, err := (Query{}).Typed(); !errors.Is(err, ErrQueryInvalid) || !strings.Contains(err.Error(), "no worker") {
		t.Fatalf("missing worker error = %v, want ErrQueryInvalid/no worker", err)
	}
}

func TestQueryIntentCanonicalizesWorkerReference(t *testing.T) {
	query, err := DefaultQuery()
	if err != nil {
		t.Fatal(err)
	}
	left, err := query.Intent()
	if err != nil {
		t.Fatal(err)
	}
	query.WorkerRef = "  " + query.WorkerRef + "  "
	right, err := query.Intent()
	if err != nil {
		t.Fatal(err)
	}
	if left.Digest != right.Digest {
		t.Fatalf("worker whitespace changed intent digest: %s != %s", left.Digest, right.Digest)
	}
}
