package clockadapter

import (
	"context"
	"fmt"
	"strings"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

// TranslateRoster converts the canonical verifier-only roster into the common
// subset supported by legacy hardware. Secrets, attestations, shifts, tips,
// and localized strings are reported rather than guessed or discarded.
func TranslateRoster(deviceID string, snapshot *timev1.RosterSnapshot) (RosterTranslation, error) {
	if strings.TrimSpace(deviceID) == "" || snapshot == nil {
		return RosterTranslation{}, fmt.Errorf("%w: roster", ErrMalformed)
	}
	r := RosterTranslation{DeviceID: deviceID}
	for _, w := range snapshot.GetWorkers() {
		if w == nil {
			continue
		}
		if w.GetRevoked() {
			r.Unsupported = append(r.Unsupported, Unsupported{"worker.revoked", w.GetWorkerId()})
			continue
		}
		r.Workers = append(r.Workers, VendorWorker{WorkerID: w.GetWorkerId(), DisplayName: w.GetDisplayName(), VerifierRef: w.GetVerifierRef(), Method: w.GetMethod()})
	}
	for _, j := range snapshot.GetJobCostCodes() {
		if j != nil {
			r.Jobs = append(r.Jobs, VendorJob{JobID: j.GetJobId(), CostCodeID: j.GetCostCodeId(), Label: j.GetLabel()})
		}
	}
	if len(snapshot.GetPublishedShifts()) > 0 {
		r.Unsupported = append(r.Unsupported, Unsupported{"published_shifts", "device protocol has no shift field"})
	}
	if len(snapshot.GetAttestationQuestions()) > 0 {
		r.Unsupported = append(r.Unsupported, Unsupported{"attestation_questions", "device protocol has no attestation field"})
	}
	if len(snapshot.GetTipRules()) > 0 {
		r.Unsupported = append(r.Unsupported, Unsupported{"tip_rules", "device protocol has no tip field"})
	}
	if len(snapshot.GetLocalizedStrings()) > 0 {
		r.Unsupported = append(r.Unsupported, Unsupported{"localized_strings", "device protocol has no localization field"})
	}
	if snapshot.GetPunchPolicyVersion() != "" {
		r.Unsupported = append(r.Unsupported, Unsupported{"punch_policy_version", snapshot.GetPunchPolicyVersion()})
	}
	return r, nil
}

// SyncRoster delegates roster retrieval to the canonical service before
// translating it, preserving cursor and revision semantics in that service.
func SyncRoster(ctx context.Context, sink RosterSink, deviceID, cursor string, maxResults int32) (RosterTranslation, error) {
	if sink == nil {
		return RosterTranslation{}, fmt.Errorf("%w: roster sink", ErrMalformed)
	}
	resp, err := sink.SyncRoster(ctx, &timev1.SyncRosterRequest{DeviceId: deviceID, Cursor: cursor, MaxResults: maxResults})
	if err != nil {
		return RosterTranslation{}, err
	}
	return TranslateRoster(deviceID, resp.GetSnapshot())
}
