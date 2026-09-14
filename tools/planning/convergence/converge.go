package convergence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// GapIdentity is the stable identity of the gap key (owner, contract,
// subject). It is independent of compiler, code, wording and input order.
func GapIdentity(owner string, contract Contract, subject string) string {
	sum := sha256.Sum256([]byte(owner + "\x00" + string(contract) + "\x00" + subject))
	return "GAP-" + hex.EncodeToString(sum[:8])
}

type gapKey struct {
	owner    string
	contract Contract
	subject  string
}

// wellFormedOwner reports whether an owner is a single printable token. An
// owner carrying whitespace or control characters could forge a collision
// with another key, so it is refused rather than normalised.
func wellFormedOwner(owner string) bool {
	for _, r := range owner {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Converge compiles the convergence register from snap. It is pure and
// total: every defect in the inputs is a named unknown or finding, never an
// error, and the same snapshot always yields the same bytes.
func Converge(snap Snapshot) Report {
	c := compiler{
		selected: map[string]SelectedOwner{},
		known:    map[string]bool{},
		todos:    map[string]TodoRef{},
	}
	for _, s := range snap.Selected {
		if _, dup := c.selected[s.Owner]; !dup {
			c.selected[s.Owner] = s
		}
		c.known[s.Owner] = true
	}
	for _, owner := range snap.KnownOwners {
		c.known[owner] = true
	}
	for _, todo := range snap.Todos {
		if _, dup := c.todos[todo.ID]; !dup {
			c.todos[todo.ID] = todo
		}
	}
	c.unknowns = append(c.unknowns, snap.Unknowns...)

	report := Report{SchemaVersion: SchemaVersion}
	observations := append([]Observation(nil), snap.Observations...)
	facts, inert := c.facts(snap.Facts, snap.Consumers)
	report.Facts = facts
	observations = append(observations, inert...)

	groups := map[gapKey][]Observation{}
	for _, o := range observations {
		o.Owner = strings.TrimSpace(o.Owner)
		o.Subject = strings.TrimSpace(o.Subject)
		if !wellFormedOwner(o.Owner) {
			c.unknown(UnknownMalformedOwner, o.Compiler+":"+o.Code, fmt.Sprintf("owner %q is not a single printable token: %s", o.Owner, o.Detail))
			continue
		}
		if !o.Contract.Valid() {
			c.unknown(UnknownUnmappedCode, o.Compiler+":"+o.Code, fmt.Sprintf("no contract class for owner %q: %s", o.Owner, o.Detail))
			continue
		}
		key := gapKey{owner: o.Owner, contract: o.Contract, subject: o.Subject}
		groups[key] = append(groups[key], o)
		report.Totals.Observations++
	}

	keys := make([]gapKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	usedRejections := map[int]bool{}
	for _, key := range keys {
		gap := c.gap(key, groups[key], snap, usedRejections)
		report.Gaps = append(report.Gaps, gap)
	}
	for i, r := range snap.Rejections {
		if !usedRejections[i] {
			c.finding(FindingStaleRejection, GapIdentity(r.Owner, r.Contract, r.Subject), fmt.Sprintf("rejection of %s/%s/%s matches no selected-scope gap", r.Owner, r.Contract, r.Subject))
		}
	}
	sort.Slice(report.Gaps, func(i, j int) bool { return report.Gaps[i].Identity < report.Gaps[j].Identity })
	report.Proposals = c.proposals
	sort.Slice(report.Proposals, func(i, j int) bool { return report.Proposals[i].ID < report.Proposals[j].ID })
	report.Unknowns = dedupUnknowns(c.unknowns)
	report.Findings = dedupFindings(c.findings)
	report.Totals = totals(report)
	report.Result = ResultResolved
	if report.Totals.SelectedUnresolved > 0 || len(report.Unknowns) > 0 || len(report.Findings) > 0 {
		report.Result = ResultUnresolved
	}
	report.Digest = digestOf(report)
	return report
}

type compiler struct {
	selected  map[string]SelectedOwner
	known     map[string]bool
	todos     map[string]TodoRef
	unknowns  []Unknown
	findings  []Finding
	proposals []ProposedTodo
}

func (c *compiler) unknown(code, ref, detail string) {
	c.unknowns = append(c.unknowns, Unknown{Code: code, Ref: ref, Detail: detail})
}

func (c *compiler) finding(code, ref, detail string) {
	c.findings = append(c.findings, Finding{Code: code, Ref: ref, Detail: detail})
}

// facts resolves every selected fact's downstream consumers and emits one
// DOWNSTREAM_CONSUMER observation per fact nothing downstream consumes. A
// consumer whose ref is one of the fact's own tokens is the fact itself and
// never counts.
func (c *compiler) facts(facts []Fact, consumers []ConsumerRef) ([]FactResult, []Observation) {
	var results []FactResult
	var inert []Observation
	seen := map[string]bool{}
	valid := make([]ConsumerRef, 0, len(consumers))
	for _, ref := range consumers {
		if !validLayer(ref.Layer) {
			c.unknown(UnknownInvalidConsumer, ref.Ref, fmt.Sprintf("consumer layer %q is not a downstream layer", ref.Layer))
			continue
		}
		valid = append(valid, ref)
	}
	for _, fact := range facts {
		if seen[fact.ID] {
			c.unknown(UnknownDuplicateFact, fact.ID, "selected fact declared twice; only the first declaration counts")
			continue
		}
		seen[fact.ID] = true
		tokens := map[string]bool{}
		for _, tok := range fact.Tokens {
			if tok = strings.TrimSpace(tok); tok != "" {
				tokens[tok] = true
			}
		}
		hits := map[ConsumerHit]bool{}
		for _, ref := range valid {
			if tokens[ref.Ref] {
				continue
			}
			for _, tok := range ref.Tokens {
				if tokens[tok] {
					hits[ConsumerHit{Layer: ref.Layer, Ref: ref.Ref}] = true
					break
				}
			}
		}
		result := FactResult{ID: fact.ID, Kind: fact.Kind, Owner: fact.Owner, Consumers: []ConsumerHit{}}
		for hit := range hits {
			result.Consumers = append(result.Consumers, hit)
		}
		sort.Slice(result.Consumers, func(i, j int) bool {
			if result.Consumers[i].Layer != result.Consumers[j].Layer {
				return result.Consumers[i].Layer < result.Consumers[j].Layer
			}
			return result.Consumers[i].Ref < result.Consumers[j].Ref
		})
		if len(result.Consumers) == 0 {
			result.Inert = true
			detail := fmt.Sprintf("selected fact %s changes no downstream slice, model, API, threat or test", fact.ID)
			if len(tokens) == 0 {
				detail += " (it carries no token a consumer could reference)"
			}
			inert = append(inert, Observation{Compiler: CompilerFacts, Code: InertFactCode, Owner: fact.Owner, Contract: ContractDownstreamConsumer, Subject: fact.ID, Detail: detail})
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results, inert
}

// gap builds one deduplicated gap and resolves it.
func (c *compiler) gap(key gapKey, observations []Observation, snap Snapshot, usedRejections map[int]bool) Gap {
	identity := GapIdentity(key.owner, key.contract, key.subject)
	gap := Gap{Identity: identity, Owner: key.owner, Contract: key.contract, Subject: key.subject}
	compilers := map[string]bool{}
	obs := map[Observation]bool{}
	for _, o := range observations {
		compilers[o.Compiler] = true
		obs[Observation{Compiler: o.Compiler, Code: o.Code, Detail: o.Detail}] = true
	}
	for name := range compilers {
		gap.Compilers = append(gap.Compilers, name)
	}
	sort.Strings(gap.Compilers)
	for o := range obs {
		gap.Observations = append(gap.Observations, o)
	}
	sort.Slice(gap.Observations, func(i, j int) bool {
		a, b := gap.Observations[i], gap.Observations[j]
		if a.Compiler != b.Compiler {
			return a.Compiler < b.Compiler
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Detail < b.Detail
	})

	owner, selected := c.selected[key.owner]
	switch {
	case key.owner == "":
		gap.Scope = ScopeUnknown
		c.unknown(UnknownOwnerless, identity, fmt.Sprintf("%s gap %q names no owner, so it cannot be placed inside or outside the selection", key.contract, key.subject))
	case selected:
		gap.Scope = ScopeSelected
	case c.known[key.owner]:
		gap.Scope = ScopeOutOfSelection
	default:
		gap.Scope = ScopeUnknown
		c.unknown(UnknownUnscopedOwner, identity, fmt.Sprintf("owner %q is neither a selected owner nor a known out-of-selection owner", key.owner))
	}

	switch gap.Scope {
	case ScopeOutOfSelection:
		gap.Resolution = Resolution{Kind: ResolutionNotRequired}
		return gap
	case ScopeUnknown:
		gap.Resolution = Resolution{Kind: ResolutionUnresolved}
		return gap
	}

	for i, r := range snap.Rejections {
		if r.Owner != key.owner || r.Contract != key.contract || r.Subject != key.subject {
			continue
		}
		usedRejections[i] = true
		if strings.TrimSpace(r.Rationale) == "" || strings.TrimSpace(r.DecidedBy) == "" {
			c.finding(FindingInvalidRejection, identity, "a rejection needs both a rationale and a decider; it was not applied")
			continue
		}
		gap.Resolution = Resolution{Kind: ResolutionRejected, Rationale: r.Rationale, DecidedBy: r.DecidedBy}
		return gap
	}

	claimants := map[string]bool{}
	for _, cl := range snap.Claims {
		if cl.Owner != key.owner || cl.Contract != key.contract || (cl.Subject != "" && cl.Subject != key.subject) {
			continue
		}
		todo, ok := c.todos[cl.TodoID]
		switch {
		case !ok:
			c.finding(FindingClaimTodoMissing, identity, fmt.Sprintf("claim from %s names todo %s, which is not in the backlog", cl.Source, cl.TodoID))
		case todo.Done || todo.Retired:
			c.finding(FindingClaimTodoClosed, identity, fmt.Sprintf("todo %s claims this gap but is already closed; a ticked or retired todo cannot own open work", cl.TodoID))
		default:
			claimants[cl.TodoID] = true
		}
	}
	switch len(claimants) {
	case 0:
	case 1:
		for id := range claimants {
			gap.Resolution = Resolution{Kind: ResolutionExistingTodo, TodoID: id}
		}
		return gap
	default:
		ids := make([]string, 0, len(claimants))
		for id := range claimants {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		c.finding(FindingAmbiguousClaim, identity, fmt.Sprintf("open todos %s all claim this gap; exactly one must own it", strings.Join(ids, ", ")))
		gap.Resolution = Resolution{Kind: ResolutionUnresolved}
		return gap
	}

	proposal := Propose(owner, key.contract, key.subject)
	if proposal.Phase == "" {
		c.finding(FindingProposalIncomplete, identity, fmt.Sprintf("selected owner %s declares no phase, so no atomic todo can be proposed", key.owner))
		gap.Resolution = Resolution{Kind: ResolutionUnresolved}
		return gap
	}
	if _, exists := c.todos[proposal.ID]; exists {
		c.finding(FindingProposalCollision, identity, fmt.Sprintf("proposed id %s already names a backlog todo that does not claim this gap", proposal.ID))
		gap.Resolution = Resolution{Kind: ResolutionUnresolved}
		return gap
	}
	c.proposals = append(c.proposals, proposal)
	gap.Resolution = Resolution{Kind: ResolutionProposedTodo, TodoID: proposal.ID}
	return gap
}

// Propose derives the one atomic todo that closes a selected gap. Its id
// and oracle are pure functions of the gap key, so re-proposing the same
// gap can never mint a second todo.
func Propose(owner SelectedOwner, contract Contract, subject string) ProposedTodo {
	identity := GapIdentity(owner.Owner, contract, subject)
	sum := sha256.Sum256([]byte(identity))
	oracle := "TestConvergence_" + camel(owner.Owner) + "_" + camel(string(contract))
	title := fmt.Sprintf("Close the %s contract for %s", contract, owner.Owner)
	if subject != "" {
		oracle += "_" + strings.ToUpper(hex.EncodeToString(sum[:3]))
		title += " (" + subject + ")"
	}
	return ProposedTodo{
		ID:          "CONV-" + strings.ToUpper(hex.EncodeToString(sum[:4])),
		GapIdentity: identity,
		Title:       title,
		Owner:       owner.Owner,
		Contract:    contract,
		Subject:     subject,
		Phase:       strings.TrimSpace(owner.Phase),
		Oracle:      oracle,
	}
}

// camel turns an identifier such as "hcmnext.people.promote_worker/v1"
// into a Go-identifier fragment "HcmnextPeoplePromoteWorkerV1".
func camel(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upper = true
			continue
		}
		if r > unicode.MaxASCII {
			upper = true
			continue
		}
		if upper {
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func dedupUnknowns(in []Unknown) []Unknown {
	seen := map[Unknown]bool{}
	out := []Unknown{}
	for _, u := range in {
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		if out[i].Ref != out[j].Ref {
			return out[i].Ref < out[j].Ref
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

func dedupFindings(in []Finding) []Finding {
	seen := map[Finding]bool{}
	out := []Finding{}
	for _, f := range in {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		if out[i].Ref != out[j].Ref {
			return out[i].Ref < out[j].Ref
		}
		return out[i].Detail < out[j].Detail
	})
	return out
}

func totals(r Report) Totals {
	t := Totals{
		Observations: r.Totals.Observations,
		Gaps:         len(r.Gaps),
		ByScope:      map[Scope]int{},
		ByResolution: map[ResolutionKind]int{},
		Facts:        len(r.Facts),
		Proposals:    len(r.Proposals),
		Unknowns:     len(r.Unknowns),
		Findings:     len(r.Findings),
	}
	for _, g := range r.Gaps {
		t.ByScope[g.Scope]++
		t.ByResolution[g.Resolution.Kind]++
		if g.Scope == ScopeSelected && g.Resolution.Kind == ResolutionUnresolved {
			t.SelectedUnresolved++
		}
	}
	t.Deduplicated = t.Observations - len(r.Gaps)
	for _, f := range r.Facts {
		if f.Inert {
			t.InertFacts++
		}
	}
	return t
}

// digestOf binds every field of the report except the digest itself.
func digestOf(r Report) string {
	r.Digest = ""
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// MarshalReport renders a report as canonical indented JSON. Every slice is
// sorted by Converge and encoding/json sorts map keys, so the bytes are a
// pure function of the snapshot.
func MarshalReport(r Report) ([]byte, error) {
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("convergence: marshal report: %w", err)
	}
	return append(out, '\n'), nil
}

// Verify recompiles snap and names every way a supplied report disagrees
// with it or with its own digest: a hidden unknown, a dropped gap or a
// hand-promoted resolution is caught even when the digest was recomputed.
func Verify(snap Snapshot, r Report) []string {
	var mismatches []string
	fresh := Converge(snap)
	if r.Digest != digestOf(r) {
		mismatches = append(mismatches, "report digest does not match its own content")
	}
	if r.Digest != fresh.Digest {
		mismatches = append(mismatches, fmt.Sprintf("report digest %s differs from the fresh compilation %s", r.Digest, fresh.Digest))
	}
	if len(r.Unknowns) != len(fresh.Unknowns) {
		mismatches = append(mismatches, fmt.Sprintf("report carries %d unknowns, the fresh compilation surfaces %d", len(r.Unknowns), len(fresh.Unknowns)))
	}
	if r.Result != fresh.Result {
		mismatches = append(mismatches, fmt.Sprintf("report result %s, the fresh compilation is %s", r.Result, fresh.Result))
	}
	freshGaps := map[string]Gap{}
	for _, g := range fresh.Gaps {
		freshGaps[g.Identity] = g
	}
	seen := map[string]bool{}
	for _, g := range r.Gaps {
		seen[g.Identity] = true
		want, ok := freshGaps[g.Identity]
		switch {
		case !ok:
			mismatches = append(mismatches, "gap "+g.Identity+" is not produced by the inputs")
		case g.Resolution != want.Resolution || g.Scope != want.Scope:
			mismatches = append(mismatches, fmt.Sprintf("gap %s claims %s/%s, the inputs give %s/%s", g.Identity, g.Scope, g.Resolution.Kind, want.Scope, want.Resolution.Kind))
		}
	}
	for id := range freshGaps {
		if !seen[id] {
			mismatches = append(mismatches, "gap "+id+" was dropped from the report")
		}
	}
	sort.Strings(mismatches)
	return mismatches
}
