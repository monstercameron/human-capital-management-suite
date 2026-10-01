package timeclockstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
)

const (
	maxRosterPages = 100
	pinIterations  = 50000
)

// CredentialRosterDevice supplies the enrolled device's current scope. It is
// deliberately separate from the credential store so a resolver cannot match
// a credential outside the device's published, site-scoped roster.
type CredentialRosterDevice interface {
	GetDevice(context.Context, string, string) (clockservice.DeviceRecord, error)
}

// ProductionCredentialResolver resolves shared-device credentials against the
// authoritative roster and durable credential lockout state. It never scans a
// tenant workforce: each query is bounded by the roster cursor contract.
type ProductionCredentialResolver struct {
	Roster      clockservice.RosterSource
	Devices     CredentialRosterDevice
	Credentials clockservice.CredentialStore
	Policy      clockservice.IdentificationPolicySource
	Clock       func() time.Time
}

// CredentialResolver is the production clock credential adapter.
type CredentialResolver = ProductionCredentialResolver

var _ clockservice.CredentialResolver = ProductionCredentialResolver{}

// ResolveDeviceCredential resolves one PIN, badge, or QR credential.
func (r ProductionCredentialResolver) ResolveDeviceCredential(ctx context.Context, tenant, device string, method clockdomain.IdentificationMethod, credential string) (string, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(device) == "" || strings.TrimSpace(credential) == "" || r.Roster == nil || r.Devices == nil || r.Credentials == nil || r.Policy == nil || r.Clock == nil {
		return "", clockservice.ErrUnavailable
	}
	now := r.now()
	locked, err := r.deviceLocked(ctx, tenant, device, now)
	if err != nil {
		return "", err
	}
	if locked {
		return "", clockservice.ErrLockedOut
	}
	d, err := r.Devices.GetDevice(ctx, tenant, device)
	if err != nil {
		return "", err
	}
	if d.TenantID != tenant || d.ID != device || d.State != "ACTIVE" || d.SiteID == "" || d.ProfileID == "" {
		return "", clockservice.ErrDeviceNotEligible
	}
	policy, err := r.Policy.IdentificationPolicy(ctx, tenant, d.SiteID, d.ProfileID)
	if err != nil {
		return "", err
	}
	if err := policy.Validate(); err != nil {
		return "", err
	}
	workers, err := r.roster(ctx, d, now)
	if err != nil {
		return "", err
	}
	matches := matchingWorkers(workers, method, credential)
	if len(matches) != 1 {
		if err := r.failed(ctx, tenant, device, "", policy, now); err != nil {
			return "", err
		}
		return "", clockservice.ErrWorkerNotEligible
	}
	worker := matches[0]
	locked, err = r.workerLocked(ctx, tenant, worker, now)
	if err != nil {
		return "", err
	}
	if locked {
		return "", clockservice.ErrLockedOut
	}
	if err := r.Credentials.ResetDeviceAttempts(ctx, tenant, device); err != nil {
		return "", err
	}
	if err := r.Credentials.ResetWorkerAttempts(ctx, tenant, worker); err != nil {
		return "", err
	}
	return worker, nil
}

func (r ProductionCredentialResolver) now() time.Time {
	return r.Clock().UTC()
}

func (r ProductionCredentialResolver) deviceLocked(ctx context.Context, tenant, device string, now time.Time) (bool, error) {
	s, err := r.Credentials.DeviceLockout(ctx, tenant, device)
	return err == nil && !s.LockedUntil.IsZero() && now.Before(s.LockedUntil), err
}

func (r ProductionCredentialResolver) workerLocked(ctx context.Context, tenant, worker string, now time.Time) (bool, error) {
	s, err := r.Credentials.WorkerLockout(ctx, tenant, worker)
	return err == nil && !s.LockedUntil.IsZero() && now.Before(s.LockedUntil), err
}

func (r ProductionCredentialResolver) failed(ctx context.Context, tenant, device, worker string, policy clockdomain.RateLimitPolicy, now time.Time) error {
	until := now.Add(policy.LockoutDuration)
	if _, err := r.Credentials.RecordFailedDeviceAttempt(ctx, tenant, device, policy.MaxAttempts, until); err != nil {
		return err
	}
	if worker != "" {
		_, err := r.Credentials.RecordFailedWorkerAttempt(ctx, tenant, worker, policy.MaxAttempts, until)
		return err
	}
	return nil
}

func (r ProductionCredentialResolver) roster(ctx context.Context, d clockservice.DeviceRecord, now time.Time) (map[string]clockservice.RosterWorker, error) {
	workers := make(map[string]clockservice.RosterWorker)
	cursor := ""
	for page := 0; page < maxRosterPages; page++ {
		delta, err := r.Roster.Delta(ctx, d.TenantID, d.SiteID, d.ProfileID, cursor, now)
		if err != nil {
			return nil, err
		}
		for _, worker := range delta.Workers {
			if worker.WorkerRef != "" && !worker.Terminated {
				workers[worker.WorkerRef] = worker
			}
		}
		if !delta.HasMore {
			return workers, nil
		}
		if delta.NextCursor == "" || delta.NextCursor == cursor {
			return nil, errors.New("timeclockstore: invalid roster cursor")
		}
		cursor = delta.NextCursor
	}
	return nil, errors.New("timeclockstore: roster exceeds bounded page limit")
}

func matchingWorkers(workers map[string]clockservice.RosterWorker, method clockdomain.IdentificationMethod, credential string) []string {
	var matches []string
	for id, worker := range workers {
		matched := method == clockdomain.MethodBadge && subtle.ConstantTimeCompare([]byte(worker.BadgeID), []byte(credential)) == 1
		matched = matched || method == clockdomain.MethodQR && subtle.ConstantTimeCompare([]byte(worker.QRKeyID), []byte(credential)) == 1
		matched = matched || method == clockdomain.MethodPIN && pinMatches(credential, worker.PINHash, worker.PINSalt)
		if matched {
			matches = append(matches, id)
		}
	}
	return matches
}

func pinMatches(pin string, expected, salt []byte) bool {
	if pin == "" || len(expected) != sha256.Size || len(salt) < 16 {
		return false
	}
	sum := []byte(pin)
	for i := 0; i < pinIterations; i++ {
		mac := hmac.New(sha256.New, salt)
		_, _ = mac.Write(sum)
		sum = mac.Sum(nil)
	}
	return subtle.ConstantTimeCompare(sum, expected) == 1
}

// ProductionPunchContextSource binds an opaque worker token to current
// worker, assignment and profile authority before an offline punch commits.
type ProductionPunchContextSource struct {
	Tokens   clockservice.TokenVerifier
	Workers  clockservice.WorkerDirectory
	Profiles clockservice.ProfileResolver
}

// PunchContextSource is the production token-to-assignment adapter.
type PunchContextSource = ProductionPunchContextSource

// CurrentAssignmentDirectory is the authoritative as-of assignment lookup
// needed when a short-lived device token intentionally carries no assignment.
type CurrentAssignmentDirectory interface {
	CurrentAssignment(context.Context, string, string, time.Time) (assignmentID, projectRef, siteRef string, ok bool, err error)
}

// AuthoritativeWorkerDirectory is the narrow read contract implemented by the
// people/assignment application source. It is intentionally injectable so a
// production composition can bind the real RLS-scoped PostgreSQL reader.
type AuthoritativeWorkerDirectory interface {
	ResolveWorker(context.Context, string, string) (workerID string, active bool, err error)
	ResolveAssignment(context.Context, string, string, string) (projectRef, siteRef string, ok bool, err error)
	CurrentAssignment(context.Context, string, string, time.Time) (assignmentID, projectRef, siteRef string, ok bool, err error)
	ResolveWorkerStatus(context.Context, string, string, string) (clockservice.WorkerStatusResult, error)
}

// ProductionWorkerDirectory adapts the authoritative people reader to the
// clock service port. No display-name or client-provided role is accepted.
type ProductionWorkerDirectory struct{ Source AuthoritativeWorkerDirectory }

// WorkerDirectory is the production authoritative worker adapter.
type WorkerDirectory = ProductionWorkerDirectory

var _ clockservice.WorkerDirectory = ProductionWorkerDirectory{}
var _ CurrentAssignmentDirectory = ProductionWorkerDirectory{}

// ResolveWorker resolves and validates current worker lifecycle state.
func (d ProductionWorkerDirectory) ResolveWorker(ctx context.Context, tenant, worker string) (string, bool, error) {
	if d.Source == nil {
		return "", false, clockservice.ErrUnavailable
	}
	return d.Source.ResolveWorker(ctx, tenant, worker)
}

// ResolveAssignment resolves an assignment through the authoritative source.
func (d ProductionWorkerDirectory) ResolveAssignment(ctx context.Context, tenant, worker, assignment string) (string, string, bool, error) {
	if d.Source == nil {
		return "", "", false, clockservice.ErrUnavailable
	}
	return d.Source.ResolveAssignment(ctx, tenant, worker, assignment)
}

// CurrentAssignment resolves the effective assignment at the event instant.
func (d ProductionWorkerDirectory) CurrentAssignment(ctx context.Context, tenant, worker string, at time.Time) (string, string, string, bool, error) {
	if d.Source == nil {
		return "", "", "", false, clockservice.ErrUnavailable
	}
	return d.Source.CurrentAssignment(ctx, tenant, worker, at)
}

// ProductionWorkerStatusSource reads status from the same authoritative
// directory after token scope has been validated by the facade.
type ProductionWorkerStatusSource struct{ Directory AuthoritativeWorkerDirectory }

// WorkerStatusSource is the production status projection adapter.
type WorkerStatusSource = ProductionWorkerStatusSource

var _ clockservice.WorkerStatusSource = ProductionWorkerStatusSource{}

// ResolveWorkerStatus returns a current status projection.
func (s ProductionWorkerStatusSource) ResolveWorkerStatus(ctx context.Context, claims clockservice.DeviceWorkerTokenClaims) (clockservice.WorkerStatusResult, error) {
	if s.Directory == nil || claims.TenantID == "" || claims.WorkerID == "" {
		return clockservice.WorkerStatusResult{}, clockservice.ErrUnavailable
	}
	return s.Directory.ResolveWorkerStatus(ctx, claims.TenantID, claims.WorkerID, claims.DeviceID)
}

// CapabilityAuthorizer is the application boundary required for clock
// authorization. The composition root must bind it to the existing registry
// gateway; this adapter intentionally has no role or capability fallback.
type CapabilityAuthorizer interface {
	AuthorizePunch(context.Context, string, string, string, string) (delegated bool, err error)
}

// TimestoreProfileResolver resolves the assignment's durable profile pin and
// then reads the immutable profile version. There is no default profile: a
// missing pin or invalid payload fails closed so time cannot be captured under
// an inferred policy.
type TimestoreProfileResolver struct{ Store *timestore.Store }

var _ clockservice.ProfileResolver = TimestoreProfileResolver{}

// Resolve returns the pinned, validated profile effective for an assignment.
func (r TimestoreProfileResolver) Resolve(ctx context.Context, tenant, _, assignment string, at time.Time) (timeprofile.TimeProfile, error) {
	if r.Store == nil || tenant == "" || assignment == "" || at.IsZero() {
		return timeprofile.TimeProfile{}, clockservice.ErrUnavailable
	}
	pin, err := r.Store.AssignmentProfilePinFor(ctx, tenant, assignment)
	if err != nil {
		return timeprofile.TimeProfile{}, err
	}
	version, err := r.Store.ProfileVersionAt(ctx, tenant, pin.ProfileID, at)
	if err != nil {
		return timeprofile.TimeProfile{}, err
	}
	if version.Version != pin.ProfileVersion {
		return timeprofile.TimeProfile{}, errors.New("timeclockstore: assignment profile pin is not effective")
	}
	var profile timeprofile.TimeProfile
	if err := json.Unmarshal(version.Payload, &profile); err != nil {
		return timeprofile.TimeProfile{}, err
	}
	if profile.ID != pin.ProfileID || profile.Version != uint64(pin.ProfileVersion) || profile.Validate() != nil {
		return timeprofile.TimeProfile{}, errors.New("timeclockstore: invalid persisted time profile")
	}
	return profile, nil
}

var _ clockservice.PunchContextSource = ProductionPunchContextSource{}

// ResolvePunchContext resolves current assignment authority for a token.
func (s ProductionPunchContextSource) ResolvePunchContext(ctx context.Context, tenant, device, token string, at time.Time) (string, string, error) {
	current, _ := s.Workers.(CurrentAssignmentDirectory)
	if s.Tokens == nil || s.Workers == nil || current == nil || s.Profiles == nil || tenant == "" || device == "" || token == "" {
		return "", "", clockservice.ErrUnavailable
	}
	claims, err := s.Tokens.VerifyDeviceWorkerToken(ctx, token)
	if err != nil || claims.TenantID != tenant || claims.DeviceID != device || claims.WorkerID == "" || at.Before(claims.IssuedAt) || !at.Before(claims.ExpiresAt) {
		return "", "", clockservice.ErrInvalidDeviceWorkerToken
	}
	worker, active, err := s.Workers.ResolveWorker(ctx, tenant, claims.WorkerID)
	if err != nil || !active {
		return "", "", clockservice.ErrWorkerNotEligible
	}
	assignment, _, _, found, err := current.CurrentAssignment(ctx, tenant, worker, at)
	if err != nil || !found {
		return "", "", clockservice.ErrAssignmentNotFound
	}
	if _, err := s.Profiles.Resolve(ctx, tenant, worker, assignment, at); err != nil {
		return "", "", err
	}
	return worker, assignment, nil
}
