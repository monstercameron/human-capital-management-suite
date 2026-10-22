package timeclockapp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type tclockRoundTripDoer struct {
	t        *testing.T
	method   string
	request  proto.Message
	response proto.Message
}

func (d tclockRoundTripDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()
	wantURL := "https://kiosk.example" + ServicePath + d.method
	if req.Method != http.MethodPost || req.URL.String() != wantURL {
		d.t.Fatalf("request = %s %s, want POST %s", req.Method, req.URL, wantURL)
	}
	if req.Header.Get("Content-Type") != "application/json" || req.Header.Get("Accept") != "application/json" {
		d.t.Fatalf("request headers = %#v", req.Header)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		d.t.Fatalf("read request: %v", err)
	}
	gotRequest := proto.Clone(d.request)
	if err := protojson.Unmarshal(body, gotRequest); err != nil {
		d.t.Fatalf("decode request: %v", err)
	}
	if !proto.Equal(gotRequest, d.request) {
		d.t.Fatalf("request proto = %v, want %v", gotRequest, d.request)
	}
	response, err := protojson.Marshal(d.response)
	if err != nil {
		d.t.Fatalf("encode response fixture: %v", err)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(response))}, nil
}

func TestTodo_TCLOCK_016_PublicHTTPMethodsRoundTrip(t *testing.T) {
	workerStatus := &timev1.WorkerPunchStatus{WorkerId: "worker-7", DisplayName: "Ava Stone", ActiveShiftId: "shift-4"}
	roster := &timev1.RosterSnapshot{SnapshotRevision: 12, NextCursor: "cursor-next", MaxOfflineAgeSeconds: 900}
	tests := []struct {
		name     string
		method   string
		request  proto.Message
		response proto.Message
		call     func(*HTTPClient) (proto.Message, error)
	}{
		{
			name:     "enrollment",
			method:   MethodEnrollDevice,
			request:  &timev1.EnrollDeviceRequest{EnrollmentCode: "one-use-code", DeviceModel: "managed-tablet"},
			response: &timev1.EnrollDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "device-7", SiteId: "site-2", Timezone: "America/New_York"}, MachineClientCredentialRef: "machine-ref"},
			call: func(c *HTTPClient) (proto.Message, error) {
				return c.EnrollDevice(context.Background(), &timev1.EnrollDeviceRequest{EnrollmentCode: "one-use-code", DeviceModel: "managed-tablet"})
			},
		},
		{
			name:     "worker identification",
			method:   MethodIdentifyWorker,
			request:  &timev1.IdentifyWorkerRequest{DeviceId: "device-7", Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN, Credential: &timev1.IdentifyWorkerRequest_Pin{Pin: "2481"}},
			response: &timev1.IdentifyWorkerResponse{PunchToken: "short-lived", Status: workerStatus},
			call: func(c *HTTPClient) (proto.Message, error) {
				return c.IdentifyWorker(context.Background(), &timev1.IdentifyWorkerRequest{DeviceId: "device-7", Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN, Credential: &timev1.IdentifyWorkerRequest_Pin{Pin: "2481"}})
			},
		},
		{
			name:     "punch submission",
			method:   MethodSubmitPunches,
			request:  &timev1.SubmitPunchesRequest{DeviceId: "device-7", Punches: []*timev1.DevicePunch{{DeviceSequence: 8, EventType: timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN}}},
			response: &timev1.SubmitPunchesResponse{HighestContiguousSequence: 8, Receipts: []*timev1.PunchReceipt{{DeviceSequence: 8, Status: timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED}}},
			call: func(c *HTTPClient) (proto.Message, error) {
				return c.SubmitPunches(context.Background(), &timev1.SubmitPunchesRequest{DeviceId: "device-7", Punches: []*timev1.DevicePunch{{DeviceSequence: 8, EventType: timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN}}})
			},
		},
		{
			name:     "heartbeat",
			method:   MethodHeartbeat,
			request:  &timev1.HeartbeatRequest{DeviceId: "device-7", QueueDepth: 2, BatteryPercent: 71},
			response: &timev1.HeartbeatResponse{MinimumSupportedAppVersion: "1"},
			call: func(c *HTTPClient) (proto.Message, error) {
				return c.Heartbeat(context.Background(), &timev1.HeartbeatRequest{DeviceId: "device-7", QueueDepth: 2, BatteryPercent: 71})
			},
		},
		{
			name:     "roster sync",
			method:   MethodSyncRoster,
			request:  &timev1.SyncRosterRequest{DeviceId: "device-7", Cursor: "cursor-old", MaxResults: 500},
			response: &timev1.SyncRosterResponse{Snapshot: roster},
			call: func(c *HTTPClient) (proto.Message, error) {
				return c.SyncRoster(context.Background(), &timev1.SyncRosterRequest{DeviceId: "device-7", Cursor: "cursor-old", MaxResults: 500})
			},
		},
		{
			name:     "worker status refresh",
			method:   MethodWorkerStatus,
			request:  &timev1.GetWorkerStatusRequest{DeviceId: "device-7", PunchToken: "short-lived"},
			response: &timev1.GetWorkerStatusResponse{Status: workerStatus},
			call: func(c *HTTPClient) (proto.Message, error) {
				return c.GetWorkerStatus(context.Background(), &timev1.GetWorkerStatusRequest{DeviceId: "device-7", PunchToken: "short-lived"})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewHTTPClient("https://kiosk.example/", tclockRoundTripDoer{t: t, method: tt.method, request: tt.request, response: tt.response})
			got, err := tt.call(client)
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if !proto.Equal(got, tt.response) {
				t.Fatalf("response = %v, want %v", got, tt.response)
			}
		})
	}
}

type tclockOptionalAPI struct {
	tclockAPI
	rosterResponse   *timev1.SyncRosterResponse
	statusResponse   *timev1.GetWorkerStatusResponse
	heartbeat        *timev1.HeartbeatResponse
	rosterErr        error
	statusErr        error
	heartbeatErr     error
	rosterRequest    *timev1.SyncRosterRequest
	statusRequest    *timev1.GetWorkerStatusRequest
	heartbeatRequest *timev1.HeartbeatRequest
}

func (a *tclockOptionalAPI) SyncRoster(_ context.Context, req *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	a.rosterRequest = proto.Clone(req).(*timev1.SyncRosterRequest)
	return a.rosterResponse, a.rosterErr
}

func (a *tclockOptionalAPI) GetWorkerStatus(_ context.Context, req *timev1.GetWorkerStatusRequest) (*timev1.GetWorkerStatusResponse, error) {
	a.statusRequest = proto.Clone(req).(*timev1.GetWorkerStatusRequest)
	return a.statusResponse, a.statusErr
}

func (a *tclockOptionalAPI) Heartbeat(_ context.Context, req *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	a.heartbeatRequest = proto.Clone(req).(*timev1.HeartbeatRequest)
	return a.heartbeat, a.heartbeatErr
}

func TestTodo_TCLOCK_016_OptionalAPIsAndProjection(t *testing.T) {
	now := time.Date(2026, 10, 21, 15, 4, 5, 0, time.UTC)
	status := &timev1.WorkerPunchStatus{WorkerId: "worker-7", DisplayName: "Ava Stone", ActiveShiftId: "shift-4"}
	roster := &timev1.RosterSnapshot{SnapshotRevision: 12, NextCursor: "cursor-next", MaxOfflineAgeSeconds: 900}
	api := &tclockOptionalAPI{rosterResponse: &timev1.SyncRosterResponse{Snapshot: roster}, statusResponse: &timev1.GetWorkerStatusResponse{Status: status}, heartbeat: &timev1.HeartbeatResponse{MinimumSupportedAppVersion: "1"}}
	model := NewKiosk(api, &tclockStorage{values: map[string]string{}}, func() time.Time { return now })
	model.device = &DeviceRecord{DeviceID: "device-7", SiteID: "site-2", Timezone: "America/New_York", KeySeed: []byte("private seed")}
	model.worker, model.token, model.screen = &timev1.WorkerPunchStatus{WorkerId: "old"}, "secret-token", ScreenConfirm
	model.online, model.busy = false, true
	model.err = errors.New("recoverable")
	model.receipt = Receipt{Sequence: 8, Event: timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN, Queued: true}
	model.queue = Queue{{Sequence: 8, OccurredAt: now.Add(-30 * time.Second)}}
	model.lastActivity = now.Add(-time.Minute)

	if model.Locale() != LocaleEnUS || model.Online() || !model.Busy() || model.Error() == nil || model.LastReceipt().Sequence != 8 {
		t.Fatal("kiosk read projection did not expose current state")
	}
	model.SetLocale(LocaleAr)
	if model.Locale() != LocaleAr || model.lastActivity != now {
		t.Fatalf("Arabic locale/activity = %q, %v", model.Locale(), model.lastActivity)
	}
	model.SetLocale(Locale("fr-FR"))
	if model.Locale() != LocaleAr {
		t.Fatalf("unsupported locale replaced current locale: %q", model.Locale())
	}
	projection := model.Snapshot()
	if projection.Screen != ScreenConfirm || projection.Locale != LocaleAr || projection.Direction != "rtl" || projection.SiteID != "site-2" || projection.Timezone != "America/New_York" || projection.QueueDepth != 1 || projection.Worker == nil || projection.Worker.WorkerId != "old" || projection.Receipt.Sequence != 8 || projection.Error == nil {
		t.Fatalf("snapshot = %#v", projection)
	}
	projection.Worker.DisplayName = "changed outside model"
	if model.worker.DisplayName == "changed outside model" {
		t.Fatal("snapshot exposed mutable worker state")
	}
	if got := model.Device(); got == nil || got.DeviceID != "device-7" || !bytes.Equal(got.KeySeed, model.device.KeySeed) {
		t.Fatalf("device copy = %#v", got)
	} else {
		got.KeySeed[0] = 'X'
		if model.device.KeySeed[0] == 'X' {
			t.Fatal("device copy exposed private seed storage")
		}
	}

	gotRoster, err := model.SyncRoster(context.Background(), "cursor-old")
	if err != nil || gotRoster != roster || model.rosterAt != now || !model.Online() {
		t.Fatalf("roster = %p, %v, at=%v, online=%v", gotRoster, err, model.rosterAt, model.Online())
	}
	wantRosterRequest := &timev1.SyncRosterRequest{DeviceId: "device-7", Cursor: "cursor-old", MaxResults: 500}
	if !proto.Equal(api.rosterRequest, wantRosterRequest) {
		t.Fatalf("roster request = %v, want %v", api.rosterRequest, wantRosterRequest)
	}
	if _, err := model.SyncRoster(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	api.rosterErr = ErrUnreachable
	if _, err := model.SyncRoster(context.Background(), "offline"); !errors.Is(err, ErrUnreachable) || model.Online() || model.roster != roster {
		t.Fatalf("failed roster refresh err=%v online=%v roster=%p", err, model.Online(), model.roster)
	}
	api.rosterErr, api.rosterResponse = nil, &timev1.SyncRosterResponse{}
	if _, err := model.SyncRoster(context.Background(), "empty"); err == nil {
		t.Fatal("empty roster snapshot accepted")
	}

	if err := model.RefreshWorkerStatus(context.Background()); err != nil || model.Worker().WorkerId != "worker-7" {
		t.Fatalf("status refresh = %v, worker=%v", err, model.Worker())
	}
	wantStatusRequest := &timev1.GetWorkerStatusRequest{DeviceId: "device-7", PunchToken: "secret-token"}
	if !proto.Equal(api.statusRequest, wantStatusRequest) {
		t.Fatalf("status request = %v, want %v", api.statusRequest, wantStatusRequest)
	}
	previousWorker := model.Worker()
	api.statusErr = ErrUnreachable
	if err := model.RefreshWorkerStatus(context.Background()); !errors.Is(err, ErrUnreachable) || model.Worker() != previousWorker {
		t.Fatalf("failed status refresh err=%v worker=%v", err, model.Worker())
	}
	api.statusErr, api.statusResponse = nil, &timev1.GetWorkerStatusResponse{}
	if err := model.RefreshWorkerStatus(context.Background()); err == nil || model.Worker() != previousWorker {
		t.Fatalf("empty status err=%v worker=%v", err, model.Worker())
	}

	heartbeat, err := model.Heartbeat(context.Background(), 73, timev1.DevicePowerState_DEVICE_POWER_STATE_ON_BATTERY)
	if err != nil || heartbeat != api.heartbeat || !model.Online() {
		t.Fatalf("heartbeat = %v, %v online=%v", heartbeat, err, model.Online())
	}
	wantHeartbeat := &timev1.HeartbeatRequest{DeviceId: "device-7", AppVersion: "1", QueueDepth: 1, OldestUnsentAgeSeconds: 30, BatteryPercent: 73, PowerState: timev1.DevicePowerState_DEVICE_POWER_STATE_ON_BATTERY}
	if !proto.Equal(api.heartbeatRequest, wantHeartbeat) {
		t.Fatalf("heartbeat request = %v, want %v", api.heartbeatRequest, wantHeartbeat)
	}
	api.heartbeatErr = &APIError{Status: http.StatusForbidden, Code: "permission_denied", Message: "device revoked"}
	if _, err := model.Heartbeat(context.Background(), 73, timev1.DevicePowerState_DEVICE_POWER_STATE_ON_BATTERY); err == nil || model.Online() || model.Screen() != ScreenRevoked || model.Worker() != nil || model.token != "" {
		t.Fatalf("revocation response err=%v screen=%q online=%v worker=%v", err, model.Screen(), model.Online(), model.Worker())
	}

	model.worker, model.token, model.screen = status, "secret-token", ScreenConfirm
	model.lastActivity = now
	if model.Expire(now.Add(59*time.Second), time.Minute) || model.Worker() != status || model.token != "secret-token" || model.Screen() != ScreenConfirm {
		t.Fatalf("worker state changed before timeout: screen=%q worker=%v token=%q", model.Screen(), model.Worker(), model.token)
	}
	if !model.Expire(now.Add(time.Minute), time.Minute) || model.Worker() != nil || model.token != "" || model.Screen() != ScreenIdle {
		t.Fatalf("worker did not expire at timeout: screen=%q worker=%v token=%q", model.Screen(), model.Worker(), model.token)
	}
}

func TestTodo_TCLOCK_016_StorageAndFormattingBoundaries(t *testing.T) {
	if got, want := StorageKeys(), []string{keyDevice, keySequence, keyQueue}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("storage keys = %v, want %v", got, want)
	}
	now := time.Date(2026, 10, 21, 15, 4, 5, 0, time.UTC)
	if got := LocaleEnUS.ShortTime(now, time.UTC); got != "3:04:05 PM" {
		t.Fatalf("English short time = %q", got)
	}
	if got := LocaleDeDE.ShortTime(now, time.UTC); got != "15:04:05" {
		t.Fatalf("German short time = %q", got)
	}
	if got := LocaleAr.ShortTime(now, time.UTC); !strings.Contains(got, "٣:٠٤:٠٥") {
		t.Fatalf("Arabic short time = %q", got)
	}
	apiErr := &APIError{Status: http.StatusBadRequest, Code: "invalid_argument", Message: "bad input"}
	if got, want := apiErr.Error(), "timeclockapp: device API refused (400 invalid_argument): bad input"; got != want {
		t.Fatalf("API error = %q, want %q", got, want)
	}
	instant := time.Date(2026, 10, 21, 12, 0, 0, 0, time.FixedZone("UTC-4", -4*60*60))
	gotTimestamp := Timestamp(instant).AsTime()
	_, offset := gotTimestamp.Zone()
	if !gotTimestamp.Equal(instant.UTC()) || offset != 0 {
		t.Fatalf("timestamp = %v, want the same instant in UTC", gotTimestamp)
	}
}

func TestTodo_TCLOCK_016_OptionalAPIsRequireEnrolledDevice(t *testing.T) {
	enrolled := NewKiosk(&tclockAPI{}, &tclockStorage{values: map[string]string{}}, time.Now)
	enrolled.device = &DeviceRecord{DeviceID: "device-7", SiteID: "site-2", Timezone: "UTC"}
	enrolled.token = "valid-punch-token"
	if _, err := enrolled.SyncRoster(context.Background(), ""); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("roster without optional capability = %v", err)
	}
	if err := enrolled.RefreshWorkerStatus(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("status without optional capability = %v", err)
	}

	unenrolled := NewKiosk(&tclockOptionalAPI{}, &tclockStorage{values: map[string]string{}}, time.Now)
	if _, err := unenrolled.SyncRoster(context.Background(), ""); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("roster without enrolled device = %v", err)
	}
	if err := unenrolled.RefreshWorkerStatus(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("status without enrolled device/token = %v", err)
	}
	if _, err := unenrolled.Heartbeat(context.Background(), 0, timev1.DevicePowerState_DEVICE_POWER_STATE_UNSPECIFIED); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("heartbeat without device = %v", err)
	}
	unenrolled.worker = &timev1.WorkerPunchStatus{WorkerId: "worker-7"}
	if unenrolled.Expire(time.Now(), 0) || unenrolled.Worker() == nil {
		t.Fatal("zero timeout cleared active worker state")
	}
}
