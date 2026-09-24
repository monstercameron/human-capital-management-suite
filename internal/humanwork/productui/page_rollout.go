package productui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// RolloutScope is one rollout target: the organization-applicability
// scope receiving the revision and the epoch second it goes live.
// Zero EffectiveFrom means live at publication. Scope strings are
// declared, never validated, here: organization existence stays
// with the organization domain, and presentation must not grow a
// second scope authority.
type RolloutScope struct {
	Scope         string `json:"scope"`
	EffectiveFrom int64  `json:"effective_from"`
}

// PageRollout binds one immutable published revision to its staged
// publication: which scopes receive it and when. Version plus digest
// pin the target revision; RecordVersion is the append-order key and
// lets a rollback target an older revision without rewriting history.
// Rollout never carries content of its
// own. A newer revision's rollout supersedes per scope once live —
// rollout advances forward only, and moving a scope backward is
// rollback (a later lifecycle step), never a rollout edit.
type PageRollout struct {
	Page          PageID         `json:"page"`
	RecordVersion int64          `json:"record_version,omitempty"`
	Version       int64          `json:"version"`
	Digest        string         `json:"digest"`
	Scopes        []RolloutScope `json:"scopes"`
}

type persistedPageRollout struct {
	Rollout PageRollout `json:"rollout"`
	Digest  string      `json:"integrity_digest"`
}

func clonePageRollout(rollout PageRollout) PageRollout {
	rollout.Scopes = append([]RolloutScope(nil), rollout.Scopes...)
	return rollout
}

// MarshalPageRollout serializes a rollout with an integrity digest over its
// complete immutable payload, including scopes and effective dates.
func MarshalPageRollout(rollout PageRollout) []byte {
	encoded, err := json.Marshal(rollout)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(encoded)
	wrapped, err := json.Marshal(persistedPageRollout{Rollout: clonePageRollout(rollout), Digest: hex.EncodeToString(sum[:])})
	if err != nil {
		return nil
	}
	return wrapped
}

// ParsePageRollout fails closed on malformed or modified stored rollout bytes.
func ParsePageRollout(encoded []byte) (PageRollout, error) {
	var stored persistedPageRollout
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return PageRollout{}, fmt.Errorf("productui: rollout bytes do not parse: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return PageRollout{}, fmt.Errorf("productui: trailing rollout bytes")
	}
	canonical, err := json.Marshal(stored.Rollout)
	if err != nil {
		return PageRollout{}, fmt.Errorf("productui: rollout bytes do not encode: %w", err)
	}
	sum := sha256.Sum256(canonical)
	if stored.Digest == "" || stored.Digest != hex.EncodeToString(sum[:]) {
		return PageRollout{}, fmt.Errorf("productui: rollout integrity digest mismatch for page %q version %d", stored.Rollout.Page, stored.Rollout.Version)
	}
	return clonePageRollout(stored.Rollout), nil
}

// RecordRollout appends a validated, digest-pinned rollout for this log's
// tenant. A page/version key is immutable and repeated identical writes are safe.
func (log *PageRevisionLog) RecordRollout(rollout PageRollout) error {
	return log.RecordRolloutContext(context.Background(), rollout)
}

// RecordRolloutContext persists a rollout before exposing it in the log.
func (log *PageRevisionLog) RecordRolloutContext(ctx context.Context, rollout PageRollout) error {
	_, err := log.RecordRolloutEventContext(ctx, rollout)
	return err
}

// RecordRolloutEventContext persists one rollout and returns its assigned
// append sequence, which orders rollback events independently of target version.
func (log *PageRevisionLog) RecordRolloutEventContext(ctx context.Context, rollout PageRollout) (PageRollout, error) {
	return log.recordRolloutEventContext(ctx, rollout, false)
}

// RecordRollbackEventContext appends an explicit rollback event. Rollbacks
// may target a revision that already has a rollout event, so they receive a
// new record sequence even when their target and scopes match older history.
func (log *PageRevisionLog) RecordRollbackEventContext(ctx context.Context, rollout PageRollout) (PageRollout, error) {
	return log.recordRolloutEventContext(ctx, rollout, true)
}

func (log *PageRevisionLog) recordRolloutEventContext(ctx context.Context, rollout PageRollout, rollback bool) (PageRollout, error) {
	if verdict := ValidatePageRollout(rollout); !verdict.Compatible {
		return PageRollout{}, fmt.Errorf("productui: invalid rollout: %v", verdict.Reasons)
	}
	if err := VerifyRolloutTarget(log, rollout); err != nil {
		return PageRollout{}, err
	}
	for _, existing := range log.rollouts {
		if existing.Page == rollout.Page && existing.Version == rollout.Version {
			if existing.Digest != rollout.Digest || !equalRolloutScopes(existing.Scopes, rollout.Scopes) {
				if !rollback {
					return PageRollout{}, fmt.Errorf("productui: rollout conflict for page %q version %d", rollout.Page, rollout.Version)
				}
			} else if !rollback {
				return clonePageRollout(existing), nil
			}
		}
	}
	rollout.RecordVersion = 1
	for _, existing := range log.rollouts {
		if existing.Page == rollout.Page && existing.RecordVersion >= rollout.RecordVersion {
			rollout.RecordVersion = existing.RecordVersion + 1
		}
	}
	if log.store != nil {
		if err := log.store.PutRollout(ctx, log.tenant, string(rollout.Page), rollout.RecordVersion, rollout.Version, rollout.Digest, MarshalPageRollout(rollout)); err != nil {
			return PageRollout{}, fmt.Errorf("productui: persist rollout: %w", err)
		}
	}
	log.rollouts = append(log.rollouts, clonePageRollout(rollout))
	return clonePageRollout(rollout), nil
}

func equalRolloutScopes(first, second []RolloutScope) bool {
	if len(first) != len(second) {
		return false
	}
	for i := range first {
		if first[i] != second[i] {
			return false
		}
	}
	return true
}

// Rollouts returns defensive copies of the tenant-bound persisted rollout set.
func (log *PageRevisionLog) Rollouts() []PageRollout {
	result := make([]PageRollout, len(log.rollouts))
	for i, rollout := range log.rollouts {
		result[i] = clonePageRollout(rollout)
	}
	return result
}

// RolloutVerdict is the rollout answer: compatible plus the stable
// reasons, in validation order, when not. Reasons stay nil on
// success.
type RolloutVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidatePageRollout checks one rollout structurally: a named
// page, a positive revision version, a pinning digest, at least one
// scope, no blank or duplicate scopes, and no negative effective
// dates. Violations accumulate in fixed order: page, version,
// digest, scope presence, then per-scope reasons in rollout order.
func ValidatePageRollout(rollout PageRollout) RolloutVerdict {
	var reasons []string
	if rollout.Page == "" {
		reasons = append(reasons, "missing rollout page")
	}
	if rollout.Version <= 0 {
		reasons = append(reasons, fmt.Sprintf("rollout revision version must be positive, got %d", rollout.Version))
	}
	if rollout.Digest == "" {
		reasons = append(reasons, "missing rollout digest")
	}
	if len(rollout.Scopes) == 0 {
		reasons = append(reasons, "rollout names no scopes")
	}
	seen := map[string]bool{}
	for _, scope := range rollout.Scopes {
		switch {
		case scope.Scope == "":
			reasons = append(reasons, "blank rollout scope")
		case seen[scope.Scope]:
			reasons = append(reasons, fmt.Sprintf("duplicate rollout scope %q", scope.Scope))
		default:
			seen[scope.Scope] = true
		}
		if scope.EffectiveFrom < 0 {
			reasons = append(reasons, fmt.Sprintf("negative effective date for scope %q", scope.Scope))
		}
	}
	if len(reasons) > 0 {
		return RolloutVerdict{Compatible: false, Reasons: reasons}
	}
	return RolloutVerdict{Compatible: true}
}

// VerifyRolloutTarget binds one rollout to an actually-published
// revision: the page and version must exist in the revision log
// and the digest must match. Unknown or tampered targets refuse.
// Durable rollout state stays with the studio platform; this is
// the presentation-side target check before a rollout publishes.
func VerifyRolloutTarget(log *PageRevisionLog, rollout PageRollout) error {
	recorded, ok := log.Revision(rollout.Page, rollout.Version)
	if !ok {
		return fmt.Errorf("productui: rollout targets unpublished revision for page %q version %d", rollout.Page, rollout.Version)
	}
	if rollout.Digest == "" || rollout.Digest != recorded.Digest {
		return fmt.Errorf("productui: rollout digest mismatch for page %q version %d", rollout.Page, rollout.Version)
	}
	return nil
}

// RolloutLiveAt reports whether one rollout serves its revision to
// a scope at one epoch second: the scope must be listed and its
// effective date reached. The query is mechanical — publish only
// validated, target-verified rollouts.
func RolloutLiveAt(rollout PageRollout, scope string, now int64) bool {
	for _, target := range rollout.Scopes {
		if target.Scope == scope {
			return now >= target.EffectiveFrom
		}
	}
	return false
}
