// Package configuration owns the configuration-semantics contracts
// (ARCH-GO-024): definition, snapshot, bundle, dependency, diff,
// validation, approval, activation, rollback and registry. Authored
// definitions are inputs; snapshots and bundles are immutable
// content-addressed values; activation and rollback bind an exact bundle
// digest to a sealed approval so customer configuration can never bypass
// publication and approval. Configuration selects behaviour: this package
// performs no domain transaction, owns no persistence and depends on no
// domain, workflow or store package.
package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Definition references one authored input (a file beneath definitions/).
type Definition struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Snapshot is one immutable content-addressed definition capture.
type Snapshot struct {
	Definition Definition `json:"definition"`
	Digest     string     `json:"digest"`
}

// Dependency pins one prerequisite bundle digest.
type Dependency struct {
	Bundle string `json:"bundle"`
}

// Bundle is one immutable ordered set of snapshots plus dependencies.
type Bundle struct {
	Name      string       `json:"name"`
	Snapshots []Snapshot   `json:"snapshots"`
	Deps      []Dependency `json:"deps"`
	Digest    string       `json:"digest"`
}

// Approval is signed evidence from an external approval authority. This
// package verifies it through an injected verifier; it never mints approval.
type Approval struct {
	Bundle    string `json:"bundle"`
	Action    string `json:"action"`
	Approver  string `json:"approver"`
	Authority string `json:"authority"`
	Expiry    string `json:"expiry"`
	Signature []byte `json:"signature"`
}

// ApprovalVerifier verifies the authority signature over an Approval's
// bundle, approver, authority and expiry fields.
type ApprovalVerifier interface {
	Verify(Approval) error
}

// ApprovalVerifierFunc adapts a function to ApprovalVerifier.
type ApprovalVerifierFunc func(Approval) error

func (f ApprovalVerifierFunc) Verify(approval Approval) error { return f(approval) }

// ActivationRecord binds an activation or rollback to its bundle digest and
// records the separately authorized actor and externally verified approval.
type ActivationRecord struct {
	Bundle      string    `json:"bundle"`
	ActivatedBy string    `json:"activated_by"`
	ActivatedAt time.Time `json:"activated_at"`
	Approval    Approval  `json:"approval"`
	Rollback    bool      `json:"rollback"`
}

// Change is one deterministic bundle diff entry.
type Change string

// Finding is one exact bundle validation failure.
type Finding struct {
	Code   string `json:"code"`
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

func (f Finding) String() string { return f.Code + "|" + f.Field + "|" + f.Detail }

// Finding codes.
const (
	ApprovalActionActivate = "ACTIVATE"
	ApprovalActionRollback = "ROLLBACK"

	UnknownBundle         = "UNKNOWN_BUNDLE"
	UnresolvedDependency  = "UNRESOLVED_DEPENDENCY"
	MissingApproval       = "MISSING_APPROVAL"
	ForeignApproval       = "FOREIGN_APPROVAL"
	BrokenSeal            = "BROKEN_SEAL"
	UnknownActivation     = "UNKNOWN_ACTIVATION"
	UnknownRollbackTarget = "UNKNOWN_ROLLBACK_TARGET"
	InvalidApproval       = "INVALID_APPROVAL"
	ExpiredApproval       = "EXPIRED_APPROVAL"
	SelfApproval          = "SELF_APPROVAL"
)

var (
	ErrInvalidApproval = errors.New("configuration: invalid approval")
	ErrExpiredApproval = errors.New("configuration: expired approval")
	ErrSelfApproval    = errors.New("configuration: approver and activator must differ")
)

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SnapshotDefinition captures one authored definition immutably.
func SnapshotDefinition(def Definition, content []byte) (Snapshot, error) {
	if strings.TrimSpace(def.Kind) == "" || strings.TrimSpace(def.Name) == "" {
		return Snapshot{}, fmt.Errorf("definition kind and name are required")
	}
	if len(content) == 0 {
		return Snapshot{}, fmt.Errorf("definition content is required")
	}
	return Snapshot{
		Definition: def,
		Digest:     digest("snapshot", def.Kind, def.Name, string(content)),
	}, nil
}

// Registry holds bundles, approvals and the activation log.
type Registry struct {
	bundles   map[string]Bundle
	approvals map[string]Approval
	log       []ActivationRecord
}

// NewRegistry returns an empty registry.
func NewRegistry() Registry {
	return Registry{bundles: make(map[string]Bundle), approvals: make(map[string]Approval)}
}

// Assemble freezes one bundle. Every dependency digest must already
// resolve in the registry; rollout targets these immutable digests.
func (r *Registry) Assemble(name string, snaps []Snapshot, deps []Dependency) (Bundle, error) {
	if strings.TrimSpace(name) == "" {
		return Bundle{}, fmt.Errorf("bundle name is required")
	}
	if len(snaps) == 0 {
		return Bundle{}, fmt.Errorf("at least one snapshot is required")
	}
	for _, dep := range deps {
		if _, ok := r.bundles[dep.Bundle]; !ok {
			return Bundle{}, fmt.Errorf("unresolved dependency %q", dep.Bundle)
		}
	}
	ordered := append([]Snapshot(nil), snaps...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Definition.Kind != ordered[j].Definition.Kind {
			return ordered[i].Definition.Kind < ordered[j].Definition.Kind
		}
		return ordered[i].Definition.Name < ordered[j].Definition.Name
	})
	parts := []string{"bundle", name}
	for _, snap := range ordered {
		parts = append(parts, snap.Digest)
	}
	depDigests := make([]string, 0, len(deps))
	for _, dep := range deps {
		depDigests = append(depDigests, dep.Bundle)
	}
	sort.Strings(depDigests)
	parts = append(parts, depDigests...)
	bundle := Bundle{Name: name, Snapshots: ordered, Deps: deps, Digest: digest(parts...)}
	r.bundles[bundle.Digest] = bundle
	return bundle, nil
}

// RecordApproval records externally signed approval for a known bundle. The
// verifier is supplied by the control plane's trusted approval authority.
func (r *Registry) RecordApproval(approval Approval, verifier ApprovalVerifier, now time.Time) error {
	if _, ok := r.bundles[approval.Bundle]; !ok {
		return fmt.Errorf("cannot approve unknown bundle %q: %w", approval.Bundle, ErrInvalidApproval)
	}
	if verifier == nil || (approval.Action != ApprovalActionActivate && approval.Action != ApprovalActionRollback) || strings.TrimSpace(approval.Approver) == "" || strings.TrimSpace(approval.Authority) == "" || len(approval.Signature) == 0 {
		return ErrInvalidApproval
	}
	expires, err := time.Parse(time.RFC3339, strings.TrimSpace(approval.Expiry))
	if err != nil {
		return fmt.Errorf("approval expiry is not RFC3339: %w", ErrInvalidApproval)
	}
	if !now.Before(expires) {
		return ErrExpiredApproval
	}
	if err := verifier.Verify(approval); err != nil {
		return fmt.Errorf("approval signature verification failed: %w", ErrInvalidApproval)
	}
	approval.Signature = append([]byte(nil), approval.Signature...)
	r.approvals[approvalKey(approval.Bundle, approval.Action)] = approval
	return nil
}

func approvalKey(bundle, action string) string { return bundle + "\x00" + action }

// Activate binds an exact bundle digest to a previously verified approval.
// Approval and activation must be performed by distinct principals.
func (r *Registry) Activate(bundleDigest, activatedBy string, at time.Time) (ActivationRecord, error) {
	return r.activate(bundleDigest, activatedBy, at, ApprovalActionActivate)
}

func (r *Registry) activate(bundleDigest, activatedBy string, at time.Time, action string) (ActivationRecord, error) {
	if _, ok := r.bundles[bundleDigest]; !ok {
		return ActivationRecord{}, fmt.Errorf("cannot activate unknown bundle %q", bundleDigest)
	}
	approval, ok := r.approvals[approvalKey(bundleDigest, action)]
	if !ok {
		return ActivationRecord{}, fmt.Errorf("activation requires a verified approval: %w", ErrInvalidApproval)
	}
	if strings.TrimSpace(activatedBy) == "" {
		return ActivationRecord{}, fmt.Errorf("activator is required: %w", ErrInvalidApproval)
	}
	if activatedBy == approval.Approver {
		return ActivationRecord{}, ErrSelfApproval
	}
	expires, err := time.Parse(time.RFC3339, approval.Expiry)
	if err != nil || !at.Before(expires) {
		return ActivationRecord{}, ErrExpiredApproval
	}
	record := ActivationRecord{Bundle: bundleDigest, ActivatedBy: activatedBy, ActivatedAt: at, Approval: approval}
	r.log = append(r.log, record)
	delete(r.approvals, approvalKey(bundleDigest, action))
	return record, nil
}

// Rollback binds one prior bundle digest to an externally verified rollback
// approval and logs the activation. Both digests must resolve; the approval
// must specifically authorize rollback of the target.
func (r *Registry) Rollback(from, to, activatedBy string, at time.Time) (ActivationRecord, error) {
	if _, ok := r.bundles[from]; !ok {
		return ActivationRecord{}, fmt.Errorf("cannot roll back unknown bundle %q", from)
	}
	if _, ok := r.bundles[to]; !ok {
		return ActivationRecord{}, fmt.Errorf("cannot roll back to unknown bundle %q", to)
	}
	record, err := r.activate(to, activatedBy, at, ApprovalActionRollback)
	if err != nil {
		return ActivationRecord{}, err
	}
	record.Rollback = true
	r.log[len(r.log)-1] = record
	return record, nil
}

// Validate reports exact findings for one bundle digest.
func (r *Registry) Validate(bundleDigest string) []Finding {
	bundle, ok := r.bundles[bundleDigest]
	if !ok {
		return []Finding{{Code: UnknownBundle, Field: "bundle", Detail: fmt.Sprintf("unknown bundle %q", bundleDigest)}}
	}
	var findings []Finding
	for i, dep := range bundle.Deps {
		if _, ok := r.bundles[dep.Bundle]; !ok {
			findings = append(findings, Finding{Code: UnresolvedDependency, Field: fmt.Sprintf("deps[%d]", i), Detail: fmt.Sprintf("unresolved dependency %q", dep.Bundle)})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].String() < findings[j].String() })
	return findings
}

// Diff compares two known bundles snapshot by snapshot, deterministically.
func (r *Registry) Diff(before, after string) []Change {
	oldBundle, ok := r.bundles[before]
	if !ok {
		return []Change{Change(fmt.Sprintf("unknown bundle %q", before))}
	}
	newBundle, ok := r.bundles[after]
	if !ok {
		return []Change{Change(fmt.Sprintf("unknown bundle %q", after))}
	}
	oldSnaps := make(map[string]string, len(oldBundle.Snapshots))
	for _, snap := range oldBundle.Snapshots {
		oldSnaps[snap.Definition.Kind+"/"+snap.Definition.Name] = snap.Digest
	}
	var changes []Change
	seen := make(map[string]bool, len(newBundle.Snapshots))
	for _, snap := range newBundle.Snapshots {
		key := snap.Definition.Kind + "/" + snap.Definition.Name
		seen[key] = true
		oldDigest, existed := oldSnaps[key]
		switch {
		case !existed:
			changes = append(changes, Change("added "+key))
		case oldDigest != snap.Digest:
			changes = append(changes, Change("changed "+key))
		}
	}
	for _, snap := range oldBundle.Snapshots {
		key := snap.Definition.Kind + "/" + snap.Definition.Name
		if !seen[key] {
			changes = append(changes, Change("removed "+key))
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i] < changes[j] })
	return changes
}
