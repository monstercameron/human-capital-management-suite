// QUAL-006: prove qualification across Job, Scheduling, Safety, Access
// and restricted Return-to-Work.
//
// EvaluateCrossQualification evaluates one worker against the five
// domain requirement sets under shared evidence, status and
// effective-time semantics. Expired or missing evidence never qualifies:
// it yields NOT_QUALIFIED or UNKNOWN, and any path that would return
// QUALIFIED from bad evidence is refused with QUAL_006_REJECTED.
// Restricted (medical) evidence never enters ordinary qualification
// output: restricted domains report a redacted verdict. The function is
// kernel-pure and persists nothing.
package qualification

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// CrossQualVersion is the rejection version for QUAL-006.
const CrossQualVersion = "qualification-cross/v1"

var (
	// ErrCrossQualRejected is the QUAL-006 seeded-defect sentinel.
	// Expired or missing evidence that returns QUALIFIED fails with this
	// error carrying the offending field, state and version, and nothing
	// is persisted.
	ErrCrossQualRejected = errors.New("QUAL_006_REJECTED")
)

// CrossQualRejection is the stable QUAL-006 failure shape.
type CrossQualRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *CrossQualRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrCrossQualRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the QUAL_006_REJECTED sentinel to errors.Is.
func (r *CrossQualRejection) Unwrap() error { return ErrCrossQualRejected }

func crossQualReject(field, state, reason string) error {
	return &CrossQualRejection{Field: field, State: state, Version: CrossQualVersion, Reason: reason}
}

// CrossDomain is the closed QUAL-006 domain vocabulary.
type CrossDomain string

const (
	CrossJob        CrossDomain = "JOB"
	CrossScheduling CrossDomain = "SCHEDULING"
	CrossSafety     CrossDomain = "SAFETY"
	CrossAccess     CrossDomain = "ACCESS"
	CrossRTW        CrossDomain = "RTW_RESTRICTED"
)

// CrossDomains is the fixed evaluation order.
var CrossDomains = []CrossDomain{CrossJob, CrossScheduling, CrossSafety, CrossAccess, CrossRTW}

// CrossVerdict is the closed per-domain outcome vocabulary.
type CrossVerdict string

const (
	CrossQualified    CrossVerdict = "QUALIFIED"
	CrossNotQualified CrossVerdict = "NOT_QUALIFIED"
	CrossUnknown      CrossVerdict = "UNKNOWN"
)

// QualEvidence is one evidence item shared across domains. Restricted
// marks medical evidence confined to the RTW domain.
type QualEvidence struct {
	Ref        string
	Kind       string
	IssuedAt   time.Time
	ExpiresAt  time.Time
	Verified   bool
	Restricted bool
}

// DomainRequirement is one domain's evidence demand.
type DomainRequirement struct {
	Domain CrossDomain
	Kinds  []string
}

// CrossQualInput evaluates one worker across all five domains at AsOf.
type CrossQualInput struct {
	Tenant       string
	WorkerRef    string
	AsOf         time.Time
	Evidence     []QualEvidence
	Requirements []DomainRequirement
}

// DomainVerdict is one domain's redacted outcome. EvidenceRefs names
// only non-restricted evidence; RestrictedUsed reports medical evidence
// participation without naming it.
type DomainVerdict struct {
	Domain         CrossDomain
	Verdict        CrossVerdict
	EvidenceRefs   []string
	RestrictedUsed bool
	Detail         string
}

// CrossQualResult is the deterministic five-domain outcome.
type CrossQualResult struct {
	Domains []DomainVerdict
	Digest  string
}

func (r CrossQualResult) computedDigest(in CrossQualInput) string {
	verdicts := make([]string, 0, len(r.Domains))
	for _, d := range r.Domains {
		refs := append([]string(nil), d.EvidenceRefs...)
		sort.Strings(refs)
		verdicts = append(verdicts, strings.Join([]string{string(d.Domain), string(d.Verdict), strings.Join(refs, ",")}, "\x00"))
	}
	sort.Strings(verdicts)
	w := canonicalbytes.New("hcmnext.domains.qualification.CrossQual", 1).
		String("tenant", in.Tenant).
		String("worker_ref", in.WorkerRef).
		String("as_of", in.AsOf.UTC().Format(time.RFC3339)).
		SortedStrings("verdicts", verdicts)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// EvaluateCrossQualification evaluates the five domains under shared
// evidence, status and effective-time semantics.
func EvaluateCrossQualification(in CrossQualInput) (CrossQualResult, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return CrossQualResult{}, crossQualReject("crossqual.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.WorkerRef) == "" {
		return CrossQualResult{}, crossQualReject("crossqual.worker_ref", "MISSING", "worker ref is required")
	}
	if in.AsOf.IsZero() {
		return CrossQualResult{}, crossQualReject("crossqual.as_of", "MISSING", "evaluation instant is required")
	}
	if len(in.Requirements) == 0 {
		return CrossQualResult{}, crossQualReject("crossqual.requirements", "MISSING", "at least one domain requirement is required")
	}
	required := map[CrossDomain]bool{}
	for i, r := range in.Requirements {
		if !r.Domain.Valid() {
			return CrossQualResult{}, crossQualReject(fmt.Sprintf("crossqual.requirements[%d].domain", i), "UNDECLARED", fmt.Sprintf("domain %q is not declared", r.Domain))
		}
		if len(r.Kinds) == 0 {
			return CrossQualResult{}, crossQualReject(fmt.Sprintf("crossqual.requirements[%d].kinds", i), "MISSING", "each domain demands at least one evidence kind")
		}
		required[r.Domain] = true
	}
	for _, d := range CrossDomains {
		if !required[d] {
			return CrossQualResult{}, crossQualReject("crossqual.requirements", "INCOMPLETE", fmt.Sprintf("domain %s has no requirement", d))
		}
	}
	for i, e := range in.Evidence {
		if strings.TrimSpace(e.Ref) == "" || strings.TrimSpace(e.Kind) == "" {
			return CrossQualResult{}, crossQualReject(fmt.Sprintf("crossqual.evidence[%d]", i), "MISSING", "evidence carries ref and kind")
		}
		if e.IssuedAt.IsZero() || e.ExpiresAt.IsZero() || !e.ExpiresAt.After(e.IssuedAt) {
			return CrossQualResult{}, crossQualReject(fmt.Sprintf("crossqual.evidence[%d]", i), "INVALID_INTERVAL", "evidence carries a valid effective interval")
		}
	}
	res := CrossQualResult{}
	for _, req := range in.Requirements {
		out := DomainVerdict{Domain: req.Domain}
		byKind := map[string][]QualEvidence{}
		for _, e := range in.Evidence {
			// Restricted medical evidence never enters ordinary
			// domains: it is visible only to RTW_RESTRICTED.
			if e.Restricted && req.Domain != CrossRTW {
				continue
			}
			byKind[e.Kind] = append(byKind[e.Kind], e)
		}
		verdict := CrossQualified
		var refs []string
		var details []string
		for _, kind := range req.Kinds {
			candidates := byKind[kind]
			if len(candidates) == 0 {
				verdict = CrossNotQualified
				details = append(details, "missing "+kind)
				continue
			}
			best := false
			for _, e := range candidates {
				if !e.Verified {
					continue
				}
				if in.AsOf.Before(e.IssuedAt) || !in.AsOf.Before(e.ExpiresAt) {
					details = append(details, "expired "+e.Ref)
					continue
				}
				best = true
				if e.Restricted {
					out.RestrictedUsed = true
				} else {
					refs = append(refs, e.Ref)
				}
				break
			}
			if !best {
				verdict = CrossNotQualified
			}
		}
		// The seeded-defect tripwire: a QUALIFIED verdict must be fully
		// backed by current verified evidence. Anything else that
		// reaches QUALIFIED is refused, never returned.
		if verdict == CrossQualified {
			for _, kind := range req.Kinds {
				backed := false
				for _, e := range byKind[kind] {
					if e.Verified && !in.AsOf.Before(e.IssuedAt) && in.AsOf.Before(e.ExpiresAt) {
						backed = true
						break
					}
				}
				if !backed {
					return CrossQualResult{}, crossQualReject("crossqual."+strings.ToLower(string(req.Domain)), "UNBACKED_QUALIFIED", fmt.Sprintf("domain %s would qualify without current verified %s", req.Domain, kind))
				}
			}
		}
		out.Verdict = verdict
		sort.Strings(refs)
		out.EvidenceRefs = refs
		out.Detail = strings.Join(details, "; ")
		if verdict == CrossQualified {
			out.Detail = "all kinds backed by current verified evidence"
		}
		res.Domains = append(res.Domains, out)
	}
	sort.Slice(res.Domains, func(i, j int) bool { return res.Domains[i].Domain < res.Domains[j].Domain })
	res.Digest = res.computedDigest(in)
	return res, nil
}

// Valid reports whether the domain is declared.
func (d CrossDomain) Valid() bool {
	switch d {
	case CrossJob, CrossScheduling, CrossSafety, CrossAccess, CrossRTW:
		return true
	default:
		return false
	}
}
