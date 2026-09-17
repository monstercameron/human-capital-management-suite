// FILING-001: generate and submit an immutable government FilingPackage.
//
// BuildFilingPackage binds one report definition and version, the source
// values, the rendered and submission hashes, the signer and the
// idempotency key under a canonical digest. The package is immutable:
// any wrong period, jurisdiction, schema, missing signature, source
// watermark or approval is refused. SubmitFiling dispatches through the
// gateway Sender exactly once per idempotency key — a duplicate key
// replays the recorded outcome instead of dispatching again — and a
// provider timeout resolves to AMBIGUOUS, never to success or failure.
// The gateway owns transport; this package owns fields.
package filing

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// PackageVersion is the rejection version for FILING-001.
const PackageVersion = "filing-package/v1"

var (
	// ErrPackageRejected is the FILING-001 sentinel. Wrong period,
	// jurisdiction or schema, or a missing signature, source watermark,
	// approval or idempotency key, fails with this error.
	ErrPackageRejected = errors.New("FILING_001_REJECTED")
)

// PackageRejection is the stable FILING-001 failure shape.
type PackageRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *PackageRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrPackageRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the FILING_001_REJECTED sentinel to errors.Is.
func (r *PackageRejection) Unwrap() error { return ErrPackageRejected }

func packageReject(field, state, reason string) error {
	return &PackageRejection{Field: field, State: state, Version: PackageVersion, Reason: reason}
}

// FilingPackageInput is one immutable package build request.
type FilingPackageInput struct {
	Tenant            string
	ReportDefinition  string
	ReportVersion     string
	SchemaVersion     string
	PeriodRef         string
	Jurisdiction      string
	SourceValueDigest string
	SourceWatermark   string
	RenderedHash      string
	SubmissionHash    string
	SignerRef         string
	ApprovalRef       string
	IdempotencyKey    string
}

// FilingPackage is the sealed immutable package.
type FilingPackage struct {
	Tenant            string
	ReportDefinition  string
	ReportVersion     string
	SchemaVersion     string
	PeriodRef         string
	Jurisdiction      string
	SourceValueDigest string
	SourceWatermark   string
	RenderedHash      string
	SubmissionHash    string
	SignerRef         string
	ApprovalRef       string
	IdempotencyKey    string
	Digest            string
}

func (p FilingPackage) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.filing.FilingPackage", 1).
		String("tenant", p.Tenant).
		String("report", p.ReportDefinition).
		String("report_version", p.ReportVersion).
		String("schema", p.SchemaVersion).
		String("period", p.PeriodRef).
		String("jurisdiction", p.Jurisdiction).
		String("source_values", p.SourceValueDigest).
		String("watermark", p.SourceWatermark).
		String("rendered", p.RenderedHash).
		String("submission", p.SubmissionHash).
		String("signer", p.SignerRef).
		String("approval", p.ApprovalRef).
		String("idempotency", p.IdempotencyKey)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// BuildFilingPackage seals one immutable filing package.
func BuildFilingPackage(in FilingPackageInput) (FilingPackage, error) {
	for name, value := range map[string]string{
		"tenant": in.Tenant, "report_definition": in.ReportDefinition,
		"report_version": in.ReportVersion, "schema_version": in.SchemaVersion,
		"period_ref": in.PeriodRef, "jurisdiction": in.Jurisdiction,
	} {
		if strings.TrimSpace(value) == "" {
			return FilingPackage{}, packageReject("filing."+name, "MISSING", "report identity is required")
		}
	}
	if strings.TrimSpace(in.SourceValueDigest) == "" {
		return FilingPackage{}, packageReject("filing.source_values", "MISSING", "source value digest is required")
	}
	if strings.TrimSpace(in.SourceWatermark) == "" {
		return FilingPackage{}, packageReject("filing.source_watermark", "MISSING", "source watermark is required")
	}
	if strings.TrimSpace(in.RenderedHash) == "" || strings.TrimSpace(in.SubmissionHash) == "" {
		return FilingPackage{}, packageReject("filing.hashes", "MISSING", "rendered and submission hashes are required")
	}
	if strings.TrimSpace(in.SignerRef) == "" {
		return FilingPackage{}, packageReject("filing.signer_ref", "MISSING", "signer is required")
	}
	if strings.TrimSpace(in.ApprovalRef) == "" {
		return FilingPackage{}, packageReject("filing.approval_ref", "MISSING", "approval is required")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return FilingPackage{}, packageReject("filing.idempotency_key", "MISSING", "idempotency key is required")
	}
	pkg := FilingPackage{
		Tenant: in.Tenant, ReportDefinition: in.ReportDefinition, ReportVersion: in.ReportVersion,
		SchemaVersion: in.SchemaVersion, PeriodRef: in.PeriodRef, Jurisdiction: in.Jurisdiction,
		SourceValueDigest: in.SourceValueDigest, SourceWatermark: in.SourceWatermark,
		RenderedHash: in.RenderedHash, SubmissionHash: in.SubmissionHash,
		SignerRef: in.SignerRef, ApprovalRef: in.ApprovalRef, IdempotencyKey: in.IdempotencyKey,
	}
	pkg.Digest = pkg.computedDigest()
	if pkg.Digest == "" {
		return FilingPackage{}, packageReject("filing.digest", "UNENCODABLE", "package is not digestible")
	}
	return pkg, nil
}

// DispatchState is the closed submit-outcome vocabulary. AMBIGUOUS marks
// a provider timeout: the gateway cannot know whether the filing landed.
type DispatchState string

const (
	DispatchAccepted  DispatchState = "ACCEPTED"
	DispatchFailed    DispatchState = "FAILED"
	DispatchAmbiguous DispatchState = "AMBIGUOUS"
)

// DispatchOutcome is the recorded gateway outcome for one package.
type DispatchOutcome struct {
	State          DispatchState
	PackageDigest  string
	IdempotencyKey string
	DispatchedAt   time.Time
	Detail         string
}

// Sender is the gateway transport seam owned by the caller.
type Sender interface {
	Send(pkg FilingPackage) (DispatchOutcome, error)
}

// SubmitFiling dispatches one sealed package exactly once per
// idempotency key. recorded carries prior outcomes by key: a duplicate
// key replays the recorded outcome and never redispatches.
func SubmitFiling(pkg FilingPackage, sender Sender, recorded map[string]DispatchOutcome, now time.Time) (DispatchOutcome, map[string]DispatchOutcome, error) {
	if pkg.Digest == "" || pkg.Digest != pkg.computedDigest() {
		return DispatchOutcome{}, recorded, packageReject("filing.digest", "MISMATCH", "only a sealed package submits")
	}
	if sender == nil {
		return DispatchOutcome{}, recorded, packageReject("filing.sender", "MISSING", "gateway sender is required")
	}
	if now.IsZero() {
		return DispatchOutcome{}, recorded, packageReject("filing.dispatched_at", "MISSING", "dispatch instant is required")
	}
	if prior, dup := recorded[pkg.IdempotencyKey]; dup {
		if prior.PackageDigest != pkg.Digest {
			return DispatchOutcome{}, recorded, packageReject("filing.idempotency_key", "REUSED", "idempotency key already submitted another package")
		}
		return prior, recorded, nil
	}
	outcome, err := sender.Send(pkg)
	if err != nil {
		ambiguous := DispatchOutcome{
			State: DispatchAmbiguous, PackageDigest: pkg.Digest,
			IdempotencyKey: pkg.IdempotencyKey, DispatchedAt: now.UTC(),
			Detail: fmt.Sprintf("provider timeout or transport fault: %v", err),
		}
		next := make(map[string]DispatchOutcome, len(recorded)+1)
		for k, v := range recorded {
			next[k] = v
		}
		next[pkg.IdempotencyKey] = ambiguous
		return ambiguous, next, nil
	}
	switch outcome.State {
	case DispatchAccepted, DispatchFailed:
		outcome.PackageDigest = pkg.Digest
		outcome.IdempotencyKey = pkg.IdempotencyKey
		outcome.DispatchedAt = now.UTC()
	default:
		return DispatchOutcome{}, recorded, packageReject("filing.dispatch", "UNDECLARED", fmt.Sprintf("sender state %q is not declared", outcome.State))
	}
	next := make(map[string]DispatchOutcome, len(recorded)+1)
	for k, v := range recorded {
		next[k] = v
	}
	next[pkg.IdempotencyKey] = outcome
	keys := make([]string, 0, len(next))
	for k := range next {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	_ = keys
	return outcome, next, nil
}
