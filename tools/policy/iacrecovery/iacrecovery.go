// Package iacrecovery owns the provider-neutral IAC-010 policy contract:
// immutable recovery copies under a trust/admin boundary that production
// identities cannot reach, fenced recovery cells that cannot contact
// production, and a readiness gate that proves known-good data, config,
// and key references before traffic. It is a pure policy package: no
// database, cloud SDK, or mutable global is involved.
package iacrecovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ProductionBoundary names the trust boundary a compromised production
// identity operates in. Recovery stores never accept writes from it.
const ProductionBoundary = "production"

// Error codes are exact: CI and operators match on them.
const (
	CodeImmutableCopy        = "IMMUTABLE_COPY"
	CodeProductionReach      = "PRODUCTION_REACH"
	CodeMissingField         = "MISSING_COPY_FIELD"
	CodeProductionDependency = "PRODUCTION_DEPENDENCY"
	CodeUnknownCopyRef       = "UNKNOWN_COPY_REF"
	CodeReadinessBlocked     = "READINESS_BLOCKED"
)

// Copy is one immutable recovery copy. There is deliberately no update or
// delete API: once appended, a copy id can never be rebound.
type Copy struct {
	ID       string
	SourceID string
	Digest   string
	Locked   bool
}

// Store holds recovery copies under a dedicated trust/admin boundary.
type Store struct {
	TrustBoundary string
	AdminBoundary string
	copies        map[string]Copy
}

// NewStore admits a recovery store only under non-production,
// explicitly named trust and admin boundaries.
func NewStore(trustBoundary, adminBoundary string) (*Store, error) {
	if strings.TrimSpace(trustBoundary) == "" || strings.TrimSpace(adminBoundary) == "" {
		return nil, errors.New("iacrecovery: " + CodeMissingField + ": trust and admin boundaries are required")
	}
	if trustBoundary == ProductionBoundary || adminBoundary == ProductionBoundary {
		return nil, errors.New("iacrecovery: " + CodeProductionReach + ": recovery store must live outside the production boundary")
	}
	return &Store{TrustBoundary: trustBoundary, AdminBoundary: adminBoundary, copies: map[string]Copy{}}, nil
}

// Append records one immutable copy. Writes carrying the production
// boundary (a compromised production identity) are refused, as is any
// attempt to rebind an existing copy id.
func (s *Store) Append(copy Copy, actorBoundary string) error {
	if actorBoundary == ProductionBoundary {
		return errors.New("iacrecovery: " + CodeProductionReach + ": production identity cannot write recovery copies")
	}
	if strings.TrimSpace(copy.ID) == "" || strings.TrimSpace(copy.SourceID) == "" || strings.TrimSpace(copy.Digest) == "" {
		return errors.New("iacrecovery: " + CodeMissingField + ": copy id, source and digest are required")
	}
	if !copy.Locked {
		return errors.New("iacrecovery: " + CodeMissingField + ": copy must carry a retention lock")
	}
	if _, exists := s.copies[copy.ID]; exists {
		return errors.New("iacrecovery: " + CodeImmutableCopy + ": copy " + copy.ID + " is immutable")
	}
	s.copies[copy.ID] = copy
	return nil
}

// Lookup resolves a copy reference.
func (s *Store) Lookup(id string) (Copy, bool) {
	copy, ok := s.copies[id]
	return copy, ok
}

// Manifest exports the immutable copy set for isolated-store rebuilds.
func (s *Store) Manifest() []Copy {
	out := make([]Copy, 0, len(s.copies))
	for _, copy := range s.copies {
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Rebuild replays a manifest into a fresh store under the same
// boundaries; the result must reproduce the source store exactly.
func Rebuild(trustBoundary, adminBoundary string, manifest []Copy, actorBoundary string) (*Store, error) {
	store, err := NewStore(trustBoundary, adminBoundary)
	if err != nil {
		return nil, err
	}
	for _, copy := range manifest {
		if err := store.Append(copy, actorBoundary); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// Cell is a fenced recovery cell provisioned from immutable copies.
type Cell struct {
	ID           string
	CopyIDs      []string
	Dependencies []string
}

// ProvisionCell resolves copy references and refuses any dependency that
// reaches a production host: a restored cell can never contact
// production dependencies.
func ProvisionCell(store *Store, cellID string, copyIDs, dependencies, productionHosts []string) (Cell, error) {
	if strings.TrimSpace(cellID) == "" {
		return Cell{}, errors.New("iacrecovery: " + CodeMissingField + ": cell id is required")
	}
	if len(copyIDs) == 0 {
		return Cell{}, errors.New("iacrecovery: " + CodeMissingField + ": at least one copy reference is required")
	}
	production := map[string]bool{}
	for _, host := range productionHosts {
		production[strings.ToLower(strings.TrimSpace(host))] = true
	}
	for _, dep := range dependencies {
		if production[strings.ToLower(strings.TrimSpace(dep))] {
			return Cell{}, errors.New("iacrecovery: " + CodeProductionDependency + ": recovery cell cannot contact " + dep)
		}
	}
	resolved := append([]string(nil), copyIDs...)
	sort.Strings(resolved)
	for _, id := range resolved {
		if _, ok := store.Lookup(id); !ok {
			return Cell{}, errors.New("iacrecovery: " + CodeUnknownCopyRef + ": " + id)
		}
	}
	deps := append([]string(nil), dependencies...)
	sort.Strings(deps)
	return Cell{ID: cellID, CopyIDs: resolved, Dependencies: deps}, nil
}

// ReadinessInput carries the known-good references the traffic gate checks.
type ReadinessInput struct {
	ConfigDigest string
	DataDigest   string
	KeyRefs      []string
	KnownKeyRefs []string
}

// ReadinessReceipt proves the gate admission for one fenced cell.
type ReadinessReceipt struct {
	CellID string
	Digest string
}

// CheckReadiness proves known-good data/config/key references before
// traffic: digests must match the pinned copy digests and every key
// reference must resolve to a known-good key.
func CheckReadiness(cell Cell, store *Store, in ReadinessInput) (ReadinessReceipt, error) {
	want := map[string]string{}
	for _, id := range cell.CopyIDs {
		copy, ok := store.Lookup(id)
		if !ok {
			return ReadinessReceipt{}, fmt.Errorf("iacrecovery: %s: %s", CodeUnknownCopyRef, id)
		}
		want[copy.SourceID] = copy.Digest
	}
	knownKeys := map[string]bool{}
	for _, ref := range in.KnownKeyRefs {
		knownKeys[ref] = true
	}
	var blocked []string
	for source, digest := range want {
		switch {
		case strings.Contains(source, "config") && in.ConfigDigest != digest:
			blocked = append(blocked, "config digest does not match pinned copy")
		case !strings.Contains(source, "config") && in.DataDigest != digest:
			blocked = append(blocked, "data digest does not match pinned copy for "+source)
		}
	}
	if strings.TrimSpace(in.ConfigDigest) == "" || strings.TrimSpace(in.DataDigest) == "" {
		blocked = append(blocked, "config and data digests are required")
	}
	for _, ref := range in.KeyRefs {
		if !knownKeys[ref] {
			blocked = append(blocked, "unknown key reference "+ref)
		}
	}
	if len(in.KeyRefs) == 0 {
		blocked = append(blocked, "at least one key reference is required")
	}
	sort.Strings(blocked)
	if len(blocked) > 0 {
		return ReadinessReceipt{}, fmt.Errorf("iacrecovery: %s: %s", CodeReadinessBlocked, strings.Join(blocked, "; "))
	}
	return ReadinessReceipt{CellID: cell.ID, Digest: readinessDigest(cell, in)}, nil
}

// VerifyReadiness recomputes the receipt digest; a non-empty finding
// means the receipt does not match the admitted cell and inputs.
func VerifyReadiness(receipt ReadinessReceipt, cell Cell, in ReadinessInput) string {
	if receipt.CellID != cell.ID {
		return "cell mismatch"
	}
	if receipt.Digest != readinessDigest(cell, in) {
		return "digest mismatch"
	}
	return ""
}

func readinessDigest(cell Cell, in ReadinessInput) string {
	keys := append([]string(nil), in.KeyRefs...)
	sort.Strings(keys)
	parts := append([]string{"iac010-readiness", cell.ID}, cell.CopyIDs...)
	parts = append(parts, cell.Dependencies...)
	parts = append(parts, in.ConfigDigest, in.DataDigest)
	parts = append(parts, keys...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
