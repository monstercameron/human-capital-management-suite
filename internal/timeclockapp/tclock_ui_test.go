package timeclockapp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

func TestTodo_TCLOCK_016_EncryptedStorageAndView(t *testing.T) {
	base := &tclockStorage{values: map[string]string{}}
	enc, err := NewEncryptedStorage(base, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.Save("x", "secret"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := base.Load("x"); raw == "secret" || len(raw) < 3 || raw[:3] != "v1:" {
		t.Fatal("plaintext was stored")
	}
	if got, ok := enc.Load("x"); !ok || got != "secret" {
		t.Fatalf("decrypted value = %q, %v", got, ok)
	}
	base.values["x"] = "v1:tampered"
	if got, ok := enc.Load("x"); !ok || got == "" {
		t.Fatal("tampered value was trusted")
	}
	if err := enc.Save("y", "other"); err != nil {
		t.Fatal(err)
	}
	base.values["x"], base.values["y"] = base.values["y"], base.values["x"]
	if got, ok := enc.Load("x"); !ok || got != invalidEncryptedState {
		t.Fatal("swapped ciphertext was trusted")
	}
	if _, err := NewEncryptedStorage(base, []byte("short")); err == nil {
		t.Fatal("short key accepted")
	}

	kiosk := NewKiosk(nil, &tclockStorage{values: map[string]string{}}, time.Now)
	markup, err := ui.RenderToString(Render(kiosk, context.Background(), time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "timeclock-enroll") || !strings.Contains(markup, "timeclock-locale") {
		t.Fatalf("enrollment markup missing: %s", markup)
	}
	if kiosk.Expire(time.Now().Add(time.Hour), time.Minute) {
		t.Fatal("idle expire cleared absent worker")
	}
}

func TestTodo_TCLOCK_016_ReceiptAndScreenViews(t *testing.T) {
	store := &tclockStorage{values: map[string]string{}}
	model := NewKiosk(nil, store, time.Now)
	for _, screen := range []Screen{ScreenEnroll, ScreenIdle, ScreenIdentify, ScreenConfirm, ScreenReceipt, ScreenRevoked} {
		model.screen = screen
		if _, err := ui.RenderToString(KioskView(KioskViewProps{Model: model, Now: time.Now()})); err != nil {
			t.Fatalf("screen %s: %v", screen, err)
		}
	}
	for _, status := range []timev1.PunchReceiptStatus{timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED, timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE, timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_HELD_FOR_REVIEW, timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_REJECTED} {
		model.screen, model.receipt = ScreenReceipt, Receipt{Sequence: 1, Status: status}
		if _, err := ui.RenderToString(receiptView(model, nil)); err != nil {
			t.Fatalf("receipt %s: %v", status, err)
		}
	}
}

func TestTodo_TCLOCK_KioskLegacyKeypadCollectsAndClearsPIN(t *testing.T) {
	model := NewKiosk(nil, &tclockStorage{values: map[string]string{}}, time.Now)
	model.device = &DeviceRecord{DeviceID: "device-1", State: timev1.ClockDeviceState_CLOCK_DEVICE_STATE_ACTIVE}
	model.screen = ScreenIdle
	model.BeginIdentify()
	if model.Screen() != ScreenIdentify {
		t.Fatalf("begin identify screen = %s", model.Screen())
	}
	for _, digit := range []string{"1", "2", "3", "4"} {
		model.AppendPIN(digit)
	}
	if model.PINDraft() != "1234" {
		t.Fatalf("pin draft = %q, want complete PIN", model.PINDraft())
	}
	model.BackspacePIN()
	if model.PINDraft() != "123" {
		t.Fatalf("after backspace = %q", model.PINDraft())
	}
	model.ClearPIN()
	if model.PINDraft() != "" {
		t.Fatalf("clear left PIN draft %q", model.PINDraft())
	}
	markup, err := ui.RenderToString(KioskView(KioskViewProps{Model: model, Now: time.Now()}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"timeclock-keypad", "Continue", "timeclock-pin-draft"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("legacy identify markup missing %q: %s", want, markup)
		}
	}
}
