// Artifact backup, restore and cryptographic lifecycle: DATA-023
// proves object and artifact backup, restore and key lifecycle.
//
// Backup inventories envelope bytes, metadata, key versions, holds,
// tombstones and erasure proofs with ledger lineage refs. Restore
// re-verifies bytes digests, access (caller's custody context must
// still open each object), key-version currency and lifecycle state:
// holds and tombstones are preserved, erased objects verify absent or
// unopenable with erasure proof and never come back, and deleted
// content is never resurrected. The restore emits no domain events or
// effects; it returns a receipt only.
package object

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

// ArtifactState is the closed lifecycle state at backup.
type ArtifactState string

// The backup lifecycle states.
const (
	ArtifactActive     ArtifactState = "ACTIVE"
	ArtifactTombstoned ArtifactState = "TOMBSTONED"
	ArtifactErased     ArtifactState = "ERASED"
)

// ArtifactBackup is one inventoried artifact. Digest is the envelope
// identity; ContentDigest is the content address (sha256 over the
// plaintext bytes) and is the stable cross-run identity, since envelope
// bytes carry fresh randomness per seal.
type ArtifactBackup struct {
	ObjectID      string
	Digest        string
	ContentDigest string
	Size          int64
	MediaType     string
	Generation    uint64
	KeyID         string
	KeyVersion    string
	Hold          bool
	State         ArtifactState
	ErasureProof  string
	LineageRef    string
}

// BackupManifest is the ordered artifact inventory.
type BackupManifest struct {
	ManifestID string
	Artifacts  []ArtifactBackup
}

// BackupInventory records the store's envelope state. Holds,
// tombstones and erasures are owned by the records layer and supplied
// here; lineage maps each object to its ledger ref.
func BackupInventory(ctx context.Context, cctxForBackup custody.Context, store *SealedObjectStore, holds, tombstones map[string]bool, erasures map[string]string, lineage func(string) string) (BackupManifest, error) {
	if store == nil {
		return BackupManifest{}, errors.New("object: store is required")
	}
	ids := store.ObjectIDs()
	backups := make([]ArtifactBackup, 0, len(ids))
	for _, id := range ids {
		info, err := store.Stat(ctx, id)
		if err != nil {
			return BackupManifest{}, err
		}
		env, err := store.Envelope(id)
		if err != nil {
			return BackupManifest{}, err
		}
		// Content is read for every present object, including tombstoned
		// ones: the tombstone flag lives in this manifest, not in the
		// store, so bytes stay verifiable while never resurrected. An
		// object already unopenable at backup time is inventoriable
		// only as erased with proof.
		contentDigest := ""
		plaintext, _, err := store.Get(ctx, cctxForBackup, id)
		if err != nil {
			if _, erased := erasures[id]; !erased {
				return BackupManifest{}, fmt.Errorf("object: cannot read %s for content digest: %w", id, err)
			}
		} else {
			contentSum := sha256.Sum256(plaintext)
			contentDigest = "sha256:" + hex.EncodeToString(contentSum[:])
		}
		backup := ArtifactBackup{
			ObjectID: id, Digest: info.Digest, ContentDigest: contentDigest, Size: info.Size,
			MediaType: info.MediaType, Generation: info.Generation,
			KeyID: env.WrappedDEK.Handle.ID, KeyVersion: env.WrappedDEK.Handle.Version,
			Hold: holds[id], State: ArtifactActive,
			LineageRef: lineage(id),
		}
		if tombstones[id] {
			backup.State = ArtifactTombstoned
		}
		if proof, erased := erasures[id]; erased {
			if strings.TrimSpace(proof) == "" {
				return BackupManifest{}, fmt.Errorf("object: erased artifact %s lacks erasure proof", id)
			}
			backup.State = ArtifactErased
			backup.ErasureProof = proof
		}
		if strings.TrimSpace(backup.LineageRef) == "" {
			return BackupManifest{}, fmt.Errorf("object: artifact %s lacks ledger lineage", id)
		}
		backups = append(backups, backup)
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].ObjectID < backups[j].ObjectID })
	sum := sha256.Sum256([]byte("data023-manifest\n" + strings.Join(manifestLines(backups), "\n")))
	return BackupManifest{
		ManifestID: "sha256:" + hex.EncodeToString(sum[:]),
		Artifacts:  backups,
	}, nil
}

func manifestLines(backups []ArtifactBackup) []string {
	lines := make([]string, 0, len(backups))
	for _, backup := range backups {
		lines = append(lines, strings.Join([]string{
			backup.ObjectID, backup.ContentDigest, fmt.Sprint(backup.Size), backup.MediaType,
			fmt.Sprint(backup.Generation), backup.KeyID, backup.KeyVersion,
			fmt.Sprint(backup.Hold), string(backup.State), backup.ErasureProof, backup.LineageRef,
		}, "\x00"))
	}
	return lines
}

// RestoreStatus is the closed restore outcome.
type RestoreStatus string

// The restore outcomes.
const (
	RestoreComplete RestoreStatus = "COMPLETE"
	RestoreFenced   RestoreStatus = "FENCED"
)

// RestorePolicy bounds the restore and names the object RPO/RTO.
type RestorePolicy struct {
	BudgetRPO   time.Duration
	BudgetRTO   time.Duration
	ObservedRPO time.Duration
	ObservedRTO time.Duration
}

// ArtifactOutcome is the closed per-artifact resolution.
type ArtifactOutcome string

// The per-artifact outcomes.
const (
	OutcomeRestored       ArtifactOutcome = "RESTORED"
	OutcomeTombstoneKept  ArtifactOutcome = "TOMBSTONE_KEPT"
	OutcomeErasureChecked ArtifactOutcome = "ERASURE_VERIFIED"
)

// ArtifactResult is one reconciled artifact.
type ArtifactResult struct {
	ObjectID string
	Outcome  ArtifactOutcome
	Digest   string
}

// RestoreFinding names one restore defect.
type RestoreFinding struct {
	Code   string
	Detail string
}

// RestoreReport is the deterministic restore receipt. It carries no
// domain events or effects: reconciling bytes to lineage emits nothing.
type RestoreReport struct {
	ManifestID     string
	Status         RestoreStatus
	Restored       int
	HoldsKept      int
	Tombstones     int
	Erased         int
	Results        []ArtifactResult
	EffectsEmitted int
	Findings       []RestoreFinding
	Digest         string
}

// RestoreArtifacts verifies and admits one artifact restore.
func RestoreArtifacts(ctx context.Context, cctx custody.Context, store *SealedObjectStore, manifest BackupManifest, policy RestorePolicy) (RestoreReport, error) {
	if store == nil {
		return RestoreReport{}, errors.New("object: store is required")
	}
	if strings.TrimSpace(manifest.ManifestID) == "" {
		return RestoreReport{}, errors.New("object: manifest id is required")
	}
	if policy.BudgetRPO <= 0 || policy.BudgetRTO <= 0 {
		return RestoreReport{}, errors.New("object: RPO and RTO budgets must be positive")
	}
	if policy.ObservedRPO < 0 || policy.ObservedRTO < 0 {
		return RestoreReport{}, errors.New("object: observed RPO and RTO cannot be negative")
	}
	report := RestoreReport{ManifestID: manifest.ManifestID, Status: RestoreComplete}
	fail := func(code, detail string) {
		report.Status = RestoreFenced
		report.Findings = append(report.Findings, RestoreFinding{Code: code, Detail: detail})
	}
	if policy.ObservedRPO > policy.BudgetRPO {
		fail("RPO_BUDGET_MISSED", fmt.Sprintf("observed RPO %s exceeds budget %s", policy.ObservedRPO, policy.BudgetRPO))
	}
	if policy.ObservedRTO > policy.BudgetRTO {
		fail("RTO_BUDGET_MISSED", fmt.Sprintf("observed RTO %s exceeds budget %s", policy.ObservedRTO, policy.BudgetRTO))
	}
	seen := map[string]bool{}
	for _, backup := range manifest.Artifacts {
		if strings.TrimSpace(backup.ObjectID) == "" || seen[backup.ObjectID] {
			fail("MANIFEST_INVALID", "manifest has empty or duplicate object ids")
			continue
		}
		seen[backup.ObjectID] = true
		switch backup.State {
		case ArtifactErased:
			if strings.TrimSpace(backup.ErasureProof) == "" {
				fail("ERASURE_UNPROVEN", "erased artifact "+backup.ObjectID+" lacks erasure proof")
				continue
			}
			if _, _, err := store.Get(ctx, cctx, backup.ObjectID); err == nil {
				fail("ERASURE_BROKEN", "erased artifact "+backup.ObjectID+" bytes still openable")
				continue
			}
			report.Erased++
			report.Results = append(report.Results, ArtifactResult{ObjectID: backup.ObjectID, Outcome: OutcomeErasureChecked})
		case ArtifactTombstoned:
			info, err := store.Stat(ctx, backup.ObjectID)
			if err != nil {
				fail("BYTES_MISSING", "tombstoned artifact "+backup.ObjectID+" envelope absent")
				continue
			}
			if info.Digest != backup.Digest {
				fail("DIGEST_MISMATCH", "tombstoned artifact "+backup.ObjectID+" envelope digest drifted")
				continue
			}
			// Bytes stay verified but the tombstone is kept: the restore
			// never resurrects deleted content as readable.
			plaintext, _, err := store.Get(ctx, cctx, backup.ObjectID)
			if err != nil {
				fail("ACCESS_STALE", "tombstoned artifact "+backup.ObjectID+" no longer verifiable")
				continue
			}
			contentSum := sha256.Sum256(plaintext)
			if content := "sha256:" + hex.EncodeToString(contentSum[:]); content != backup.ContentDigest {
				fail("DIGEST_MISMATCH", "tombstoned artifact "+backup.ObjectID+" content digest drifted")
				continue
			}
			report.Tombstones++
			report.Results = append(report.Results, ArtifactResult{ObjectID: backup.ObjectID, Outcome: OutcomeTombstoneKept, Digest: backup.ContentDigest})
		case ArtifactActive:
			info, err := store.Stat(ctx, backup.ObjectID)
			if err != nil {
				fail("BYTES_MISSING", "artifact "+backup.ObjectID+" envelope absent")
				continue
			}
			if info.Digest != backup.Digest {
				fail("DIGEST_MISMATCH", "artifact "+backup.ObjectID+" envelope digest drifted")
				continue
			}
			env, err := store.Envelope(backup.ObjectID)
			if err != nil {
				fail("KEY_UNKNOWN", "artifact "+backup.ObjectID+" envelope unavailable")
				continue
			}
			if env.WrappedDEK.Handle.Version != backup.KeyVersion {
				fail("REWRAP_REQUIRED", "artifact "+backup.ObjectID+" key rotated since backup")
				continue
			}
			plaintext, _, err := store.Get(ctx, cctx, backup.ObjectID)
			if err != nil {
				fail("ACCESS_STALE", "artifact "+backup.ObjectID+" no longer openable by the restoring context")
				continue
			}
			contentSum := sha256.Sum256(plaintext)
			if content := "sha256:" + hex.EncodeToString(contentSum[:]); content != backup.ContentDigest {
				fail("DIGEST_MISMATCH", "artifact "+backup.ObjectID+" content digest drifted")
				continue
			}
			if backup.Hold {
				report.HoldsKept++
			}
			report.Restored++
			report.Results = append(report.Results, ArtifactResult{ObjectID: backup.ObjectID, Outcome: OutcomeRestored, Digest: backup.ContentDigest})
		default:
			fail("STATE_UNKNOWN", "artifact "+backup.ObjectID+" has an unlisted lifecycle state")
		}
	}
	sort.Slice(report.Results, func(i, j int) bool { return report.Results[i].ObjectID < report.Results[j].ObjectID })
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Code != report.Findings[j].Code {
			return report.Findings[i].Code < report.Findings[j].Code
		}
		return report.Findings[i].Detail < report.Findings[j].Detail
	})
	report.Digest = digestRestoreReport(report)
	return report, nil
}

func digestRestoreReport(report RestoreReport) string {
	parts := []string{"data023-restore", report.ManifestID, string(report.Status),
		fmt.Sprint(report.Restored), fmt.Sprint(report.HoldsKept),
		fmt.Sprint(report.Tombstones), fmt.Sprint(report.Erased), fmt.Sprint(report.EffectsEmitted)}
	for _, result := range report.Results {
		parts = append(parts, result.ObjectID+"\x00"+string(result.Outcome)+"\x00"+result.Digest)
	}
	for _, finding := range report.Findings {
		parts = append(parts, finding.Code+"\x00"+finding.Detail)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
