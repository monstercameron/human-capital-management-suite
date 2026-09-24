// Package documentsecurity owns the trust boundary for uploaded documents.
// Raw uploads never become consumable artifacts: only a scanner-produced,
// content-addressed derivative with a SAFE verdict can be admitted.
package documentsecurity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type State string

const (
	Safe        State = "SAFE"
	Unsafe      State = "UNSAFE"
	Unscannable State = "UNSCANNABLE"
	Quarantined State = "QUARANTINED"
)

var (
	ErrNotSafe        = errors.New("document is not safe for use")
	ErrInvalidUpload  = errors.New("invalid document upload")
	ErrLimitExceeded  = errors.New("document upload exceeds configured limit")
	ErrDigestMismatch = errors.New("document digest mismatch")
)

// Limits are recorded with every scan decision so a decision is reproducible.
type Limits struct {
	MaxBytes            int64
	MaxDerivativeBytes  int64
	MaxCompressionRatio int64
}

func (l Limits) valid() bool {
	return l.MaxBytes > 0 && l.MaxDerivativeBytes > 0 && l.MaxCompressionRatio > 0
}

type Upload struct {
	ID          string
	Name        string
	ContentType string
	Bytes       []byte
}

type ScanInput struct {
	ID, Name, ContentType, Digest string
	Bytes                         []byte
}

type Verdict struct {
	State            State
	Scanner          string
	ScannerVersion   string
	Reason           string
	Derivative       []byte
	DerivativeDigest string
}

// Scanner is deliberately a narrow provider boundary. Implementations may
// call an AV service, but this package has no scanner/network dependency.
type Scanner interface {
	Scan(context.Context, ScanInput, Limits) (Verdict, error)
}

type Artifact struct {
	ID, Name, ContentType           string
	DetectedContentType             string
	OriginalDigest                  string
	DerivativeDigest                string
	State                           State
	Scanner, ScannerVersion, Reason string
	Limits                          Limits
}

// Validator is the fail-closed gate used by preview, extraction, indexing and
// workflow consumers. It contains no access to raw upload bytes.
type Validator struct{}

func (Validator) Validate(a Artifact) error {
	if a.State != Safe || a.ID == "" || a.OriginalDigest == "" || a.DerivativeDigest == "" ||
		a.Scanner == "" || a.ScannerVersion == "" || !a.Limits.valid() {
		return fmt.Errorf("%w: state=%s", ErrNotSafe, a.State)
	}
	if !digestString(a.OriginalDigest) || !digestString(a.DerivativeDigest) {
		return fmt.Errorf("%w: malformed content address", ErrDigestMismatch)
	}
	return nil
}

// Registry is an in-memory model useful to adapters and tests. Production
// storage can persist the same immutable Artifact record and quarantine event.
type Registry struct {
	mu      sync.RWMutex
	records map[string]Artifact
}

func NewRegistry() *Registry { return &Registry{records: make(map[string]Artifact)} }

func (r *Registry) Scan(ctx context.Context, u Upload, limits Limits, scanner Scanner) (Artifact, error) {
	if r == nil || scanner == nil || u.ID == "" || len(u.Bytes) == 0 || !limits.valid() {
		return Artifact{}, ErrInvalidUpload
	}
	original := digest(u.Bytes)
	a := Artifact{ID: u.ID, Name: u.Name, ContentType: u.ContentType, OriginalDigest: original,
		State: Quarantined, Limits: limits}
	r.mu.Lock()
	r.records[u.ID] = a
	r.mu.Unlock()
	if int64(len(u.Bytes)) > limits.MaxBytes {
		a.State, a.Reason = Unsafe, "upload exceeds maximum bytes"
		r.put(a)
		return a, ErrLimitExceeded
	}
	v, err := scanner.Scan(ctx, ScanInput{ID: u.ID, Name: u.Name, ContentType: u.ContentType, Digest: original, Bytes: append([]byte(nil), u.Bytes...)}, limits)
	if err != nil {
		a.State, a.Reason = Unscannable, err.Error()
		r.put(a)
		return a, nil
	}
	a.State, a.Scanner, a.ScannerVersion, a.Reason = v.State, v.Scanner, v.ScannerVersion, v.Reason
	if a.State != Safe && a.State != Unsafe && a.State != Unscannable && a.State != Quarantined {
		a.State, a.Reason = Unscannable, "scanner returned unknown state"
	}
	if a.State == Safe {
		if a.Scanner == "" || a.ScannerVersion == "" {
			a.State, a.Reason = Unscannable, "scanner identity/version missing"
		} else if len(v.Derivative) == 0 || int64(len(v.Derivative)) > limits.MaxDerivativeBytes {
			a.State, a.Reason = Unscannable, "scanner did not produce a bounded derivative"
		} else {
			a.DerivativeDigest = digest(v.Derivative)
			a.DetectedContentType = strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(v.Derivative), ";")[0]))
			if v.DerivativeDigest != "" && v.DerivativeDigest != a.DerivativeDigest {
				a.State, a.Reason = Unscannable, "derivative digest mismatch"
			}
		}
	}
	r.put(a)
	if a.State != Safe {
		return a, nil
	}
	return a, nil
}

func (r *Registry) Get(id string) (Artifact, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.records[id]
	return a, ok
}

// Rescan replaces the prior verdict. Any previously SAFE artifact is revoked
// immediately while the new scan is pending; unsafe results therefore revoke
// all descendants without rewriting historical records.
func (r *Registry) Rescan(ctx context.Context, id string, scanner Scanner) (Artifact, error) {
	a, ok := r.Get(id)
	if !ok {
		return Artifact{}, ErrInvalidUpload
	}
	a.State, a.Reason, a.DerivativeDigest = Quarantined, "rescan pending", ""
	r.put(a)
	// The original bytes are intentionally not retained by this model. A storage
	// adapter must fetch them by digest and supply a fresh upload to Scan.
	return a, fmt.Errorf("%w: rescan requires source bytes", ErrNotSafe)
}

// RescanUpload performs a fresh scan from source bytes. The old verdict is
// first revoked, so a failed or unavailable rescan cannot leave descendants
// usable. The returned record has the same stable upload identity.
func (r *Registry) RescanUpload(ctx context.Context, u Upload, limits Limits, scanner Scanner) (Artifact, error) {
	if r == nil || scanner == nil || u.ID == "" {
		return Artifact{}, ErrInvalidUpload
	}
	if old, ok := r.Get(u.ID); ok {
		old.State, old.Reason, old.DerivativeDigest = Quarantined, "rescan pending", ""
		r.put(old)
	}
	return r.Scan(ctx, u, limits, scanner)
}

// Revoke makes an artifact and any derivative reference unusable immediately.
func (r *Registry) Revoke(id, reason string) bool {
	a, ok := r.Get(id)
	if !ok {
		return false
	}
	a.State, a.Reason, a.DerivativeDigest = Quarantined, reason, ""
	r.put(a)
	return true
}

func (r *Registry) put(a Artifact) { r.mu.Lock(); r.records[a.ID] = a; r.mu.Unlock() }
func digest(b []byte) string       { s := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(s[:]) }
func digestString(s string) bool {
	p := strings.TrimPrefix(s, "sha256:")
	if len(p) != 64 {
		return false
	}
	_, e := hex.DecodeString(p)
	return e == nil
}
