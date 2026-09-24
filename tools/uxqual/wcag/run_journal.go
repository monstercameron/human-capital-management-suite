package wcag

// UX-003 execution evidence is appended only by the explicit ux003run command.
// Routine tests and report generation are read-only with respect to this
// checked-in journal.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

const runJournalEnv = "UX003_RUN_JOURNAL"
const PrimaryGateScope = "SSR primary release route only"

type RunResult struct {
	Surface string `json:"surface"`
	Name    string `json:"name"`
	Pass    bool   `json:"pass"`
	Detail  string `json:"detail"`
}

// RunRecord is one durable UX-003 qualification execution. GatePassed records
// only the SSR primary release route gate; Results preserves per-surface SSR
// and GWC findings independently. Digest chains the record to the previous
// journal entry; ResultsDigest and EvidenceDigest bind the exact scorecard and
// manual-scenario evidence used for that run.
type RunRecord struct {
	Sequence       uint64      `json:"sequence"`
	ID             string      `json:"id"`
	At             time.Time   `json:"at"`
	GatePassed     bool        `json:"gate_passed"`
	GateScope      string      `json:"gate_scope,omitempty"`
	Results        []RunResult `json:"results"`
	ResultsDigest  string      `json:"results_digest"`
	EvidenceDigest string      `json:"evidence_digest"`
	PreviousDigest string      `json:"previous_digest,omitempty"`
	Digest         string      `json:"digest"`
}

func runJournalPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv(runJournalEnv)); path != "" {
		return path, nil
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("wcag: cannot locate UX-003 run journal")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return filepath.Join(root, "definitions", "ux", "wcag", "ux-003-run-journal.jsonl"), nil
}

func resultsFor(ssrResults, gwcResults []qual.CriterionResult) []RunResult {
	out := make([]RunResult, 0, len(ssrResults)+len(gwcResults))
	for _, result := range ssrResults {
		out = append(out, RunResult{Surface: "SSR", Name: result.Name, Pass: result.Pass, Detail: result.Detail})
	}
	for _, result := range gwcResults {
		out = append(out, RunResult{Surface: "GWC", Name: result.Name, Pass: result.Pass, Detail: result.Detail})
	}
	return out
}

func digestJSON(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// ResultsDigest returns the stable digest of a recorded, surface-labelled
// UX-003 result set.
func ResultsDigest(results []RunResult) (string, error) { return digestJSON(results) }

func (r RunRecord) digestValue() (string, error) { r.Digest = ""; return digestJSON(r) }

func (r RunRecord) Validate(previous string, sequence uint64) error {
	if r.Sequence != sequence || r.Sequence == 0 {
		return fmt.Errorf("wcag: run journal sequence %d, want %d", r.Sequence, sequence)
	}
	if r.ID == "" || r.At.IsZero() || r.ResultsDigest == "" || r.EvidenceDigest == "" {
		return errors.New("wcag: incomplete UX-003 run record")
	}
	if r.GateScope != "" && r.GateScope != PrimaryGateScope {
		return fmt.Errorf("wcag: unsupported UX-003 gate scope %q", r.GateScope)
	}
	if r.PreviousDigest != previous {
		return fmt.Errorf("wcag: run journal chain mismatch at sequence %d", r.Sequence)
	}
	resultsDigest, err := ResultsDigest(r.Results)
	if err != nil {
		return err
	}
	if r.ResultsDigest != resultsDigest {
		return fmt.Errorf("wcag: UX-003 results digest mismatch at sequence %d", r.Sequence)
	}
	want, err := r.digestValue()
	if err != nil {
		return err
	}
	if r.Digest != want {
		return fmt.Errorf("wcag: UX-003 run journal digest mismatch at sequence %d", r.Sequence)
	}
	wantID := fmt.Sprintf("ux003-%06d-%s", r.Sequence, r.ResultsDigest[:12])
	if r.ID != wantID {
		return fmt.Errorf("wcag: invalid UX-003 run identity at sequence %d", r.Sequence)
	}
	return nil
}

// AllResults returns the persisted scorecard in the same order it was run.
func (r RunRecord) AllResults() []qual.CriterionResult {
	out := make([]qual.CriterionResult, 0, len(r.Results))
	for _, result := range r.Results {
		out = append(out, qual.CriterionResult{Name: result.Name, Pass: result.Pass, Detail: result.Detail})
	}
	return out
}

// MatchesResults compares a newly computed scorecard to the scorecard recorded
// by the explicit UX-003 run command. It detects changed checks or fixtures
// that have not had a corresponding release-gate execution.
func (r RunRecord) MatchesResults(ssrResults, gwcResults []qual.CriterionResult) bool {
	digest, err := digestJSON(resultsFor(ssrResults, gwcResults))
	return err == nil && digest == r.ResultsDigest
}

// RecordUX003Run appends computed UX-003 results and current scenario evidence
// to the journal. Production callers should be the ux003run command, which
// renders fixtures and evaluates the gate before calling this function.
func RecordUX003Run(ssrResults, gwcResults []qual.CriterionResult, evidence Evidence, gatePassed bool) (RunRecord, error) {
	path, err := runJournalPath()
	if err != nil {
		return RunRecord{}, err
	}
	entries, err := LoadRunJournal()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return RunRecord{}, err
	}
	var previous string
	var sequence uint64 = 1
	if len(entries) > 0 {
		previous = entries[len(entries)-1].Digest
		sequence = entries[len(entries)-1].Sequence + 1
	}
	results := resultsFor(ssrResults, gwcResults)
	resultsDigest, err := ResultsDigest(results)
	if err != nil {
		return RunRecord{}, err
	}
	evidenceDigest, err := digestJSON(evidence)
	if err != nil {
		return RunRecord{}, err
	}
	r := RunRecord{Sequence: sequence, ID: fmt.Sprintf("ux003-%06d-%s", sequence, resultsDigest[:12]), At: time.Now().UTC(), GatePassed: gatePassed, GateScope: PrimaryGateScope, Results: results, ResultsDigest: resultsDigest, EvidenceDigest: evidenceDigest, PreviousDigest: previous}
	r.Digest, err = r.digestValue()
	if err != nil {
		return RunRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return RunRecord{}, err
	}
	line, err := json.Marshal(r)
	if err != nil {
		return RunRecord{}, err
	}
	line = append(line, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return RunRecord{}, err
	}
	defer f.Close()
	if _, err = f.Write(line); err != nil {
		return RunRecord{}, err
	}
	if err = f.Sync(); err != nil {
		return RunRecord{}, err
	}
	return r, nil
}

// LoadRunJournal reads and verifies every journal entry and its digest chain.
func LoadRunJournal() ([]RunRecord, error) {
	path, err := runJournalPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries := []RunRecord{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	previous := ""
	for scanner.Scan() {
		var record RunRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("wcag: decode UX-003 run journal: %w", err)
		}
		want := uint64(len(entries) + 1)
		if err := record.Validate(previous, want); err != nil {
			return nil, err
		}
		entries = append(entries, record)
		previous = record.Digest
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("wcag: UX-003 run journal is empty")
	}
	return entries, nil
}

// LatestRunRecord returns the newest verified gate execution.
func LatestRunRecord() (RunRecord, error) {
	entries, err := LoadRunJournal()
	if err != nil {
		return RunRecord{}, err
	}
	return entries[len(entries)-1], nil
}

// EvidenceDigest returns a canonical digest for current UX-003 scenario
// evidence, allowing consumers to detect evidence edits after a gate run.
func (e Evidence) EvidenceDigest() (string, error) { return digestJSON(e) }
