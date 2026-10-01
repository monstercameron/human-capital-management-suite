package productclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/timeclockapp"
)

type kioskClientStorage struct{ values map[string]string }

func (s *kioskClientStorage) Load(key string) (string, bool) {
	value, ok := s.values[key]
	return value, ok
}
func (s *kioskClientStorage) Save(key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

type kioskClientAPI struct {
	err          error
	enroll       *timev1.EnrollDeviceResponse
	identify     *timev1.IdentifyWorkerResponse
	submit       *timev1.SubmitPunchesResponse
	seenIdentify []*timev1.IdentifyWorkerRequest
	seen         []*timev1.SubmitPunchesRequest
}

func (a *kioskClientAPI) EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	return a.enroll, a.err
}
func (a *kioskClientAPI) IdentifyWorker(_ context.Context, request *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	a.seenIdentify = append(a.seenIdentify, request)
	return a.identify, a.err
}
func (a *kioskClientAPI) SubmitPunches(_ context.Context, request *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	a.seen = append(a.seen, request)
	return a.submit, a.err
}
func (a *kioskClientAPI) Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	return &timev1.HeartbeatResponse{}, a.err
}

func kioskClientFixture() (*kioskClientAPI, *kioskClientStorage) {
	return &kioskClientAPI{
		enroll:   &timev1.EnrollDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "device-1", SiteId: "site-1", Timezone: "UTC", State: timev1.ClockDeviceState_CLOCK_DEVICE_STATE_ACTIVE}, MachineClientCredentialRef: "credential-ref"},
		identify: &timev1.IdentifyWorkerResponse{PunchToken: "memory-only-token", Status: &timev1.WorkerPunchStatus{DisplayName: "Ava Stone", ActiveShiftId: "shift-1", SessionStatus: timev1.SessionStatus_SESSION_STATUS_CLOSED}},
		submit:   &timev1.SubmitPunchesResponse{HighestContiguousSequence: 1, Receipts: []*timev1.PunchReceipt{{DeviceSequence: 1, Status: timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED}}},
	}, &kioskClientStorage{values: map[string]string{}}
}

func enrolledKioskClient(t *testing.T, cfg KioskClientConfig) (*KioskClient, *kioskClientAPI) {
	t.Helper()
	api, storage := kioskClientFixture()
	cfg.API, cfg.Storage = api, storage
	client := NewKioskClient(cfg)
	if err := client.Enroll("one-time"); err != nil {
		t.Fatal(err)
	}
	return client, api
}

func TestTodo_TCLOCK_KioskCredentialRouting(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		method timev1.IdentificationMethod
		check  func(*timev1.IdentifyWorkerRequest) string
	}{
		{name: "pin", value: "1234", method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN, check: func(r *timev1.IdentifyWorkerRequest) string { return r.GetPin() }},
		{name: "badge", value: "badge-1", method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_BADGE, check: func(r *timev1.IdentifyWorkerRequest) string { return r.GetBadgeId() }},
		{name: "qr", value: "QR:qr-key-1", method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_QR, check: func(r *timev1.IdentifyWorkerRequest) string { return r.GetQrKeyId() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, api := enrolledKioskClient(t, KioskClientConfig{Now: time.Now})
			if err := client.Identify(tc.value); err != nil {
				t.Fatal(err)
			}
			if len(api.seenIdentify) != 1 {
				t.Fatalf("identify calls = %d, want 1", len(api.seenIdentify))
			}
			request := api.seenIdentify[0]
			if request.GetMethod() != tc.method || tc.check(request) != strings.TrimPrefix(tc.value, "QR:") {
				t.Fatalf("identify request = %+v", request)
			}
		})
	}
}

func TestTodo_TCLOCK_KioskDynamicLabelsUseLocaleCatalog(t *testing.T) {
	for _, locale := range []timeclockapp.Locale{timeclockapp.LocaleEnUS, timeclockapp.LocaleDeDE, timeclockapp.LocaleAr} {
		t.Run(string(locale), func(t *testing.T) {
			client, _ := enrolledKioskClient(t, KioskClientConfig{Now: time.Now})
			client.Model().SetLocale(locale)
			if err := client.Identify("1234"); err != nil {
				t.Fatal(err)
			}
			got := client.Projection().StatusLabel
			want := productui.KioskCopy(productui.ResolveProductLocale(string(locale)), "status_out")
			if got != want || got == "Clocked out" && locale != timeclockapp.LocaleEnUS {
				t.Fatalf("status label = %q, want %q", got, want)
			}
			if _, err := client.Punch(productui.KioskPunchRequest{Event: productui.KioskPunchClockIn}); err != nil {
				t.Fatal(err)
			}
			receiptWant := productui.KioskCopy(productui.ResolveProductLocale(string(locale)), "receipt_recorded")
			if got := client.Projection().ReceiptLabel; got != receiptWant {
				t.Fatalf("receipt label = %q, want %q", got, receiptWant)
			}
		})
	}
}

func TestTodo_TCLOCK_KioskReceiptTimerExpiresAndNotifiesHost(t *testing.T) {
	changed := make(chan struct{}, 1)
	client, _ := enrolledKioskClient(t, KioskClientConfig{Now: time.Now, ReceiptTimeout: 20 * time.Millisecond, OnChange: func() { changed <- struct{}{} }})
	if err := client.Identify("badge-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Punch(productui.KioskPunchRequest{Event: productui.KioskPunchClockIn}); err != nil {
		t.Fatal(err)
	}
	if got := client.Projection().AutoResetSeconds; got != 1 {
		t.Fatalf("receipt countdown = %d, want 1 second minimum", got)
	}
	select {
	case <-changed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("receipt timer did not notify host")
	}
	if client.Model().Screen() != timeclockapp.ScreenIdle || client.Model().Worker() != nil {
		t.Fatalf("receipt timer did not return to idle: screen=%s worker=%v", client.Model().Screen(), client.Model().Worker())
	}
}

func TestTodo_TCLOCK_016_Integration(t *testing.T) {
	api, storage := kioskClientFixture()
	client := NewKioskClient(KioskClientConfig{API: api, Storage: storage, Now: time.Now})
	if err := client.Enroll("one-time"); err != nil {
		t.Fatal(err)
	}
	if err := client.Identify("badge-1"); err != nil {
		t.Fatal(err)
	}
	_, err := client.Punch(productui.KioskPunchRequest{Event: productui.KioskPunchClockIn, Attestation: "safe", TipDeclaration: "3.00", JobID: "job-1", CostCodeID: "cost-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.seen) != 1 || api.seen[0].Punches[0].GetDeviceSequence() != 1 || api.seen[0].Punches[0].GetJobId() != "job-1" || api.seen[0].Punches[0].GetTipDeclaration() != "3.00" || len(api.seen[0].Punches[0].GetAttestationAnswers()) != 1 {
		t.Fatalf("public punch projection = %+v", api.seen)
	}
}

func TestTodo_TCLOCK_016_Security(t *testing.T) {
	for _, method := range PublicDeviceMethods() {
		if method == "CreateEnrollmentCode" || method == "RotateDeviceKey" || method == "RevokeDevice" {
			t.Fatalf("admin method leaked into kiosk allow-list: %s", method)
		}
	}
	if len(PublicDeviceMethods()) != len(timeclockapp.DeviceMethods()) {
		t.Fatal("kiosk method projection changed the public allow-list")
	}
}

func TestTodo_TCLOCK_016_Recovery(t *testing.T) {
	api, storage := kioskClientFixture()
	client := NewKioskClient(KioskClientConfig{API: api, Storage: storage, Now: time.Now, IdleTimeout: time.Minute})
	if err := client.Enroll("one-time"); err != nil {
		t.Fatal(err)
	}
	if err := client.Identify("badge-1"); err != nil {
		t.Fatal(err)
	}
	client.Model().Touch()
	if !client.Expire(time.Now().Add(2 * time.Minute)) {
		t.Fatal("idle timeout did not clear worker state")
	}
	if client.Model().Worker() != nil || client.Projection().Screen != productui.KioskScreenIdle {
		t.Fatalf("worker state survived idle expiry: %+v", client.Projection())
	}
}

func TestTodo_TCLOCK_016_Browser(t *testing.T) {
	api, storage := kioskClientFixture()
	client := NewKioskClient(KioskClientConfig{API: api, Storage: storage, Now: time.Now})
	view := productui.NewView(productui.PageClock, "tenant", "principal", "scope")
	markup, err := ui.RenderToString(client.Render(view))
	if err != nil {
		t.Fatal(err)
	}
	if markup == "" || !strings.Contains(markup, "timeclock-kiosk") {
		t.Fatalf("client did not mount kiosk page: %s", markup)
	}
}

func TestTodo_TCLOCK_016_ClientRejectsEmptyCredential(t *testing.T) {
	api, storage := kioskClientFixture()
	client := NewKioskClient(KioskClientConfig{API: api, Storage: storage})
	err := client.Identify("  ")
	if err == nil || !strings.Contains(err.Error(), "empty kiosk credential") || errors.Is(err, context.Canceled) {
		t.Fatal("empty credential was not rejected")
	}
}
