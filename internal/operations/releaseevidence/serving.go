package releaseevidence

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// ServingContractID identifies the release-evidence contract composed by a
// served application cell.
const ServingContractID = "hcmnext.conformance.release-evidence/v1"

// ValidateServingContract proves that the shipped migration artifact can be
// bound into an immutable release record. It performs no database work: the
// PostgreSQL chronology itself remains an explicit release-evidence harness,
// while the served path proves this package is linked and its record boundary
// is usable before the cell accepts traffic.
func ValidateServingContract() error {
	files, err := migrations.Files()
	if err != nil {
		return fmt.Errorf("releaseevidence: serving contract files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("releaseevidence: serving contract has no migrations")
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		return fmt.Errorf("releaseevidence: serving contract artifact digest: %w", err)
	}
	release := Release{
		Slice: "promotion", Version: "serving", BinaryRevision: "serving",
		SchemaDigest: digest, SchemaVersion: files[len(files)-1].Version,
		Definitions:  map[string]string{"serving.contract": ServingContractID},
		EvidenceRefs: []string{"serving:" + ServingContractID},
		RecordedBy:   "hcmnext:serve", RecordedAt: time.Unix(1, 0).UTC(),
	}
	sealed, err := release.Seal()
	if err != nil {
		return fmt.Errorf("releaseevidence: serving contract seal: %w", err)
	}
	if err := sealed.Verify(); err != nil {
		return fmt.Errorf("releaseevidence: serving contract verify: %w", err)
	}
	recorded, err := NewJournal().Record(sealed)
	if err != nil {
		return fmt.Errorf("releaseevidence: serving contract journal: %w", err)
	}
	if recorded.Digest != sealed.Digest {
		return fmt.Errorf("releaseevidence: serving contract journal changed the sealed digest")
	}
	return nil
}
