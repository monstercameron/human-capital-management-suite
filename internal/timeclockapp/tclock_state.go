package timeclockapp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Screen is the finite set of screens a kiosk can show. It has no route for
// an admin workspace, which keeps a managed tablet in single-app mode.
type Screen string

const (
	ScreenEnroll   Screen = "enroll"
	ScreenIdle     Screen = "idle"
	ScreenIdentify Screen = "identify"
	ScreenConfirm  Screen = "confirm"
	ScreenReceipt  Screen = "receipt"
	ScreenRevoked  Screen = "revoked"
	ScreenRecovery Screen = "recovery"
)

// Receipt is the user-facing result of a punch attempt.
type Receipt struct {
	Sequence uint64
	Event    timev1.PunchEventType
	Status   timev1.PunchReceiptStatus
	Reason   timev1.PunchRejectionReason
	Detail   string
	Queued   bool
	At       time.Time
}

// PunchOptions carries the optional device declarations accepted by the
// public punch contract.
type PunchOptions struct {
	AttestationAnswers []*timev1.AttestationAnswerInput
	TipDeclaration     string
	JobID              string
	CostCodeID         string
}

// Snapshot is the read-only projection a page mount needs to decide which
// kiosk view to render. It contains no credentials or PIN material.
type Snapshot struct {
	Screen     Screen
	Locale     Locale
	Direction  string
	Online     bool
	Busy       bool
	QueueDepth int
	SiteID     string
	Timezone   string
	Worker     *timev1.WorkerPunchStatus
	Receipt    Receipt
	Error      error
}

// Kiosk is the device-local state machine. State is persisted only through
// Storage; worker identity and punch tokens are intentionally memory-only.
type Kiosk struct {
	api          DeviceAPI
	storage      Storage
	now          func() time.Time
	device       *DeviceRecord
	worker       *timev1.WorkerPunchStatus
	token        string
	method       timev1.IdentificationMethod
	queue        Queue
	lastSeq      uint64
	locale       Locale
	screen       Screen
	online       bool
	busy         bool
	err          error
	receipt      Receipt
	pinDraft     string
	lastActivity time.Time
	corrupt      bool
	roster       *timev1.RosterSnapshot
	rosterAt     time.Time
}

// NewKiosk restores durable device state and starts at the safe idle screen.
func NewKiosk(api DeviceAPI, storage Storage, now func() time.Time) *Kiosk {
	if now == nil {
		now = time.Now
	}
	p := loadState(storage)
	screen := ScreenEnroll
	if p.device != nil {
		screen = ScreenIdle
	}
	if p.device != nil && p.device.State == timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED {
		screen = ScreenRevoked
	}
	if p.corrupt {
		screen = ScreenRecovery
	}
	return &Kiosk{api: api, storage: storage, now: now, device: p.device, queue: p.queue, lastSeq: p.lastSeq, locale: LocaleEnUS, screen: screen, online: p.device != nil, err: corruptError(p.corrupt), lastActivity: now(), corrupt: p.corrupt}
}

func corruptError(corrupt bool) error {
	if corrupt {
		return ErrStorage
	}
	return nil
}

// Screen returns the currently visible kiosk screen.
func (k *Kiosk) Screen() Screen { return k.screen }

// Locale returns the current presentation locale.
func (k *Kiosk) Locale() Locale { return k.locale }

// SetLocale changes presentation language and direction without changing state.
func (k *Kiosk) SetLocale(locale Locale) {
	k.Touch()
	for _, supported := range Locales() {
		if locale == supported {
			k.locale = locale
			return
		}
	}
}

// Touch records user activity for the host's idle policy.
func (k *Kiosk) Touch() { k.lastActivity = k.now() }

// BeginIdentify moves an enrolled kiosk from its resting screen to the
// credential screen. The PIN draft remains memory-only until it is submitted.
func (k *Kiosk) BeginIdentify() {
	if k.device == nil || k.deviceStateRevoked() || k.corrupt {
		return
	}
	k.pinDraft = ""
	k.screen = ScreenIdentify
	k.Touch()
}

// AppendPIN, ClearPIN and BackspacePIN own the legacy keypad draft. Keeping
// this state on the device model prevents a host from submitting one digit at
// a time through IdentifyPIN.
func (k *Kiosk) AppendPIN(digit string) {
	if len(digit) == 1 && digit[0] >= '0' && digit[0] <= '9' {
		k.pinDraft += digit
		k.Touch()
	}
}

func (k *Kiosk) ClearPIN() { k.pinDraft = ""; k.Touch() }

func (k *Kiosk) BackspacePIN() {
	if k.pinDraft != "" {
		k.pinDraft = k.pinDraft[:len(k.pinDraft)-1]
		k.Touch()
	}
}

func (k *Kiosk) PINDraft() string { return k.pinDraft }

// Expire clears transient worker state after timeout and returns true when it
// performed a reset.
func (k *Kiosk) Expire(now time.Time, timeout time.Duration) bool {
	if k.worker == nil || timeout <= 0 || now.Sub(k.lastActivity) < timeout {
		return false
	}
	k.ClearWorker()
	return true
}

// Device returns a copy of the enrolled public device metadata.
func (k *Kiosk) Device() *DeviceRecord {
	if k.device == nil {
		return nil
	}
	c := *k.device
	c.KeySeed = append([]byte(nil), k.device.KeySeed...)
	return &c
}

// QueueDepth reports punches waiting for server acknowledgement.
func (k *Kiosk) QueueDepth() int { return len(k.queue) }

// Online reports whether the most recent API operation reached the server.
func (k *Kiosk) Online() bool { return k.online }

// Busy reports whether a user action is in flight.
func (k *Kiosk) Busy() bool { return k.busy }

// Error returns the latest recoverable error, if any.
func (k *Kiosk) Error() error { return k.err }

// Worker returns the transient identified worker status.
func (k *Kiosk) Worker() *timev1.WorkerPunchStatus { return k.worker }

// LastReceipt returns the most recent punch result.
func (k *Kiosk) LastReceipt() Receipt { return k.receipt }

// Snapshot returns the current render projection without exposing device keys.
func (k *Kiosk) Snapshot() Snapshot {
	var worker *timev1.WorkerPunchStatus
	if k.worker != nil {
		worker = proto.Clone(k.worker).(*timev1.WorkerPunchStatus)
	}
	s := Snapshot{Screen: k.screen, Locale: k.locale, Direction: k.locale.Dir(), Online: k.online, Busy: k.busy, QueueDepth: len(k.queue), Worker: worker, Receipt: k.receipt, Error: k.err}
	if k.device != nil {
		s.SiteID, s.Timezone = k.device.SiteID, k.device.Timezone
	}
	return s
}

// Enroll creates a device key pair and exchanges the one-time code with the
// public enrollment method. The server remains authoritative for site data.
func (k *Kiosk) Enroll(ctx context.Context, code string) error {
	if k.corrupt {
		return ErrStorage
	}
	if strings.TrimSpace(code) == "" {
		return errors.New("enrollment code is required")
	}
	if k.api == nil {
		return ErrUnreachable
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	k.busy, k.err = true, nil
	challenge := []byte(strings.TrimSpace(code))
	idempotency := make([]byte, 16)
	if _, err := rand.Read(idempotency); err != nil {
		k.busy = false
		k.err = err
		return err
	}
	resp, err := k.api.EnrollDevice(ctx, &timev1.EnrollDeviceRequest{EnrollmentCode: strings.TrimSpace(code), PublicKey: pub, ChallengeSignature: ed25519.Sign(private, challenge), DeviceModel: "managed-tablet", AppVersion: "1", IdempotencyKey: fmt.Sprintf("%x", idempotency)})
	k.busy = false
	if err != nil {
		k.online = !errors.Is(err, ErrUnreachable)
		k.err = err
		return err
	}
	if resp == nil || resp.Device == nil || resp.Device.DeviceId == "" {
		k.err = errors.New("enrollment response did not include a device")
		return k.err
	}
	if resp.Device.SiteId == "" || resp.Device.Timezone == "" || resp.MachineClientCredentialRef == "" {
		k.err = errors.New("enrollment response was missing device binding")
		return k.err
	}
	if _, err := time.LoadLocation(resp.Device.Timezone); err != nil {
		k.err = errors.New("enrollment response had an invalid timezone")
		return k.err
	}
	seed := append([]byte(nil), private.Seed()...)
	candidate := DeviceRecord{DeviceID: resp.Device.DeviceId, SiteID: resp.Device.SiteId, Timezone: resp.Device.Timezone, CredentialRef: resp.MachineClientCredentialRef, KeySeed: seed, State: resp.Device.State}
	if err := saveDevice(k.storage, candidate); err != nil {
		k.err = err
		return err
	}
	k.device = &candidate
	k.online, k.screen, k.err = true, ScreenIdle, nil
	return nil
}

// IdentifyPIN authenticates a worker using a PIN. PINs are never persisted.
func (k *Kiosk) IdentifyPIN(ctx context.Context, pin string) error {
	if k.device == nil || k.api == nil {
		return ErrUnreachable
	}
	return k.identify(ctx, &timev1.IdentifyWorkerRequest{DeviceId: k.device.DeviceID, Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_PIN, Credential: &timev1.IdentifyWorkerRequest_Pin{Pin: pin}})
}

// IdentifyBadge authenticates a worker using a badge or QR verifier.
func (k *Kiosk) IdentifyBadge(ctx context.Context, value string, qr bool) error {
	if k.device == nil || k.api == nil {
		return ErrUnreachable
	}
	if qr {
		return k.identify(ctx, &timev1.IdentifyWorkerRequest{DeviceId: k.device.DeviceID, Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_QR, Credential: &timev1.IdentifyWorkerRequest_QrKeyId{QrKeyId: value}})
	}
	return k.identify(ctx, &timev1.IdentifyWorkerRequest{DeviceId: k.device.DeviceID, Method: timev1.IdentificationMethod_IDENTIFICATION_METHOD_BADGE, Credential: &timev1.IdentifyWorkerRequest_BadgeId{BadgeId: value}})
}

func (k *Kiosk) identify(ctx context.Context, request *timev1.IdentifyWorkerRequest) error {
	if k.corrupt {
		return ErrStorage
	}
	if k.device == nil || k.api == nil {
		return ErrUnreachable
	}
	k.busy, k.err = true, nil
	resp, err := k.api.IdentifyWorker(ctx, request)
	k.busy = false
	if err != nil {
		k.online = !errors.Is(err, ErrUnreachable)
		k.err = err
		return err
	}
	if resp == nil || resp.Status == nil || resp.PunchToken == "" {
		k.err = errors.New("worker identification response was incomplete")
		return k.err
	}
	k.online, k.worker, k.token, k.method, k.screen, k.err = true, resp.Status, resp.PunchToken, request.Method, ScreenConfirm, nil
	return nil
}

// Punch records an event locally first, then attempts delivery. A network
// failure leaves a durable queue entry and a receipt marked Queued.
func (k *Kiosk) Punch(ctx context.Context, event timev1.PunchEventType) (Receipt, error) {
	return k.PunchWithOptions(ctx, event, PunchOptions{})
}

// PunchWithOptions records an event and its attestations, tip and job data
// locally before attempting delivery.
func (k *Kiosk) PunchWithOptions(ctx context.Context, event timev1.PunchEventType, options PunchOptions) (Receipt, error) {
	if k.corrupt {
		return Receipt{}, ErrStorage
	}
	if k.roster != nil && k.roster.MaxOfflineAgeSeconds > 0 && k.now().Sub(k.rosterAt) > time.Duration(k.roster.MaxOfflineAgeSeconds)*time.Second {
		return Receipt{}, errors.New("roster is too old for a new punch")
	}
	if k.device == nil || k.worker == nil || k.token == "" {
		return Receipt{}, errors.New("worker is not identified")
	}
	nextSeq := k.lastSeq + 1
	p := QueuedPunch{Sequence: nextSeq, Event: event, PunchToken: k.token, Method: k.method, OccurredAt: k.now().UTC(), AttestationAnswers: options.AttestationAnswers, TipDeclaration: options.TipDeclaration, JobID: options.JobID, CostCodeID: options.CostCodeID}
	nextQueue := append(append(Queue(nil), k.queue...), p)
	if err := saveQueue(k.storage, nextQueue, nextSeq); err != nil {
		k.err = err
		return Receipt{}, err
	}
	k.lastSeq, k.queue = nextSeq, nextQueue
	k.receipt = Receipt{Sequence: p.Sequence, Event: event, At: p.OccurredAt, Queued: true}
	k.screen = ScreenReceipt
	if err := k.Flush(ctx); err != nil && !errors.Is(err, ErrUnreachable) {
		return k.receipt, err
	}
	return k.receipt, nil
}

// SyncRoster refreshes the server-published worker and policy snapshot when
// the optional device API is available.
func (k *Kiosk) SyncRoster(ctx context.Context, cursor string) (*timev1.RosterSnapshot, error) {
	api, ok := k.api.(RosterAPI)
	if !ok || k.device == nil {
		return nil, ErrUnreachable
	}
	resp, err := api.SyncRoster(ctx, &timev1.SyncRosterRequest{DeviceId: k.device.DeviceID, Cursor: cursor, MaxResults: 500})
	if err != nil {
		k.online = false
		return nil, err
	}
	if resp == nil || resp.Snapshot == nil {
		return nil, errors.New("empty roster response")
	}
	k.roster, k.rosterAt, k.online = resp.Snapshot, k.now(), true
	return resp.Snapshot, nil
}

// RefreshWorkerStatus updates the identified worker status through the
// optional status endpoint while keeping the punch token in memory only.
func (k *Kiosk) RefreshWorkerStatus(ctx context.Context) error {
	api, ok := k.api.(WorkerStatusAPI)
	if !ok || k.device == nil || k.token == "" {
		return ErrUnreachable
	}
	resp, err := api.GetWorkerStatus(ctx, &timev1.GetWorkerStatusRequest{DeviceId: k.device.DeviceID, PunchToken: k.token})
	if err != nil {
		return err
	}
	if resp == nil || resp.Status == nil {
		return errors.New("empty worker status response")
	}
	k.worker = resp.Status
	return nil
}

// Flush retries the durable queue and trims only server-acknowledged entries.
func (k *Kiosk) Flush(ctx context.Context) error {
	if len(k.queue) == 0 || k.device == nil || k.api == nil {
		return nil
	}
	resp, err := k.api.SubmitPunches(ctx, &timev1.SubmitPunchesRequest{DeviceId: k.device.DeviceID, Punches: k.queue.Proto()})
	if err != nil {
		k.online = false
		k.err = err
		return err
	}
	k.online, k.err = true, nil
	if resp == nil {
		k.err = errors.New("empty punch response")
		return k.err
	}
	if resp.GetHighestContiguousSequence() > k.lastSeq {
		k.err = errors.New("punch response acknowledged an unknown sequence")
		return k.err
	}
	k.queue = k.queue.Trim(resp.GetHighestContiguousSequence())
	if err := saveQueue(k.storage, k.queue, k.lastSeq); err != nil {
		k.err = err
		return err
	}
	for _, receipt := range resp.GetReceipts() {
		if receipt.GetDeviceSequence() == k.receipt.Sequence {
			k.receipt.Status, k.receipt.Reason, k.receipt.Detail, k.receipt.Queued = receipt.GetStatus(), receipt.GetRejectionReason(), receipt.GetRejectionDetail(), false
		}
	}
	return nil
}

// ClearWorker returns to the shared-device start screen after every action.
func (k *Kiosk) ClearWorker() {
	k.worker, k.token, k.err, k.pinDraft = nil, "", nil, ""
	if k.device == nil {
		k.screen = ScreenEnroll
	} else if k.deviceStateRevoked() {
		k.screen = ScreenRevoked
	} else {
		k.screen = ScreenIdle
	}
}

func (k *Kiosk) deviceStateRevoked() bool {
	return k.device != nil && k.device.State == timev1.ClockDeviceState_CLOCK_DEVICE_STATE_REVOKED
}

// Heartbeat sends queue health and returns the server response.
func (k *Kiosk) Heartbeat(ctx context.Context, battery int32, power timev1.DevicePowerState) (*timev1.HeartbeatResponse, error) {
	if k.device == nil || k.api == nil {
		return nil, ErrUnreachable
	}
	resp, err := k.api.Heartbeat(ctx, &timev1.HeartbeatRequest{DeviceId: k.device.DeviceID, AppVersion: "1", QueueDepth: int32(len(k.queue)), OldestUnsentAgeSeconds: int64(k.queue.OldestAge(k.now().UTC()).Seconds()), BatteryPercent: battery, PowerState: power})
	if err != nil {
		k.online = false
		if apiErr, ok := err.(*APIError); ok && (apiErr.Status == 401 || apiErr.Status == 403 || apiErr.Code == "revoked") {
			k.screen = ScreenRevoked
			k.worker, k.token = nil, ""
		}
		return nil, err
	}
	k.online = true
	return resp, nil
}

// SeedBase64 is a diagnostic-safe representation useful to platform adapters.
// Timestamp converts the kiosk clock to the wire representation.
func Timestamp(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t.UTC()) }
