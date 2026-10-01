package clockservice

import (
	"context"
	"crypto/ed25519"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// SyncRoster returns the published site roster and policy delta for the current
// enrolled machine identity. The source owns cursor validation and pagination;
// no workers are copied into a device table.
func (s Service) SyncRoster(ctx context.Context, p *trust.Principal, deviceID, cursor string) (RosterDelta, error) {
	device, err := s.authenticatedRosterDevice(ctx, p, deviceID)
	if err != nil {
		return RosterDelta{}, err
	}
	if s.Roster == nil || len(cursor) > 4096 {
		if s.Roster == nil {
			return RosterDelta{}, ErrUnavailable
		}
		return RosterDelta{}, ErrInvalidRequest
	}
	delta, err := s.Roster.Delta(ctx, device.TenantID, device.SiteID, device.ProfileID, cursor, s.now())
	if err != nil {
		return RosterDelta{}, err
	}
	if err := validateRosterDelta(delta); err != nil {
		return RosterDelta{}, err
	}
	return copyRosterDelta(delta), nil
}

// authenticatedRosterDevice rechecks the enrolled identity on every device call.
// ClientID is server-derived from the machine credential, never a request field.
func (s Service) authenticatedRosterDevice(ctx context.Context, p *trust.Principal, deviceID string) (DeviceRecord, error) {
	if err := validPrincipal(p); err != nil {
		return DeviceRecord{}, err
	}
	if strings.TrimSpace(deviceID) == "" || p.SubjectKind() != trust.SubjectKindService || p.ClientID() != deviceID {
		return DeviceRecord{}, ErrDeviceNotEligible
	}
	if s.Devices == nil || s.Clock == nil {
		return DeviceRecord{}, ErrUnavailable
	}
	if !s.now().Before(p.ExpiresAt()) || s.now().Before(p.IssuedAt()) {
		return DeviceRecord{}, ErrInvalidPrincipal
	}
	d, err := s.Devices.GetDevice(ctx, tenantOf(p), deviceID)
	if err != nil {
		return DeviceRecord{}, err
	}
	if d.ID != deviceID || d.TenantID != tenantOf(p) || d.State != "ACTIVE" || d.Revision < 1 ||
		!requireNonEmpty(d.SiteID, d.ProfileID, d.Timezone) || len(d.PublicKey) != ed25519.PublicKeySize {
		return DeviceRecord{}, ErrDeviceNotEligible
	}
	if _, err := time.LoadLocation(d.Timezone); err != nil {
		return DeviceRecord{}, ErrDeviceNotEligible
	}
	return d, nil
}

func validateRosterDelta(d RosterDelta) error {
	if !requireNonEmpty(d.SnapshotRevision) || d.MaxOfflineAge <= 0 || d.PunchPolicyVersion < 1 ||
		len(d.Workers) > 1000 || len(d.NextCursor) > 4096 || (d.HasMore && d.NextCursor == "") {
		return ErrInvalidRequest
	}
	seen := make(map[string]bool, len(d.Workers))
	for _, w := range d.Workers {
		if !requireNonEmpty(w.WorkerRef) || seen[w.WorkerRef] {
			return ErrInvalidRequest
		}
		seen[w.WorkerRef] = true
		// A removal may carry no credential at all. It may never retain a
		// usable verifier, including in a termination delta.
		if w.Terminated {
			if len(w.PINHash)+len(w.PINSalt) > 0 || w.BadgeID != "" || w.QRKeyID != "" {
				return ErrInvalidRequest
			}
			continue
		}
		if (len(w.PINHash) != 0 || len(w.PINSalt) != 0) && (len(w.PINHash) != 32 || len(w.PINSalt) < 16) {
			return ErrInvalidRequest
		}
		if len(w.PINHash) == 0 && w.BadgeID == "" && w.QRKeyID == "" {
			return ErrInvalidRequest
		}
	}
	for _, ref := range d.UrgentRemovals {
		if !requireNonEmpty(ref) {
			return ErrInvalidRequest
		}
		for _, w := range d.Workers {
			if w.WorkerRef == ref && !w.Terminated {
				return ErrInvalidRequest
			}
		}
	}
	if d.Questions.ID != "" {
		return d.Questions.Validate()
	}
	return nil
}

func copyRosterDelta(d RosterDelta) RosterDelta {
	d.Workers = slices.Clone(d.Workers)
	for i := range d.Workers {
		d.Workers[i].PINHash = slices.Clone(d.Workers[i].PINHash)
		d.Workers[i].PINSalt = slices.Clone(d.Workers[i].PINSalt)
	}
	d.JobCodes = slices.Clone(d.JobCodes)
	d.Shifts = slices.Clone(d.Shifts)
	d.UrgentRemovals = slices.Clone(d.UrgentRemovals)
	d.Questions.Questions = slices.Clone(d.Questions.Questions)
	for i := range d.Questions.Questions {
		d.Questions.Questions[i].Options = slices.Clone(d.Questions.Questions[i].Options)
	}
	stringsCopy := make(map[string]string, len(d.Strings))
	for key, value := range d.Strings {
		stringsCopy[key] = value
	}
	d.Strings = stringsCopy
	return d
}
