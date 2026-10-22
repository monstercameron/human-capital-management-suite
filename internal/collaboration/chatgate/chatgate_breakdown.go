package chatgate

import "sort"

// CHATGATE-010: results of a poll addressed to segments, broken down by segment
// without exposing anyone. The standard is k = 5: a group is reported only when
// five or more people in it voted, and a report is refused when subtracting one
// published group from another, or from the total, would leave fewer than five.

// MinGroup is the smallest number of voters a reported group may have.
const MinGroup = 5

// Cell is the votes of everyone who belongs to exactly the same segments of the
// poll's audience (its signature). A poll keeps its tally by cell so that the
// overlap of segments is known without keeping who voted.
type Cell struct {
	Signature []string
	Voters    int
	Options   map[string]int
}

// SegmentResult is one reported group.
type SegmentResult struct {
	Segment string
	Voters  int
	Options map[string]int
}

// Breakdown is what a poll shows by segment.
type Breakdown struct {
	K        int
	Total    int
	Segments []SegmentResult
	// Withheld is how many segments had voters but are not reported, because
	// reporting them would show fewer than K people's votes.
	Withheld int
}

func hasSegment(sig []string, name string) bool { return contains(sig, name) }

// regionVoters counts voters in cells that are in every segment of in and in
// none of out.
func regionVoters(cells []Cell, in, out []string) int {
	n := 0
	for _, c := range cells {
		ok := true
		for _, s := range in {
			ok = ok && hasSegment(c.Signature, s)
		}
		for _, s := range out {
			ok = ok && !hasSegment(c.Signature, s)
		}
		if ok {
			n += c.Voters
		}
	}
	return n
}

func smallOrZero(n, k int) bool { return n == 0 || n >= k }

// maxReported bounds how many groups one poll reports, which bounds the check
// below (it looks at every pair of unions of reported groups).
const maxReported = 6

// safeToAdd is the disclosure rule. A candidate group may be reported with the
// groups already reported only if, for every union A of reported groups and
// every union B of them (B may be empty), the voters in A and not in B are none
// or at least k, and so are the voters outside A (the total minus A). Then no
// sum or difference of reported tallies leaves fewer than k people's votes.
func safeToAdd(cells []Cell, published []string, candidate string, k int) bool {
	if len(published) >= maxReported || regionVoters(cells, []string{candidate}, nil) < k {
		return false
	}
	set := append(append([]string(nil), published...), candidate)
	atoms := map[int]int{}
	for _, c := range cells {
		mask := 0
		for i, g := range set {
			if hasSegment(c.Signature, g) {
				mask |= 1 << i
			}
		}
		atoms[mask] += c.Voters
	}
	count := func(pred func(mask int) bool) int {
		n := 0
		for mask, v := range atoms {
			if pred(mask) {
				n += v
			}
		}
		return n
	}
	for a := 1; a < 1<<len(set); a++ {
		if !smallOrZero(count(func(m int) bool { return m&a == 0 }), k) {
			return false
		}
		for b := 1; b < 1<<len(set); b++ {
			if !smallOrZero(count(func(m int) bool { return m&a != 0 && m&b == 0 }), k) {
				return false
			}
		}
	}
	return true
}

// BreakDown reports a poll's results by segment. k below MinGroup is raised to
// it: no caller can ask for a smaller group.
func BreakDown(cells []Cell, k int) Breakdown {
	if k < MinGroup {
		k = MinGroup
	}
	out := Breakdown{K: k}
	names := []string{}
	for _, c := range cells {
		out.Total += c.Voters
		for _, s := range c.Signature {
			if !contains(names, s) {
				names = append(names, s)
			}
		}
	}
	size := map[string]int{}
	for _, n := range names {
		size[n] = regionVoters(cells, []string{n}, nil)
	}
	sort.Slice(names, func(i, j int) bool {
		if size[names[i]] != size[names[j]] {
			return size[names[i]] > size[names[j]]
		}
		return names[i] < names[j]
	})
	published := []string{}
	for _, n := range names {
		if size[n] > 0 && safeToAdd(cells, published, n, k) {
			published = append(published, n)
		} else if size[n] > 0 {
			out.Withheld++
		}
	}
	sort.Strings(published)
	for _, n := range published {
		r := SegmentResult{Segment: n, Voters: size[n], Options: map[string]int{}}
		for _, c := range cells {
			if hasSegment(c.Signature, n) {
				for o, v := range c.Options {
					r.Options[o] += v
				}
			}
		}
		out.Segments = append(out.Segments, r)
	}
	return out
}

// PollSignature is the segments of a poll's audience that a voter is in. It is
// empty for somebody outside the audience, who may not vote.
func PollSignature(st SegmentState, person string, audience []string) []string {
	return st.Signature(person, audience)
}
