package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalChattoneQualifyCommand is the migrate subcommand that runs the writing
// style qualification against the live model and writes the document it earns.
const LocalChattoneQualifyCommand = "migrate qualify-chattone -profile local-dev"

// localChattoneEvaluationRecordPath is where the measured record of the last
// run is kept, beside the deployment document.
const localChattoneEvaluationRecordPath = ".artifacts/lanes/agent-dev/chat-writing-style-evaluation.json"

// LocalChattoneQualifyConfig asks for a real evaluation of the writing-style
// model. APIKey is the provider key the caller read from its own environment;
// it is used for the calls and is never written, logged or returned. Model is
// optional: empty selects the model the prepared local deployment prices.
type LocalChattoneQualifyConfig struct {
	Profile string
	APIKey  string
	Model   string
}

// LocalChattoneQualifyResult is what a run produced. Written is false when the
// document already held exactly the evidence this run earned.
type LocalChattoneQualifyResult struct {
	Qualification ChattoneQualification
	Path          string
	RecordPath    string
	Written       bool
}

// ValidateLocalChattoneQualifyConfig refuses anything but the local-dev profile
// and a provider key, before a file is read or a call is made.
func ValidateLocalChattoneQualifyConfig(config LocalChattoneQualifyConfig) error {
	if config.Profile != ServeProfileLocalDev {
		return errors.New("the writing style qualification command writes local development files and can only run with -profile local-dev")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return errors.New("set MODEL_API_KEY to the provider key to evaluate the model with")
	}
	return nil
}

// RunLocalChattoneQualification evaluates the model on the writing-style and
// reword suites (synthetic drafts only) and, only when every case passes, writes
// the approval document carrying the run's own suite digest where a local-dev
// serve reads it. The record of the run (digests and verdicts, never model text
// or the key) is kept beside it whether or not the model passed. A model that
// does not pass writes no document and returns ErrChattoneNotQualified with the
// record, so the failing cases can be read.
func RunLocalChattoneQualification(ctx context.Context, config LocalChattoneQualifyConfig, now func() time.Time) (LocalChattoneQualifyResult, error) {
	if err := ValidateLocalChattoneQualifyConfig(config); err != nil {
		return LocalChattoneQualifyResult{}, err
	}
	basePath := localAgentDevFile(localPersonaModelDeploymentPath)
	if _, err := os.Stat(basePath); errors.Is(err, os.ErrNotExist) {
		return LocalChattoneQualifyResult{}, fmt.Errorf("the prepared model deployment %s does not exist: run migrate agent-demo -profile local-dev first", basePath)
	}
	base, err := LoadPersonaModelDeployment(basePath)
	if err != nil {
		return LocalChattoneQualifyResult{}, fmt.Errorf("the prepared model deployment %s cannot be used: %w", basePath, err)
	}
	const latency, cost = 20 * time.Second, int64(20000)
	q, dep, qualifyErr := QualifyChattoneModel(ctx, ChattoneQualifyConfig{Base: base, APIKey: strings.TrimSpace(config.APIKey), Model: config.Model, MaxLatency: latency, MaxCostMicros: cost, Now: now, RewordSuite: ChattoneRewordSuite()})
	result := LocalChattoneQualifyResult{Qualification: q}
	if len(q.Cases) == 0 {
		// The run never reached the suite (no price, bad deployment): nothing to record.
		return result, qualifyErr
	}
	result.RecordPath = localAgentDevFile(localChattoneEvaluationRecordPath)
	if err := writeLocalChattoneRecord(result.RecordPath, q); err != nil {
		return result, fmt.Errorf("the evaluation record cannot be written: %w", err)
	}
	if qualifyErr != nil {
		return result, qualifyErr
	}
	result.Path = localAgentDevFile(localChattoneModelDeploymentPath)
	if result.Written, err = writeLocalAgentDemoDeployment(result.Path, dep); err != nil {
		return result, fmt.Errorf("the writing style deployment cannot be written: %w", err)
	}
	return result, nil
}

// writeLocalChattoneRecord keeps the run's record; a record identical to the one
// on disk is left alone so a rerun with nothing new changes nothing.
func writeLocalChattoneRecord(path string, q ChattoneQualification) error {
	raw, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, raw) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}
