// Package procurement composes verified accessibility and typed pentest
// evidence into a government authorization procurement pack.
package procurement

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant/govauth"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/securityassurance/pentest"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/vpat"
)

var ErrInvalidPentestAnswer = errors.New("procurement: pentest answer is invalid")

var ErrTrustedSignerRequired = errors.New("procurement: trusted penetration-test signer is required")

// GeneratePack loads the checked-in VPAT and validates it against the latest
// UX-003 journal entry before adapting source evidence to govauth. The
// profile and pentest answer are explicit caller inputs; their provenance is
// not independently authenticated by this composition helper.
func GeneratePack(profile govauth.GovernmentAuthorizationProfile, answer pentest.ProcurementAnswer) (govauth.ProcurementPack, error) {
	return govauth.ProcurementPack{}, ErrTrustedSignerRequired
}

// GeneratePackTrusted loads current accessibility evidence and requires the
// answer to verify against this program's out-of-band Ed25519 trust set.
func GeneratePackTrusted(profile govauth.GovernmentAuthorizationProfile, answer pentest.ProcurementAnswer, trustedKeys [][]byte) (govauth.ProcurementPack, error) {
	report, err := vpat.LoadCurrentReport()
	if err != nil {
		return govauth.ProcurementPack{}, fmt.Errorf("procurement: load current VPAT: %w", err)
	}
	return GeneratePackFromReportTrusted(profile, answer, report, trustedKeys)
}

// GeneratePackFromReport is the same composition path with an explicit
// report value, for callers that already loaded it. It refuses mutated,
// stale, or caller-invented report content by checking integrity and source
// against the latest UX-003 run.
func GeneratePackFromReport(profile govauth.GovernmentAuthorizationProfile, answer pentest.ProcurementAnswer, report vpat.Report) (govauth.ProcurementPack, error) {
	return govauth.ProcurementPack{}, ErrTrustedSignerRequired
}

// GeneratePackFromReportTrusted composes only an answer signed by an
// allowlisted assurance key; a self-consistent unkeyed digest is insufficient.
func GeneratePackFromReportTrusted(profile govauth.GovernmentAuthorizationProfile, answer pentest.ProcurementAnswer, report vpat.Report, trustedKeys [][]byte) (govauth.ProcurementPack, error) {
	if err := answer.VerifyTrusted(trustedKeys); err != nil {
		return govauth.ProcurementPack{}, fmt.Errorf("%w: %v", ErrInvalidPentestAnswer, err)
	}
	if err := report.ValidateCurrent(); err != nil {
		return govauth.ProcurementPack{}, fmt.Errorf("procurement: VPAT is not current: %w", err)
	}
	answerDate, err := time.Parse("2006-01-02", answer.AsOf)
	if err != nil {
		return govauth.ProcurementPack{}, fmt.Errorf("%w: invalid answer date", ErrInvalidPentestAnswer)
	}
	ref := report.Reference()
	// Answer dates have day precision. Anchor pack generation at the verified
	// source run timestamp so a same-day report is not treated as future-dated.
	asOf := ref.SourceRunAt.UTC()
	if !answerDate.Equal(time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)) {
		return govauth.ProcurementPack{}, fmt.Errorf("%w: answer date %s does not match current evidence date %s", ErrInvalidPentestAnswer, answer.AsOf, asOf.Format("2006-01-02"))
	}
	accessibility := govauth.VPATReportReference{
		Version: ref.Version, Digest: ref.Digest, SourceRunID: ref.SourceRunID,
		SourceRunAt: ref.SourceRunAt, LatestRunAt: ref.SourceRunAt,
	}
	pentestEvidence := govauth.PenetrationTestProcurementEvidence{
		Answer: answer.Answer, AsOf: answer.AsOf, Status: answer.Status,
		EvidenceDigest: answer.EvidenceDigest, AnswerDigest: answer.Digest,
		ArtifactRef:     pentestArtifactRef(answer),
		SignerPublicKey: answer.SignerPublicKey, Signature: answer.Signature,
	}
	inputs := govauth.ProcurementAssuranceInputs{
		AsOf: asOf.UTC(), PenetrationTest: pentestEvidence, Accessibility: accessibility,
	}
	verified, err := govauth.VerifyProcurementAssurance(inputs, trustedKeys)
	if err != nil {
		return govauth.ProcurementPack{}, err
	}
	return govauth.GenerateProcurementPackWithAssurance(profile, verified)
}

func pentestArtifactRef(answer pentest.ProcurementAnswer) string {
	if answer.Status == "no_completed_engagement" {
		return "pentest:no-engagement:sha256:" + answer.Digest
	}
	return fmt.Sprintf("pentest:%s:v%d:sha256:%s", answer.EngagementID, answer.EngagementVersion, answer.EvidenceDigest)
}
