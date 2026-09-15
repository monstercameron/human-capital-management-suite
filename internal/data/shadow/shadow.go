// Package shadow owns the pure compare-and-promote kernel for shadow
// projection versions (DATA-011). A shadow projection is rebuilt in an
// isolated namespace (see internal/data/rebuild); this package decides
// whether the shadow may replace the active pointer.
//
// Comparison reports exact differences across row counts, semantic digests,
// authorization behavior, replay completeness and lag. Only an approved,
// fully-replayed, compatible shadow promotes, and promotion retains the
// previous active version as the rollback pointer. Source history is never
// rewritten: the kernel holds pointers, not history.
//
// The kernel performs no I/O and consults no clock, database, or queue. All
// inputs arrive from the caller; persistence of the pointer is the caller's
// responsibility.
package shadow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	// ErrInvalidInput reports a malformed snapshot, approval or pointer.
	ErrInvalidInput = errors.New("shadow: invalid input")
	// ErrTenantMismatch reports a shadow built for a different tenant.
	ErrTenantMismatch = errors.New("shadow: tenant mismatch")
	// ErrIncompleteReplay reports a shadow whose replay did not reach the
	// source head.
	ErrIncompleteReplay = errors.New("shadow: incomplete replay")
	// ErrLagExceeded reports a shadow older than the promotion lag bound.
	ErrLagExceeded = errors.New("shadow: lag exceeds bound")
	// ErrComparisonGap reports exact active/shadow differences. The error
	// carries the full Comparison report as a *GapError.
	ErrComparisonGap = errors.New("shadow: active/shadow differences forbid promotion")
	// ErrNotApproved reports a missing, unapproved or self-approved
	// promotion approval.
	ErrNotApproved = errors.New("shadow: promotion is not approved")
	// ErrNoRollback reports a rollback request with no retained pointer.
	ErrNoRollback = errors.New("shadow: no rollback pointer retained")
)

// Snapshot is the caller-supplied comparison evidence for one projection
// version. Digest covers the semantic rows; AuthzDigest covers the
// authorization behavior exercised over those rows.
type Snapshot struct {
	Tenant         string
	Projection     string
	Watermark      int64
	Rows           int64
	Digest         string
	AuthzDigest    string
	ReplayComplete bool
	Lag            time.Duration
}

// Version is one immutable projection version named by the active pointer.
type Version struct {
	Digest    string
	Watermark int64
}

// Pointer is the switchable active pointer with its retained rollback.
type Pointer struct {
	Active   Version
	Rollback *Version
}

// Approval is the governed release decision for one promotion. Approver must
// name a real operator and must differ from Requester when a requester is
// recorded, so production activation can never approve itself.
type Approval struct {
	Approver  string
	Approved  bool
	Reason    string
	Requester string
}

// Comparison is the exact-difference report for one active/shadow pair.
// Compatible is true only when every difference class is empty.
type Comparison struct {
	ActiveProjection string   `json:"active_projection"`
	ShadowWatermark  int64    `json:"shadow_watermark"`
	Compatible       bool     `json:"compatible"`
	Differences      []string `json:"differences,omitempty"`
	Digest           string   `json:"digest"`
}

// GapError carries the Comparison report for an incompatible pair. It
// matches ErrComparisonGap under errors.Is.
type GapError struct {
	Report Comparison
}

// Error implements error.
func (e *GapError) Error() string {
	return fmt.Sprintf("%s: %s", ErrComparisonGap, strings.Join(e.Report.Differences, "; "))
}

// Is reports a match on the comparison-gap sentinel.
func (e *GapError) Is(target error) bool { return target == ErrComparisonGap }

// Compare reports the exact differences between the active and shadow
// snapshots. It returns a compatible Comparison, or the incompatible report
// wrapped as a *GapError matching ErrComparisonGap. Tenant isolation,
// replay completeness and the lag bound are gates, not differences: they
// return their typed errors before any comparison.
func Compare(active, shadow Snapshot, maxLag time.Duration) (Comparison, error) {
	if strings.TrimSpace(active.Tenant) == "" || strings.TrimSpace(shadow.Tenant) == "" ||
		strings.TrimSpace(active.Projection) == "" || strings.TrimSpace(shadow.Projection) == "" ||
		strings.TrimSpace(active.Digest) == "" || strings.TrimSpace(shadow.Digest) == "" {
		return Comparison{}, fmt.Errorf("%w: snapshot needs tenant, projection and digest", ErrInvalidInput)
	}
	if active.Tenant != shadow.Tenant {
		return Comparison{}, fmt.Errorf("%w: active %q vs shadow %q", ErrTenantMismatch, active.Tenant, shadow.Tenant)
	}
	if active.Projection != shadow.Projection {
		return Comparison{}, fmt.Errorf("%w: active %q vs shadow %q", ErrInvalidInput, active.Projection, shadow.Projection)
	}
	if !shadow.ReplayComplete {
		return Comparison{}, fmt.Errorf("%w: shadow watermark %d did not reach the source head", ErrIncompleteReplay, shadow.Watermark)
	}
	if maxLag < 0 {
		return Comparison{}, fmt.Errorf("%w: negative lag bound", ErrInvalidInput)
	}
	if shadow.Lag > maxLag {
		return Comparison{}, fmt.Errorf("%w: shadow lag %s exceeds bound %s", ErrLagExceeded, shadow.Lag, maxLag)
	}
	var differences []string
	if active.Rows != shadow.Rows {
		differences = append(differences, fmt.Sprintf("rows: active=%d shadow=%d", active.Rows, shadow.Rows))
	}
	if active.Digest != shadow.Digest {
		differences = append(differences, fmt.Sprintf("digest: active=%s shadow=%s", active.Digest, shadow.Digest))
	}
	if strings.TrimSpace(shadow.AuthzDigest) == "" {
		differences = append(differences, "authz: shadow has no authorization-behavior digest")
	} else if active.AuthzDigest != shadow.AuthzDigest {
		differences = append(differences, fmt.Sprintf("authz: active=%s shadow=%s", active.AuthzDigest, shadow.AuthzDigest))
	}
	if shadow.Watermark < active.Watermark {
		differences = append(differences, fmt.Sprintf("watermark: shadow=%d trails active=%d", shadow.Watermark, active.Watermark))
	}
	sort.Strings(differences)
	report := Comparison{
		ActiveProjection: strings.TrimSpace(active.Projection),
		ShadowWatermark:  shadow.Watermark,
		Compatible:       len(differences) == 0,
		Differences:      differences,
	}
	report.Digest = comparisonDigest(report)
	if !report.Compatible {
		return Comparison{}, &GapError{Report: report}
	}
	return report, nil
}

// Promote atomically switches the active pointer to the compared shadow
// version. Only a compatible comparison with an explicit, non-self approval
// promotes; the previous active version is retained as the rollback
// pointer. It never falls back to latest or active on a gap.
func Promote(pointer Pointer, shadow Snapshot, approval Approval, comparison Comparison) (Pointer, error) {
	if !approval.Approved || strings.TrimSpace(approval.Approver) == "" || strings.TrimSpace(approval.Reason) == "" {
		return Pointer{}, fmt.Errorf("%w: approval needs an approver, an approval flag and a reason", ErrNotApproved)
	}
	if strings.TrimSpace(approval.Requester) != "" && approval.Requester == approval.Approver {
		return Pointer{}, fmt.Errorf("%w: approver %q cannot approve their own promotion", ErrNotApproved, approval.Approver)
	}
	if !comparison.Compatible {
		return Pointer{}, fmt.Errorf("%w: %s", ErrComparisonGap, strings.Join(comparison.Differences, "; "))
	}
	if strings.TrimSpace(shadow.Digest) == "" {
		return Pointer{}, fmt.Errorf("%w: shadow version needs a digest", ErrInvalidInput)
	}
	previous := pointer.Active
	return Pointer{
		Active:   Version{Digest: strings.TrimSpace(shadow.Digest), Watermark: shadow.Watermark},
		Rollback: &previous,
	}, nil
}

// Rollback restores the retained rollback version as active. The restored
// pointer carries no further rollback: history is a single retained step,
// never a rewritten chain.
func Rollback(pointer Pointer) (Pointer, error) {
	if pointer.Rollback == nil {
		return Pointer{}, fmt.Errorf("%w: pointer was never promoted", ErrNoRollback)
	}
	return Pointer{Active: *pointer.Rollback}, nil
}

func comparisonDigest(report Comparison) string {
	canonical, _ := json.Marshal(struct {
		ActiveProjection string   `json:"active_projection"`
		ShadowWatermark  int64    `json:"shadow_watermark"`
		Differences      []string `json:"differences"`
	}{
		ActiveProjection: report.ActiveProjection,
		ShadowWatermark:  report.ShadowWatermark,
		Differences:      report.Differences,
	})
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}
