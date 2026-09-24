// Continuous invariant verification: DATA-012 scans ledger and
// relational snapshots for sequence gaps, digest mismatches, orphan
// events/outbox/projection/provenance rows and duplicate settlements.
//
// Targeted, sample, partition and full scans emit exact findings with
// affected set, watermark and severity, and open incident/repair refs.
// Any finding degrades quality; an unscannable stream reports unknown
// rather than healthy. The verifier holds read-only authority: it
// classifies snapshots and never mutates them.
package hashchain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// ScanMode is the closed scan coverage.
type ScanMode string

// The scan modes.
const (
	ScanTargeted  ScanMode = "TARGETED"
	ScanSample    ScanMode = "SAMPLE"
	ScanPartition ScanMode = "PARTITION"
	ScanFull      ScanMode = "FULL"
)

// Finding codes are exact: operators match on them.
const (
	FindingSequenceGap       = "SEQUENCE_GAP"
	FindingDigestMismatch    = "DIGEST_MISMATCH"
	FindingOrphanEvent       = "ORPHAN_EVENT"
	FindingOrphanOutbox      = "ORPHAN_OUTBOX"
	FindingOrphanProjection  = "ORPHAN_PROJECTION"
	FindingProvenanceMissing = "PROVENANCE_MISSING"
	FindingDuplicateSettle   = "DUPLICATE_SETTLEMENT"
	FindingStreamUnscannable = "STREAM_UNSCANNABLE"
)

// Quality is the closed verifier health.
type Quality string

// The quality states.
const (
	QualityHealthy  Quality = "HEALTHY"
	QualityDegraded Quality = "DEGRADED"
	QualityUnknown  Quality = "UNKNOWN"
)

// StreamView is the read-only snapshot one scan classifies.
type StreamView struct {
	StreamKey string
	Links     []ChainedLink
	Events    []EventDigest
	// Outbox holds the event IDs with outbox entries.
	Outbox []uuid.UUID
	// ProjectedThrough is the max sequence the projection consumed.
	ProjectedThrough int64
	// Provenance holds event IDs with provenance rows.
	Provenance []uuid.UUID
	// Settlements holds settlement keys; duplicates are defects.
	Settlements []string
	// SampleEvery classifies every Nth link in SAMPLE mode.
	SampleEvery int64
	// Partition selects [PartitionLo, PartitionHi] sequences in
	// PARTITION mode; TARGETED scans one StreamKey.
	PartitionLo, PartitionHi int64
	// Unscannable marks a stream the scanner could not read.
	Unscannable bool
}

// ScanInput is one verification request.
type ScanInput struct {
	Mode    ScanMode
	Streams []StreamView
	// Target selects the stream for TARGETED mode.
	Target string
}

// ScanFinding is one exact invariant defect.
type ScanFinding struct {
	Code        string
	StreamKey   string
	Affected    []string
	Watermark   int64
	Severity    string
	IncidentRef string
	RepairRef   string
}

// ScanReport is the deterministic verification receipt.
type ScanReport struct {
	Quality     Quality
	Watermark   int64
	Findings    []ScanFinding
	IncidentRef string
	RepairRef   string
	Digest      string
}

// Scan classifies read-only snapshots without mutating them.
func Scan(digester *Digester, in ScanInput) (ScanReport, error) {
	if digester == nil {
		return ScanReport{}, fmt.Errorf("hashchain: scanner digester is required")
	}
	report := ScanReport{Quality: QualityHealthy}
	streams := in.Streams
	if in.Mode == ScanTargeted {
		kept := streams[:0:0]
		for _, stream := range streams {
			if stream.StreamKey == in.Target {
				kept = append(kept, stream)
			}
		}
		streams = kept
		if len(streams) == 0 {
			return ScanReport{}, fmt.Errorf("hashchain: targeted stream %q absent", in.Target)
		}
	}
	for _, stream := range streams {
		scanStream(digester, in.Mode, stream, &report)
	}
	// An unscannable stream reports UNKNOWN rather than healthy, and
	// stays UNKNOWN even beside other defects: unknown is stickier than
	// degraded because nothing was proven about that stream.
	if len(report.Findings) > 0 {
		if report.Quality == QualityHealthy {
			report.Quality = QualityDegraded
		}
		report.IncidentRef = "INC-scan"
		report.RepairRef = "REPAIR-scan"
		for i := range report.Findings {
			report.Findings[i].IncidentRef = report.IncidentRef
			report.Findings[i].RepairRef = report.RepairRef
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Code != report.Findings[j].Code {
			return report.Findings[i].Code < report.Findings[j].Code
		}
		return report.Findings[i].StreamKey < report.Findings[j].StreamKey
	})
	report.Digest = digestScan(report)
	return report, nil
}

func scanStream(digester *Digester, mode ScanMode, stream StreamView, report *ScanReport) {
	emit := func(code, severity string, affected []string, watermark int64) {
		report.Findings = append(report.Findings, ScanFinding{
			Code: code, StreamKey: stream.StreamKey, Affected: affected,
			Watermark: watermark, Severity: severity,
		})
	}
	if stream.Unscannable {
		report.Quality = QualityUnknown
		emit(FindingStreamUnscannable, "high", nil, 0)
		return
	}
	links := stream.Links
	if mode == ScanSample && stream.SampleEvery > 1 {
		kept := links[:0:0]
		for _, link := range links {
			if link.Sequence%stream.SampleEvery == 0 {
				kept = append(kept, link)
			}
		}
		links = kept
	}
	if mode == ScanPartition {
		kept := links[:0:0]
		for _, link := range links {
			if link.Sequence >= stream.PartitionLo && link.Sequence <= stream.PartitionHi {
				kept = append(kept, link)
			}
		}
		links = kept
	}
	events := map[int64]EventDigest{}
	for _, event := range stream.Events {
		events[event.Sequence] = event
	}
	// Continuity disciplines differ by mode, documented here because a
	// sampler that pretended to prove continuity would lie: FULL and
	// TARGETED anchor at genesis and prove gap-free linkage; PARTITION
	// proves contiguity and linkage inside its window without a genesis
	// anchor; SAMPLE recomputes each sampled link without continuity, so
	// a gap between samples is explicitly out of scope.
	continuous := mode == ScanFull || mode == ScanTargeted
	anchored := continuous || mode == ScanPartition
	verified := int64(0)
	prevHash := GenesisHash
	for i, link := range links {
		if continuous {
			wantSequence := int64(i + 1)
			if link.Sequence != wantSequence {
				emit(FindingSequenceGap, "high", []string{fmt.Sprintf("sequence %d", wantSequence)}, verified)
				return
			}
		}
		event, ok := events[link.Sequence]
		if !ok || event.EventID != link.EventID {
			emit(FindingOrphanEvent, "high", []string{fmt.Sprintf("sequence %d", link.Sequence)}, verified)
			return
		}
		if anchored {
			if i == 0 && continuous && link.PrevHash != GenesisHash {
				emit(FindingDigestMismatch, "high", []string{fmt.Sprintf("sequence %d", link.Sequence)}, verified)
				return
			}
			if i > 0 && link.PrevHash != prevHash {
				emit(FindingDigestMismatch, "high", []string{fmt.Sprintf("sequence %d", link.Sequence)}, verified)
				return
			}
			prevHash = link.PrevHash
		} else {
			prevHash = link.PrevHash
		}
		_, chainHash, err := digester.Link(prevHash, event.Digest)
		if err != nil {
			emit(FindingDigestMismatch, "high", []string{fmt.Sprintf("sequence %d", link.Sequence)}, verified)
			return
		}
		if chainHash != link.ChainHash {
			emit(FindingDigestMismatch, "high", []string{fmt.Sprintf("sequence %d", link.Sequence)}, verified)
			return
		}
		prevHash = link.ChainHash
		verified = link.Sequence
	}
	if verified > report.Watermark {
		report.Watermark = verified
	}
	// nil means this source does not expose a trustworthy event-to-outbox
	// relationship. An empty but non-nil slice means the relation was scanned
	// and contains no rows.
	if stream.Outbox != nil {
		outbox := map[uuid.UUID]bool{}
		for _, id := range stream.Outbox {
			outbox[id] = true
		}
		for _, event := range stream.Events {
			if !outbox[event.EventID] {
				emit(FindingOrphanOutbox, "medium", []string{event.EventID.String()}, verified)
			}
		}
		for id := range outbox {
			known := false
			for _, event := range stream.Events {
				if event.EventID == id {
					known = true
				}
			}
			if !known {
				emit(FindingOrphanEvent, "medium", []string{id.String()}, verified)
			}
		}
	}
	if stream.ProjectedThrough < verified {
		emit(FindingOrphanProjection, "medium",
			[]string{fmt.Sprintf("projection through %d behind %d", stream.ProjectedThrough, verified)}, verified)
	}
	provenance := map[uuid.UUID]bool{}
	for _, id := range stream.Provenance {
		provenance[id] = true
	}
	for _, event := range stream.Events {
		if !provenance[event.EventID] {
			emit(FindingProvenanceMissing, "low", []string{event.EventID.String()}, verified)
		}
	}
	seenSettlements := map[string]int{}
	for _, key := range stream.Settlements {
		seenSettlements[key]++
		if seenSettlements[key] == 2 {
			emit(FindingDuplicateSettle, "high", []string{key}, verified)
		}
	}
}

func digestScan(report ScanReport) string {
	parts := []string{"data012-scan", string(report.Quality), fmt.Sprint(report.Watermark)}
	for _, finding := range report.Findings {
		affected := append([]string(nil), finding.Affected...)
		sort.Strings(affected)
		parts = append(parts, strings.Join([]string{
			finding.Code, finding.StreamKey, strings.Join(affected, ","),
			fmt.Sprint(finding.Watermark), finding.Severity,
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
