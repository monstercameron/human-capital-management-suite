package timestore

import (
	"context"
	"testing"
)

func TestTodo_TCLOCK_002_PrepareMachineEnrollmentRejectsUnboundInput(t *testing.T) {
	_, err := (&Store{}).PrepareMachineEnrollment(context.Background(), MachineEnrollmentInput{Tenant: "tenant-a"})
	if err != ErrInvalid {
		t.Fatalf("err=%v, want ErrInvalid", err)
	}
}

func TestTodo_TCLOCK_002_ActivateMachineEnrollmentRejectsMissingCredential(t *testing.T) {
	_, err := (&Store{}).ActivateMachineEnrollment(context.Background(), "tenant-a", "device-a", "")
	if err != ErrInvalid {
		t.Fatalf("err=%v, want ErrInvalid", err)
	}
}
