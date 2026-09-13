package convergence

import (
	"bytes"
	"fmt"
	"sort"
)

// Delta is the exact bounded difference between two convergence reports.
type Delta struct {
	Added             []string `json:"added"`
	Removed           []string `json:"removed"`
	ResolutionChanged []string `json:"resolution_changed"`
}

// Empty reports whether the delta names no change at all.
func (d Delta) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.ResolutionChanged) == 0
}

// Diff names every gap identity next adds or removes relative to prev, and
// every identity whose scope or resolution changed.
func Diff(prev, next Report) Delta {
	before := map[string]Gap{}
	for _, g := range prev.Gaps {
		before[g.Identity] = g
	}
	d := Delta{Added: []string{}, Removed: []string{}, ResolutionChanged: []string{}}
	after := map[string]bool{}
	for _, g := range next.Gaps {
		after[g.Identity] = true
		old, ok := before[g.Identity]
		switch {
		case !ok:
			d.Added = append(d.Added, g.Identity)
		case old.Scope != g.Scope || old.Resolution != g.Resolution:
			d.ResolutionChanged = append(d.ResolutionChanged, g.Identity)
		}
	}
	for id := range before {
		if !after[id] {
			d.Removed = append(d.Removed, id)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.ResolutionChanged)
	return d
}

// Adopt returns a copy of snap in which every proposal is an open backlog
// todo claiming exactly its own gap key, which is what the orchestrator
// writing the proposals into the backlog would produce.
func Adopt(snap Snapshot, proposals []ProposedTodo) Snapshot {
	out := snap
	out.Todos = append([]TodoRef(nil), snap.Todos...)
	out.Claims = append([]Claim(nil), snap.Claims...)
	for _, p := range proposals {
		out.Todos = append(out.Todos, TodoRef{ID: p.ID, Phase: p.Phase})
		out.Claims = append(out.Claims, Claim{TodoID: p.ID, Owner: p.Owner, Contract: p.Contract, Subject: p.Subject, Source: "adopted proposal"})
	}
	return out
}

// Pass is one compiler pass of a fixed-point run.
type Pass struct {
	Index      int    `json:"index"`
	Digest     string `json:"digest"`
	Identities int    `json:"identities"`
	Proposals  int    `json:"proposals"`
	Delta      Delta  `json:"delta_from_previous"`
}

// FixedPoint is the outcome of running the gate to a fixed point.
type FixedPoint struct {
	Passes []Pass `json:"passes"`
	// Stable is true when an unchanged re-run is byte-identical and no pass
	// after the first introduced a gap identity.
	Stable bool `json:"stable"`
	// Reasons names every way stability failed.
	Reasons []string `json:"reasons"`
	// First is the first-pass report, whose proposals are what the
	// orchestrator must adopt; Final is the report after adoption.
	First Report `json:"-"`
	Final Report `json:"-"`
}

// RunToFixedPoint compiles snap, re-compiles it unchanged to prove the pass
// is byte-identical, then adopts the proposals and re-compiles until two
// consecutive passes are byte-identical or maxPasses is reached. Adoption
// may change resolutions from PROPOSED_TODO to EXISTING_TODO; it may never
// add a gap identity or propose a second todo for a gap.
func RunToFixedPoint(snap Snapshot, maxPasses int) FixedPoint {
	return runToFixedPoint(snap, maxPasses, Converge)
}

// runToFixedPoint is RunToFixedPoint over an injectable compiler, so the
// instability checks can be proven against a compiler that is not pure.
func runToFixedPoint(snap Snapshot, maxPasses int, converge func(Snapshot) Report) FixedPoint {
	if maxPasses < 3 {
		maxPasses = 3
	}
	fp := FixedPoint{Stable: true, Reasons: []string{}}
	first := converge(snap)
	firstBytes, _ := MarshalReport(first)
	fp.First = first
	fp.Passes = append(fp.Passes, Pass{Index: 1, Digest: first.Digest, Identities: len(first.Gaps), Proposals: len(first.Proposals), Delta: Diff(first, first)})

	again := converge(snap)
	againBytes, _ := MarshalReport(again)
	if !bytes.Equal(firstBytes, againBytes) {
		fp.Stable = false
		fp.Reasons = append(fp.Reasons, "an unchanged second pass is not byte-identical to the first")
	}
	if d := Diff(first, again); len(d.Added) > 0 {
		fp.Stable = false
		fp.Reasons = append(fp.Reasons, fmt.Sprintf("an unchanged second pass added gap identities %v", d.Added))
	}

	current, currentBytes := first, firstBytes
	adopted := map[string]bool{}
	for index := 2; index <= maxPasses; index++ {
		var fresh []ProposedTodo
		for _, p := range current.Proposals {
			if !adopted[p.ID] {
				adopted[p.ID] = true
				fresh = append(fresh, p)
			}
		}
		snap = Adopt(snap, fresh)
		next := converge(snap)
		for _, p := range next.Proposals {
			if adopted[p.ID] {
				fp.Stable = false
				fp.Reasons = append(fp.Reasons, fmt.Sprintf("pass %d re-proposed already adopted todo %s", index, p.ID))
			}
		}
		nextBytes, _ := MarshalReport(next)
		delta := Diff(current, next)
		fp.Passes = append(fp.Passes, Pass{Index: index, Digest: next.Digest, Identities: len(next.Gaps), Proposals: len(next.Proposals), Delta: delta})
		if len(delta.Added) > 0 {
			fp.Stable = false
			fp.Reasons = append(fp.Reasons, fmt.Sprintf("pass %d added gap identities %v", index, delta.Added))
		}
		if len(delta.Removed) > 0 {
			fp.Stable = false
			fp.Reasons = append(fp.Reasons, fmt.Sprintf("pass %d removed gap identities %v without an input change that closes them", index, delta.Removed))
		}
		done := bytes.Equal(currentBytes, nextBytes)
		current, currentBytes = next, nextBytes
		if done {
			fp.Final = current
			return fp
		}
	}
	fp.Final = current
	fp.Stable = false
	fp.Reasons = append(fp.Reasons, fmt.Sprintf("no two consecutive passes were byte-identical within %d passes", maxPasses))
	return fp
}
