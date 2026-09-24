package humanwork

import (
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/candidate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Rule identifiers cited by every exclusion resolution makes. They are exported
// because "explained with rule IDs" is only true if a caller can match on the
// same identifiers the resolver emits.
const (
	// RuleRequesterIsApprover excludes the principal who raised the proposal,
	// including a delegate acting on the requester's own authority.
	RuleRequesterIsApprover = "humanwork.sod.requester_is_approver"
	// RuleSubjectIsApprover excludes the worker the proposal is about.
	RuleSubjectIsApprover = "humanwork.sod.subject_is_approver"
	// RuleDualRole excludes a principal already filling another requirement in
	// the same set.
	RuleDualRole = "humanwork.sod.principal_fills_two_requirements"
	// RuleManagerChainConflict excludes a principal on the requester's manager
	// chain in either direction.
	RuleManagerChainConflict = "humanwork.sod.requester_manager_chain"
	// RuleDelegationExpired excludes a delegate whose delegation is outside its
	// validity window at the effective time.
	RuleDelegationExpired = "humanwork.delegation.expired"
	// RuleDelegationOutOfScope excludes a delegate whose delegation does not
	// cover this requirement, its authority floor or its scope.
	RuleDelegationOutOfScope = "humanwork.delegation.out_of_scope"
	// RuleAuthorityFloor excludes a directly-resolved principal who does not
	// hold every authority the requirement needs.
	RuleAuthorityFloor = "humanwork.authority.floor_not_met"
	// RuleFallbackNoBroadening excludes a fallback candidate who does not hold
	// every authority the requirement needs. Escalation moves attention; it
	// never hands authority to someone who did not already have it.
	RuleFallbackNoBroadening = "humanwork.fallback.may_not_broaden_authority"
	// RulePrincipalUnknown excludes a principal the directory does not know.
	RulePrincipalUnknown = "humanwork.availability.unknown_principal"
	// RulePrincipalInactive excludes a departed or suspended principal.
	RulePrincipalInactive = "humanwork.availability.inactive"
	// RulePrincipalUnavailable excludes a principal who cannot act right now.
	RulePrincipalUnavailable = "humanwork.availability.unavailable"
)

// CandidateSource says how a principal reached the candidate set.
type CandidateSource = candidate.Source

// Candidate sources.
const (
	// SourceUnspecified is the zero value and never appears on a candidate.
	SourceUnspecified = candidate.SourceUnspecified
	// SourceDirect means the resolution expression named them.
	SourceDirect = candidate.SourceDirect
	// SourceDelegated means they hold an in-scope, unexpired delegation from a
	// principal the expression named.
	SourceDelegated = candidate.SourceDelegated
	// SourceFallback means the requirement's escalation fallback reached them
	// after the primary set could not form quorum.
	SourceFallback = candidate.SourceFallback
)

// ResolutionOutcome is the verdict of one resolution.
type ResolutionOutcome string

// Resolution outcomes.
const (
	// OutcomeResolved means the surviving candidate set can form the quorum.
	OutcomeResolved ResolutionOutcome = "RESOLVED"
	// OutcomeNoAuthorizedApprover means it cannot. It is a stated answer with a
	// full exclusion list, never an error and never an empty success.
	OutcomeNoAuthorizedApprover ResolutionOutcome = "NO_AUTHORIZED_APPROVER"
)

// Candidate is one principal authorized to decide a requirement.
//
// A candidate is not an approver and not a task recipient. It is the set a
// decision-time authority check is made against; routing a task to one of them
// grants nothing.
type Candidate struct {
	PrincipalID string
	Via         CandidateSource
	// TermRef cites the expression term that produced them.
	TermRef string
	// DelegationID and DelegatedFrom are set only when Via is DELEGATED.
	DelegationID     string
	DelegatedFrom    string
	DelegationExpiry values.Instant
	// IdentityAssuranceRef is carried forward into decision evidence.
	IdentityAssuranceRef string
}

// Exclusion is one principal the resolver removed, and why.
type Exclusion struct {
	PrincipalID string
	RuleID      string
	Reason      string
}

// ResolutionInput is everything resolution needs that is not in the
// requirement itself.
type ResolutionInput struct {
	// RequesterPrincipalID raised the proposal.
	RequesterPrincipalID string
	// SubjectPrincipalIDs are the workers the proposal is about, as principals.
	SubjectPrincipalIDs []string
	// EffectiveAt is the instant every directory question is asked at.
	EffectiveAt values.Instant
	// ClaimedBy maps a principal to the requirement they already fill in this
	// set. It is what makes the one-role-per-principal rule a property of the
	// set rather than of a single requirement.
	ClaimedBy map[string]string
	// DeadlinePassed reports that the requirement's decision deadline has
	// elapsed, which is the other trigger for the escalation fallback besides
	// an unfillable primary set.
	DeadlinePassed bool
}

// Resolution is the whole answer for one requirement: who may decide, everyone
// who was removed and under which rule, and the evidence needed to replay it.
type Resolution struct {
	RequirementID       string
	RequirementRevision uint64
	Outcome             ResolutionOutcome

	Candidates []Candidate
	Excluded   []Exclusion

	// FallbackUsed reports whether the escalation fallback contributed.
	FallbackUsed bool

	ResolvedAt       values.Instant
	EffectiveAt      values.Instant
	DirectoryVersion string
	ExpressionDigest string
	// RequirementDigest proves which compiled requirement revision produced
	// this candidate set.
	RequirementDigest string
	QuorumRequired    uint32
}

// Authorizes reports whether principalID is in the candidate set, and returns
// the candidate record when it is.
func (r Resolution) Authorizes(principalID string) (Candidate, bool) {
	for _, c := range r.Candidates {
		if c.PrincipalID == principalID {
			return c, true
		}
	}
	return Candidate{}, false
}

// Explain renders the resolution as deterministic, rule-cited lines.
func (r Resolution) Explain() string {
	lines := make([]string, 0, len(r.Candidates)+len(r.Excluded)+2)
	lines = append(lines, string(r.Outcome)+" "+r.RequirementID+
		" quorum="+itoa(int(r.QuorumRequired))+
		" candidates="+itoa(len(r.Candidates)))
	for _, c := range r.Candidates {
		line := "  candidate " + c.PrincipalID + " via " + string(c.Via) + " from " + c.TermRef
		if c.DelegationID != "" {
			line += " delegation " + c.DelegationID + " of " + c.DelegatedFrom
		}
		lines = append(lines, line)
	}
	for _, e := range r.Excluded {
		lines = append(lines, "  excluded "+e.PrincipalID+" ["+e.RuleID+"] "+e.Reason)
	}
	return strings.Join(lines, "\n")
}

// Resolve computes the candidate set for one requirement.
//
// The order is fixed and is the contract: resolve the expression, expand
// delegations from the principals it named, apply the authority floor, apply
// every separation constraint, and only then - if the survivors cannot form the
// quorum - reach for the escalation fallback and put it through the same
// filters. Fallback is last and is never wider, which is how "fallback broadens
// authority" is prevented rather than merely discouraged.
func Resolve(req ApprovalRequirement, in ResolutionInput, dir Directory, clock Clock) (Resolution, error) {
	if dir == nil {
		return Resolution{}, newError("Resolve", "directory", ErrInvalidResolution,
			"no directory supplied")
	}
	if clock == nil {
		return Resolution{}, newError("Resolve", "clock", ErrInvalidResolution,
			"no clock supplied")
	}
	if !in.EffectiveAt.IsSet() {
		return Resolution{}, newError("Resolve", "effective_at", ErrInvalidResolution,
			"resolution names no effective time")
	}
	if in.RequesterPrincipalID == "" {
		return Resolution{}, newError("Resolve", "requester_principal_id", ErrInvalidResolution,
			"resolution names no requester, so separation of duties cannot be decided")
	}
	if req.RequirementID == "" || req.ExpressionDigest == "" {
		return Resolution{}, newError("Resolve", "requirement", ErrInvalidRequirement,
			"requirement was not produced by Compile")
	}
	now := clock()
	if !now.IsSet() {
		return Resolution{}, newError("Resolve", "resolved_at", ErrInvalidResolution,
			"clock returned an unset instant")
	}

	r := &resolver{req: req, in: in, dir: dir, at: in.EffectiveAt}
	if err := r.run(); err != nil {
		return Resolution{}, err
	}

	sort.Slice(r.candidates, func(i, j int) bool {
		return r.candidates[i].PrincipalID < r.candidates[j].PrincipalID
	})
	sort.Slice(r.excluded, func(i, j int) bool {
		if r.excluded[i].PrincipalID != r.excluded[j].PrincipalID {
			return r.excluded[i].PrincipalID < r.excluded[j].PrincipalID
		}
		return r.excluded[i].RuleID < r.excluded[j].RuleID
	})

	outcome := OutcomeResolved
	if uint32(len(r.candidates)) < req.Quorum.MinApprovals {
		outcome = OutcomeNoAuthorizedApprover
	}
	return Resolution{
		RequirementID:       req.RequirementID,
		RequirementRevision: req.Revision,
		Outcome:             outcome,
		Candidates:          r.candidates,
		Excluded:            r.excluded,
		FallbackUsed:        r.fallbackUsed,
		ResolvedAt:          now,
		EffectiveAt:         in.EffectiveAt,
		DirectoryVersion:    dir.Version(),
		ExpressionDigest:    req.ExpressionDigest,
		RequirementDigest:   req.Digest(),
		QuorumRequired:      req.Quorum.MinApprovals,
	}, nil
}

// resolver holds one resolution in progress. It exists so the pass order above
// reads as five named steps rather than one long function.
type resolver struct {
	req ApprovalRequirement
	in  ResolutionInput
	dir Directory
	at  values.Instant

	candidates   []Candidate
	excluded     []Exclusion
	seen         map[string]bool
	fallbackUsed bool

	requesterChain map[string]bool
}

func (r *resolver) run() error {
	r.seen = map[string]bool{}
	if err := r.loadRequesterChain(); err != nil {
		return err
	}
	direct, err := resolveExpression(r.req.Candidates, r.dir, r.at)
	if err != nil {
		return err
	}
	if err := r.admit(direct, SourceDirect); err != nil {
		return err
	}
	if err := r.admitDelegates(direct); err != nil {
		return err
	}
	if !r.needsFallback() {
		return nil
	}
	fb := r.req.Escalation.Fallback
	if r.req.Escalation.OnDeadline != EscalationReassignToFallback || fb == nil {
		return nil
	}
	r.fallbackUsed = true
	set, err := resolveExpression(*fb, r.dir, r.at)
	if err != nil {
		return err
	}
	// Delegations are deliberately not expanded off the fallback set. One
	// widening step is escalation; chaining a delegation onto it would be a
	// second, and the two together are how authority quietly travels.
	return r.admit(set, SourceFallback)
}

// needsFallback reports whether the primary pass left the requirement
// unfillable, or the deadline has passed and escalation is due.
func (r *resolver) needsFallback() bool {
	return uint32(len(r.candidates)) < r.req.Quorum.MinApprovals || r.in.DeadlinePassed
}

func (r *resolver) loadRequesterChain() error {
	r.requesterChain = map[string]bool{}
	if !r.req.Separation.ForbidRequesterManagerChain {
		return nil
	}
	up, err := r.dir.ManagerChain(r.in.RequesterPrincipalID, r.at)
	if err != nil {
		return newError("Resolve", "manager_chain", ErrDirectory, "%v", err)
	}
	for _, id := range up {
		r.requesterChain[id] = true
	}
	return nil
}

// admit puts each principal in set through the authority floor and the
// separation constraints, recording either a candidate or an exclusion.
func (r *resolver) admit(set map[string]string, via CandidateSource) error {
	for _, id := range sortedKeys(set) {
		if r.seen[id] {
			continue
		}
		facts, ok, err := r.dir.Facts(id, r.at)
		if err != nil {
			return newError("Resolve", "facts", ErrDirectory, "%v", err)
		}
		r.seen[id] = true
		if !ok {
			r.exclude(id, RulePrincipalUnknown, "directory does not know this principal")
			continue
		}
		if !r.available(facts) {
			continue
		}
		if !facts.HoldsAll(r.req.AuthorityFloor) {
			rule := RuleAuthorityFloor
			if via == SourceFallback {
				rule = RuleFallbackNoBroadening
			}
			r.exclude(id, rule, "does not hold "+strings.Join(facts.Missing(r.req.AuthorityFloor), ","))
			continue
		}
		if r.violatesSeparation(id, "") {
			continue
		}
		r.candidates = append(r.candidates, Candidate{
			PrincipalID:          id,
			Via:                  via,
			TermRef:              set[id],
			IdentityAssuranceRef: facts.IdentityAssuranceRef,
		})
	}
	return nil
}

// admitDelegates expands each directly-named principal into the delegates who
// hold authority from them. A delegate is checked against the delegation's
// bounds, not against their own roles: the whole point of delegation is that
// the authority is borrowed, and the whole point of Covers is that it can only
// ever be borrowed downward.
func (r *resolver) admitDelegates(direct map[string]string) error {
	for _, from := range sortedKeys(direct) {
		dels, err := r.dir.DelegationsFrom(from, r.at)
		if err != nil {
			return newError("Resolve", "delegations", ErrDirectory, "%v", err)
		}
		for _, d := range dels {
			if err := d.Validate(); err != nil {
				r.exclude(d.ToPrincipalID, RuleDelegationOutOfScope, "delegation is malformed")
				continue
			}
			if !d.ActiveAt(r.at) {
				r.exclude(d.ToPrincipalID, RuleDelegationExpired,
					"delegation "+d.DelegationID+" is not valid at "+r.at.String())
				continue
			}
			if !d.Covers(r.req.RequirementID, r.req.AuthorityFloor, r.termScopeFor(direct[from])) {
				r.exclude(d.ToPrincipalID, RuleDelegationOutOfScope,
					"delegation "+d.DelegationID+" does not cover "+r.req.RequirementID+
						" at the required authority and scope")
				continue
			}
			if r.seen[d.ToPrincipalID] {
				continue
			}
			facts, ok, err := r.dir.Facts(d.ToPrincipalID, r.at)
			if err != nil {
				return newError("Resolve", "facts", ErrDirectory, "%v", err)
			}
			r.seen[d.ToPrincipalID] = true
			if !ok {
				r.exclude(d.ToPrincipalID, RulePrincipalUnknown,
					"directory does not know this principal")
				continue
			}
			if !r.available(facts) {
				continue
			}
			if r.violatesSeparation(d.ToPrincipalID, d.FromPrincipalID) {
				continue
			}
			r.candidates = append(r.candidates, Candidate{
				PrincipalID:          d.ToPrincipalID,
				Via:                  SourceDelegated,
				TermRef:              direct[from],
				DelegationID:         d.DelegationID,
				DelegatedFrom:        d.FromPrincipalID,
				DelegationExpiry:     d.Expiry,
				IdentityAssuranceRef: facts.IdentityAssuranceRef,
			})
		}
	}
	return nil
}

// termScopeFor recovers the scope of the term that produced a principal, so a
// delegation can be checked against the frame the authority was resolved in.
func (r *resolver) termScopeFor(termRef string) Scope {
	for _, t := range r.req.Candidates.Terms() {
		if t.Ref() == termRef {
			return t.Scope
		}
	}
	return Scope{}
}

func (r *resolver) available(f PrincipalFacts) bool {
	if !f.Active {
		r.exclude(f.PrincipalID, RulePrincipalInactive, "principal is not an active actor")
		return false
	}
	if !f.Available {
		r.exclude(f.PrincipalID, RulePrincipalUnavailable, "principal cannot act at the effective time")
		return false
	}
	return true
}

// violatesSeparation applies every separation constraint. delegatedFrom is the
// delegator when the candidate is borrowing authority, so that a requester who
// delegates to a colleague cannot approve their own request by proxy.
func (r *resolver) violatesSeparation(id, delegatedFrom string) bool {
	c := r.req.Separation
	if c.RequesterMayNotApprove {
		if id == r.in.RequesterPrincipalID {
			r.exclude(id, RuleRequesterIsApprover, "principal raised this proposal")
			return true
		}
		if delegatedFrom != "" && delegatedFrom == r.in.RequesterPrincipalID {
			r.exclude(id, RuleRequesterIsApprover,
				"principal would act on the requester's own delegated authority")
			return true
		}
	}
	if c.SubjectMayNotApprove {
		for _, s := range r.in.SubjectPrincipalIDs {
			if id == s {
				r.exclude(id, RuleSubjectIsApprover, "principal is the subject of this proposal")
				return true
			}
		}
	}
	if c.OneRequirementPerPrincipal {
		if other, ok := r.in.ClaimedBy[id]; ok && other != r.req.RequirementID {
			r.exclude(id, RuleDualRole, "principal already fills "+other)
			return true
		}
	}
	if c.ForbidRequesterManagerChain && r.requesterChain[id] {
		r.exclude(id, RuleManagerChainConflict, "principal is on the requester's manager chain")
		return true
	}
	return false
}

func (r *resolver) exclude(id, rule, reason string) {
	if id == "" {
		return
	}
	for _, e := range r.excluded {
		if e.PrincipalID == id && e.RuleID == rule {
			return
		}
	}
	r.excluded = append(r.excluded, Exclusion{PrincipalID: id, RuleID: rule, Reason: reason})
}

// resolveExpression evaluates the compiled tree against the directory and
// returns principal id -> the term reference that produced them. ANY and QUORUM
// union their children, ALL intersects them, and QUORUM additionally yields
// nothing at all unless MinDistinct children each contributed somebody.
func resolveExpression(e Expression, dir Directory, at values.Instant) (map[string]string, error) {
	if e.Kind.IsLeaf() {
		t := e.term()
		// A pinned principal needs no directory lookup: the expression already
		// names them. Whether that person is real, active, available and
		// carries the required authority is still decided from their facts, so
		// pinning never skips a check - it only skips a search.
		if e.Kind == ExprNamed {
			return map[string]string{e.PrincipalID: t.Ref()}, nil
		}
		ids, err := dir.Holders(t, at)
		if err != nil {
			return nil, newError("Resolve", "holders", ErrDirectory, "%s: %v", t.Ref(), err)
		}
		out := make(map[string]string, len(ids))
		for _, id := range ids {
			if id != "" {
				out[id] = t.Ref()
			}
		}
		return out, nil
	}

	sets := make([]map[string]string, 0, len(e.Children))
	for _, c := range e.Children {
		s, err := resolveExpression(c, dir, at)
		if err != nil {
			return nil, err
		}
		sets = append(sets, s)
	}

	switch e.Kind {
	case ExprAll:
		return intersect(sets), nil
	case ExprQuorum:
		contributing := uint32(0)
		for _, s := range sets {
			if len(s) > 0 {
				contributing++
			}
		}
		if contributing < e.MinDistinct {
			return map[string]string{}, nil
		}
		return union(sets), nil
	default:
		return union(sets), nil
	}
}

func union(sets []map[string]string) map[string]string {
	out := map[string]string{}
	for _, s := range sets {
		for id, ref := range s {
			if prev, ok := out[id]; !ok || ref < prev {
				out[id] = ref
			}
		}
	}
	return out
}

func intersect(sets []map[string]string) map[string]string {
	if len(sets) == 0 {
		return map[string]string{}
	}
	out := map[string]string{}
	for id, ref := range sets[0] {
		in := true
		for _, s := range sets[1:] {
			other, ok := s[id]
			if !ok {
				in = false
				break
			}
			if other < ref {
				ref = other
			}
		}
		if in {
			out[id] = ref
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
