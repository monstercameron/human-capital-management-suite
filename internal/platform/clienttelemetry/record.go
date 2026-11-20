// Package clienttelemetry is the contract for the Chat client's real-user
// monitoring beacon: one small, versioned, PII-free record the browser sends
// when the page is hidden or the first error happens. Everything here is an
// allow-list. A field that is not named below is refused, so message content,
// conversation names and ids cannot ride along by accident.
package clienttelemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// Version is the only record version this server accepts.
	Version = 1
	// MaxBodyBytes bounds one beacon. A real record is well under a kilobyte.
	MaxBodyBytes = 4096
	// MaxMarks and MaxPanicSites bound the two open-ended maps.
	MaxMarks      = 24
	MaxPanicSites = 32

	maxMarkMillis = 3_600_000
	maxCount      = 100_000

	ReasonPageHide = "pagehide"
	ReasonError    = "error"
)

var (
	// ErrTooLarge means the body went over MaxBodyBytes.
	ErrTooLarge = errors.New("clienttelemetry: record too large")
	// ErrInvalid means the body is not a record this server accepts.
	ErrInvalid = errors.New("clienttelemetry: invalid record")
)

// StreamKeys are the live-stream state transitions the client may count.
var StreamKeys = map[string]bool{
	"subscribe": true, "open": true, "open-failed": true, "ended": true,
	"gave-up": true, "cancelled": true, "replaced": true, "gap": true, "rollover": true,
}

var (
	buildPattern   = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
	localePattern  = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,2}$`)
	sessionPattern = regexp.MustCompile(`^[a-f0-9]{16,32}$`)
	markPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	sitePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
)

// Record is one beacon. Marks are milliseconds since navigation start, Stream
// counts live-stream transitions, and Panics counts recovered panics by site.
type Record struct {
	Version int            `json:"v"`
	Build   string         `json:"build"`
	Locale  string         `json:"locale"`
	Reason  string         `json:"reason"`
	Session string         `json:"session"`
	Marks   map[string]int `json:"marks,omitempty"`
	Stream  map[string]int `json:"stream,omitempty"`
	Panics  map[string]int `json:"panics,omitempty"`
}

// Decode reads and validates one record. It never reads more than
// MaxBodyBytes+1 bytes.
func Decode(r io.Reader) (Record, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxBodyBytes+1))
	if err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if len(raw) > MaxBodyBytes {
		return Record{}, ErrTooLarge
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var rec Record
	if err := decoder.Decode(&rec); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Record{}, fmt.Errorf("%w: trailing data", ErrInvalid)
	}
	if err := rec.Validate(); err != nil {
		return Record{}, err
	}
	return rec, nil
}

// Validate checks every field against its allow-list.
func (r Record) Validate() error {
	bad := func(what string) error { return fmt.Errorf("%w: %s", ErrInvalid, what) }
	switch {
	case r.Version != Version:
		return bad("version")
	case !buildPattern.MatchString(r.Build):
		return bad("build")
	case !localePattern.MatchString(r.Locale):
		return bad("locale")
	case r.Reason != ReasonPageHide && r.Reason != ReasonError:
		return bad("reason")
	case !sessionPattern.MatchString(r.Session):
		return bad("session")
	case len(r.Marks) > MaxMarks:
		return bad("marks")
	case len(r.Panics) > MaxPanicSites:
		return bad("panics")
	}
	for name, ms := range r.Marks {
		if !markPattern.MatchString(name) || ms < 0 || ms > maxMarkMillis {
			return bad("mark")
		}
	}
	for key, n := range r.Stream {
		if !StreamKeys[key] || n < 0 || n > maxCount {
			return bad("stream")
		}
	}
	for site, n := range r.Panics {
		if !sitePattern.MatchString(site) || n < 0 || n > maxCount {
			return bad("panic site")
		}
	}
	return nil
}

// SessionHash is the tenant-scoped, opaque session label that is logged in
// place of the client's own nonce. The tenant is part of the hash, so the same
// nonce in two tenants gives two labels, and neither input can be read back.
func SessionHash(tenant, session string) string {
	sum := sha256.Sum256([]byte("hcm-chat-rum\x00" + tenant + "\x00" + session))
	return hex.EncodeToString(sum[:8])
}

// Attrs are the structured log attributes for one accepted record. The caller
// supplies the session hash, never the raw nonce.
func (r Record) Attrs(sessionHash string) []any {
	return []any{
		"session", sessionHash,
		"build", r.Build,
		"locale", r.Locale,
		"reason", r.Reason,
		"marks", flatten(r.Marks),
		"stream", flatten(r.Stream),
		"panic_sites", flatten(r.Panics),
	}
}

func flatten(values map[string]int) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, values[key]))
	}
	return strings.Join(parts, ",")
}

// Limiter caps beacons per session inside a window and bounds its own table.
// A full table refuses new sessions instead of growing, so a flood of fresh
// nonces cannot cost memory.
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	maxKeys int
	now     func() time.Time
	seen    map[string]*limitEntry
}

type limitEntry struct {
	start time.Time
	count int
}

// NewLimiter allows max calls per key per window across at most maxKeys keys.
func NewLimiter(max int, window time.Duration, maxKeys int, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{max: max, window: window, maxKeys: maxKeys, now: now, seen: map[string]*limitEntry{}}
}

// Allow reports whether this key may send now, and counts the call.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry, ok := l.seen[key]
	if ok && now.Sub(entry.start) >= l.window {
		delete(l.seen, key)
		ok = false
	}
	if !ok {
		if len(l.seen) >= l.maxKeys {
			for k, e := range l.seen {
				if now.Sub(e.start) >= l.window {
					delete(l.seen, k)
				}
			}
			if len(l.seen) >= l.maxKeys {
				return false
			}
		}
		l.seen[key] = &limitEntry{start: now, count: 1}
		return l.max > 0
	}
	if entry.count >= l.max {
		return false
	}
	entry.count++
	return true
}
