// Package reservation contains the shared, domain-neutral resource
// reservation protocol. Position, budget and appointment domains own their
// capacity rules; this package owns identity, fencing and lifecycle truth.
package reservation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	Held      Status = "HELD"
	Committed Status = "COMMITTED"
	Consumed  Status = "CONSUMED"
	Released  Status = "RELEASED"
	Expired   Status = "EXPIRED"
)

func (s Status) Terminal() bool { return s == Consumed || s == Released || s == Expired }
func (s Status) Valid() bool    { return s == Held || s == Committed || s.Terminal() }

// Fence is the compare-and-swap identity required for a lifecycle transition.
type Fence struct {
	ID    uuid.UUID
	Token uint64
}

type Interval struct{ From, To time.Time }

func (i Interval) Validate() error {
	if i.From.IsZero() || i.To.IsZero() || !i.From.Before(i.To) {
		return ErrInvalidInterval
	}
	return nil
}
func (i Interval) Overlaps(other Interval) bool {
	return i.From.Before(other.To) && other.From.Before(i.To)
}

// Quantity is an integer in the domain's declared smallest unit (for example,
// thousandths of an FTE). Floating point quantities are deliberately absent.
type Quantity struct {
	Value int64
	Scale uint8
}

func (q Quantity) Validate() error {
	if q.Value <= 0 {
		return ErrInvalidQuantity
	}
	return nil
}
func (q Quantity) Add(other Quantity) (Quantity, error) {
	if q.Scale != other.Scale {
		return Quantity{}, ErrScaleMismatch
	}
	if other.Value > 0 && q.Value > int64(^uint64(0)>>1)-other.Value {
		return Quantity{}, ErrQuantityOverflow
	}
	return Quantity{Value: q.Value + other.Value, Scale: q.Scale}, nil
}

type Request struct {
	Resource        string
	Version         uint64
	Quantity        Quantity
	Interval        Interval
	Owner           string
	Priority        int
	ExpiresAt       time.Time
	ProposalDigest  string
	AuthorityDigest string
	IdempotencyKey  string
}

func (r Request) Validate(now time.Time) error {
	if strings.TrimSpace(r.Resource) == "" || r.Version == 0 || strings.TrimSpace(r.Owner) == "" {
		return ErrInvalidRequest
	}
	if err := r.Quantity.Validate(); err != nil {
		return err
	}
	if err := r.Interval.Validate(); err != nil {
		return err
	}
	if r.ExpiresAt.IsZero() || !r.ExpiresAt.After(now) {
		return ErrExpired
	}
	if !ValidDigest(r.ProposalDigest) || !ValidDigest(r.AuthorityDigest) {
		return ErrInvalidDigest
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return ErrInvalidRequest
	}
	return nil
}

type Reservation struct {
	ID        uuid.UUID
	Request   Request
	Capacity  Quantity
	Status    Status
	Fence     uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Event struct {
	Sequence      uint64
	ReservationID uuid.UUID
	From, To      Status
	Fence         uint64
	At            time.Time
}

var (
	ErrInvalidRequest    = errors.New("reservation: invalid request")
	ErrInvalidInterval   = errors.New("reservation: interval must be a non-empty half-open range")
	ErrInvalidQuantity   = errors.New("reservation: quantity must be positive")
	ErrScaleMismatch     = errors.New("reservation: quantity scales differ")
	ErrQuantityOverflow  = errors.New("reservation: quantity overflow")
	ErrInvalidDigest     = errors.New("reservation: digest must be lowercase SHA-256 hex")
	ErrExpired           = errors.New("reservation: expiry is not in the future")
	ErrCapacity          = errors.New("reservation: insufficient capacity")
	ErrConflict          = errors.New("reservation: competing reservation or changed proposal")
	ErrFence             = errors.New("reservation: stale fencing token")
	ErrNotFound          = errors.New("reservation: not found")
	ErrInvalidTransition = errors.New("reservation: invalid lifecycle transition")
)

type Error struct {
	Code string
	Err  error
	ID   uuid.UUID
}

func (e *Error) Error() string { return fmt.Sprintf("reservation: %s: %v", e.Code, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

const (
	CodeInvalid    = "RESERVATION_INVALID"
	CodeCapacity   = "RESERVATION_CAPACITY_CONFLICT"
	CodeConflict   = "RESERVATION_PROPOSAL_CONFLICT"
	CodeFence      = "RESERVATION_STALE_FENCE"
	CodeTransition = "RESERVATION_INVALID_TRANSITION"
)

func wrap(code string, id uuid.UUID, err error) error { return &Error{Code: code, ID: id, Err: err} }

// CodeOf returns the stable protocol code carried by err.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func ValidDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && hex.EncodeToString(b) == s
}
func Digest(data []byte) string { s := sha256.Sum256(data); return hex.EncodeToString(s[:]) }
