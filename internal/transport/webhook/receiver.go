// Package webhook exposes authenticated provider callbacks through a thin
// HTTP boundary. Durable receipt and dispatch policy stays behind Sink.
package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	corewebhook "github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
)

// Path is the provider callback ingress route.
const Path = "/api/v1/integrations/provider-receipts"

const (
	PayrollProvider = "payroll"
	IAMProvider     = "iam"
)

// PathForProvider returns the fixed route for one supported provider. The
// provider is selected by the deployment's route composition, never by the
// callback body or headers.
func PathForProvider(provider string) string {
	switch provider {
	case PayrollProvider, IAMProvider:
		return Path + "/" + provider
	default:
		return ""
	}
}

// OverlayProviderReceivers mounts only the configured fixed-provider
// receivers, leaving every other path to the existing edge handler.
func OverlayProviderReceivers(next, payroll, iam http.Handler) http.Handler {
	if payroll == nil && iam == nil {
		return next
	}
	if next == nil {
		next = http.NotFoundHandler()
	}
	mux := http.NewServeMux()
	if payroll != nil {
		mux.Handle(PathForProvider(PayrollProvider), payroll)
	}
	if iam != nil {
		mux.Handle(PathForProvider(IAMProvider), iam)
	}
	mux.Handle("/", next)
	return mux
}

// Sink atomically persists a verified receipt and its protected signed bytes.
// Implementations must enqueue any downstream observation in the same durable
// transaction; duplicate identical calls must return the original disposition.
type Sink interface {
	Accept(context.Context, providerreceipt.Parsed, time.Time) (Disposition, error)
	Replay(context.Context, string, corewebhook.ReplayApproval) (providerreceipt.Parsed, error)
}

// Disposition is the durable receipt decision returned by Sink.
type Disposition string

const (
	DispositionAccepted  Disposition = "ACCEPTED"
	DispositionDuplicate Disposition = "DUPLICATE"
)

// Receiver composes provider signature verification with durable receipt
// admission. Verifier must use the same durable idempotency store configured
// by the serving application; Sink owns durable quarantine and dispatch.
type Receiver struct {
	Verifier *providerreceipt.Verifier
	Sink     Sink
	Now      func() time.Time
	MaxBytes int64
}

func (h Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Verifier == nil || h.Sink == nil {
		http.Error(w, "webhook receiver unavailable", http.StatusServiceUnavailable)
		return
	}
	limit := h.MaxBytes
	if limit <= 0 {
		limit = providerreceipt.DefaultMaxBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid request body", http.StatusBadRequest)
		}
		return
	}
	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	receivedAt := now()
	parsed, err := h.Verifier.Parse(r.Header, body, receivedAt)
	if err != nil {
		http.Error(w, "callback rejected", statusFor(err))
		return
	}
	disposition, err := h.Sink.Accept(r.Context(), parsed, receivedAt)
	if err != nil {
		http.Error(w, "receipt unavailable", http.StatusServiceUnavailable)
		return
	}
	if disposition != DispositionAccepted && disposition != DispositionDuplicate {
		http.Error(w, "receipt unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}

// Replay redelivers an already persisted receipt only with an explicit
// approved actor and purpose. It is intended for an authenticated internal
// application call, not an unauthenticated HTTP route.
func (h Receiver) Replay(ctx context.Context, receiptID string, approval corewebhook.ReplayApproval) (providerreceipt.Parsed, error) {
	if h.Sink == nil || strings.TrimSpace(receiptID) == "" || !approval.Approved || strings.TrimSpace(approval.Actor) == "" || strings.TrimSpace(approval.Purpose) == "" {
		return providerreceipt.Parsed{}, corewebhook.ErrReplayNotAuthorized
	}
	return h.Sink.Replay(ctx, receiptID, approval)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, providerreceipt.ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, providerreceipt.ErrOutsideWindow), errors.Is(err, providerreceipt.ErrDuplicateDifferent):
		return http.StatusConflict
	case errors.Is(err, providerreceipt.ErrBadSignature), errors.Is(err, providerreceipt.ErrWrongTenant):
		return http.StatusUnauthorized
	case errors.Is(err, providerreceipt.ErrUnknownEvent), errors.Is(err, providerreceipt.ErrUnknownSchema), errors.Is(err, providerreceipt.ErrMalformed):
		return http.StatusBadRequest
	default:
		return http.StatusServiceUnavailable
	}
}
