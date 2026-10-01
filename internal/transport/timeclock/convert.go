package timeclock

import (
	"context"
	"fmt"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func principal(ctx context.Context) (*trust.Principal, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return nil, clockservice.ErrInvalidPrincipal
	}
	return p, nil
}

func validTimestamp(ts *timestamppb.Timestamp) (time.Time, error) {
	if ts == nil || !ts.IsValid() {
		return time.Time{}, fmt.Errorf("%w: timestamp is required and must be valid", clockservice.ErrInvalidRequest)
	}
	return ts.AsTime().UTC(), nil
}

func device(d clockservice.DeviceRecord) *timev1.ClockDevice {
	state := timev1.ClockDeviceState_CLOCK_DEVICE_STATE_UNSPECIFIED
	switch d.State {
	case "ACTIVE":
		state = timev1.ClockDeviceState_CLOCK_DEVICE_STATE_ACTIVE
	case "SUSPENDED":
		state = timev1.ClockDeviceState_CLOCK_DEVICE_STATE_SUSPENDED
	case "REVOKED":
		state = timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED
	}
	return &timev1.ClockDevice{DeviceId: d.ID, TenantId: d.TenantID, SiteId: d.SiteID, ProfileRef: d.ProfileID, Timezone: d.Timezone, State: state, Revision: uint64(max64(d.Revision)), PublicKeyRef: "key:" + d.ID}
}

func max64(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func punchKind(v timev1.PunchEventType) (timesession.PunchKind, error) {
	switch v {
	case timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN:
		return timesession.PunchIn, nil
	case timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT:
		return timesession.PunchOut, nil
	case timev1.PunchEventType_PUNCH_EVENT_TYPE_BREAK_START:
		return timesession.PunchBreakStart, nil
	case timev1.PunchEventType_PUNCH_EVENT_TYPE_BREAK_END:
		return timesession.PunchBreakEnd, nil
	case timev1.PunchEventType_PUNCH_EVENT_TYPE_MEAL_START:
		return timesession.PunchMealStart, nil
	case timev1.PunchEventType_PUNCH_EVENT_TYPE_MEAL_END:
		return timesession.PunchMealEnd, nil
	case timev1.PunchEventType_PUNCH_EVENT_TYPE_JOB_TRANSFER:
		return timesession.PunchTransfer, nil
	default:
		return "", fmt.Errorf("%w: unsupported punch event", clockservice.ErrInvalidRequest)
	}
}

func identMethod(v timev1.IdentificationMethod) (clockdomain.IdentificationMethod, error) {
	switch v {
	case timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN:
		return clockdomain.MethodPIN, nil
	case timev1.IdentificationMethod_IDENTIFICATION_METHOD_BADGE:
		return clockdomain.MethodBadge, nil
	case timev1.IdentificationMethod_IDENTIFICATION_METHOD_QR:
		return clockdomain.MethodQR, nil
	case timev1.IdentificationMethod_IDENTIFICATION_METHOD_SUPERVISOR_OVERRIDE:
		return clockdomain.MethodSupervisorOverride, nil
	default:
		return "", fmt.Errorf("%w: unsupported identification method", clockservice.ErrInvalidRequest)
	}
}

func roster(d clockservice.RosterDelta) *timev1.RosterSnapshot {
	out := &timev1.RosterSnapshot{SnapshotRevision: parseRevision(d.SnapshotRevision), NextCursor: d.NextCursor, PunchPolicyVersion: fmt.Sprint(d.PunchPolicyVersion), MaxOfflineAgeSeconds: int64(d.MaxOfflineAge / time.Second), UrgentRemovedWorkerIds: append([]string(nil), d.UrgentRemovals...)}
	for _, w := range d.Workers {
		out.Workers = append(out.Workers, &timev1.WorkerCredentialVerifier{WorkerId: w.WorkerRef, DisplayName: w.DisplayName, VerifierRef: fmt.Sprintf("v1:%x:%x", w.PINSalt, w.PINHash), Revoked: w.Terminated})
	}
	for _, j := range d.JobCodes {
		out.JobCostCodes = append(out.JobCostCodes, &timev1.JobCostCode{JobId: j.Code, Label: j.Name})
	}
	return out
}
func parseRevision(s string) uint64 { var n uint64; _, _ = fmt.Sscan(s, &n); return n }
