package clockadapter

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BatchFormat selects a supported export layout.
type BatchFormat string

const (
	FormatADP           BatchFormat = "adp"
	FormatPaychex       BatchFormat = "paychex"
	FormatQuickBooksIIF BatchFormat = "quickbooks_iif"
)

// ImportBatch hashes and atomically claims a file before mapping and submitting
// it. A second content-identical file is rejected before canonical ingest.
func ImportAuthenticatedBatch(ctx context.Context, store BatchClaimStore, sink PunchSink, device AuthenticatedDevice, content []byte, format BatchFormat) (*timev1.SubmitPunchesResponse, string, []Unsupported, error) {
	if store == nil || sink == nil || strings.TrimSpace(device.DeviceID) == "" || strings.TrimSpace(device.Tenant) == "" {
		return nil, "", nil, fmt.Errorf("%w: batch dependencies", ErrMalformed)
	}
	sum := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	lease, err := store.BeginBatch(ctx, device, digest)
	if err != nil {
		return nil, digest, nil, err
	}
	tr, err := ParseBatch(device.DeviceID, content, format)
	if err != nil {
		_ = store.ReleaseBatch(ctx, lease)
		return nil, digest, nil, err
	}
	resp, err := sink.SubmitPunches(ctx, device, tr.Request)
	if err != nil {
		_ = store.ReleaseBatch(ctx, lease)
	}
	if err == nil {
		err = store.CompleteBatch(ctx, lease)
	}
	return resp, digest, tr.Unsupported, err
}

// ParseBatch maps ADP and Paychex CSV, plus QuickBooks IIF timer entries.
func ParseBatch(deviceID string, content []byte, format BatchFormat) (Translation, error) {
	if len(content) > defaultMaxBody {
		return Translation{}, fmt.Errorf("%w: body too large", ErrMalformed)
	}
	if format == FormatQuickBooksIIF {
		return Translation{}, fmt.Errorf("%w: quickbooks IIF time rows unsupported", ErrMalformed)
	}
	if format != FormatADP && format != FormatPaychex {
		return Translation{}, fmt.Errorf("%w: unsupported batch format", ErrMalformed)
	}
	r := csv.NewReader(strings.NewReader(string(content)))
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return Translation{}, fmt.Errorf("%w: header: %v", ErrMalformed, err)
	}
	idx := make(map[string]int, len(head))
	for i, h := range head {
		key := normalize(h)
		if _, exists := idx[key]; exists {
			return Translation{}, fmt.Errorf("%w: duplicate header", ErrMalformed)
		}
		idx[key] = i
	}
	worker, stamp, kind := firstIndex(idx, "worker", "employee", "employeeid", "employeeidentifier"), firstIndex(idx, "timestamp", "datetime", "date", "time"), firstIndex(idx, "event", "type", "status", "punchtype")
	seq := firstIndex(idx, "sequence", "seq", "recordid")
	if worker < 0 || stamp < 0 || kind < 0 || seq < 0 {
		return Translation{}, fmt.Errorf("%w: worker/timestamp columns", ErrMalformed)
	}
	var out []*timev1.DevicePunch
	var unsupported []Unsupported
	n := uint64(0)
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return Translation{}, fmt.Errorf("%w: csv: %v", ErrMalformed, e)
		}
		if len(row) <= worker || len(row) <= stamp {
			return Translation{}, fmt.Errorf("%w: short row", ErrMalformed)
		}
		if strings.TrimSpace(row[worker]) == "" {
			return Translation{}, fmt.Errorf("%w: worker", ErrMalformed)
		}
		n++
		if n > 1000 {
			return Translation{}, fmt.Errorf("%w: too many rows", ErrMalformed)
		}
		s := n
		if seq < len(row) {
			parsed, e := strconv.ParseUint(strings.TrimSpace(row[seq]), 10, 64)
			if e != nil || parsed == 0 {
				return Translation{}, fmt.Errorf("%w: sequence", ErrMalformed)
			}
			s = parsed
		}
		t, e := parseVendorTime(row[stamp], time.UTC)
		if e != nil {
			return Translation{}, fmt.Errorf("%w: timestamp", ErrMalformed)
		}
		if kind >= len(row) || strings.TrimSpace(row[kind]) == "" {
			return Translation{}, fmt.Errorf("%w: event", ErrMalformed)
		}
		event := row[kind]
		typ, e := admsEvent(event)
		if e != nil {
			return Translation{}, e
		}
		out = append(out, &timev1.DevicePunch{DeviceSequence: s, EventType: typ, Worker: &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_VerifierRef{VerifierRef: strings.TrimSpace(row[worker])}}, DeviceOccurredAt: timestamppb.New(t)})
	}
	for name := range idx {
		if name != normalize("worker") && name != normalize("employee") && name != normalize("employeeid") && name != normalize("employeeidentifier") && name != normalize("timestamp") && name != normalize("datetime") && name != normalize("date") && name != normalize("time") && name != normalize("event") && name != normalize("type") && name != normalize("status") && name != normalize("punchtype") && name != normalize("sequence") && name != normalize("seq") && name != normalize("recordid") {
			unsupported = append(unsupported, Unsupported{Field: name})
		}
	}
	if len(out) == 0 {
		return Translation{}, fmt.Errorf("%w: no rows", ErrMalformed)
	}
	return Translation{Request: &timev1.SubmitPunchesRequest{DeviceId: deviceID, Punches: out}, Unsupported: unsupported}, nil
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s)
	return s
}
func firstIndex(m map[string]int, names ...string) int {
	for _, n := range names {
		if i, ok := m[normalize(n)]; ok {
			return i
		}
	}
	return -1
}
