package productclient

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/timeclockapp"
)

// KioskSetupGuide is the reference deployment note carried with the client
// boundary so installers do not confuse a shared clock with the workspace.
const KioskSetupGuide = "Apple Single App Mode or Autonomous Single App Mode under supervised MDM; Guided Access for unmanaged iPad; Android Enterprise dedicated-device mode."

// KioskClientConfig contains only the public device API and durable storage
// seams. The adapter never accepts a worker, tenant, or admin credential from
// the page.
type KioskClientConfig struct {
	API         timeclockapp.DeviceAPI
	Storage     timeclockapp.Storage
	Context     context.Context
	Now         func() time.Time
	IdleTimeout time.Duration
	// ReceiptTimeout controls how long a completed receipt remains visible.
	// OnChange lets the host rerender after the automatic reset.
	ReceiptTimeout time.Duration
	OnChange       func()
}

// KioskClient wires the existing durable device state machine to the product
// kiosk projection. Rerendering remains the host's responsibility, which
// keeps this adapter usable by native GWC and WASM mounts.
type KioskClient struct {
	model          *timeclockapp.Kiosk
	ctx            context.Context
	now            func() time.Time
	idleTimeout    time.Duration
	receiptTimeout time.Duration
	onChange       func()
	draft          productui.KioskPunchRequest
	mu             sync.Mutex
	resetTimer     *time.Timer
}

// NewKioskClient restores the device queue and enrolled device from storage.
func NewKioskClient(cfg KioskClientConfig) *KioskClient {
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 2 * time.Minute
	}
	if cfg.ReceiptTimeout <= 0 {
		cfg.ReceiptTimeout = 10 * time.Second
	}
	return &KioskClient{model: timeclockapp.NewKiosk(cfg.API, cfg.Storage, cfg.Now), ctx: cfg.Context, now: cfg.Now, idleTimeout: cfg.IdleTimeout, receiptTimeout: cfg.ReceiptTimeout, onChange: cfg.OnChange}
}

// Model exposes the state-machine seam for a host that needs to schedule a
// heartbeat or a roster refresh. It is not rendered directly.
func (c *KioskClient) Model() *timeclockapp.Kiosk {
	if c == nil {
		return nil
	}
	return c.model
}

// Projection translates device-local state into the standalone product page.
func (c *KioskClient) Projection() productui.KioskProjection {
	if c == nil || c.model == nil {
		return productui.KioskProjection{Screen: productui.KioskScreenRecovery, ErrorLabel: productui.KioskCopy(productui.ResolveProductLocale("en-US"), "unavailable")}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot := c.model.Snapshot()
	locale := productui.ResolveProductLocale(string(snapshot.Locale))
	projection := productui.KioskProjection{
		Screen:      productui.KioskScreen(snapshot.Screen),
		Online:      snapshot.Online,
		Busy:        snapshot.Busy,
		QueueDepth:  snapshot.QueueDepth,
		SiteLabel:   snapshot.SiteID,
		Attestation: c.draft.Attestation,
		Tips:        c.draft.TipDeclaration,
		JobID:       c.draft.JobID,
		CostCodeID:  c.draft.CostCodeID,
	}
	if snapshot.Screen == timeclockapp.ScreenReceipt {
		projection.AutoResetSeconds = kioskTimeoutSeconds(c.receiptTimeout)
	}
	if snapshot.Worker != nil {
		projection.WorkerLabel = snapshot.Worker.GetDisplayName()
		projection.ShiftLabel = snapshot.Worker.GetActiveShiftId()
		projection.StatusLabel = sessionStatusLabel(locale, snapshot.Worker.GetSessionStatus())
		projection.Phase = kioskPhase(snapshot.Worker.GetSessionStatus())
	}
	if snapshot.Receipt.Sequence > 0 {
		projection.ReceiptSeq = snapshot.Receipt.Sequence
		projection.ReceiptLabel = receiptLabel(locale, snapshot.Receipt)
		switch snapshot.Receipt.Event {
		case timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN:
			projection.ReceiptEvent = productui.KioskPunchClockIn
		case timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT:
			projection.ReceiptEvent = productui.KioskPunchClockOut
		}
		if !snapshot.Receipt.At.IsZero() {
			face := snapshot.Locale.Face(snapshot.Receipt.At, timeclockapp.SiteLocation(snapshot.Timezone))
			projection.ReceiptTime = strings.TrimSpace(face.HourMinute + " " + face.Meridiem)
		}
	}
	if snapshot.Error != nil {
		projection.ErrorLabel = productui.KioskCopy(locale, "error")
	}
	return projection
}

// Render mounts the projection-driven product surface with host callbacks.
func (c *KioskClient) Render(view productui.View) ui.Node {
	if c != nil && c.model != nil {
		view.Locale = productui.ResolveProductLocale(string(c.model.Locale()))
	}
	projection := c.Projection()
	if c == nil {
		return productui.KioskPage(view, projection)
	}
	projection.OnEnroll = func(code string) { _ = c.Enroll(code) }
	projection.OnIdentify = func(credential string) { _ = c.Identify(credential) }
	projection.OnPunch = func(request productui.KioskPunchRequest) { _, _ = c.Punch(request) }
	projection.OnDone = c.CompleteInteraction
	projection.OnCancel = c.CompleteInteraction
	projection.OnStart = c.touch
	projection.OnDraftChange = func(request productui.KioskPunchRequest) {
		c.mu.Lock()
		c.draft = request
		c.mu.Unlock()
	}
	return productui.KioskPage(view, projection)
}

func (c *KioskClient) touch() {
	if c == nil || c.model == nil {
		return
	}
	c.mu.Lock()
	c.model.Touch()
	c.mu.Unlock()
}

// Enroll uses the public enrollment exchange and persists the device binding
// through the state machine.
func (c *KioskClient) Enroll(code string) error {
	if c == nil || c.model == nil {
		return errors.New("productclient: nil kiosk")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model.Touch()
	return c.model.Enroll(c.ctx, code)
}

// Identify performs the public worker identification flow. The credential is
// memory-only and is never copied into the durable device queue.
func (c *KioskClient) Identify(credential string) error {
	if c == nil || c.model == nil {
		return errors.New("productclient: nil kiosk")
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return errors.New("productclient: empty kiosk credential")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model.Touch()
	if isPINCredential(credential) {
		return c.model.IdentifyPIN(c.ctx, credential)
	}
	if strings.HasPrefix(strings.ToUpper(credential), "QR:") {
		qrKey := strings.TrimSpace(credential[len("QR:"):])
		if qrKey == "" {
			return errors.New("productclient: empty kiosk QR credential")
		}
		return c.model.IdentifyBadge(c.ctx, qrKey, true)
	}
	return c.model.IdentifyBadge(c.ctx, credential, false)
}

// Punch submits a clock action, attestation, tip declaration, or job transfer
// using the one public SubmitPunches route. The state machine persists before
// attempting delivery, preserving the sequence across restart and outage.
func (c *KioskClient) Punch(request productui.KioskPunchRequest) (timeclockapp.Receipt, error) {
	if c == nil || c.model == nil {
		return timeclockapp.Receipt{}, errors.New("productclient: nil kiosk")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model.Touch()
	event := timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN
	switch request.Event {
	case productui.KioskPunchClockOut:
		event = timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT
	case productui.KioskPunchAttestation:
		event = timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT
	case productui.KioskPunchTips:
		event = timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT
	case productui.KioskPunchJobTransfer:
		event = timev1.PunchEventType_PUNCH_EVENT_TYPE_JOB_TRANSFER
	}
	options := timeclockapp.PunchOptions{TipDeclaration: request.TipDeclaration, JobID: request.JobID, CostCodeID: request.CostCodeID}
	if strings.TrimSpace(request.Attestation) != "" {
		options.AttestationAnswers = []*timev1.AttestationAnswerInput{{QuestionRef: "kiosk", Answer: request.Attestation}}
	}
	receipt, err := c.model.PunchWithOptions(c.ctx, event, options)
	if err == nil || errors.Is(err, timeclockapp.ErrUnreachable) {
		c.draft = productui.KioskPunchRequest{}
	}
	if c.model.Screen() == timeclockapp.ScreenReceipt {
		c.armReceiptTimerLocked()
	}
	return receipt, err
}

// CompleteInteraction clears worker identity and returns to the shared-device
// start screen after a receipt or explicit cancellation.
func (c *KioskClient) CompleteInteraction() {
	if c == nil || c.model == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopReceiptTimerLocked()
	c.model.ClearWorker()
	c.draft = productui.KioskPunchRequest{}
}

// Expire applies the short idle policy. The host should call it from its
// timer; no worker identity survives the timeout.
func (c *KioskClient) Expire(now time.Time) bool {
	if c == nil || c.model == nil {
		return false
	}
	c.mu.Lock()
	timeout := c.idleTimeout
	if c.model.Screen() == timeclockapp.ScreenReceipt {
		timeout = c.receiptTimeout
	}
	if !c.model.Expire(now, timeout) {
		c.mu.Unlock()
		return false
	}
	c.stopReceiptTimerLocked()
	c.draft = productui.KioskPunchRequest{}
	onChange := c.onChange
	c.mu.Unlock()
	if onChange != nil {
		onChange()
	}
	return true
}

func (c *KioskClient) armReceiptTimerLocked() {
	c.stopReceiptTimerLocked()
	if c.receiptTimeout <= 0 || c.model.Screen() != timeclockapp.ScreenReceipt {
		return
	}
	deadline := c.now().Add(c.receiptTimeout)
	c.resetTimer = time.AfterFunc(c.receiptTimeout, func() { c.Expire(deadline) })
}

func (c *KioskClient) stopReceiptTimerLocked() {
	if c.resetTimer != nil {
		c.resetTimer.Stop()
		c.resetTimer = nil
	}
}

func isPINCredential(credential string) bool {
	if credential == "" {
		return false
	}
	for _, r := range credential {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func kioskTimeoutSeconds(timeout time.Duration) int {
	if timeout <= 0 {
		return 0
	}
	seconds := int(timeout / time.Second)
	if timeout%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

// PublicDeviceMethods reports the exact allow-list used by the HTTP adapter.
func PublicDeviceMethods() []string { return timeclockapp.DeviceMethods() }

// kioskPhase tells the page which single punch to offer first. An unknown
// status stays unknown so the page asks instead of guessing.
func kioskPhase(status timev1.SessionStatus) productui.KioskPhase {
	switch status {
	case timev1.SessionStatus_SESSION_STATUS_OPEN:
		return productui.KioskPhaseIn
	case timev1.SessionStatus_SESSION_STATUS_ON_BREAK:
		return productui.KioskPhaseBreak
	case timev1.SessionStatus_SESSION_STATUS_CLOSED:
		return productui.KioskPhaseOut
	default:
		return ""
	}
}

func sessionStatusLabel(locale productui.LocaleContext, status timev1.SessionStatus) string {
	switch status {
	case timev1.SessionStatus_SESSION_STATUS_OPEN:
		return productui.KioskCopy(locale, "status_in")
	case timev1.SessionStatus_SESSION_STATUS_ON_BREAK:
		return productui.KioskCopy(locale, "status_break")
	case timev1.SessionStatus_SESSION_STATUS_CLOSED:
		return productui.KioskCopy(locale, "status_out")
	default:
		return productui.KioskCopy(locale, "status_unknown")
	}
}

func receiptLabel(locale productui.LocaleContext, receipt timeclockapp.Receipt) string {
	if receipt.Queued {
		return productui.KioskCopy(locale, "receipt_queued")
	}
	switch receipt.Status {
	case timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_ACCEPTED:
		return productui.KioskCopy(locale, "receipt_recorded")
	case timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_DUPLICATE:
		return productui.KioskCopy(locale, "receipt_duplicate")
	case timev1.PunchReceiptStatus_PUNCH_RECEIPT_STATUS_HELD_FOR_REVIEW:
		return productui.KioskCopy(locale, "receipt_held")
	default:
		return productui.KioskCopy(locale, "receipt_rejected")
	}
}
