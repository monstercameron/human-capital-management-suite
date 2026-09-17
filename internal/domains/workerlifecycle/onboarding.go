package workerlifecycle

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrReadinessRejected is the WORKER-LIFE-002 refusal boundary. Resolution
// is pure: it returns readiness, never a child intent, task or effect.
var ErrReadinessRejected = errors.New("WORKER_LIFE_002_REJECTED")

// RequirementKind is the closed onboarding-requirement vocabulary.
type RequirementKind string

const (
	RequirementWorkAuthorization RequirementKind = "WORK_AUTHORIZATION"
	RequirementPayroll           RequirementKind = "PAYROLL"
	RequirementAccess            RequirementKind = "ACCESS"
	RequirementEquipment         RequirementKind = "EQUIPMENT"
	RequirementTraining          RequirementKind = "TRAINING"
	RequirementLegal             RequirementKind = "LEGAL"
	RequirementMedical           RequirementKind = "MEDICAL"
	RequirementIdentity          RequirementKind = "IDENTITY"
	RequirementTask              RequirementKind = "TASK"
	RequirementOptional          RequirementKind = "OPTIONAL"
)

func (k RequirementKind) Valid() bool {
	switch k {
	case RequirementWorkAuthorization, RequirementPayroll, RequirementAccess,
		RequirementEquipment, RequirementTraining, RequirementLegal,
		RequirementMedical, RequirementIdentity, RequirementTask, RequirementOptional:
		return true
	default:
		return false
	}
}

// authoritativeKinds need a governed waiver authority; a manager's word
// never suffices for them.
func authoritativeKind(kind RequirementKind) bool {
	switch kind {
	case RequirementLegal, RequirementWorkAuthorization, RequirementMedical, RequirementIdentity:
		return true
	default:
		return false
	}
}

// Waiver authorities.
const (
	WaiverLegalAuthority = "LEGAL_AUTHORITY"
	WaiverHROfficer      = "HR_OFFICER"
)

// RequirementInput overlays policy meaning on one plan requirement.
type RequirementInput struct {
	RequirementID string
	Kind          RequirementKind
	Optional      bool
	Protected     bool
	FreshDays     int
}

// WorkerFact is one observed piece of requirement evidence.
type WorkerFact struct {
	RequirementID string
	ObservedAt    values.LocalDate
	Evidence      values.EntityRef
	Summary       string
}

// Waiver records a governed decision to set one requirement aside.
type Waiver struct {
	RequirementID string
	ByRole        string
	At            values.LocalDate
}

// ResolutionRequest asks for the readiness of one sealed plan at one date.
type ResolutionRequest struct {
	Plan         WorkerLifecyclePlan
	Requirements []RequirementInput
	Facts        []WorkerFact
	Waivers      []Waiver
	AsOf         values.LocalDate
}

// RequirementStatus is the per-requirement outcome vocabulary.
type RequirementStatus string

const (
	StatusSatisfied       RequirementStatus = "SATISFIED"
	StatusPending         RequirementStatus = "PENDING"
	StatusBlocked         RequirementStatus = "BLOCKED"
	StatusUnknown         RequirementStatus = "UNKNOWN"
	StatusSkippedOptional RequirementStatus = "SKIPPED_OPTIONAL"
)

// ReadinessAggregate is the aggregate outcome vocabulary.
type ReadinessAggregate string

const (
	AggregateReady       ReadinessAggregate = "READY"
	AggregateConditional ReadinessAggregate = "CONDITIONAL"
	AggregateBlocked     ReadinessAggregate = "BLOCKED"
	AggregateUnknown     ReadinessAggregate = "UNKNOWN"
)

// RequirementResult is the readiness of one requirement. Summary carries
// the fact's human account; Protected marks evidence whose content only
// its owner audience may read.
type RequirementResult struct {
	RequirementID string
	Owner         string
	Kind          RequirementKind
	Status        RequirementStatus
	Blocker       string
	ObservedAt    values.LocalDate
	Evidence      values.EntityRef
	Summary       string
	Protected     bool
}

// ReadinessResolution is the immutable readiness of one plan at one date.
type ReadinessResolution struct {
	PlanDigest string
	AsOf       values.LocalDate
	Results    []RequirementResult
	Aggregate  ReadinessAggregate
	Blockers   []string
	Waivers    []Waiver
	Digest     string
}

// ResolveOnboardingReadiness evaluates every requirement against pinned
// worker context. Missing hard facts block, stale or conflicting evidence
// is unknown, optional work never blocks, and nothing is emitted.
func ResolveOnboardingReadiness(req ResolutionRequest) (ReadinessResolution, error) {
	fail := func(format string, args ...any) (ReadinessResolution, error) {
		return ReadinessResolution{}, errors.Join(ErrReadinessRejected, fmt.Errorf(format, args...))
	}
	if err := req.Plan.Validate(); err != nil {
		return fail("plan: %v", err)
	}
	if err := req.AsOf.Validate(); err != nil {
		return fail("as-of: %v", err)
	}
	planDigest := req.Plan.CanonicalDigest
	if planDigest == "" {
		computed, err := req.Plan.Digest()
		if err != nil {
			return fail("plan digest: %v", err)
		}
		planDigest = computed
	}
	planReq := map[string]Requirement{}
	for _, r := range req.Plan.Requirements {
		planReq[r.ID] = r
	}
	inputs := map[string]RequirementInput{}
	for i, input := range req.Requirements {
		if !input.Kind.Valid() {
			return fail("requirement %d: kind %q is not declared", i, input.Kind)
		}
		if _, ok := planReq[input.RequirementID]; !ok {
			return fail("requirement %d: %q is not a plan requirement", i, input.RequirementID)
		}
		if _, ok := inputs[input.RequirementID]; ok {
			return fail("duplicate input for %q", input.RequirementID)
		}
		if input.FreshDays < 0 {
			return fail("requirement %q: freshness cannot be negative", input.RequirementID)
		}
		inputs[input.RequirementID] = input
	}
	for id := range planReq {
		if _, ok := inputs[id]; !ok {
			return fail("plan requirement %q has no input", id)
		}
	}
	facts := map[string][]WorkerFact{}
	for i, fact := range req.Facts {
		if _, ok := planReq[fact.RequirementID]; !ok {
			return fail("fact %d: %q is not a plan requirement", i, fact.RequirementID)
		}
		facts[fact.RequirementID] = append(facts[fact.RequirementID], fact)
	}
	waivers := map[string]Waiver{}
	for i, waiver := range req.Waivers {
		if _, ok := planReq[waiver.RequirementID]; !ok {
			return fail("waiver %d: %q is not a plan requirement", i, waiver.RequirementID)
		}
		if strings.TrimSpace(waiver.ByRole) == "" {
			return fail("waiver %d: authority role is required", i)
		}
		if err := waiver.At.Validate(); err != nil {
			return fail("waiver %d: %v", i, err)
		}
		if _, ok := waivers[waiver.RequirementID]; ok {
			return fail("duplicate waiver for %q", waiver.RequirementID)
		}
		waivers[waiver.RequirementID] = waiver
	}
	ordered := append([]Requirement(nil), req.Plan.Requirements...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	out := ReadinessResolution{PlanDigest: planDigest, AsOf: req.AsOf, Waivers: append([]Waiver(nil), req.Waivers...)}
	for _, r := range ordered {
		input := inputs[r.ID]
		result := RequirementResult{RequirementID: r.ID, Owner: r.Owner, Kind: input.Kind, Protected: input.Protected}
		due := req.Plan.EventDate.AddDays(r.Due.OffsetDays)
		optional := input.Optional || !r.Required
		observed := facts[r.ID]
		waiver, waived := waivers[r.ID]
		switch {
		case len(observed) > 1:
			result.Status = StatusUnknown
			result.Blocker = "conflicting evidence observations"
		case waived && authoritativeKind(input.Kind) && waiver.ByRole != WaiverLegalAuthority && waiver.ByRole != WaiverHROfficer:
			result.Status = StatusBlocked
			result.Blocker = fmt.Sprintf("waiver by %s lacks authority for %s", waiver.ByRole, input.Kind)
		case waived && len(observed) == 0:
			result.Status = StatusSatisfied
			result.Blocker = fmt.Sprintf("waived by %s", waiver.ByRole)
		case len(observed) == 0 && optional:
			result.Status = StatusSkippedOptional
		case len(observed) == 0 && due.Compare(req.AsOf) >= 0:
			result.Status = StatusPending
			result.Blocker = fmt.Sprintf("awaiting %s evidence, due %s", input.Kind, due)
		case len(observed) == 0:
			result.Status = StatusBlocked
			result.Blocker = fmt.Sprintf("missing %s evidence, due %s", input.Kind, due)
		default:
			fact := observed[0]
			if err := fact.Evidence.Validate(); err != nil {
				return fail("requirement %q evidence: %v", r.ID, err)
			}
			if fact.Evidence.Tenant != req.Plan.Worker.Tenant {
				return fail("requirement %q evidence tenant differs", r.ID)
			}
			if err := fact.ObservedAt.Validate(); err != nil {
				return fail("requirement %q observation date: %v", r.ID, err)
			}
			if fact.ObservedAt.Compare(req.AsOf) > 0 {
				result.Status = StatusUnknown
				result.Blocker = "observation is dated after as-of"
				break
			}
			if fact.ObservedAt.Compare(req.AsOf.AddDays(-input.FreshDays)) < 0 {
				result.Status = StatusUnknown
				result.Blocker = fmt.Sprintf("evidence observed %s is stale", fact.ObservedAt)
				break
			}
			result.Status = StatusSatisfied
			result.ObservedAt = fact.ObservedAt
			result.Evidence = fact.Evidence
			result.Summary = fact.Summary
		}
		out.Results = append(out.Results, result)
	}
	out.Aggregate = AggregateReady
	for _, result := range out.Results {
		switch result.Status {
		case StatusBlocked:
			out.Aggregate = AggregateBlocked
		case StatusUnknown:
			if out.Aggregate != AggregateBlocked {
				out.Aggregate = AggregateUnknown
			}
		case StatusPending:
			if out.Aggregate != AggregateBlocked && out.Aggregate != AggregateUnknown {
				out.Aggregate = AggregateConditional
			}
		}
		if result.Status == StatusBlocked || result.Status == StatusUnknown {
			out.Blockers = append(out.Blockers, result.RequirementID+": "+result.Blocker)
		}
	}
	out.Digest = canonicalbytes.Digest(out.body())
	return out, nil
}

func (r ReadinessResolution) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.workerlifecycle.ReadinessResolution", 1).
		String("plan_digest", r.PlanDigest).Value("as_of", r.AsOf).
		String("aggregate", string(r.Aggregate)).Count("results", len(r.Results))
	for _, result := range r.Results {
		w.String("requirement_id", result.RequirementID).String("owner", result.Owner).
			String("kind", string(result.Kind)).String("status", string(result.Status)).
			String("blocker", result.Blocker)
		observed := result.Status == StatusSatisfied
		w.Optional("observed_at", observed, result.ObservedAt)
		w.Optional("evidence", observed, result.Evidence)
		w.String("summary", result.Summary).Bool("protected", result.Protected)
	}
	w.Count("waivers", len(r.Waivers))
	for _, waiver := range r.Waivers {
		w.String("waiver_requirement", waiver.RequirementID).String("waiver_role", waiver.ByRole).
			Value("waiver_at", waiver.At)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Validate rechecks aggregate precedence, blocker coverage and the digest.
func (r ReadinessResolution) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrReadinessRejected, fmt.Errorf(format, args...))
	}
	aggregate := AggregateReady
	for _, result := range r.Results {
		switch result.Status {
		case StatusSatisfied, StatusSkippedOptional:
		case StatusPending:
			if aggregate != AggregateBlocked && aggregate != AggregateUnknown {
				aggregate = AggregateConditional
			}
		case StatusBlocked:
			aggregate = AggregateBlocked
		case StatusUnknown:
			if aggregate != AggregateBlocked {
				aggregate = AggregateUnknown
			}
		default:
			return fail("requirement %q status %q is not declared", result.RequirementID, result.Status)
		}
	}
	if aggregate != r.Aggregate {
		return fail("aggregate %s, recomputed %s", r.Aggregate, aggregate)
	}
	if r.Digest != canonicalbytes.Digest(r.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}

// Audience names who reads an explanation.
type Audience string

const (
	AudienceOwner   Audience = "OWNER"
	AudienceManager Audience = "MANAGER"
)

// ExplainFor renders per-requirement readiness with exact blockers, owners
// and dates. Protected summaries are REDACTED for managers; owners see all.
func (r ReadinessResolution) ExplainFor(audience Audience) string {
	var sb strings.Builder
	sb.WriteString("onboarding readiness " + string(r.Aggregate) + " plan=" + r.PlanDigest + "\n")
	return sb.String() + explainResults(r, audience)
}

func explainResults(r ReadinessResolution, audience Audience) string {
	var sb strings.Builder
	for _, result := range r.Results {
		sb.WriteString("- " + result.RequirementID + " " + string(result.Status) + " owner=" + result.Owner)
		if result.Blocker != "" {
			sb.WriteString(" blocker=" + result.Blocker)
		}
		if result.Summary != "" {
			if result.Protected && audience != AudienceOwner {
				sb.WriteString(" evidence=REDACTED")
			} else {
				sb.WriteString(" evidence=" + result.Summary)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
