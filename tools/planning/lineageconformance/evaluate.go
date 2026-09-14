package lineageconformance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Record is one lineage node as an owner publishes it. Digest seals every
// identity, causation, watermark, classification and derivation field and
// the payload digest, but not the payload itself, so a redacted view keeps
// the digest a full view carries.
type Record struct {
	ID               string   `json:"id"`
	Tenant           string   `json:"tenant"`
	Case             string   `json:"case"`
	Link             Link     `json:"link"`
	CausedBy         string   `json:"caused_by,omitempty"`
	PrevDigest       string   `json:"prev_digest,omitempty"`
	Watermark        string   `json:"watermark"`
	SourceDigest     string   `json:"source_digest,omitempty"`
	Classification   string   `json:"classification,omitempty"`
	PayloadDigest    string   `json:"payload_digest,omitempty"`
	Payload          string   `json:"payload,omitempty"`
	Redacted         bool     `json:"redacted,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	Trigger          string   `json:"trigger,omitempty"`
	DerivedFrom      []string `json:"derived_from,omitempty"`
	ProjectionDigest string   `json:"projection_digest,omitempty"`
	SourceHead       string   `json:"source_head,omitempty"`
	Corrects         string   `json:"corrects,omitempty"`
	Digest           string   `json:"digest"`
}

// Graph is one tenant's lineage records as a producer returns them.
type Graph struct {
	Tenant  string   `json:"tenant"`
	Records []Record `json:"records"`
}

// Reader is who reads the lineage: the tenant they act in and the
// classifications they are cleared for. An unclassified record is visible
// to every reader of its tenant.
type Reader struct {
	Tenant     string
	Clearances []string
}

func (r Reader) cleared(classification string) bool {
	return classification == "" || slices.Contains(r.Clearances, classification)
}

func sum(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(h[:])
}

// PayloadDigest is the digest of one record payload.
func PayloadDigest(payload string) string { return sum("payload", payload) }

// ComputeDigest recomputes a record's seal.
func (r Record) ComputeDigest() string {
	parts := []string{
		"lineage-record", r.ID, r.Tenant, r.Case, string(r.Link), r.CausedBy, r.PrevDigest,
		r.Watermark, r.SourceDigest, r.Classification, r.PayloadDigest, r.Trigger,
		strings.Join(r.DerivedFrom, ","), r.ProjectionDigest, r.SourceHead, r.Corrects,
	}
	return sum(parts...)
}

// ProjectionReplay is the semantic replay digest of a projection value
// rebuilt from its source event records' digests, in order.
func ProjectionReplay(eventDigests []string) string {
	return sum(append([]string{"projection-replay"}, eventDigests...)...)
}

// Chain orders one case's records in lineage order and links each to its
// predecessor: the first names cause (empty for a ROOT intent) and every
// later record names the record before it. Several CORRECTION records stay
// in their given order, each correcting forward from the last.
func Chain(cause string, records []Record) []Record {
	out := slices.Clone(records)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Link.rank() < out[j].Link.rank() })
	for i := range out {
		if i == 0 {
			out[i].CausedBy = cause
			continue
		}
		out[i].CausedBy = out[i-1].ID
	}
	return out
}

// Seal fills payload digests, predecessor digests and record digests in
// slice order, which must place every cause before its successor. It is
// the publishing step; Evaluate never trusts it and recomputes everything.
func Seal(records []Record) []Record {
	out := slices.Clone(records)
	byID := map[string]int{}
	for i := range out {
		if out[i].Payload != "" && out[i].PayloadDigest == "" {
			out[i].PayloadDigest = PayloadDigest(out[i].Payload)
		}
		out[i].PrevDigest = ""
		if j, ok := byID[out[i].CausedBy]; ok && out[i].CausedBy != "" {
			out[i].PrevDigest = out[j].Digest
		}
		if out[i].Link == LinkProjection && out[i].ProjectionDigest == "" && len(out[i].DerivedFrom) > 0 {
			digests := make([]string, 0, len(out[i].DerivedFrom))
			for _, id := range out[i].DerivedFrom {
				if j, ok := byID[id]; ok {
					digests = append(digests, out[j].Digest)
				}
			}
			if len(digests) == len(out[i].DerivedFrom) {
				out[i].ProjectionDigest = ProjectionReplay(digests)
			}
		}
		out[i].Digest = out[i].ComputeDigest()
		byID[out[i].ID] = i
	}
	return out
}

// View returns what reader may see of g: records outside the reader's
// tenant are withheld entirely, and records above the reader's clearance
// keep their identity, watermark and digests but lose their payload,
// carrying the owner's redaction reason instead.
func View(g Graph, reader Reader) Graph {
	out := Graph{Tenant: g.Tenant}
	if g.Tenant != reader.Tenant {
		return Graph{Tenant: reader.Tenant}
	}
	for _, rec := range g.Records {
		if rec.Tenant != reader.Tenant {
			continue
		}
		if !reader.cleared(rec.Classification) {
			rec.Payload = ""
			rec.Redacted = true
			if strings.TrimSpace(rec.Reason) == "" {
				rec.Reason = "classification " + rec.Classification + " above reader clearance"
			}
		}
		out.Records = append(out.Records, rec)
	}
	return out
}

// GraphDigest is the order-independent digest of a graph's record seals.
func GraphDigest(g Graph) string {
	seals := make([]string, 0, len(g.Records))
	for _, rec := range g.Records {
		seals = append(seals, rec.ID+"="+rec.Digest)
	}
	sort.Strings(seals)
	return sum(append([]string{"lineage-graph", g.Tenant}, seals...)...)
}

// Evaluate runs the shared lineage assertions for one case over the graph
// a reader was given. It never fails: every problem is a finding, and the
// status is derived from link states and findings by the same rule the
// live report and VerifyReport use.
func Evaluate(c Case, g Graph, reader Reader) CaseResult {
	res := CaseResult{Case: c}
	byID := map[string]Record{}
	seen := map[string]string{}
	var own []Record
	for _, rec := range g.Records {
		if prior, dup := seen[rec.ID]; dup && prior != rec.Digest {
			res.Findings = append(res.Findings, Finding{Case: c.ID, Link: rec.Link, Code: CodeHistoryRewritten, Record: rec.ID,
				Detail: "one record identity carries two different seals"})
		}
		seen[rec.ID] = rec.Digest
		byID[rec.ID] = rec
		if rec.Case == c.ID {
			own = append(own, rec)
		}
	}
	if g.Tenant != reader.Tenant {
		res.Findings = append(res.Findings, Finding{Case: c.ID, Code: CodeCrossTenant,
			Detail: fmt.Sprintf("graph of tenant %q handed to a reader of tenant %q", g.Tenant, reader.Tenant)})
	}
	for _, rec := range own {
		if !rec.Link.Valid() {
			res.Findings = append(res.Findings, Finding{Case: c.ID, Link: rec.Link, Code: CodeUnknownLink, Record: rec.ID,
				Detail: "record names a link outside the lineage vocabulary"})
		}
		for _, na := range c.NotApplicable {
			if na.Link == rec.Link {
				res.Findings = append(res.Findings, Finding{Case: c.ID, Link: rec.Link, Code: CodeNotApplicableFound, Record: rec.ID,
					Detail: "definition declares " + na.Reason + " but a record exists"})
			}
		}
	}

	var prev *Record
	for _, link := range c.Required {
		var recs []Record
		for _, rec := range own {
			if rec.Link == link {
				recs = append(recs, rec)
			}
		}
		status := LinkStatus{Link: link}
		switch {
		case len(recs) == 0:
			status.State = StateUnknown
			status.Detail = "no lineage record for " + string(link)
			res.Findings = append(res.Findings, Finding{Case: c.ID, Link: link, Code: CodeLinkMissing,
				Detail: "required link has no lineage record"})
		case len(recs) > 1 && link != LinkCorrection:
			status.State = StateDefect
			status.Detail = fmt.Sprintf("%d records claim %s", len(recs), link)
			res.Findings = append(res.Findings, Finding{Case: c.ID, Link: link, Code: CodeLinkDuplicate, Record: recs[1].ID,
				Detail: status.Detail})
		default:
			var found []Finding
			for i := range recs {
				rec := recs[i]
				found = append(found, checkRecord(c, g, reader, rec, prev, byID)...)
				prev = &recs[i]
			}
			res.Findings = append(res.Findings, found...)
			status.State = StateProven
			for _, rec := range recs {
				status.Evidence = append(status.Evidence, rec.ID+" "+rec.Watermark+" "+rec.Digest)
			}
			if len(found) > 0 {
				status.State = StateDefect
				status.Detail = found[0].Code + ": " + found[0].Detail
			}
		}
		res.Links = append(res.Links, status)
	}
	sortFindings(res.Findings)
	res.Status = statusOf(res.Links, res.Findings)
	return res
}

func checkRecord(c Case, g Graph, reader Reader, rec Record, prev *Record, byID map[string]Record) []Finding {
	var out []Finding
	add := func(code, detail string) {
		out = append(out, Finding{Case: c.ID, Link: rec.Link, Code: code, Record: rec.ID, Detail: detail})
	}
	if rec.Tenant != g.Tenant || rec.Tenant != reader.Tenant {
		add(CodeCrossTenant, fmt.Sprintf("record of tenant %q in lineage of tenant %q read by tenant %q", rec.Tenant, g.Tenant, reader.Tenant))
	}
	if strings.TrimSpace(rec.Watermark) == "" {
		add(CodeWatermarkMissing, "record carries no publisher watermark")
	}
	if rec.Digest == "" || rec.Digest != rec.ComputeDigest() {
		add(CodeDigestMismatch, "record seal does not recompute")
	}
	if rec.Payload != "" && rec.PayloadDigest != PayloadDigest(rec.Payload) {
		add(CodeDigestMismatch, "payload does not match its payload digest")
	}

	// Causation and predecessor digest.
	switch {
	case rec.Link == LinkCausation && c.Path == PathChild:
		cause, ok := byID[rec.CausedBy]
		switch {
		case !ok || cause.Case != c.Parent:
			add(CodeBrokenCausation, fmt.Sprintf("child causation names %q, not a record of parent case %s", rec.CausedBy, c.Parent))
		case cause.Tenant != rec.Tenant:
			add(CodeCrossTenant, "child causation crosses into tenant "+cause.Tenant)
		case rec.PrevDigest != cause.Digest:
			add(CodeBrokenCausation, "child causation predecessor digest does not match the parent record")
		}
	case rec.Link == LinkCausation && c.Path == PathTrigger:
		if rec.Trigger != c.Trigger {
			add(CodeTriggerMismatch, fmt.Sprintf("causation fired by %q, case trigger is %q", rec.Trigger, c.Trigger))
		}
		if rec.CausedBy != "" || rec.PrevDigest != "" {
			add(CodeBrokenCausation, "trigger firing must head its lineage")
		}
	case prev == nil:
		if rec.CausedBy != "" || rec.PrevDigest != "" {
			add(CodeBrokenCausation, "first lineage record names a predecessor")
		}
	default:
		cause, ok := byID[rec.CausedBy]
		switch {
		case rec.CausedBy != prev.ID:
			add(CodeBrokenCausation, fmt.Sprintf("caused by %q, want preceding record %q", rec.CausedBy, prev.ID))
		case !ok || cause.Case != c.ID:
			add(CodeBrokenCausation, "predecessor is not a record of this case")
		case cause.Tenant != rec.Tenant:
			add(CodeCrossTenant, "predecessor belongs to tenant "+cause.Tenant)
		case rec.PrevDigest != cause.Digest:
			add(CodeBrokenCausation, "predecessor digest does not match the preceding record's seal")
		}
	}

	// Redaction safety for this reader.
	if !reader.cleared(rec.Classification) || rec.Tenant != reader.Tenant {
		if rec.Payload != "" || !rec.Redacted {
			add(CodeOverDisclosed, fmt.Sprintf("classification %q disclosed to a reader of tenant %q cleared for %v", rec.Classification, reader.Tenant, reader.Clearances))
		}
		if rec.Redacted && strings.TrimSpace(rec.Reason) == "" {
			add(CodeRedactionReason, "redacted record carries no reason, so partial lineage is not explained")
		}
	}

	switch rec.Link {
	case LinkProjection:
		out = append(out, checkProjection(c, rec, byID)...)
	case LinkCorrection:
		target, ok := byID[rec.Corrects]
		switch {
		case rec.Corrects == "" || !ok:
			add(CodeHistoryBroken, fmt.Sprintf("correction targets %q, which is not in the lineage", rec.Corrects))
		case target.Case != c.ID || target.Tenant != rec.Tenant:
			add(CodeHistoryBroken, "correction targets a record outside this case or tenant")
		case target.ID == rec.ID:
			add(CodeHistoryBroken, "correction targets itself")
		}
	}
	return out
}

func checkProjection(c Case, rec Record, byID map[string]Record) []Finding {
	var out []Finding
	add := func(detail string) {
		out = append(out, Finding{Case: c.ID, Link: LinkProjection, Code: CodeNotRebuildable, Record: rec.ID, Detail: detail})
	}
	if len(rec.DerivedFrom) == 0 {
		add("projection names no source events to rebuild from")
		return out
	}
	digests := make([]string, 0, len(rec.DerivedFrom))
	var last Record
	for _, id := range rec.DerivedFrom {
		ev, ok := byID[id]
		if !ok || ev.Link != LinkEvent || ev.Case != c.ID || ev.Tenant != rec.Tenant {
			add(fmt.Sprintf("projection source %q is not an event of this case and tenant", id))
			return out
		}
		digests = append(digests, ev.Digest)
		last = ev
	}
	if rec.ProjectionDigest == "" || ProjectionReplay(digests) != rec.ProjectionDigest {
		add("replaying the source events does not reproduce the projection digest")
	}
	if rec.SourceHead != last.Watermark {
		add(fmt.Sprintf("projection source head %q is not the last source event watermark %q", rec.SourceHead, last.Watermark))
	}
	return out
}

// CheckAppendOnly proves a later graph only appended to an earlier one:
// every earlier record is still present with its exact seal.
func CheckAppendOnly(caseID string, prior, current Graph) []Finding {
	now := map[string]Record{}
	for _, rec := range current.Records {
		now[rec.ID] = rec
	}
	var out []Finding
	for _, rec := range prior.Records {
		got, ok := now[rec.ID]
		switch {
		case !ok:
			out = append(out, Finding{Case: caseID, Link: rec.Link, Code: CodeHistoryRewritten, Record: rec.ID,
				Detail: "historical record disappeared"})
		case got.Digest != rec.Digest:
			out = append(out, Finding{Case: caseID, Link: rec.Link, Code: CodeHistoryRewritten, Record: rec.ID,
				Detail: "historical record was rewritten in place"})
		}
	}
	sortFindings(out)
	return out
}

// statusOf is the one status rule: any defect finding makes a case
// DEFECTIVE; every required link PROVEN makes it COMPLETE; a proven link
// beyond INTENT and CAUSATION makes it PARTIAL; otherwise it is UNKNOWN.
func statusOf(links []LinkStatus, findings []Finding) Status {
	for _, f := range findings {
		if !isGap(f.Code) {
			return StatusDefective
		}
	}
	for _, l := range links {
		if l.State == StateDefect {
			return StatusDefective
		}
	}
	allProven, downstream := len(links) > 0, false
	for _, l := range links {
		if l.State != StateProven {
			allProven = false
			continue
		}
		if l.Link != LinkIntent && l.Link != LinkCausation {
			downstream = true
		}
	}
	switch {
	case allProven && len(findings) == 0:
		return StatusComplete
	case downstream:
		return StatusPartial
	default:
		return StatusUnknown
	}
}
