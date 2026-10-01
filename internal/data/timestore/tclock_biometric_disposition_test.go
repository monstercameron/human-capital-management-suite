package timestore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTodo_TCLOCK_006_DispositionRejectsUnboundRequests(t *testing.T) {
	var store *Store
	cases := []struct {
		name string
		call func() error
	}{
		{"withdraw tenant", func() error {
			_, err := store.WithdrawBiometricConsent(context.Background(), "", "consent", "worker", "actor", "reason", "idem", 1)
			return err
		}},
		{"claim time", func() error {
			_, err := store.ClaimBiometricTombstone(context.Background(), "tenant", "id", "actor", time.Time{})
			return err
		}},
		{"mark time", func() error {
			_, err := store.MarkBiometricKeyDestroyed(context.Background(), "tenant", "id", "actor", time.Time{})
			return err
		}},
		{"reconcile limit", func() error {
			_, err := store.ReconcileBiometricDisposition(context.Background(), "tenant", 0)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err=%v, want ErrInvalid", err)
			}
		})
	}
}

func TestTodo_TCLOCK_017_ReconcileRejectsUnboundedTenant(t *testing.T) {
	var store *Store
	if _, err := store.ReconcileBiometricDisposition(context.Background(), "", 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v, want ErrInvalid", err)
	}
}
