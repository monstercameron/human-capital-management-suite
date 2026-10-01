package timeclockkit

import (
	"context"
	"fmt"
	"time"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Run drives the partner through the complete device lifecycle. The simulator
// does not infer success from requests: every criterion is checked against the
// response returned by the partner endpoint.
func Run(ctx context.Context, client Adapter, o Options) (Result, error) {
	result := Result{Profile: o.ProfileRef, TenantID: o.TenantID, StartedAt: o.Now.UTC()}
	if err := o.Validate(); err != nil {
		return result, err
	}
	if client == nil {
		return result, fmt.Errorf("%w: nil adapter", ErrInvalidOptions)
	}
	check := func(name string, passed bool, detail string) {
		result.Observations = append(result.Observations, Observation{Name: name, Passed: passed, Detail: detail})
	}
	scope := &commonv1.ScopeContext{TenantId: o.TenantID, OrganizationScopeId: o.SiteID, Purpose: "clock-partner-conformance"}

	code, err := client.CreateEnrollmentCode(ctx, &timev1.CreateEnrollmentCodeRequest{
		ScopeContext: scope, SiteId: o.SiteID, ProfileRef: o.ProfileRef,
		Timezone: "UTC", TtlSeconds: int64(o.EnrollmentTTL / time.Second), IdempotencyKey: "timeclockkit-enroll",
	})
	if err != nil || code == nil || code.GetCode() == "" {
		return finish(result, check, "enrollment-code", false, errorDetail(err, "enrollment code was not issued"))
	}
	check("enrollment-code", true, "sandbox enrollment code issued")

	enrolled, err := client.EnrollDevice(ctx, &timev1.EnrollDeviceRequest{
		EnrollmentCode: code.GetCode(), PublicKey: []byte("partner-public-key"), ChallengeSignature: []byte("signature-over-challenge"),
		DeviceModel: "timeclockkit-simulator", AppVersion: "tcclock-018", IdempotencyKey: "timeclockkit-device",
	})
	if err != nil || enrolled == nil || enrolled.GetDevice() == nil {
		return finish(result, check, "enrollment", false, errorDetail(err, "device enrollment was not observed"))
	}
	device := enrolled.GetDevice()
	result.DeviceID = device.GetDeviceId()
	check("tenant-isolation", device.GetTenantId() == o.TenantID && device.GetSiteId() == o.SiteID,
		fmt.Sprintf("device tenant=%q site=%q", device.GetTenantId(), device.GetSiteId()))
	check("enrollment", result.DeviceID != "" && enrolled.GetMachineClientCredentialRef() != "", "device identity and credential reference observed")

	heartbeat, err := client.Heartbeat(ctx, &timev1.HeartbeatRequest{
		DeviceId: result.DeviceID, AppVersion: "tcclock-018", QueueDepth: 2, OldestUnsentAgeSeconds: 60,
		BatteryPercent: 90, PowerState: timev1.DevicePowerState_DEVICE_POWER_STATE_AC_POWERED, MeasuredClockOffsetSeconds: 3600,
	})
	check("heartbeat", err == nil && heartbeat != nil && heartbeat.GetServerTime() != nil && heartbeat.GetServerTime().IsValid(), errorDetail(err, "heartbeat did not return server time"))

	roster, err := client.SyncRoster(ctx, &timev1.SyncRosterRequest{DeviceId: result.DeviceID, MaxResults: 100})
	if err != nil || roster == nil || roster.GetSnapshot() == nil {
		return finish(result, check, "roster-sync", false, errorDetail(err, "roster snapshot was not observed"))
	}
	check("roster-sync", roster.GetSnapshot().GetSnapshotRevision() > 0 && roster.GetSnapshot().GetMaxOfflineAgeSeconds() > 0, "roster revision and offline policy observed")

	identified, err := client.IdentifyWorker(ctx, &timev1.IdentifyWorkerRequest{
		DeviceId: result.DeviceID, Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN,
		Credential: &timev1.IdentifyWorkerRequest_Pin{Pin: "partner-test-pin"},
	})
	if err != nil || identified == nil || identified.GetPunchToken() == "" {
		return finish(result, check, "identify", false, errorDetail(err, "worker punch token was not issued"))
	}
	check("identify", identified.GetStatus() != nil && identified.GetStatus().GetWorkerId() == o.WorkerID, "worker identity was resolved")

	punch := func(sequence uint64, occurred time.Time) *timev1.DevicePunch {
		return &timev1.DevicePunch{
			DeviceSequence: sequence, EventType: timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN,
			Worker:           &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_PunchToken{PunchToken: identified.GetPunchToken()}},
			DeviceOccurredAt: timestamppb.New(occurred), DeviceClockOffsetSeconds: 3600,
			IdentificationMethod: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN,
		}
	}
	first, err := client.SubmitPunches(ctx, &timev1.SubmitPunchesRequest{DeviceId: result.DeviceID, Punches: []*timev1.DevicePunch{punch(1, o.Now.Add(-time.Hour))}})
	firstReceipt := receiptID(first)
	check("offline-replay", err == nil && hasStatus(first, timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED) && first.GetHighestContiguousSequence() == 1, detailResponse(err, first))
	check("clock-drift", err == nil && firstReceipt != "", "device clock offset was carried to the receipt")

	gap, err := client.SubmitPunches(ctx, &timev1.SubmitPunchesRequest{DeviceId: result.DeviceID, Punches: []*timev1.DevicePunch{punch(3, o.Now.Add(-time.Minute))}})
	check("sequence-gap", err == nil && hasRejection(gap, timev1.PunchRejectionReason_PUNCH_REJECTION_REASON_SEQUENCE_GAP) && gap.GetHighestContiguousSequence() == 1, detailResponse(err, gap))
	duplicate, err := client.SubmitPunches(ctx, &timev1.SubmitPunchesRequest{DeviceId: result.DeviceID, Punches: []*timev1.DevicePunch{punch(1, o.Now.Add(-time.Hour))}})
	check("duplicate-replay", err == nil && hasStatus(duplicate, timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE) && originalReceiptID(duplicate) == firstReceipt, detailResponse(err, duplicate))

	if webhookErr := o.Webhook.Receive(ctx, WebhookEvent{TenantID: o.TenantID, Type: "clock.punch.accepted", ReceiptID: firstReceipt}); webhookErr != nil {
		check("webhook-receipt", false, webhookErr.Error())
	} else {
		check("webhook-receipt", firstReceipt != "", "partner webhook receipt observed")
	}
	revoked, err := client.RevokeDevice(ctx, &timev1.RevokeDeviceRequest{ScopeContext: scope, DeviceId: result.DeviceID, ExpectedRevision: device.GetRevision(), Reason: "TCLOCK-018 certification", IdempotencyKey: "timeclockkit-revoke"})
	check("revocation", err == nil && revoked != nil && revoked.GetDevice() != nil && revoked.GetDevice().GetState() == timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED, errorDetail(err, "device revocation was not observed"))
	return finish(result, check, "complete", true, "all device partner criteria observed")
}

func finish(result Result, check func(string, bool, string), name string, passed bool, detail string) (Result, error) {
	if name != "complete" {
		check(name, passed, detail)
	}
	result.FinishedAt = result.StartedAt.Add(time.Nanosecond)
	result.Passed = len(result.Observations) == len(mandatoryObservations)
	seen := make(map[string]bool, len(result.Observations))
	for _, observation := range result.Observations {
		if !observation.Passed || seen[observation.Name] {
			result.Passed = false
		}
		seen[observation.Name] = true
	}
	for _, name := range mandatoryObservations {
		if !seen[name] {
			result.Passed = false
		}
	}
	if !result.Passed {
		return result, fmt.Errorf("%w: %s", ErrConformance, detail)
	}
	result.provenance = resultDigest(result)
	return result, nil
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

func receiptID(response *timev1.SubmitPunchesResponse) string {
	if response != nil && len(response.GetReceipts()) > 0 {
		return response.GetReceipts()[0].GetReceiptId()
	}
	return ""
}

func originalReceiptID(response *timev1.SubmitPunchesResponse) string {
	if response != nil && len(response.GetReceipts()) > 0 {
		return response.GetReceipts()[0].GetOriginalReceiptId()
	}
	return ""
}

func hasStatus(response *timev1.SubmitPunchesResponse, want timev1.PunchReceiptStatus) bool {
	return response != nil && len(response.GetReceipts()) > 0 && response.GetReceipts()[0].GetStatus() == want
}

func hasRejection(response *timev1.SubmitPunchesResponse, want timev1.PunchRejectionReason) bool {
	return response != nil && len(response.GetReceipts()) > 0 && response.GetReceipts()[0].GetRejectionReason() == want
}
