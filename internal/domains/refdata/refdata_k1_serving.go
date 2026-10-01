package refdata

import (
	"fmt"
	"strings"
	"time"
)

// ServingContractID identifies the side-effect-free reference-data lifecycle
// check composed by a running application cell.
const ServingContractID = "hcmnext.conformance.reference-data/v1"

// ValidateServingContract exercises the same release, adoption, pin and
// rollback symbols used by callers. It performs no I/O, but keeps the
// semantic owner in the serving-binary dependency closure and fails startup
// if the lifecycle contract regresses.
func ValidateServingContract() error {
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	member, err := NewMember("SERVING", "Serving reference value", true, false, base, time.Time{}, nil)
	if err != nil {
		return fmt.Errorf("refdata: serving member: %w", err)
	}
	makeRelease := func(version string) (Release, error) {
		release, err := NewRelease(Release{
			DatasetID: "serving.reference",
			Version:   version,
			Source: SourceEvidence{
				SourceRef: "serving-source", SourceVersion: "v1", SourceDigest: digest,
				SignatureRef: "sig:serving", LicenseRef: "license:serving",
				Coverage: "serving-contract", RetrievedAt: base,
			},
			SchemaDigest: digest, Applicability: []string{"global", "tenant"},
			Members: []Member{member}, ConsumerRefs: []string{"intent-service"},
			AffectedIntents: []string{"serving-contract"}, EffectiveFrom: base, KnownAt: base,
		})
		if err != nil {
			return Release{}, err
		}
		validation, err := ValidateRelease(release, base.Add(time.Hour))
		if err != nil {
			return Release{}, err
		}
		validated, err := MarkValidated(release, validation)
		if err != nil {
			return Release{}, err
		}
		return PublishRelease(validated, validation, base.Add(2*time.Hour))
	}

	first, err := makeRelease("1.0.0")
	if err != nil {
		return fmt.Errorf("refdata: serving first release: %w", err)
	}
	second, err := makeRelease("2.0.0")
	if err != nil {
		return fmt.Errorf("refdata: serving second release: %w", err)
	}
	firstAdoption, err := Adopt(nil, first, AdoptionRequest{
		TenantID: "serving-tenant", EffectiveAt: base.Add(24 * time.Hour),
		Actor: "serving-contract", RolloutDigest: digest,
	}, base.Add(3*time.Hour))
	if err != nil {
		return fmt.Errorf("refdata: serving adoption: %w", err)
	}
	secondAdoption, err := Adopt(&firstAdoption, second, AdoptionRequest{
		TenantID: "serving-tenant", EffectiveAt: base.Add(24 * time.Hour),
		Actor: "serving-contract", RolloutDigest: digest,
	}, base.Add(4*time.Hour))
	if err != nil {
		return fmt.Errorf("refdata: serving upgrade: %w", err)
	}
	rollback, err := Rollback(secondAdoption, first, "serving-contract", base.Add(48*time.Hour), base.Add(5*time.Hour))
	if err != nil {
		return fmt.Errorf("refdata: serving rollback: %w", err)
	}
	if secondAdoption.PreviousVersion != first.Version || rollback.PreviousVersion != second.Version ||
		Pin(secondAdoption).ReleaseDigest != second.Digest || Pin(rollback).ReleaseDigest != first.Digest {
		return fmt.Errorf("refdata: serving lifecycle lost immutable release pins")
	}
	return nil
}
