package clockadapter

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RESTPunch is the deliberately small provider-neutral shape used by vendor
// cloud clients after authentication and HTTP concerns are handled upstream.
type RESTPunch struct {
	Sequence   uint64 `json:"sequence"`
	WorkerID   string `json:"worker_id"`
	Event      string `json:"event"`
	OccurredAt string `json:"occurred_at"`
	JobID      string `json:"job_id,omitempty"`
	CostCodeID string `json:"cost_code_id,omitempty"`
}

// MapRESTPunches translates a vendor REST response to the canonical batch.
func MapRESTPunches(deviceID string, payload []byte) (Translation, error) {
	var rows []RESTPunch
	if err := json.Unmarshal(payload, &rows); err != nil {
		return Translation{}, fmt.Errorf("%w: json: %v", ErrMalformed, err)
	}
	out := make([]*timev1.DevicePunch, 0, len(rows))
	var unsupported []Unsupported
	for _, row := range rows {
		if row.Sequence == 0 || strings.TrimSpace(row.WorkerID) == "" {
			return Translation{}, fmt.Errorf("%w: REST identity/sequence", ErrMalformed)
		}
		t, e := parseVendorTime(row.OccurredAt, time.UTC)
		if e != nil {
			return Translation{}, fmt.Errorf("%w: REST timestamp", ErrMalformed)
		}
		typ, e := admsEvent(row.Event)
		if e != nil {
			return Translation{}, e
		}
		out = append(out, &timev1.DevicePunch{DeviceSequence: row.Sequence, EventType: typ, Worker: &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_VerifierRef{VerifierRef: row.WorkerID}}, DeviceOccurredAt: timestamppb.New(t), JobId: row.JobID, CostCodeId: row.CostCodeID})
	}
	if len(out) == 0 {
		return Translation{}, fmt.Errorf("%w: empty REST response", ErrMalformed)
	}
	return Translation{Request: &timev1.SubmitPunchesRequest{DeviceId: deviceID, Punches: out}, Unsupported: unsupported}, nil
}
