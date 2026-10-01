package clockservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

func TestHMACDeviceWorkerTokens_roundTripAndTamperRejection(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	issuer := HMACDeviceWorkerTokens{Key: []byte(strings.Repeat("k", 32)), Clock: func() time.Time { return now }}
	claims := DeviceWorkerTokenClaims{TenantID: "tenant", DeviceID: "device", WorkerID: "worker", Method: clockdomain.MethodPIN, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	tok, err := issuer.IssueDeviceWorkerToken(context.Background(), claims)
	if err != nil {
		t.Fatal(err)
	}
	got, err := issuer.VerifyDeviceWorkerToken(context.Background(), tok.Value)
	if err != nil || got != claims {
		t.Fatalf("claims = %#v, err=%v", got, err)
	}
	bad := tok.Value[:len(tok.Value)-1] + "x"
	if _, err := issuer.VerifyDeviceWorkerToken(context.Background(), bad); !errors.Is(err, ErrInvalidDeviceWorkerToken) {
		t.Fatalf("tampered token err = %v", err)
	}
}

func TestHMACDeviceWorkerTokens_rejectsWeakKeyAndInvalidLifetime(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	issuer := HMACDeviceWorkerTokens{Key: []byte("short"), Clock: func() time.Time { return now }}
	claims := DeviceWorkerTokenClaims{TenantID: "tenant", DeviceID: "device", WorkerID: "worker", Method: clockdomain.MethodQR, IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
	if _, err := issuer.IssueDeviceWorkerToken(context.Background(), claims); !errors.Is(err, ErrInvalidDeviceWorkerToken) {
		t.Fatalf("weak key err = %v", err)
	}
	issuer.Key = []byte(strings.Repeat("k", 32))
	claims.ExpiresAt = now.Add(6 * time.Minute)
	if _, err := issuer.IssueDeviceWorkerToken(context.Background(), claims); !errors.Is(err, ErrInvalidDeviceWorkerToken) {
		t.Fatalf("long lifetime err = %v", err)
	}
	claims.ExpiresAt = now
	if _, err := issuer.IssueDeviceWorkerToken(context.Background(), claims); !errors.Is(err, ErrInvalidDeviceWorkerToken) {
		t.Fatalf("expired lifetime err = %v", err)
	}
}
