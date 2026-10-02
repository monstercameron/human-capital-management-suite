package timeclockapp

import (
	"sort"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// QueuedPunch is one punch persisted on the device until the server has
// given it a non-pending receipt. It carries the credential handle the
// server needs to attribute it (a punch token, or an offline badge/QR
// verifier) and nothing else about the worker.
type QueuedPunch struct {
	Sequence           uint64                           `json:"seq"`
	Event              timev1.PunchEventType            `json:"event"`
	PunchToken         string                           `json:"token,omitempty"`
	VerifierRef        string                           `json:"verifier,omitempty"`
	Method             timev1.IdentificationMethod      `json:"method"`
	OccurredAt         time.Time                        `json:"at"`
	ClockOffsetSeconds int64                            `json:"offset"`
	AttestationAnswers []*timev1.AttestationAnswerInput `json:"attestation_answers,omitempty"`
	TipDeclaration     string                           `json:"tip_declaration,omitempty"`
	JobID              string                           `json:"job_id,omitempty"`
	CostCodeID         string                           `json:"cost_code_id,omitempty"`
}

// Queue is the device's unsent punches in device-sequence order.
type Queue []QueuedPunch

// Sorted returns a copy ordered by device sequence.
func (q Queue) Sorted() Queue {
	out := append(Queue(nil), q...)
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out
}

// LastSequence is the highest sequence in the queue, or zero.
func (q Queue) LastSequence() uint64 {
	var max uint64
	for _, p := range q {
		if p.Sequence > max {
			max = p.Sequence
		}
	}
	return max
}

// Trim drops every punch at or below the server's highest contiguous
// sequence: each of those has a non-pending receipt (accepted, duplicate,
// held or rejected), so resending it could only produce a duplicate.
func (q Queue) Trim(highestContiguous uint64) Queue {
	out := make(Queue, 0, len(q))
	for _, p := range q {
		if p.Sequence > highestContiguous {
			out = append(out, p)
		}
	}
	return out
}

// OldestAge is how long the oldest unsent punch has waited at now.
func (q Queue) OldestAge(now time.Time) time.Duration {
	if len(q) == 0 {
		return 0
	}
	oldest := q[0].OccurredAt
	for _, p := range q[1:] {
		if p.OccurredAt.Before(oldest) {
			oldest = p.OccurredAt
		}
	}
	if age := now.Sub(oldest); age > 0 {
		return age
	}
	return 0
}

// Proto converts the queue to SubmitPunches' wire shape, in order.
func (q Queue) Proto() []*timev1.DevicePunch {
	out := make([]*timev1.DevicePunch, 0, len(q))
	for _, p := range q {
		punch := &timev1.DevicePunch{
			DeviceSequence:           p.Sequence,
			EventType:                p.Event,
			DeviceOccurredAt:         timestamppb.New(p.OccurredAt),
			DeviceClockOffsetSeconds: p.ClockOffsetSeconds,
			IdentificationMethod:     p.Method,
			AttestationAnswers:       p.AttestationAnswers,
			TipDeclaration:           p.TipDeclaration,
			JobId:                    p.JobID,
			CostCodeId:               p.CostCodeID,
		}
		if p.PunchToken != "" {
			punch.Worker = &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_PunchToken{PunchToken: p.PunchToken}}
		} else {
			punch.Worker = &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_VerifierRef{VerifierRef: p.VerifierRef}}
		}
		out = append(out, punch)
	}
	return out
}
