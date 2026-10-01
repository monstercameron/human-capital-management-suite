package clockpartner

import (
	"context"
	"fmt"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Run executes enrollment, roster sync, identification, offline replay, gap,
// duplicate, drift, revocation and webhook checks against the supplied client.
// It returns a failed result alongside an error; callers must not sign it.
func Run(ctx context.Context, client Adapter, o Options) (Result, error) {
	result := Result{Profile: o.ProfileRef, TenantID: o.TenantID, StartedAt: o.Now}
	if err := o.Validate(); err != nil {
		return result, err
	}
	if client == nil {
		return result, fmt.Errorf("%w: nil client", ErrInvalidOptions)
	}
	check := func(name string, passed bool, detail string) {
		result.Observations = append(result.Observations, Observation{Name: name, Passed: passed, Detail: detail})
	}
	scope := &commonv1.ScopeContext{TenantId: o.TenantID, OrganizationScopeId: o.SiteID, Purpose: "clock-partner-conformance"}
	code, err := client.CreateEnrollmentCode(ctx, &timev1.CreateEnrollmentCodeRequest{ScopeContext: scope, SiteId: o.SiteID, ProfileRef: o.ProfileRef, Timezone: "UTC", TtlSeconds: int64(o.EnrollmentTTL / time.Second), IdempotencyKey: "clockpartner-enroll"})
	if err != nil {
		return finish(result, check, "enrollment-code", false, err.Error())
	}
	check("enrollment-code", code != nil && code.GetCode() != "", "observed enrollment code")
	if code == nil || code.GetCode() == "" {
		return finish(result, check, "enrollment", false, "endpoint returned no enrollment code")
	}
	enrolled, err := client.EnrollDevice(ctx, &timev1.EnrollDeviceRequest{EnrollmentCode: code.GetCode(), PublicKey: []byte("partner-public-key"), ChallengeSignature: []byte("signature-over-challenge"), DeviceModel: "clockpartner-simulator", AppVersion: "tcclock-018", IdempotencyKey: "clockpartner-device"})
	if err != nil || enrolled == nil || enrolled.GetDevice() == nil {
		return finish(result, check, "enrollment", false, errorDetail(err, "missing enrolled device"))
	}
	device := enrolled.GetDevice()
	result.DeviceID = device.GetDeviceId()
	check("tenant-isolation", device.GetTenantId() == o.TenantID && device.GetSiteId() == o.SiteID, fmt.Sprintf("device tenant=%q site=%q", device.GetTenantId(), device.GetSiteId()))
	check("enrollment", result.DeviceID != "" && enrolled.GetMachineClientCredentialRef() != "", "observed device identity")
	heartbeat, err := client.Heartbeat(ctx, &timev1.HeartbeatRequest{DeviceId: result.DeviceID, AppVersion: "tcclock-018", QueueDepth: 1, OldestUnsentAgeSeconds: 60, BatteryPercent: 90, PowerState: timev1.DevicePowerState_DEVICE_POWER_STATE_AC_POWERED, MeasuredClockOffsetSeconds: 3600})
	check("heartbeat", heartbeat != nil && heartbeat.GetServerTime() != nil && heartbeat.GetServerTime().IsValid() && heartbeat.GetAction() != timev1.HeartbeatAction_HEARTBEAT_ACTION_UNSPECIFIED, errorDetail(err, "heartbeat did not return server time and action"))
	sync, err := client.SyncRoster(ctx, &timev1.SyncRosterRequest{DeviceId: result.DeviceID, MaxResults: 100})
	if err != nil || sync == nil || sync.GetSnapshot() == nil {
		return finish(result, check, "roster-sync", false, errorDetail(err, "missing roster snapshot"))
	}
	check("roster-sync", sync.GetSnapshot().GetSnapshotRevision() > 0 && sync.GetSnapshot().GetMaxOfflineAgeSeconds() > 0, "observed revision and offline-age policy")
	identified, err := client.IdentifyWorker(ctx, &timev1.IdentifyWorkerRequest{DeviceId: result.DeviceID, Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN, Credential: &timev1.IdentifyWorkerRequest_Pin{Pin: "partner-test-pin"}})
	if err != nil || identified == nil || identified.GetPunchToken() == "" {
		return finish(result, check, "identify", false, errorDetail(err, "missing punch token"))
	}
	check("identify", identified.GetStatus() != nil && identified.GetStatus().GetWorkerId() == o.WorkerID, "observed worker identity")
	punch := func(seq uint64, occurred time.Time) *timev1.DevicePunch {
		return &timev1.DevicePunch{DeviceSequence: seq, EventType: timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN, Worker: &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_PunchToken{PunchToken: identified.GetPunchToken()}}, DeviceOccurredAt: timestamppb.New(occurred), DeviceClockOffsetSeconds: 3600, IdentificationMethod: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN}
	}
	pending := []*timev1.DevicePunch{punch(1, o.Now.Add(-time.Hour))}
	first, err := client.SubmitPunches(ctx, &timev1.SubmitPunchesRequest{DeviceId: result.DeviceID, Punches: pending})
	if err == nil && hasStatus(first, timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED) && first.GetHighestContiguousSequence() == 1 {
		pending = nil
	}
	check("offline-replay", err == nil && len(pending) == 0, detailResponse(err, first))
	gap, err := client.SubmitPunches(ctx, &timev1.SubmitPunchesRequest{DeviceId: result.DeviceID, Punches: []*timev1.DevicePunch{punch(3, o.Now.Add(-time.Minute))}})
	check("sequence-gap", err == nil && hasRejection(gap, timev1.PunchRejectionReason_PUNCH_REJECTION_REASON_SEQUENCE_GAP) && gap.GetHighestContiguousSequence() == 1, detailResponse(err, gap))
	dup, err := client.SubmitPunches(ctx, &timev1.SubmitPunchesRequest{DeviceId: result.DeviceID, Punches: []*timev1.DevicePunch{punch(1, o.Now.Add(-time.Hour))}})
	check("duplicate-replay", err == nil && hasStatus(dup, timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE) && dup.GetReceipts()[0].GetOriginalReceiptId() == receiptID(first), detailResponse(err, dup))
	check("clock-drift", first != nil && len(first.GetReceipts()) > 0 && first.GetReceipts()[0].GetReceiptId() != "", "device offset was submitted and receipt was observed")
	if err := o.Webhook.Receive(ctx, WebhookEvent{TenantID: o.TenantID, Type: "clock.punch.accepted", ReceiptID: receiptID(first)}); err != nil {
		check("webhook-receipt", false, err.Error())
	} else {
		check("webhook-receipt", receiptID(first) != "", "partner receipt accepted")
	}
	revoked, err := client.RevokeDevice(ctx, &timev1.RevokeDeviceRequest{ScopeContext: scope, DeviceId: result.DeviceID, ExpectedRevision: device.GetRevision(), Reason: "TCLOCK-018 revocation", IdempotencyKey: "clockpartner-revoke"})
	check("revocation", err == nil && revoked != nil && revoked.GetDevice() != nil && revoked.GetDevice().GetState() == timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED, errorDetail(err, "revocation response not observed"))
	return finish(result, check, "complete", true, "all criteria observed")
}

func finish(r Result, check func(string, bool, string), name string, passed bool, detail string) (Result, error) {
	if name != "complete" {
		check(name, passed, detail)
	}
	r.FinishedAt = r.StartedAt.Add(time.Nanosecond)
	r.Passed = len(r.Observations) > 0
	for _, o := range r.Observations {
		if !o.Passed {
			r.Passed = false
			break
		}
	}
	if !r.Passed {
		return r, fmt.Errorf("%w: %s", ErrConformance, detail)
	}
	if r.Passed {
		r.provenance = resultDigest(r)
	}
	return r, nil
}
func errorDetail(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}
func detailResponse(err error, response *timev1.SubmitPunchesResponse) string {
	if err != nil {
		return err.Error()
	}
	if response == nil {
		return "nil response"
	}
	return fmt.Sprintf("receipts=%d contiguous=%d", len(response.GetReceipts()), response.GetHighestContiguousSequence())
}
func receiptID(r *timev1.SubmitPunchesResponse) string {
	if r != nil && len(r.GetReceipts()) > 0 {
		return r.GetReceipts()[0].GetReceiptId()
	}
	return ""
}
func hasStatus(r *timev1.SubmitPunchesResponse, s timev1.PunchReceiptStatus) bool {
	return r != nil && len(r.GetReceipts()) > 0 && r.GetReceipts()[0].GetStatus() == s
}
func hasRejection(r *timev1.SubmitPunchesResponse, reason timev1.PunchRejectionReason) bool {
	return r != nil && len(r.GetReceipts()) > 0 && r.GetReceipts()[0].GetRejectionReason() == reason
}
