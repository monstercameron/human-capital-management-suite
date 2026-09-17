package releaseevidence

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// VersionStore is the owned boundary the PostgreSQL chronology harness
// drives. Implementations apply exactly one pending migration per ApplyNext
// call and report the resulting schema version; the harness never depends
// on a migration framework directly.
type VersionStore interface {
	CurrentVersion(ctx context.Context) (int64, error)
	ApplyNext(ctx context.Context) (int64, error)
}

// ChronologyStep is one applied migration with the artifact identity the
// release vouches for.
type ChronologyStep struct {
	Version  int64  `json:"version"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
}

// ChronologyReport is the evidence that the whole embedded migration tree
// applied in order and the migrated schema matches the release.
type ChronologyReport struct {
	Slice          string           `json:"slice"`
	ReleaseVersion string           `json:"release_version"`
	ReleaseDigest  string           `json:"release_digest"`
	ArtifactDigest string           `json:"artifact_digest"`
	TargetVersion  int64            `json:"target_version"`
	AppliedVersion int64            `json:"applied_version"`
	Steps          []ChronologyStep `json:"steps"`
}

// RunChronology replays the embedded migration chronology against an empty
// schema, one migration at a time, and proves the migrated tree matches the
// recorded release. The release must bind the current artifact digest and
// target schema version; the store must start empty. A failure returns the
// steps applied so far with the blocking migration named.
func RunChronology(ctx context.Context, store VersionStore, release Release, observed Runtime) (ChronologyReport, error) {
	if store == nil {
		return ChronologyReport{}, fmt.Errorf("%w: version store is required", ErrInvalid)
	}
	if err := release.Verify(); err != nil {
		return ChronologyReport{}, err
	}
	files, err := migrations.Files()
	if err != nil {
		return ChronologyReport{}, err
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		return ChronologyReport{}, err
	}
	target := files[len(files)-1].Version
	if release.SchemaDigest != digest {
		return ChronologyReport{}, fmt.Errorf("%w: embedded migration tree changed under release %s %s", ErrInvalid, release.Slice, release.Version)
	}
	if release.SchemaVersion != target {
		return ChronologyReport{}, fmt.Errorf("%w: release targets schema %d, embedded target is %d", ErrInvalid, release.SchemaVersion, target)
	}
	current, err := store.CurrentVersion(ctx)
	if err != nil {
		return ChronologyReport{}, fmt.Errorf("read database version: %w", err)
	}
	if current != 0 {
		return ChronologyReport{}, fmt.Errorf("%w: chronology starts from an empty schema, database is at %d", ErrInvalid, current)
	}
	report := ChronologyReport{Slice: release.Slice, ReleaseVersion: release.Version,
		ReleaseDigest: release.Digest, ArtifactDigest: digest, TargetVersion: target}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return report, fmt.Errorf("chronology canceled at migration %d: %w", f.Version, err)
		}
		applied, err := store.ApplyNext(ctx)
		if err != nil {
			return report, fmt.Errorf("apply migration %d: %w", f.Version, err)
		}
		if applied != f.Version {
			return report, fmt.Errorf("%w: expected migration %d, database reports %d", ErrInvalid, f.Version, applied)
		}
		report.Steps = append(report.Steps, ChronologyStep{Version: f.Version, Name: f.Name, Checksum: f.Checksum})
	}
	report.AppliedVersion = target
	observed.AppliedSchemaVersion = report.AppliedVersion
	skews, err := Detect(release, observed)
	if err != nil {
		return report, err
	}
	if len(skews) != 0 {
		return report, fmt.Errorf("%w: post-chronology runtime does not match release %s: %v", ErrInvalid, release.Version, skews)
	}
	return report, nil
}

// Verify rechecks a report against the embedded tree: every step must match
// its migration file in order and the digests must still bind the release.
func (r ChronologyReport) Verify() error {
	files, err := migrations.Files()
	if err != nil {
		return err
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		return err
	}
	if r.ArtifactDigest != digest || r.TargetVersion != files[len(files)-1].Version {
		return fmt.Errorf("%w: report does not bind the embedded migration tree", ErrInvalid)
	}
	if r.AppliedVersion != r.TargetVersion || len(r.Steps) != len(files) {
		return fmt.Errorf("%w: report covers %d of %d migrations", ErrInvalid, len(r.Steps), len(files))
	}
	for i, f := range files {
		if step := r.Steps[i]; step.Version != f.Version || step.Name != f.Name || step.Checksum != f.Checksum {
			return fmt.Errorf("%w: step %d does not match migration %s", ErrInvalid, i, f.Name)
		}
	}
	if r.Slice == "" || r.ReleaseVersion == "" || r.ReleaseDigest == "" {
		return fmt.Errorf("%w: report names no release", ErrInvalid)
	}
	return nil
}

// Summary returns the redaction-safe one-line chronology evidence.
func (r ChronologyReport) Summary() string {
	return fmt.Sprintf("chronology %s %s steps=%d applied=%d target=%d artifact=%s release=%s",
		r.Slice, r.ReleaseVersion, len(r.Steps), r.AppliedVersion, r.TargetVersion, r.ArtifactDigest, r.ReleaseDigest)
}
