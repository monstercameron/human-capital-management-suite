package clockadapter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultMaxBody = 1 << 20

// ADMSHandler is the device-push receiver for the ZKTeco ADMS-style protocol.
type ADMSHandler struct {
	Auth         DeviceAuthenticator
	Sink         PunchSink
	MaxBodyBytes int64
	Location     *time.Location
}

// HTTPHandler returns an HTTP handler for /iclock/cdata-compatible requests.
func (h ADMSHandler) HTTPHandler() http.Handler { return http.HandlerFunc(h.serveHTTP) }

func (h ADMSHandler) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	max := h.MaxBodyBytes
	if max <= 0 {
		max = defaultMaxBody
	}
	readLimit := max
	if max < int64(^uint(0)>>1) {
		readLimit = max + 1
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, readLimit))
	if err != nil {
		http.Error(w, "read failed", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > max {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	cred := r.Header.Get("Authorization")
	if cred == "" {
		cred = r.Header.Get("X-Device-Credential")
	}
	ctx := ctxOrBackground(r)
	if h.Auth == nil || h.Sink == nil {
		http.Error(w, ErrUnauthenticated.Error(), http.StatusUnauthorized)
		return
	}
	device, err := h.Auth.Authenticate(ctx, cred)
	if err == nil {
		err = validateDevice(device)
	}
	var tr Translation
	if err == nil {
		tr, err = ParseADMS(device.DeviceID, body, h.Location)
	}
	var result *timev1.SubmitPunchesResponse
	if err == nil {
		result, err = h.Sink.SubmitPunches(ctx, device, tr.Request)
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrUnauthenticated) {
			status = http.StatusUnauthorized
		}
		http.Error(w, err.Error(), status)
		return
	}
	if len(tr.Unsupported) > 0 || !responseDurable(result, len(tr.Request.GetPunches())) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, responseMessage(result, tr.Unsupported))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "OK\n")
}

func ctxOrBackground(r *http.Request) context.Context {
	if r.Context() != nil {
		return r.Context()
	}
	return context.Background()
}

// Handle authenticates first, parses a bounded ADMS attendance body, and
// delegates the canonical batch to Sink. The body serial is never identity.
func (h ADMSHandler) Handle(ctx context.Context, credential, bodySerial string, body []byte) (*timev1.SubmitPunchesResponse, error) {
	if h.Auth == nil || h.Sink == nil {
		return nil, fmt.Errorf("%w: adapter dependencies", ErrUnauthenticated)
	}
	device, err := h.Auth.Authenticate(ctx, credential)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	if err := validateDevice(device); err != nil {
		return nil, err
	}
	tr, err := ParseADMS(device.DeviceID, body, h.Location)
	if err != nil {
		return nil, err
	}
	return h.Sink.SubmitPunches(ctx, device, tr.Request)
}

// ParseADMS parses the documented tab-delimited ADMS ATTLOG rows and common
// key/value form rows. Unknown columns are reported in Translation.
func ParseADMS(deviceID string, body []byte, loc *time.Location) (Translation, error) {
	if strings.TrimSpace(deviceID) == "" {
		return Translation{}, fmt.Errorf("%w: device id", ErrMalformed)
	}
	if len(body) > defaultMaxBody {
		return Translation{}, fmt.Errorf("%w: body too large", ErrMalformed)
	}
	if loc == nil {
		loc = time.UTC
	}
	text := string(body)
	if strings.TrimSpace(text) == "" {
		return Translation{}, fmt.Errorf("%w: empty body", ErrMalformed)
	}
	var punches []*timev1.DevicePunch
	var unsupported []Unsupported
	s := bufio.NewScanner(strings.NewReader(text))
	s.Buffer(make([]byte, 256), defaultMaxBody)
	count := 0
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitADMS(line)
		if len(fields) < 7 {
			return Translation{}, fmt.Errorf("%w: attendance row", ErrMalformed)
		}
		pin, stamp, status := fields[0], fields[1], fields[2]
		if strings.TrimSpace(pin) == "" {
			return Translation{}, fmt.Errorf("%w: worker credential", ErrMalformed)
		}
		if len(fields) >= 4 {
			if fields[3] != "" {
				unsupported = append(unsupported, Unsupported{"verify", fields[3]})
			}
		}
		if len(fields) >= 5 && fields[4] != "" {
			unsupported = append(unsupported, Unsupported{"work_code", fields[4]})
		}
		if len(fields) >= 6 && fields[5] != "" {
			unsupported = append(unsupported, Unsupported{"reserved", fields[5]})
		}
		seq, e := strconv.ParseUint(strings.TrimSpace(fields[6]), 10, 64)
		if e != nil || seq == 0 {
			return Translation{}, fmt.Errorf("%w: explicit sequence required", ErrMissingSequence)
		}
		count++
		if count > 1000 {
			return Translation{}, fmt.Errorf("%w: too many rows", ErrMalformed)
		}
		at, err := parseVendorTime(stamp, loc)
		if err != nil {
			return Translation{}, fmt.Errorf("%w: occurred_at: %v", ErrMalformed, err)
		}
		if err := timestamppb.New(at).CheckValid(); err != nil {
			return Translation{}, fmt.Errorf("%w: timestamp: %v", ErrMalformed, err)
		}
		kind, err := admsEvent(status)
		if err != nil {
			return Translation{}, err
		}
		punches = append(punches, &timev1.DevicePunch{DeviceSequence: seq, EventType: kind, Worker: &timev1.WorkerCredentialRef{Ref: &timev1.WorkerCredentialRef_VerifierRef{VerifierRef: pin}}, DeviceOccurredAt: timestamppb.New(at), IdentificationMethod: timev1.IdentificationMethod_IDENTIFICATION_METHOD_BADGE})
	}
	if err := s.Err(); err != nil {
		return Translation{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if len(punches) == 0 {
		return Translation{}, fmt.Errorf("%w: no attendance rows", ErrMalformed)
	}
	return Translation{Request: &timev1.SubmitPunchesRequest{DeviceId: deviceID, Punches: punches}, Unsupported: unsupported}, nil
}

func splitADMS(line string) []string {
	if strings.Contains(line, "\t") {
		return strings.Split(line, "\t")
	}
	if strings.Contains(line, "=") {
		q, e := url.ParseQuery(line)
		if e == nil {
			return []string{q.Get("PIN"), q.Get("Timestamp"), q.Get("Status"), q.Get("Verify"), q.Get("WorkCode"), q.Get("Reserved"), q.Get("Seq")}
		}
	}
	return strings.Split(line, ",")
}
func parseVendorTime(v string, loc *time.Location) (time.Time, error) {
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", time.RFC3339}
	for _, l := range layouts {
		if t, e := time.ParseInLocation(l, strings.TrimSpace(v), loc); e == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time %q", v)
}
func admsEvent(v string) (timev1.PunchEventType, error) {
	switch strings.TrimSpace(v) {
	case "0", "IN", "CHECK_IN":
		return timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN, nil
	case "1", "OUT", "CHECK_OUT":
		return timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT, nil
	case "2", "BREAK_START":
		return timev1.PunchEventType_PUNCH_EVENT_TYPE_BREAK_START, nil
	case "3", "BREAK_END":
		return timev1.PunchEventType_PUNCH_EVENT_TYPE_BREAK_END, nil
	default:
		return 0, fmt.Errorf("%w: status", ErrMalformed)
	}
}

func validateDevice(device AuthenticatedDevice) error {
	if strings.TrimSpace(device.DeviceID) == "" || strings.TrimSpace(device.Tenant) == "" {
		return fmt.Errorf("%w: enrolled identity", ErrUnauthenticated)
	}
	return nil
}

func responseDurable(resp *timev1.SubmitPunchesResponse, count int) bool {
	if resp == nil || int(resp.GetHighestContiguousSequence()) < count {
		return false
	}
	for _, receipt := range resp.GetReceipts() {
		if receipt.GetStatus() != timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED && receipt.GetStatus() != timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE {
			return false
		}
	}
	return true
}

func responseMessage(resp *timev1.SubmitPunchesResponse, unsupported []Unsupported) string {
	if resp == nil {
		return "batch not acknowledged\n"
	}
	return fmt.Sprintf("batch not fully acknowledged: contiguous=%d receipts=%d unsupported=%d\n", resp.GetHighestContiguousSequence(), len(resp.GetReceipts()), len(unsupported))
}
