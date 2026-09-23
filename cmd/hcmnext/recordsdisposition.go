package main

// Governed records disposition operator command (REV-004-02).
//
// `hcmnext records-disposition` drives the same DispositionGate the serve
// cell composes (internal/application) from an operator entry point: it
// creates the envelope's holds, runs the retention simulation, evaluates
// the legal-hold gate and, for `execute`, certifies verified deletion. The
// command holds no state of its own; every refusal and certificate comes
// from the gate, and the JSON result on stdout carries the decision, the
// certificate and the persisted hold findings.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legalhold"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/records"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const recordsDispositionUsage = "usage: hcmnext records-disposition <evaluate|execute> -tenant <id> -compartment <name> -record <ref> -evidence <id> -input <envelope.json>"

// recordsDispositionHold is one hold the envelope seeds before evaluation.
type recordsDispositionHold struct {
	ID        string   `json:"id"`
	Reason    string   `json:"reason"`
	Authority string   `json:"authority"`
	Records   []string `json:"record_refs"`
	Classes   []string `json:"class_refs"`
}

// recordsDispositionEnvelope is the operator's complete disposition
// request. Retention and deletion are optional on evaluate; execute
// requires the deletion envelope.
type recordsDispositionEnvelope struct {
	Holds     []recordsDispositionHold   `json:"holds"`
	Retention *records.SimulationRequest `json:"retention"`
	Deletion  *records.DeletionRequest   `json:"deletion"`
}

// recordsDispositionDecision is the JSON view of a hold decision. It
// exists because legalhold.Evidence carries a values.Instant whose text
// marshaling refuses an unset instant, while an allowed decision legitimately
// has no evidence time.
type recordsDispositionDecision struct {
	Allowed    bool   `json:"allowed"`
	Code       string `json:"code"`
	HoldID     string `json:"hold_id,omitempty"`
	EvidenceID string `json:"evidence_id,omitempty"`
	EvidenceAt string `json:"evidence_at,omitempty"`
}

// recordsDispositionFinding is the JSON view of one persisted hold finding.
type recordsDispositionFinding struct {
	ID          string `json:"id"`
	Action      string `json:"action"`
	RecordRef   string `json:"record_ref,omitempty"`
	HoldID      string `json:"hold_id,omitempty"`
	Code        string `json:"code"`
	Tenant      string `json:"tenant,omitempty"`
	Compartment string `json:"compartment,omitempty"`
	At          string `json:"at,omitempty"`
}

func describeDispositionDecision(d legalhold.Decision) recordsDispositionDecision {
	out := recordsDispositionDecision{Allowed: d.Allowed, Code: d.Code, HoldID: d.HoldID}
	out.EvidenceID = d.Evidence.ID
	if d.Evidence.At.IsSet() {
		out.EvidenceAt = d.Evidence.At.String()
	}
	return out
}

func describeDispositionFindings(evidence []legalhold.Evidence) []recordsDispositionFinding {
	out := make([]recordsDispositionFinding, 0, len(evidence))
	for _, e := range evidence {
		finding := recordsDispositionFinding{
			ID: e.ID, Action: e.Action, RecordRef: e.RecordRef, HoldID: e.HoldID,
			Code: e.Code, Tenant: e.Tenant, Compartment: e.Compartment,
		}
		if e.At.IsSet() {
			finding.At = e.At.String()
		}
		out = append(out, finding)
	}
	return out
}

// recordsDispositionResult is the JSON the command prints: the hold
// decision, the retention report and certificate when produced, and the
// gate's persisted hold findings.
type recordsDispositionResult struct {
	Action      string                       `json:"action"`
	Decision    recordsDispositionDecision   `json:"decision"`
	Retention   *records.Report              `json:"retention,omitempty"`
	Certificate *records.DeletionCertificate `json:"certificate,omitempty"`
	HoldFinding []recordsDispositionFinding  `json:"hold_findings"`
}

func runRecordsDisposition(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, recordsDispositionUsage)
		return 2
	}
	action := strings.ToLower(args[0])
	if action != "evaluate" && action != "execute" {
		fmt.Fprintf(stderr, "hcmnext records-disposition: unknown action %q; %s\n", args[0], recordsDispositionUsage)
		return 2
	}
	fs := flag.NewFlagSet("records-disposition "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	tenantFlag := fs.String("tenant", "", "tenant the record and deletion belong to (required)")
	compartmentFlag := fs.String("compartment", "", "records compartment (required)")
	recordFlag := fs.String("record", "", "record reference disposition acts on (required)")
	evidenceFlag := fs.String("evidence", "", "evidence identity the finding is persisted under (required)")
	inputFlag := fs.String("input", "", "disposition envelope JSON file (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	tenant := strings.TrimSpace(*tenantFlag)
	compartment := strings.TrimSpace(*compartmentFlag)
	recordRef := strings.TrimSpace(*recordFlag)
	evidenceID := strings.TrimSpace(*evidenceFlag)
	inputPath := strings.TrimSpace(*inputFlag)
	if tenant == "" || compartment == "" || recordRef == "" || evidenceID == "" || inputPath == "" {
		fmt.Fprintln(stderr, "hcmnext records-disposition: -tenant, -compartment, -record, -evidence and -input are required")
		return 2
	}
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext records-disposition: read envelope: %v\n", err)
		return 1
	}
	var envelope recordsDispositionEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		fmt.Fprintf(stderr, "hcmnext records-disposition: parse envelope: %v\n", err)
		return 1
	}
	if action == "execute" && envelope.Deletion == nil {
		fmt.Fprintln(stderr, "hcmnext records-disposition: execute requires the deletion envelope")
		return 2
	}

	at := now().UTC()
	gate := application.NewDispositionGate()
	for _, h := range envelope.Holds {
		hold := legalhold.Hold{
			ID:          strings.TrimSpace(h.ID),
			Tenant:      tenant,
			Compartment: compartment,
			Scope: legalhold.Scope{
				Tenant:      tenant,
				Compartment: compartment,
				RecordRefs:  h.Records,
				ClassRefs:   h.Classes,
			},
			Reason:    strings.TrimSpace(h.Reason),
			Authority: strings.TrimSpace(h.Authority),
			CreatedAt: values.NewInstant(at),
		}
		if err := gate.CreateHold(hold); err != nil {
			fmt.Fprintf(stderr, "hcmnext records-disposition: seed hold %q: %v\n", h.ID, err)
			return 1
		}
	}

	result := recordsDispositionResult{Action: action}
	if envelope.Retention != nil {
		report, err := gate.EvaluateRetention(*envelope.Retention)
		if err != nil {
			fmt.Fprintf(stderr, "hcmnext records-disposition: retention simulation: %v\n", err)
			return 1
		}
		result.Retention = &report
	}
	record := legalhold.Record{Tenant: tenant, Compartment: compartment, Ref: recordRef}
	if action == "evaluate" {
		result.Decision = describeDispositionDecision(gate.DecideDisposition(record, evidenceID, at))
		result.HoldFinding = describeDispositionFindings(gate.HoldEvidence())
		return writeRecordsDisposition(stdout, stderr, result, result.Decision.Code == "HOLD_BLOCKED")
	}
	deletion := *envelope.Deletion
	if deletion.Tenant == "" {
		deletion.Tenant = tenant
	}
	outcome, err := gate.ExecuteVerifiedDeletion(application.VerifiedDeletionRequest{
		Deletion:   deletion,
		Record:     record,
		EvidenceID: evidenceID,
		At:         at,
	})
	result.Decision = describeDispositionDecision(outcome.Decision)
	if err == nil {
		result.Certificate = &outcome.Certificate
	}
	result.HoldFinding = describeDispositionFindings(gate.HoldEvidence())
	if err != nil {
		if outcome.Decision.Code == "HOLD_BLOCKED" {
			// A held deletion is a governed refusal with persisted
			// evidence, not a command failure: still print the result so
			// the finding is auditable, and exit held.
			if code := writeRecordsDisposition(stdout, stderr, result, true); code != 0 {
				return code
			}
			fmt.Fprintf(stderr, "hcmnext records-disposition: %v\n", err)
			return 3
		}
		fmt.Fprintf(stderr, "hcmnext records-disposition: %v\n", err)
		return 1
	}
	if !outcome.Certificate.Complete {
		if code := writeRecordsDisposition(stdout, stderr, result, true); code != 0 {
			return code
		}
		fmt.Fprintln(stderr, "hcmnext records-disposition: deletion certificate is incomplete")
		return 3
	}
	return writeRecordsDisposition(stdout, stderr, result, false)
}

// writeRecordsDisposition prints the result as JSON. blocked decides the
// exit code: 3 for a governed refusal, 0 otherwise.
func writeRecordsDisposition(stdout, stderr io.Writer, result recordsDispositionResult, blocked bool) int {
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext records-disposition: encode result: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(encoded))
	if blocked {
		return 3
	}
	return 0
}
