// Package iacdrift owns the provider-neutral IAC-011 policy contract:
// scheduled drift reports that map every change to an owner and
// capability, and two-person break-glass protection for destructive
// changes to protected resources. It is a pure policy package: no
// database, cloud SDK, or mutable global is involved.
package iacdrift

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Error codes are exact: CI and operators match on them.
const (
	CodeDriftDetected      = "DRIFT_DETECTED"
	CodeBreakGlassRequired = "BREAK_GLASS_REQUIRED"
	CodeMissingField       = "MISSING_DRIFT_FIELD"
)

// Resource is the observed or desired provider-neutral surface of one
// infrastructure resource.
type Resource struct {
	ID           string
	Kind         string
	Owner        string
	Capability   string
	ConfigDigest string
	Protected    bool
}

// DriftEntry maps one detected change to its owner and capability.
type DriftEntry struct {
	ResourceID string
	Kind       string
	Owner      string
	Capability string
	From       string
	To         string
	Added      bool
	Removed    bool
	Protected  bool
}

// Report is a scheduled drift report over one cell inventory.
type Report struct {
	Entries []DriftEntry
	Digest  string
}

// Detect compares desired against observed state. Console mutations,
// state mismatches, additions and removals all surface as entries;
// identical inventories yield an empty report.
func Detect(desired, observed []Resource) (Report, error) {
	want := map[string]Resource{}
	for _, resource := range desired {
		if strings.TrimSpace(resource.ID) == "" || strings.TrimSpace(resource.Owner) == "" || strings.TrimSpace(resource.Capability) == "" {
			return Report{}, errors.New("iacdrift: " + CodeMissingField + ": desired resource id, owner and capability are required")
		}
		want[resource.ID] = resource
	}
	var entries []DriftEntry
	for _, resource := range observed {
		if strings.TrimSpace(resource.ID) == "" {
			return Report{}, errors.New("iacdrift: " + CodeMissingField + ": observed resource id is required")
		}
		prior, tracked := want[resource.ID]
		delete(want, resource.ID)
		switch {
		case !tracked:
			entries = append(entries, DriftEntry{ResourceID: resource.ID, Kind: resource.Kind, Owner: resource.Owner, Capability: resource.Capability, To: resource.ConfigDigest, Added: true, Protected: resource.Protected})
		case prior.Kind != resource.Kind || prior.ConfigDigest != resource.ConfigDigest:
			entries = append(entries, DriftEntry{ResourceID: resource.ID, Kind: resource.Kind, Owner: prior.Owner, Capability: prior.Capability, From: prior.ConfigDigest, To: resource.ConfigDigest, Protected: prior.Protected})
		}
	}
	for _, missing := range want {
		entries = append(entries, DriftEntry{ResourceID: missing.ID, Kind: missing.Kind, Owner: missing.Owner, Capability: missing.Capability, From: missing.ConfigDigest, Removed: true, Protected: missing.Protected})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ResourceID < entries[j].ResourceID })
	return Report{Entries: entries, Digest: reportDigest(entries)}, nil
}

func reportDigest(entries []DriftEntry) string {
	parts := []string{"iac011-drift-report"}
	for _, entry := range entries {
		parts = append(parts, strings.Join([]string{entry.ResourceID, entry.Kind, entry.Owner, entry.Capability, entry.From, entry.To, fmt.Sprint(entry.Added), fmt.Sprint(entry.Removed), fmt.Sprint(entry.Protected)}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DestructiveChange describes a proposed destructive operation.
type DestructiveChange struct {
	Destroy     bool
	Replacement bool
}

// BreakGlass is the explicit two-person authorization a destructive
// change to a protected resource requires. It is scoped to one
// resource: approval for one protected resource never transfers to
// another.
type BreakGlass struct {
	ResourceID       string
	ApproverOne      string
	ApproverTwo      string
	Reason           string
	ExpiresAt        string
	RecoveryEvidence string
}

// AdmitDestructive gates destructive changes: unprotected resources
// pass, protected resources require two distinct approvers, a reason,
// an expiry, and recovery evidence.
func AdmitDestructive(entry DriftEntry, change DestructiveChange, glass BreakGlass) error {
	if !change.Destroy && !change.Replacement {
		return nil
	}
	if !entryProtected(entry) {
		return nil
	}
	var missing []string
	if strings.TrimSpace(glass.ResourceID) == "" || glass.ResourceID != entry.ResourceID {
		missing = append(missing, "break-glass is scoped to its named resource")
	}
	if strings.TrimSpace(glass.ApproverOne) == "" || strings.TrimSpace(glass.ApproverTwo) == "" {
		missing = append(missing, "two approvers are required")
	}
	if glass.ApproverOne != "" && glass.ApproverOne == glass.ApproverTwo {
		missing = append(missing, "approvers must be distinct people")
	}
	if strings.TrimSpace(glass.Reason) == "" {
		missing = append(missing, "reason is required")
	}
	if strings.TrimSpace(glass.ExpiresAt) == "" {
		missing = append(missing, "expiry is required")
	}
	if strings.TrimSpace(glass.RecoveryEvidence) == "" {
		missing = append(missing, "recovery evidence is required")
	}
	if len(missing) > 0 {
		return fmt.Errorf("iacdrift: %s: protected resource %s: %s", CodeBreakGlassRequired, entry.ResourceID, strings.Join(missing, "; "))
	}
	return nil
}

// entryProtected reports whether the drifted resource is protected.
// The desired inventory flag governs; database, key and bucket kinds
// are additionally protected by default so an unmarked irreplaceable
// resource still gates destructive change.
func entryProtected(entry DriftEntry) bool {
	if entry.Protected {
		return true
	}
	switch entry.Kind {
	case "postgres", "secrets", "object":
		return true
	}
	return false
}
