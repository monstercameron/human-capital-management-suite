// Package migrationci validates migration manifests and models the durable
// upgrade decision policy without applying migrations. Its pure Rehearse
// function consumes caller-supplied state and is not evidence that a database
// or mixed-version deployment was actually exercised. Production adapters
// still own execution, observation, and journal writes.
package migrationci

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const schemaVersion = 1

// Version returns the manifest policy version.
func Version() int { return schemaVersion }

// Explain returns a stable description suitable for policy output.
func Explain() string { return "migration manifest clean-upgrade rehearsal policy" }

type Phase string

const (
	PhaseExpand   Phase = "EXPAND"
	PhaseBackfill Phase = "BACKFILL"
	PhaseShadow   Phase = "SHADOW"
	PhaseCutover  Phase = "CUTOVER"
	PhaseContract Phase = "CONTRACT"
)

// Entry is the CI identity of one migration artifact.
type Entry struct {
	Version       int64
	Name          string
	Checksum      string
	Compatibility string
	Direction     string
	Requires      []int64
}

// Manifest is immutable CI evidence for the migration sequence.
type Manifest struct {
	ReleaseDigest     string
	Entries           []Entry
	Phases            []Phase
	AdoptionWatermark uint64
}

// Input describes the state CI found before the rehearsal.
type Input struct {
	Manifest               Manifest
	Applied                []Entry
	ChecksumMismatch       bool
	Dirty                  bool
	LockHeld               bool
	BackfillComplete       bool
	ShadowExact            bool
	MixedVersionCompatible bool
	AbortRequested         bool
}

// Finding is a stable, field-oriented policy diagnostic.
type Finding struct {
	Code   string
	Field  string
	State  string
	Detail string
}

// Result is the deterministic rehearsal outcome.
type Result struct {
	Status         string
	Steps          []Phase
	Findings       []Finding
	ManifestDigest string
}

var (
	ErrInvalidManifest = errors.New("migration ci: invalid manifest")
	ErrRehearsalFailed = errors.New("migration ci: rehearsal failed")
)

// Validate checks ordering, checksums, dependency declarations and the
// required expand/contract phase sequence.
func Validate(manifest Manifest) error {
	if len(manifest.Entries) == 0 || len(manifest.Phases) != 5 {
		return fmt.Errorf("%w: entries and five lifecycle phases are required", ErrInvalidManifest)
	}
	if len(manifest.ReleaseDigest) != sha256.Size*2 {
		return fmt.Errorf("%w: release digest must be sha256", ErrInvalidManifest)
	}
	if _, err := hex.DecodeString(manifest.ReleaseDigest); err != nil {
		return fmt.Errorf("%w: release digest is not hexadecimal", ErrInvalidManifest)
	}
	wantPhases := []Phase{PhaseExpand, PhaseBackfill, PhaseShadow, PhaseCutover, PhaseContract}
	for i, phase := range wantPhases {
		if manifest.Phases[i] != phase {
			return fmt.Errorf("%w: phase %d is %s, want %s", ErrInvalidManifest, i, manifest.Phases[i], phase)
		}
	}
	seen := map[int64]bool{}
	for i, entry := range manifest.Entries {
		if entry.Version <= 0 || strings.TrimSpace(entry.Name) == "" || entry.Direction != "UP" {
			return fmt.Errorf("%w: entry %d has invalid identity or direction", ErrInvalidManifest, i)
		}
		if i > 0 && manifest.Entries[i-1].Version >= entry.Version {
			return fmt.Errorf("%w: entries are not strictly ordered", ErrInvalidManifest)
		}
		if seen[entry.Version] {
			return fmt.Errorf("%w: duplicate version %d", ErrInvalidManifest, entry.Version)
		}
		seen[entry.Version] = true
		if len(entry.Checksum) != sha256.Size*2 {
			return fmt.Errorf("%w: entry %d checksum must be sha256", ErrInvalidManifest, i)
		}
		if _, err := hex.DecodeString(entry.Checksum); err != nil {
			return fmt.Errorf("%w: entry %d checksum is not hexadecimal", ErrInvalidManifest, i)
		}
		switch entry.Compatibility {
		case "UNREVIEWED", "BACKWARD_COMPATIBLE", "FORWARD_COMPATIBLE", "FULL", "BREAKING":
		default:
			return fmt.Errorf("%w: entry %d has unknown compatibility class", ErrInvalidManifest, i)
		}
		for _, dependency := range entry.Requires {
			if !seen[dependency] {
				return fmt.Errorf("%w: entry %d requires unapplied version %d", ErrInvalidManifest, entry.Version, dependency)
			}
		}
	}
	return nil
}

// Rehearse validates the manifest, then simulates the five lifecycle-policy
// decisions against caller-supplied state. This is a policy model only; the
// migrationci command refuses to represent it as observed database evidence.
func Rehearse(input Input) (Result, error) {
	result := Result{Status: "REJECTED"}
	if err := Validate(input.Manifest); err != nil {
		result.Findings = append(result.Findings, Finding{Code: "MANIFEST_INVALID", Field: "manifest", State: "INVALID", Detail: "manifest validation failed"})
		return result, err
	}
	result.ManifestDigest = manifestDigest(input.Manifest)
	for _, entry := range input.Manifest.Entries {
		if entry.Compatibility == "UNREVIEWED" {
			result.Findings = append(result.Findings, Finding{Code: "COMPATIBILITY_UNREVIEWED", Field: "compatibility", State: "UNREVIEWED", Detail: "migration compatibility has no reviewed classification"})
		}
	}
	if len(result.Findings) > 0 {
		return result, ErrRehearsalFailed
	}
	if input.Dirty {
		result.Findings = append(result.Findings, Finding{Code: "DIRTY_TREE", Field: "working_tree", State: "DIRTY", Detail: "uncommitted or generated drift is present"})
	}
	if input.LockHeld {
		result.Findings = append(result.Findings, Finding{Code: "MIGRATION_LOCK", Field: "migration_lock", State: "HELD", Detail: "another migration owner holds the lock"})
	}
	if input.ChecksumMismatch {
		result.Findings = append(result.Findings, Finding{Code: "CHECKSUM_MISMATCH", Field: "migration_checksum", State: "MISMATCH", Detail: "recorded migration bytes differ from the manifest"})
	}
	if !input.MixedVersionCompatible {
		result.Findings = append(result.Findings, Finding{Code: "MIXED_VERSION_REJECTED", Field: "compatibility", State: "INCOMPATIBLE", Detail: "old and new binaries do not overlap"})
	}
	if len(result.Findings) > 0 {
		return result, ErrRehearsalFailed
	}
	result.Steps = append(result.Steps, PhaseExpand)
	if !input.BackfillComplete {
		result.Findings = append(result.Findings, Finding{Code: "BACKFILL_INCOMPLETE", Field: "backfill", State: "INCOMPLETE", Detail: "checkpoint has not reached the source watermark"})
		return result, ErrRehearsalFailed
	}
	result.Steps = append(result.Steps, PhaseBackfill)
	if !input.ShadowExact {
		result.Findings = append(result.Findings, Finding{Code: "SHADOW_MISMATCH", Field: "shadow", State: "MISMATCH", Detail: "source and target digests differ"})
		return result, ErrRehearsalFailed
	}
	result.Steps = append(result.Steps, PhaseShadow, PhaseCutover)
	if input.AbortRequested {
		result.Findings = append(result.Findings, Finding{Code: "ABORTED_BEFORE_CONTRACT", Field: "contract", State: "ABORTED", Detail: "contract was fenced after cutover rehearsal"})
		return result, ErrRehearsalFailed
	}
	result.Steps = append(result.Steps, PhaseContract)
	result.Status = "PASS"
	return result, nil
}

// ExplainResult renders only stable state and phase names.
func ExplainResult(result Result) string {
	parts := make([]string, 0, len(result.Steps))
	for _, step := range result.Steps {
		parts = append(parts, string(step))
	}
	return fmt.Sprintf("migration rehearsal %s phases=%s findings=%d", result.Status, strings.Join(parts, ">"), len(result.Findings))
}

func manifestDigest(manifest Manifest) string {
	entries := append([]Entry(nil), manifest.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Version < entries[j].Version })
	h := sha256.New()
	fmt.Fprintf(h, "migration-ci.v%d\x00%s\x00%d\x00", schemaVersion, manifest.ReleaseDigest, manifest.AdoptionWatermark)
	for _, entry := range entries {
		fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%s\x00", entry.Version, entry.Name, entry.Checksum, entry.Compatibility, entry.Direction)
	}
	return hex.EncodeToString(h.Sum(nil))
}
