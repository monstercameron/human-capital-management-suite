package clockservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

const (
	workerTokenVersion = "v1"
	maxWorkerTokenSize = 4096
	maxWorkerTokenTTL  = 5 * time.Minute
)

// ErrInvalidDeviceWorkerToken indicates a malformed, unauthenticated or
// expired worker token. It intentionally does not reveal which check failed.
var ErrInvalidDeviceWorkerToken = errors.New("clockservice: invalid device worker token")

// HMACDeviceWorkerTokens issues and verifies opaque, short-lived worker
// tokens. The key must be injected by composition and is never derived from
// an administrator or device credential.
type HMACDeviceWorkerTokens struct {
	Key   []byte
	Clock func() time.Time
}

type workerTokenPayload struct {
	TenantID  string `json:"tenant_id"`
	DeviceID  string `json:"device_id"`
	WorkerID  string `json:"worker_id"`
	Method    string `json:"method"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
	Version   string `json:"version"`
}

func (h HMACDeviceWorkerTokens) now() time.Time {
	if h.Clock != nil {
		return h.Clock().UTC()
	}
	return time.Now().UTC()
}

func (h HMACDeviceWorkerTokens) validKey() bool { return len(h.Key) >= 32 }

// IssueDeviceWorkerToken signs claims with the injected HMAC key.
func (h HMACDeviceWorkerTokens) IssueDeviceWorkerToken(_ context.Context, claims DeviceWorkerTokenClaims) (DeviceWorkerToken, error) {
	if !h.validKey() || !validTokenClaims(claims) {
		return DeviceWorkerToken{}, ErrInvalidDeviceWorkerToken
	}
	now := h.now()
	if claims.IssuedAt.IsZero() {
		claims.IssuedAt = now
	}
	if claims.ExpiresAt.IsZero() {
		claims.ExpiresAt = claims.IssuedAt.Add(maxWorkerTokenTTL)
	}
	if !validTokenTimes(claims, now, true) {
		return DeviceWorkerToken{}, ErrInvalidDeviceWorkerToken
	}
	payload := workerTokenPayload{TenantID: claims.TenantID, DeviceID: claims.DeviceID, WorkerID: claims.WorkerID, Method: string(claims.Method), IssuedAt: claims.IssuedAt.Unix(), ExpiresAt: claims.ExpiresAt.Unix(), Version: workerTokenVersion}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return DeviceWorkerToken{}, ErrInvalidDeviceWorkerToken
	}
	token := h.sign(encoded)
	if len(token) > maxWorkerTokenSize {
		return DeviceWorkerToken{}, ErrInvalidDeviceWorkerToken
	}
	return DeviceWorkerToken{Value: token, IssuedAt: claims.IssuedAt.UTC(), ExpiresAt: claims.ExpiresAt.UTC()}, nil
}

// VerifyDeviceWorkerToken authenticates and validates one opaque token.
func (h HMACDeviceWorkerTokens) VerifyDeviceWorkerToken(_ context.Context, token string) (DeviceWorkerTokenClaims, error) {
	if !h.validKey() || len(token) == 0 || len(token) > maxWorkerTokenSize {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != workerTokenVersion {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	supplied, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	expected := h.mac(payload)
	if len(supplied) != len(expected) || subtle.ConstantTimeCompare(supplied, expected) != 1 {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	var value workerTokenPayload
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	if value.Version != workerTokenVersion {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	claims := DeviceWorkerTokenClaims{TenantID: value.TenantID, DeviceID: value.DeviceID, WorkerID: value.WorkerID, Method: clockMethod(value.Method), IssuedAt: time.Unix(value.IssuedAt, 0).UTC(), ExpiresAt: time.Unix(value.ExpiresAt, 0).UTC()}
	if !validTokenClaims(claims) || !validTokenTimes(claims, h.now(), false) {
		return DeviceWorkerTokenClaims{}, ErrInvalidDeviceWorkerToken
	}
	return claims, nil
}

func (h HMACDeviceWorkerTokens) sign(payload []byte) string {
	return workerTokenVersion + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(h.mac(payload))
}

func (h HMACDeviceWorkerTokens) mac(payload []byte) []byte {
	sum := hmac.New(sha256.New, h.Key)
	_, _ = sum.Write(payload)
	return sum.Sum(nil)
}

func validTokenClaims(c DeviceWorkerTokenClaims) bool {
	return strings.TrimSpace(c.TenantID) != "" && strings.TrimSpace(c.DeviceID) != "" && strings.TrimSpace(c.WorkerID) != "" && validClockMethod(c.Method)
}

func validTokenTimes(c DeviceWorkerTokenClaims, now time.Time, issuing bool) bool {
	if c.IssuedAt.IsZero() || c.ExpiresAt.IsZero() || !c.ExpiresAt.After(c.IssuedAt) || c.ExpiresAt.Sub(c.IssuedAt) > maxWorkerTokenTTL {
		return false
	}
	if issuing {
		return !now.Before(c.IssuedAt) && now.Before(c.ExpiresAt) && !c.ExpiresAt.After(now.Add(maxWorkerTokenTTL))
	}
	return !now.Before(c.IssuedAt) && now.Before(c.ExpiresAt)
}

func validClockMethod(method clockdomain.IdentificationMethod) bool {
	return method == clockdomain.MethodPIN || method == clockdomain.MethodBadge || method == clockdomain.MethodQR || method == clockdomain.MethodSupervisorOverride
}

func clockMethod(method string) clockdomain.IdentificationMethod {
	return clockdomain.IdentificationMethod(method)
}
