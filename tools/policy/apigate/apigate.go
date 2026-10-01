// Package apigate enforces Protobuf compatibility and checked-in consumer
// adoption evidence for the public API.
package apigate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/compatibility"
	"gopkg.in/yaml.v3"
)

const (
	defaultSchemaModule = "schema/proto"
	defaultBaseline     = "tools/gen/compatibility/testdata/schema_baseline.binpb"
	defaultRegister     = "tools/policy/apigate/testdata/consumer-register.json"
)

// FieldDependency identifies one message field a consumer reads or writes.
type FieldDependency struct {
	Message string `json:"message" yaml:"message"`
	Field   string `json:"field" yaml:"field"`
}

// Consumer is an independently versioned API consumer. Methods and Fields
// are deliberately explicit: a consumer cannot claim adoption of a service
// without naming the operations and wire fields it depends on.
type Consumer struct {
	ID             string            `json:"id" yaml:"id"`
	Owner          string            `json:"owner" yaml:"owner"`
	Service        string            `json:"service" yaml:"service"`
	AdoptedVersion uint32            `json:"adopted_version" yaml:"adopted_version"`
	Watermark      string            `json:"watermark" yaml:"watermark"`
	Sunset         string            `json:"sunset" yaml:"sunset"`
	Methods        []string          `json:"methods" yaml:"methods"`
	Fields         []FieldDependency `json:"fields" yaml:"fields"`
}

// Disposition identifies the accountable owner and governance class for this
// gate. The checker is library tooling, but its compatibility decision still
// needs one authoritative owner before it can be used as a release gate.
type Disposition struct {
	Specification string `json:"specification" yaml:"specification"`
	Owner         string `json:"owner" yaml:"owner"`
	Class         string `json:"class" yaml:"class"`
	Status        string `json:"status" yaml:"status"`
}

// Register is the checked-in consumer adoption register.
type Register struct {
	Version     int         `json:"version" yaml:"version"`
	Disposition Disposition `json:"disposition" yaml:"disposition"`
	Consumers   []Consumer  `json:"consumers" yaml:"consumers"`
}

// Finding names a compatibility violation that affects a registered
// consumer. Path and detail are retained so migration ownership is explicit.
type Finding struct {
	ConsumerID string `json:"consumer_id"`
	Path       string `json:"path"`
	Detail     string `json:"detail"`
}

// Outcome is the explicit gate disposition. MIGRATE is a consumer-level
// outcome: the overall gate remains BLOCK until the affected consumer has
// moved past the registered adoption watermark.
type Outcome string

const (
	OutcomeCompatible Outcome = "COMPATIBLE"
	OutcomeMigrate    Outcome = "MIGRATE"
	OutcomeBlock      Outcome = "BLOCK"
)

// ConsumerDecision records the decision for one registered consumer and
// carries the owner and sunset evidence needed to act on it.
type ConsumerDecision struct {
	ConsumerID string    `json:"consumer_id"`
	Decision   Outcome   `json:"decision"`
	Owner      string    `json:"owner"`
	Sunset     string    `json:"sunset"`
	Findings   []Finding `json:"findings,omitempty"`
}

// Report is the complete API gate result.
type Report struct {
	Decision          Outcome                         `json:"decision"`
	Disposition       Disposition                     `json:"disposition"`
	RegisterDigest    string                          `json:"register_digest"`
	Compatibility     compatibility.BufBreakingReport `json:"compatibility"`
	ConsumerFindings  []Finding                       `json:"consumer_findings"`
	ConsumerDecisions []ConsumerDecision              `json:"consumer_decisions"`
}

// OK reports whether wire compatibility and consumer adoption both pass.
func (r Report) OK() bool {
	return r.Decision == OutcomeCompatible && r.Compatibility.OK() && len(r.ConsumerFindings) == 0
}

// Validate checks register identity, ownership, and dependency completeness.
func Validate(r Register) error {
	if r.Version <= 0 {
		return fmt.Errorf("apigate: register version must be positive")
	}
	if strings.TrimSpace(r.Disposition.Specification) == "" ||
		strings.TrimSpace(r.Disposition.Owner) == "" ||
		strings.TrimSpace(r.Disposition.Class) == "" ||
		strings.TrimSpace(r.Disposition.Status) == "" {
		return fmt.Errorf("apigate: disposition requires specification, owner, class, and status")
	}
	if len(r.Consumers) == 0 {
		return fmt.Errorf("apigate: consumer register is empty")
	}
	seen := map[string]bool{}
	for i, consumer := range r.Consumers {
		if consumer.ID == "" || consumer.Owner == "" || consumer.Service == "" {
			return fmt.Errorf("apigate: consumer %d requires id, owner, and service", i)
		}
		if consumer.AdoptedVersion == 0 || consumer.Watermark == "" || consumer.Sunset == "" {
			return fmt.Errorf("apigate: consumer %q requires adopted_version, watermark, and sunset evidence", consumer.ID)
		}
		if seen[consumer.ID] {
			return fmt.Errorf("apigate: consumer %q is registered more than once", consumer.ID)
		}
		seen[consumer.ID] = true
		if len(consumer.Methods) == 0 {
			return fmt.Errorf("apigate: consumer %q names no service methods", consumer.ID)
		}
		if len(consumer.Fields) == 0 {
			return fmt.Errorf("apigate: consumer %q names no message fields", consumer.ID)
		}
		for _, method := range consumer.Methods {
			if strings.TrimSpace(method) == "" {
				return fmt.Errorf("apigate: consumer %q has an empty method dependency", consumer.ID)
			}
		}
		for _, field := range consumer.Fields {
			if strings.TrimSpace(field.Message) == "" || strings.TrimSpace(field.Field) == "" {
				return fmt.Errorf("apigate: consumer %q has an incomplete field dependency", consumer.ID)
			}
		}
	}
	return nil
}

// LoadRegister reads JSON or YAML and validates the checked-in register.
func LoadRegister(path string) (Register, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Register{}, fmt.Errorf("apigate: read consumer register %s: %w", path, err)
	}
	var register Register
	if err := yaml.Unmarshal(data, &register); err != nil {
		return Register{}, fmt.Errorf("apigate: parse consumer register %s: %w", path, err)
	}
	if err := Validate(register); err != nil {
		return Register{}, err
	}
	return register, nil
}

// CanonicalJSON returns the stable checked-in representation of a register.
func CanonicalJSON(r Register) ([]byte, error) {
	if err := Validate(r); err != nil {
		return nil, err
	}
	copyRegister := Register{Version: r.Version, Disposition: r.Disposition, Consumers: append([]Consumer(nil), r.Consumers...)}
	sort.Slice(copyRegister.Consumers, func(i, j int) bool { return copyRegister.Consumers[i].ID < copyRegister.Consumers[j].ID })
	for i := range copyRegister.Consumers {
		copyRegister.Consumers[i].Methods = append([]string(nil), copyRegister.Consumers[i].Methods...)
		copyRegister.Consumers[i].Fields = append([]FieldDependency(nil), copyRegister.Consumers[i].Fields...)
		sort.Strings(copyRegister.Consumers[i].Methods)
		sort.Slice(copyRegister.Consumers[i].Fields, func(a, b int) bool {
			if copyRegister.Consumers[i].Fields[a].Message != copyRegister.Consumers[i].Fields[b].Message {
				return copyRegister.Consumers[i].Fields[a].Message < copyRegister.Consumers[i].Fields[b].Message
			}
			return copyRegister.Consumers[i].Fields[a].Field < copyRegister.Consumers[i].Fields[b].Field
		})
	}
	data, err := json.MarshalIndent(copyRegister, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Digest returns the SHA-256 identity of the canonical register.
func Digest(r Register) (string, error) {
	data, err := CanonicalJSON(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ConsumerFindings matches buf's precise paths/messages against the fields
// and methods named by each consumer. A generic Buf violation remains a gate
// failure even when it has no registered consumer; this function adds the
// explicit adoption evidence required for affected consumers.
func ConsumerFindings(r Register, violations []compatibility.BufBreakingViolation) []Finding {
	var findings []Finding
	for _, consumer := range r.Consumers {
		for _, violation := range violations {
			text := strings.ToLower(violation.Path + " " + violation.Message)
			matched := false
			for _, field := range consumer.Fields {
				needle := strings.ToLower(field.Message + "." + field.Field)
				if strings.Contains(text, needle) {
					matched = true
					break
				}
			}
			for _, method := range consumer.Methods {
				needle := strings.ToLower(consumer.Service + "." + method)
				shortNeedle := strings.ToLower(method)
				if strings.Contains(text, needle) || (strings.Contains(text, strings.ToLower(consumer.Service)) && strings.Contains(text, shortNeedle)) {
					matched = true
					break
				}
			}
			if matched {
				findings = append(findings, Finding{ConsumerID: consumer.ID, Path: violation.Path, Detail: violation.Message})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].ConsumerID != findings[j].ConsumerID {
			return findings[i].ConsumerID < findings[j].ConsumerID
		}
		return findings[i].Path < findings[j].Path
	})
	return findings
}

// ConsumerDecisions produces one explicit outcome per registered consumer.
// A matched compatibility violation requires migration evidence; the gate
// remains blocked until that consumer advances beyond its watermark.
func ConsumerDecisions(r Register, violations []compatibility.BufBreakingViolation) []ConsumerDecision {
	findings := ConsumerFindings(r, violations)
	byConsumer := make(map[string][]Finding, len(findings))
	for _, finding := range findings {
		byConsumer[finding.ConsumerID] = append(byConsumer[finding.ConsumerID], finding)
	}
	decisions := make([]ConsumerDecision, 0, len(r.Consumers))
	for _, consumer := range r.Consumers {
		decision := ConsumerDecision{
			ConsumerID: consumer.ID,
			Decision:   OutcomeCompatible,
			Owner:      consumer.Owner,
			Sunset:     consumer.Sunset,
		}
		if affected := byConsumer[consumer.ID]; len(affected) > 0 {
			decision.Decision = OutcomeMigrate
			decision.Findings = append([]Finding(nil), affected...)
		}
		decisions = append(decisions, decision)
	}
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].ConsumerID < decisions[j].ConsumerID })
	return decisions
}

// Options configures Run. Empty paths use the repository's checked-in API
// module, baseline, register, and local buf executable.
type Options struct {
	SchemaModule string
	Baseline     string
	RegisterPath string
	BufBinary    string
}

func (o Options) withRoot(root string) Options {
	if o.SchemaModule == "" {
		o.SchemaModule = filepath.Join(root, defaultSchemaModule)
	} else if !filepath.IsAbs(o.SchemaModule) {
		o.SchemaModule = filepath.Join(root, o.SchemaModule)
	}
	if o.Baseline == "" {
		o.Baseline = filepath.Join(root, defaultBaseline)
	} else if !filepath.IsAbs(o.Baseline) {
		o.Baseline = filepath.Join(root, o.Baseline)
	}
	if o.RegisterPath == "" {
		o.RegisterPath = filepath.Join(root, defaultRegister)
	} else if !filepath.IsAbs(o.RegisterPath) {
		o.RegisterPath = filepath.Join(root, o.RegisterPath)
	}
	return o
}

// Run loads the register and performs the offline buf-breaking comparison.
func Run(root string, options Options) (Report, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("apigate: resolve root: %w", err)
	}
	options = options.withRoot(root)
	register, err := LoadRegister(options.RegisterPath)
	if err != nil {
		return Report{}, err
	}
	digest, err := Digest(register)
	if err != nil {
		return Report{}, err
	}
	bufBinary := options.BufBinary
	if bufBinary == "" {
		bufBinary, err = compatibility.ResolveBufBinary()
		if err != nil {
			return Report{}, err
		}
	}
	compatibilityReport, err := compatibility.RunBufBreaking(bufBinary, options.SchemaModule, options.Baseline)
	if err != nil {
		return Report{}, err
	}
	consumerFindings := ConsumerFindings(register, compatibilityReport.Violations)
	decision := OutcomeCompatible
	if !compatibilityReport.OK() || len(consumerFindings) != 0 {
		decision = OutcomeBlock
	}
	return Report{
		Decision:          decision,
		Disposition:       register.Disposition,
		RegisterDigest:    digest,
		Compatibility:     compatibilityReport,
		ConsumerFindings:  consumerFindings,
		ConsumerDecisions: ConsumerDecisions(register, compatibilityReport.Violations),
	}, nil
}

// Evaluate is a compatibility spelling for callers that use policy-check
// terminology.
func Evaluate(root string, options Options) (Report, error) { return Run(root, options) }

// DefaultPaths exposes the checked-in locations without duplicating path
// knowledge in command callers.
func DefaultPaths(root string) (schemaModule, baseline, register string) {
	return filepath.Join(root, defaultSchemaModule), filepath.Join(root, defaultBaseline), filepath.Join(root, defaultRegister)
}

// EqualCanonical reports whether data is exactly the canonical register.
func EqualCanonical(data []byte, register Register) bool {
	want, err := CanonicalJSON(register)
	return err == nil && bytes.Equal(data, want)
}
