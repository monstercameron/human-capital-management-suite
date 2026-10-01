package timeclockstore

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// CredentialAdapter maps credential verification, lockout and audit evidence.
type CredentialAdapter struct{ Store *timestore.Store }

var _ clockservice.CredentialStore = CredentialAdapter{}

// IssuePINCredential stores a salted PIN verifier.
func (a CredentialAdapter) IssuePINCredential(ctx context.Context, tenant, worker, pin, actor string) (clockservice.CredentialRecord, error) {
	if a.Store == nil {
		return clockservice.CredentialRecord{}, clockservice.ErrUnavailable
	}
	c, err := a.Store.IssuePINCredential(ctx, tenant, worker, pin, actor)
	return credentialRecord(c), err
}

// IssueBadgeCredential issues a badge credential.
func (a CredentialAdapter) IssueBadgeCredential(ctx context.Context, tenant, worker, badge, actor string) (clockservice.CredentialRecord, error) {
	if a.Store == nil {
		return clockservice.CredentialRecord{}, clockservice.ErrUnavailable
	}
	c, err := a.Store.IssueBadgeCredential(ctx, tenant, worker, badge, actor)
	return credentialRecord(c), err
}

// IssueQRCredential issues a QR credential.
func (a CredentialAdapter) IssueQRCredential(ctx context.Context, tenant, worker, qr, actor string) (clockservice.CredentialRecord, error) {
	if a.Store == nil {
		return clockservice.CredentialRecord{}, clockservice.ErrUnavailable
	}
	c, err := a.Store.IssueQRCredential(ctx, tenant, worker, qr, actor)
	return credentialRecord(c), err
}

// RevokeCredential revision-checks and revokes a credential.
func (a CredentialAdapter) RevokeCredential(ctx context.Context, tenant, worker, kind, actor string, rev int64) (clockservice.CredentialRecord, error) {
	if a.Store == nil {
		return clockservice.CredentialRecord{}, clockservice.ErrUnavailable
	}
	c, err := a.Store.RevokeCredential(ctx, tenant, worker, kind, actor, rev)
	return credentialRecord(c), err
}

// VerifyPIN verifies an active PIN using timestore's constant-time check.
func (a CredentialAdapter) VerifyPIN(ctx context.Context, tenant, worker, pin string) (bool, error) {
	if a.Store == nil {
		return false, clockservice.ErrUnavailable
	}
	return a.Store.VerifyPIN(ctx, tenant, worker, pin)
}

// RecordFailedDeviceAttempt increments a device lockout counter.
func (a CredentialAdapter) RecordFailedDeviceAttempt(ctx context.Context, tenant, device string, threshold int, until time.Time) (int, error) {
	if a.Store == nil {
		return 0, clockservice.ErrUnavailable
	}
	return a.Store.RecordFailedDeviceAttempt(ctx, tenant, device, threshold, until)
}

// RecordFailedWorkerAttempt increments a worker lockout counter.
func (a CredentialAdapter) RecordFailedWorkerAttempt(ctx context.Context, tenant, worker string, threshold int, until time.Time) (int, error) {
	if a.Store == nil {
		return 0, clockservice.ErrUnavailable
	}
	return a.Store.RecordFailedWorkerAttempt(ctx, tenant, worker, threshold, until)
}

// ResetDeviceAttempts clears device lockout evidence.
func (a CredentialAdapter) ResetDeviceAttempts(ctx context.Context, tenant, device string) error {
	if a.Store == nil {
		return clockservice.ErrUnavailable
	}
	return a.Store.ResetDeviceAttempts(ctx, tenant, device)
}

// ResetWorkerAttempts clears worker lockout evidence.
func (a CredentialAdapter) ResetWorkerAttempts(ctx context.Context, tenant, worker string) error {
	if a.Store == nil {
		return clockservice.ErrUnavailable
	}
	return a.Store.ResetWorkerAttempts(ctx, tenant, worker)
}

// DeviceLockout reads device lockout evidence.
func (a CredentialAdapter) DeviceLockout(ctx context.Context, tenant, device string) (clockservice.LockoutState, error) {
	if a.Store == nil {
		return clockservice.LockoutState{}, clockservice.ErrUnavailable
	}
	s, err := a.Store.DeviceLockout(ctx, tenant, device)
	return clockservice.LockoutState{FailedCount: s.FailedCount, LockedUntil: valueTime(s.LockedUntil)}, err
}

// WorkerLockout reads worker lockout evidence.
func (a CredentialAdapter) WorkerLockout(ctx context.Context, tenant, worker string) (clockservice.LockoutState, error) {
	if a.Store == nil {
		return clockservice.LockoutState{}, clockservice.ErrUnavailable
	}
	s, err := a.Store.WorkerLockout(ctx, tenant, worker)
	return clockservice.LockoutState{FailedCount: s.FailedCount, LockedUntil: valueTime(s.LockedUntil)}, err
}

// RecordSupervisorOverride appends supervisor override evidence.
func (a CredentialAdapter) RecordSupervisorOverride(ctx context.Context, tenant, device, worker, credential, reason string) (clockservice.SupervisorOverrideRecord, error) {
	if a.Store == nil {
		return clockservice.SupervisorOverrideRecord{}, clockservice.ErrUnavailable
	}
	r, err := a.Store.RecordSupervisorOverride(ctx, tenant, device, worker, credential, reason)
	return clockservice.SupervisorOverrideRecord{ID: r.ID, DeviceID: r.DeviceID, WorkerID: r.WorkerID, SupervisorCredentialRef: r.SupervisorCredentialRef, Reason: r.Reason, CreatedAt: r.CreatedAt}, err
}

func valueTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}
