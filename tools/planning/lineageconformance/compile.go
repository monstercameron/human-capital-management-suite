package lineageconformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
)

// Sentinel verification failures.
var (
	// ErrFalseCompletion reports a status or aggregate that claims more
	// than its links and findings support.
	ErrFalseCompletion = errors.New("lineageconformance: report claims completion its links do not support")

	// ErrDigestMismatch reports a report whose digest does not recompute.
	ErrDigestMismatch = errors.New("lineageconformance: report digest does not match its content")
)

// Input is everything the live report reads.
type Input struct {
	AsOf        string
	Witnesses   closurewitness.Report
	Definitions []intent.Definition
	Bindings    []intent.Binding
	Producers   []Producer
	Todos       []closurewitness.TodoRow
	TestExists  map[string]bool
}

// Compile builds the live per-family lineage report. INTENT is proven by
// the case's closure witness binding its source; every other link is
// proven only by a validated producer for exactly that case, so a case no
// producer covers stays UNKNOWN and a partly covered case stays PARTIAL.
func Compile(in Input) (Report, error) {
	if len(in.Witnesses.Witnesses) == 0 {
		return Report{}, fmt.Errorf("lineageconformance: no closure witnesses to generate cases from")
	}
	cases, findings := GenerateCases(CaseInput{Witnesses: in.Witnesses, Definitions: in.Definitions, Bindings: in.Bindings})
	witnesses := map[string]closurewitness.Witness{}
	for _, w := range in.Witnesses.Witnesses {
		witnesses[w.Definition] = w
	}
	caseIDs := map[string]bool{}
	for _, c := range cases {
		caseIDs[c.ID] = true
	}

	type claim struct {
		producer Producer
		err      error
	}
	claims := map[string]map[Link][]claim{}
	for _, p := range in.Producers {
		err := ValidateProducer(p, in.Todos, in.TestExists)
		if !caseIDs[p.Case] {
			findings = append(findings, Finding{Case: p.Case, Code: CodeProducerOrphan,
				Detail: fmt.Sprintf("producer %s names a case no witness generates", p.ID)})
			continue
		}
		if err != nil {
			findings = append(findings, Finding{Case: p.Case, Code: CodeProducerInvalid, Detail: err.Error()})
		}
		if claims[p.Case] == nil {
			claims[p.Case] = map[Link][]claim{}
		}
		for _, l := range p.Links {
			claims[p.Case][l] = append(claims[p.Case][l], claim{producer: p, err: err})
		}
	}

	var results []CaseResult
	for _, c := range cases {
		res := CaseResult{Case: c}
		for _, f := range findings {
			if f.Case == c.ID {
				res.Findings = append(res.Findings, f)
			}
		}
		for _, link := range c.Required {
			status := LinkStatus{Link: link, State: StateUnknown}
			if link == LinkIntent {
				if w, ok := witnesses[c.Definition]; ok && sourceBound(w) {
					status.State = StateProven
					status.Detail = "closure witness binds the definition's source"
					status.Evidence = []string{"closurewitness " + w.Definition + " SOURCE=BOUND " + w.Digest}
					res.Links = append(res.Links, status)
					continue
				}
			}
			var invalid []string
			for _, cl := range claims[c.ID][link] {
				if cl.err != nil {
					invalid = append(invalid, cl.err.Error())
					continue
				}
				status.State = StateProven
				status.Evidence = append(status.Evidence, cl.producer.ID+" ("+cl.producer.Todo+": "+strings.Join(cl.producer.Tests, ", ")+")")
			}
			switch {
			case status.State == StateProven:
				status.Detail = "published by a validated lineage producer"
			case len(invalid) > 0:
				status.Detail = strings.Join(invalid, "; ")
				res.Findings = append(res.Findings, Finding{Case: c.ID, Link: link, Code: CodeProducerInvalid, Detail: status.Detail})
			default:
				status.Detail = fmt.Sprintf("no lineage producer implements %s for %s", link, c.ID)
				res.Findings = append(res.Findings, Finding{Case: c.ID, Link: link, Code: CodeNoProducer, Detail: status.Detail})
			}
			res.Links = append(res.Links, status)
		}
		sortFindings(res.Findings)
		res.Status = statusOf(res.Links, res.Findings)
		results = append(results, res)
	}
	var reportFindings []Finding
	for _, f := range findings {
		if !caseIDs[f.Case] {
			reportFindings = append(reportFindings, f)
		}
	}
	return Assemble(in.AsOf, in.Witnesses.Digest, results, reportFindings), nil
}

func sourceBound(w closurewitness.Witness) bool {
	for _, cs := range w.Classes {
		if cs.Class == closurewitness.ClassSource {
			return cs.State == closurewitness.StateBound
		}
	}
	return false
}

// Assemble aggregates case results into a sealed report. Complete is true
// only when there is at least one case, every case is COMPLETE and the
// report itself carries no finding.
func Assemble(asOf, witnessDigest string, results []CaseResult, findings []Finding) Report {
	rep := Report{SchemaVersion: SchemaVersion, AsOf: asOf, WitnessDigest: witnessDigest,
		Cases: append([]CaseResult(nil), results...), Findings: append([]Finding(nil), findings...)}
	sort.Slice(rep.Cases, func(i, j int) bool { return rep.Cases[i].Case.ID < rep.Cases[j].Case.ID })
	sortFindings(rep.Findings)
	rep.Counts, rep.ByPath, rep.Complete = aggregate(rep.Cases, rep.Findings)
	rep.Digest = reportDigest(rep)
	return rep
}

func aggregate(cases []CaseResult, findings []Finding) (map[Status]int, map[PathKind]map[Status]int, bool) {
	counts := map[Status]int{}
	byPath := map[PathKind]map[Status]int{}
	for _, s := range Statuses() {
		counts[s] = 0
	}
	complete := len(cases) > 0 && len(findings) == 0
	for _, c := range cases {
		counts[c.Status]++
		if byPath[c.Case.Path] == nil {
			byPath[c.Case.Path] = map[Status]int{}
		}
		byPath[c.Case.Path][c.Status]++
		if c.Status != StatusComplete {
			complete = false
		}
	}
	return counts, byPath, complete
}

// SealDigest recomputes only a report's digest. It is what any publisher
// (or forger) can do, which is why VerifyReport never trusts a digest
// alone and recomputes every status and aggregate.
func SealDigest(rep Report) Report {
	rep.Digest = reportDigest(rep)
	return rep
}

func reportDigest(rep Report) string {
	rep.Digest = ""
	raw, err := json.Marshal(rep)
	if err != nil {
		return ""
	}
	return sum("lineage-report", string(raw))
}

// VerifyReport recomputes every case status from its links and findings,
// every aggregate count and the completion flag, and the digest. A report
// whose aggregate says complete while any link is missing fails with
// ErrFalseCompletion even if its digest was recomputed to match.
func VerifyReport(rep Report) error {
	var problems []error
	for _, c := range rep.Cases {
		for _, link := range c.Case.Required {
			if !hasLink(c.Links, link) {
				problems = append(problems, fmt.Errorf("%w: case %s omits required link %s", ErrFalseCompletion, c.Case.ID, link))
			}
		}
		for _, l := range c.Links {
			if l.State == StateProven && len(l.Evidence) == 0 {
				problems = append(problems, fmt.Errorf("%w: case %s marks %s PROVEN with no evidence", ErrFalseCompletion, c.Case.ID, l.Link))
			}
		}
		if want := statusOf(c.Links, c.Findings); c.Status != want {
			problems = append(problems, fmt.Errorf("%w: case %s reports %s, links and findings give %s", ErrFalseCompletion, c.Case.ID, c.Status, want))
		}
	}
	counts, byPath, complete := aggregate(rep.Cases, rep.Findings)
	if complete != rep.Complete {
		problems = append(problems, fmt.Errorf("%w: aggregate complete=%t, cases give %t", ErrFalseCompletion, rep.Complete, complete))
	}
	for _, s := range Statuses() {
		if rep.Counts[s] != counts[s] {
			problems = append(problems, fmt.Errorf("%w: %s count %d, cases give %d", ErrFalseCompletion, s, rep.Counts[s], counts[s]))
		}
	}
	for path, want := range byPath {
		for s, n := range want {
			if rep.ByPath[path][s] != n {
				problems = append(problems, fmt.Errorf("%w: %s %s count %d, cases give %d", ErrFalseCompletion, path, s, rep.ByPath[path][s], n))
			}
		}
	}
	if rep.Digest != reportDigest(rep) {
		problems = append(problems, ErrDigestMismatch)
	}
	return errors.Join(problems...)
}

func hasLink(links []LinkStatus, l Link) bool {
	for _, s := range links {
		if s.Link == l {
			return true
		}
	}
	return false
}

// MarshalReport renders a report as canonical indented JSON.
func MarshalReport(rep Report) ([]byte, error) {
	out, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("lineageconformance: marshal report: %w", err)
	}
	return append(out, '\n'), nil
}

// Summary renders per-status and per-path counts and one line per case
// naming every link that is not proven.
func Summary(rep Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "lineage conformance as of %s: complete=%t cases=%d digest=%s\n", rep.AsOf, rep.Complete, len(rep.Cases), rep.Digest)
	parts := make([]string, 0, len(Statuses()))
	for _, s := range Statuses() {
		parts = append(parts, fmt.Sprintf("%s=%d", s, rep.Counts[s]))
	}
	fmt.Fprintf(&b, "  %s\n", strings.Join(parts, " "))
	for _, path := range []PathKind{PathRoot, PathChild, PathTrigger} {
		parts = parts[:0]
		for _, s := range Statuses() {
			if n := rep.ByPath[path][s]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s=%d", s, n))
			}
		}
		fmt.Fprintf(&b, "  %-7s %s\n", path, strings.Join(parts, " "))
	}
	for _, c := range rep.Cases {
		var open []string
		for _, l := range c.Links {
			if l.State != StateProven {
				open = append(open, string(l.Link)+"="+string(l.State))
			}
		}
		fmt.Fprintf(&b, "  %-9s %s [%s]\n", c.Status, c.Case.ID, strings.Join(open, " "))
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(&b, "  finding %s %s: %s\n", f.Code, f.Case, f.Detail)
	}
	return b.String()
}
