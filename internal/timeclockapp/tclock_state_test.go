package timeclockapp

import (
	"context"
	"errors"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

type tclockStorage struct {
	values map[string]string
	fail   bool
}

func (s *tclockStorage) Load(key string) (string, bool) { value, ok := s.values[key]; return value, ok }
func (s *tclockStorage) Save(key, value string) error {
	if s.fail {
		return errors.New("save failed")
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

type tclockAPI struct {
	enroll   *timev1.EnrollDeviceResponse
	identify *timev1.IdentifyWorkerResponse
	submit   *timev1.SubmitPunchesResponse
	err      error
	seen     []*timev1.SubmitPunchesRequest
}

func (a *tclockAPI) EnrollDevice(context.Context, *timev1.EnrollDeviceRequest) (*timev1.EnrollDeviceResponse, error) {
	return a.enroll, a.err
}
func (a *tclockAPI) IdentifyWorker(context.Context, *timev1.IdentifyWorkerRequest) (*timev1.IdentifyWorkerResponse, error) {
	return a.identify, a.err
}
func (a *tclockAPI) SubmitPunches(_ context.Context, in *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	a.seen = append(a.seen, in)
	return a.submit, a.err
}
func (a *tclockAPI) Heartbeat(context.Context, *timev1.HeartbeatRequest) (*timev1.HeartbeatResponse, error) {
	return &timev1.HeartbeatResponse{}, a.err
}

func enrolledFixture() *timev1.EnrollDeviceResponse {
	return &timev1.EnrollDeviceResponse{Device: &timev1.ClockDevice{DeviceId: "dev-1", SiteId: "site-a", Timezone: "UTC", State: timev1.ClockDeviceState_CLOCK_DEVICE_STATE_ACTIVE}, MachineClientCredentialRef: "cred-1"}
}

func TestTodo_TCLOCK_016_StateMachine(t *testing.T) {
	now := time.Date(2026, 9, 28, 14, 30, 0, 0, time.UTC)
	store := &tclockStorage{values: map[string]string{}}
	api := &tclockAPI{enroll: enrolledFixture(), identify: &timev1.IdentifyWorkerResponse{PunchToken: "token", Status: &timev1.WorkerPunchStatus{DisplayName: "Ava Stone", SessionStatus: timev1.SessionStatus_SESSION_STATUS_CLOSED}}, submit: &timev1.SubmitPunchesResponse{HighestContiguousSequence: 1, Receipts: []*timev1.PunchReceipt{{DeviceSequence: 1, Status: timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED}}}}
	kiosk := NewKiosk(api, store, func() time.Time { return now })
	if kiosk.Screen() != ScreenEnroll {
		t.Fatalf("initial screen = %q", kiosk.Screen())
	}
	if err := kiosk.Enroll(context.Background(), "  one-use  "); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if kiosk.Screen() != ScreenIdle || kiosk.Device().DeviceID != "dev-1" {
		t.Fatalf("enrolled state = %q", kiosk.Screen())
	}
	if err := kiosk.IdentifyPIN(context.Background(), "1234"); err != nil {
		t.Fatalf("identify: %v", err)
	}
	if kiosk.Screen() != ScreenConfirm || kiosk.Worker().DisplayName != "Ava Stone" {
		t.Fatalf("identified state = %q", kiosk.Screen())
	}
	receipt, err := kiosk.Punch(context.Background(), timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN)
	if err != nil || receipt.Status != timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED || kiosk.QueueDepth() != 0 {
		t.Fatalf("punch = %#v, %v, queue=%d", receipt, err, kiosk.QueueDepth())
	}
	if len(api.seen) != 1 || api.seen[0].Punches[0].DeviceSequence != 1 {
		t.Fatalf("submitted punches = %#v", api.seen)
	}
	kiosk.ClearWorker()
	if kiosk.Screen() != ScreenIdle || kiosk.Worker() != nil {
		t.Fatalf("clear state = %q", kiosk.Screen())
	}
}

func TestTodo_TCLOCK_016_OfflineQueueSurvivesRestart(t *testing.T) {
	store := &tclockStorage{values: map[string]string{}}
	api := &tclockAPI{enroll: enrolledFixture(), identify: &timev1.IdentifyWorkerResponse{PunchToken: "token", Status: &timev1.WorkerPunchStatus{DisplayName: "Ava", SessionStatus: timev1.SessionStatus_SESSION_STATUS_CLOSED}}}
	kiosk := NewKiosk(api, store, time.Now)
	if err := kiosk.Enroll(context.Background(), "code"); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	// A fresh model can only be constructed after a successful enrollment.
	api.err = ErrUnreachable
	if err := kiosk.IdentifyBadge(context.Background(), "badge", false); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("identify error = %v", err)
	}
	api.err = nil
	api.identify = &timev1.IdentifyWorkerResponse{PunchToken: "token", Status: &timev1.WorkerPunchStatus{DisplayName: "Ava", SessionStatus: timev1.SessionStatus_SESSION_STATUS_CLOSED}}
	if err := kiosk.IdentifyBadge(context.Background(), "badge", false); err != nil {
		t.Fatalf("badge identify: %v", err)
	}
	api.err = ErrUnreachable
	receipt, err := kiosk.Punch(context.Background(), timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT)
	if err != nil || !receipt.Queued || kiosk.QueueDepth() != 1 {
		t.Fatalf("offline punch = %#v, %v, queue=%d", receipt, err, kiosk.QueueDepth())
	}
	restored := NewKiosk(&tclockAPI{}, store, time.Now)
	if restored.QueueDepth() != 1 || restored.Device() == nil {
		t.Fatalf("restored queue=%d device=%v", restored.QueueDepth(), restored.Device())
	}
}

func TestTodo_TCLOCK_016_LocalesAndDirection(t *testing.T) {
	for _, locale := range Locales() {
		if locale.Text("app.title") == "" || locale.Dir() == "" {
			t.Fatalf("incomplete locale %q", locale)
		}
	}
	if ResolveLocale("ar-EG").Dir() != "rtl" || ResolveLocale("de-AT") != LocaleDeDE {
		t.Fatal("locale resolution failed")
	}
	if got := LocaleAr.Digits("12:30"); got != "١٢:٣٠" {
		t.Fatalf("arabic digits = %q", got)
	}
}

func TestTodo_TCLOCK_016_RejectsUnknownAcknowledgement(t *testing.T) {
	store := &tclockStorage{values: map[string]string{}}
	api := &tclockAPI{enroll: enrolledFixture(), identify: &timev1.IdentifyWorkerResponse{PunchToken: "t", Status: &timev1.WorkerPunchStatus{DisplayName: "A", SessionStatus: timev1.SessionStatus_SESSION_STATUS_CLOSED}}, submit: &timev1.SubmitPunchesResponse{HighestContiguousSequence: 99}}
	kiosk := NewKiosk(api, store, time.Now)
	if err := kiosk.Enroll(context.Background(), "code"); err != nil {
		t.Fatal(err)
	}
	if err := kiosk.IdentifyPIN(context.Background(), "1234"); err != nil {
		t.Fatal(err)
	}
	if _, err := kiosk.Punch(context.Background(), timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN); err == nil {
		t.Fatal("unknown acknowledgement accepted")
	}
	if kiosk.QueueDepth() != 1 {
		t.Fatal("durable queue was trimmed on invalid acknowledgement")
	}
}
